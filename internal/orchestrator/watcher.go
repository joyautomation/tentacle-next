//go:build orchestrator || all

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	otypes "github.com/joyautomation/tentacle/internal/types"
)

// reconcilerContext holds all dependencies for the reconciliation loop.
type reconcilerContext struct {
	b      bus.Bus
	config *OrchestratorConfig
	mod    *Module // back-reference for monolith mode
	log    *slog.Logger
}

// dispatchReconcile routes to the correct reconciler based on mode.
func dispatchReconcile(desired otypes.DesiredServiceKV, rctx *reconcilerContext) {
	if rctx.mod != nil && rctx.mod.IsMonolith() {
		reconcileModuleMonolith(desired, rctx)
	} else {
		reconcileModule(desired, rctx)
	}
}

// reconcileModuleMonolith manages a module as an in-process goroutine.
func reconcileModuleMonolith(desired otypes.DesiredServiceKV, rctx *reconcilerContext) {
	entry := getRegistryEntry(desired.ModuleID)
	if entry == nil {
		rctx.log.Warn("reconcile: unknown module in desired_services", "moduleId", desired.ModuleID)
		return
	}

	factory, hasFactory := rctx.mod.factories[desired.ModuleID]
	if !hasFactory {
		reportStatus(rctx.b, entry, statusOpts{
			SystemdState:   "not-found",
			ReconcileState: "error",
			LastError:      fmt.Sprintf("Module %q not compiled into this binary", desired.ModuleID),
		}, rctx.log)
		return
	}

	// Check required config before starting
	if len(entry.RequiredConfig) > 0 && desired.Running {
		moduleConfig, err := getModuleConfig(rctx.b, entry.ModuleID)
		if err != nil {
			rctx.log.Warn("reconcile: failed to read config", "moduleId", entry.ModuleID, "error", err)
		}

		var missing []string
		for _, cf := range entry.RequiredConfig {
			if cf.Required {
				if val, ok := moduleConfig[cf.EnvVar]; !ok || val == "" {
					if cf.Default == "" {
						missing = append(missing, cf.EnvVar)
					}
				}
			}
		}
		if len(missing) > 0 {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: []string{"embedded"},
				ActiveVersion:     "embedded",
				SystemdState:      "inactive",
				ReconcileState:    "needs_config",
				LastError:         fmt.Sprintf("Missing required config: %v", missing),
			}, rctx.log)
			return
		}
	}

	// Ensure system dependencies (apt packages) are installed.
	if desired.Running && len(entry.AptDeps) > 0 {
		if !ensureDeps(entry, rctx.log) {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: []string{"embedded"},
				ActiveVersion:     "embedded",
				SystemdState:      "inactive",
				ReconcileState:    "error",
				LastError:         "Failed to install system dependencies",
			}, rctx.log)
			return
		}
	}

	rctx.mod.mu.Lock()
	_, isRunning := rctx.mod.running[desired.ModuleID]
	rctx.mod.mu.Unlock()

	if desired.Running && !isRunning {
		// Start module as goroutine
		mod := factory(desired.ModuleID)
		ctx, cancel := context.WithCancel(context.Background())

		rctx.mod.mu.Lock()
		rctx.mod.running[desired.ModuleID] = &runningModule{mod: mod, cancel: cancel}
		rctx.mod.mu.Unlock()

		rctx.log.Info("reconcile: starting in-process module", "moduleId", desired.ModuleID)
		go func() {
			if err := mod.Start(ctx, rctx.b); err != nil {
				rctx.log.Error("reconcile: in-process module failed", "moduleId", desired.ModuleID, "error", err)
			}
			rctx.mod.mu.Lock()
			delete(rctx.mod.running, desired.ModuleID)
			rctx.mod.mu.Unlock()
		}()

		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: []string{"embedded"},
			ActiveVersion:     "embedded",
			SystemdState:      "active",
			ReconcileState:    "ok",
		}, rctx.log)
	} else if !desired.Running && isRunning {
		// Stop module
		rctx.log.Info("reconcile: stopping in-process module", "moduleId", desired.ModuleID)
		rctx.mod.mu.Lock()
		rm := rctx.mod.running[desired.ModuleID]
		delete(rctx.mod.running, desired.ModuleID)
		rctx.mod.mu.Unlock()

		if err := rm.mod.Stop(); err != nil {
			rctx.log.Warn("reconcile: module stop error", "moduleId", desired.ModuleID, "error", err)
		}
		rm.cancel()

		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: []string{"embedded"},
			ActiveVersion:     "embedded",
			SystemdState:      "inactive",
			ReconcileState:    "ok",
		}, rctx.log)
	} else {
		// Already in desired state
		state := "inactive"
		if isRunning {
			state = "active"
		}
		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: []string{"embedded"},
			ActiveVersion:     "embedded",
			SystemdState:      state,
			ReconcileState:    "ok",
		}, rctx.log)
	}
}

// reconcileModule ensures a single module's actual state matches desired (bare-metal mode).
func reconcileModule(desired otypes.DesiredServiceKV, rctx *reconcilerContext) {
	entry := getRegistryEntry(desired.ModuleID)
	if entry == nil {
		rctx.log.Warn("reconcile: unknown module in desired_services", "moduleId", desired.ModuleID)
		return
	}

	// Self-update is handled specially
	if desired.ModuleID == selfModuleID {
		currentVersion := getActiveVersion(&selfEntry, rctx.config)
		resolvedVersion := resolveVersion(&selfEntry, desired.Version, rctx.config, rctx.log)
		if resolvedVersion != "" && resolvedVersion != currentVersion {
			rctx.log.Info("reconcile: self-update requested", "from", currentVersion, "to", resolvedVersion)
			selfUpdate(resolvedVersion, rctx.config, rctx.log)
		}
		return
	}

	// Step 1: Resolve version
	resolvedVersion := resolveVersion(entry, desired.Version, rctx.config, rctx.log)
	if resolvedVersion == "" {
		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     getActiveVersion(entry, rctx.config),
			SystemdState:      getSystemdState(entry.ModuleID),
			ReconcileState:    "version_unavailable",
			LastError:         fmt.Sprintf("Cannot resolve version %q (offline?)", desired.Version),
		}, rctx.log)
		return
	}

	// Step 2: Ensure version is installed on disk
	if !isVersionInstalled(entry.ModuleID, resolvedVersion, rctx.config) {
		if !checkInternet() {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     getActiveVersion(entry, rctx.config),
				SystemdState:      getSystemdState(entry.ModuleID),
				ReconcileState:    "version_unavailable",
				LastError:         fmt.Sprintf("Version %s not installed and no internet", resolvedVersion),
			}, rctx.log)
			return
		}

		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     getActiveVersion(entry, rctx.config),
			SystemdState:      getSystemdState(entry.ModuleID),
			ReconcileState:    "downloading",
		}, rctx.log)

		if !installVersion(entry, resolvedVersion, rctx.config, rctx.log) {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     getActiveVersion(entry, rctx.config),
				SystemdState:      getSystemdState(entry.ModuleID),
				ReconcileState:    "error",
				LastError:         fmt.Sprintf("Failed to download/install %s", resolvedVersion),
			}, rctx.log)
			return
		}
	}

	// Step 2.5: Ensure system dependencies are installed
	if len(entry.AptDeps) > 0 || len(entry.BuildDeps) > 0 {
		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     getActiveVersion(entry, rctx.config),
			SystemdState:      getSystemdState(entry.ModuleID),
			ReconcileState:    "installing_deps",
		}, rctx.log)
		if !ensureDeps(entry, rctx.log) {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     getActiveVersion(entry, rctx.config),
				SystemdState:      getSystemdState(entry.ModuleID),
				ReconcileState:    "error",
				LastError:         "Failed to install system dependencies",
			}, rctx.log)
			return
		}
	}

	// Step 3: Ensure correct version is active (symlinked)
	currentActiveVersion := getActiveVersion(entry, rctx.config)
	if currentActiveVersion != resolvedVersion {
		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     currentActiveVersion,
			SystemdState:      getSystemdState(entry.ModuleID),
			ReconcileState:    "installing",
		}, rctx.log)

		// Stop service if running before switching
		if getSystemdState(entry.ModuleID) == "active" {
			systemctlStop(entry.ModuleID, rctx.log)
		}

		if !updateSymlink(entry, resolvedVersion, rctx.config, rctx.log) {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     currentActiveVersion,
				SystemdState:      getSystemdState(entry.ModuleID),
				ReconcileState:    "error",
				LastError:         "Failed to update symlink",
			}, rctx.log)
			return
		}

		// Regenerate systemd unit (DENO_DIR path changes per version)
		writeSystemdUnit(entry, resolvedVersion, rctx.config, rctx.log)
		systemctlDaemonReload(rctx.log)
		systemctlEnable(entry.ModuleID, rctx.log)
	}

	// Step 3.5: Check required config is present before starting
	var moduleConfig map[string]string
	if len(entry.RequiredConfig) > 0 && desired.Running {
		var err error
		moduleConfig, err = getModuleConfig(rctx.b, entry.ModuleID)
		if err != nil {
			rctx.log.Warn("reconcile: failed to read config", "moduleId", entry.ModuleID, "error", err)
		}

		var missing []string
		for _, cf := range entry.RequiredConfig {
			if cf.Required {
				if val, ok := moduleConfig[cf.EnvVar]; !ok || val == "" {
					// Check if there's a default
					if cf.Default == "" {
						missing = append(missing, cf.EnvVar)
					}
				}
			}
		}
		if len(missing) > 0 {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     resolvedVersion,
				SystemdState:      getSystemdState(entry.ModuleID),
				ReconcileState:    "needs_config",
				LastError:         fmt.Sprintf("Missing required config: %v", missing),
			}, rctx.log)
			return
		}
	}

	// Step 4: Ensure running state matches desired
	systemdState := getSystemdState(entry.ModuleID)

	if desired.Running && systemdState != "active" {
		// Always regenerate the unit before starting to pick up config/dependency changes
		writeSystemdUnit(entry, resolvedVersion, rctx.config, rctx.log, moduleConfig)
		systemctlDaemonReload(rctx.log)

		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     resolvedVersion,
			SystemdState:      systemdState,
			ReconcileState:    "starting",
		}, rctx.log)
		if !systemctlStart(entry.ModuleID, rctx.log) {
			finalState := getSystemdState(entry.ModuleID)
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     resolvedVersion,
				SystemdState:      finalState,
				ReconcileState:    "error",
				LastError:         fmt.Sprintf("Failed to start %s", unitName(entry.ModuleID)),
			}, rctx.log)
			return
		}
	} else if !desired.Running && systemdState == "active" {
		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     resolvedVersion,
			SystemdState:      systemdState,
			ReconcileState:    "stopping",
		}, rctx.log)
		systemctlStop(entry.ModuleID, rctx.log)
	}

	// Final status report
	reportStatus(rctx.b, entry, statusOpts{
		InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
		ActiveVersion:     resolvedVersion,
		SystemdState:      getSystemdState(entry.ModuleID),
		ReconcileState:    "ok",
	}, rctx.log)
}

// fullSweep reads all desired_services entries and reconciles each one,
// then cleans up orphaned service_status entries.
func fullSweep(rctx *reconcilerContext) {
	desired, err := getAllDesiredServices(rctx.b)
	if err != nil {
		rctx.log.Error("reconcile: failed to read desired services", "error", err)
		return
	}
	rctx.log.Debug("reconcile: sweep", "count", len(desired))

	desiredSet := make(map[string]bool, len(desired))
	for _, d := range desired {
		desiredSet[d.ModuleID] = true
		func() {
			defer func() {
				if r := recover(); r != nil {
					rctx.log.Error("reconcile: panic", "moduleId", d.ModuleID, "recover", r)
				}
			}()
			dispatchReconcile(d, rctx)
		}()
	}

	// Clean up orphaned service_status entries
	statusKeys, err := rctx.b.KVKeys(topics.BucketServiceStatus)
	if err == nil {
		for _, key := range statusKeys {
			if !desiredSet[key] {
				rctx.log.Info("reconcile: cleaning up orphaned status", "moduleId", key)
				if err := rctx.b.KVDelete(topics.BucketServiceStatus, key); err != nil {
					rctx.log.Warn("reconcile: failed to delete orphaned status", "moduleId", key, "error", err)
				}
			}
		}
	}
}

// startReconciler starts the reconciliation loop with KV watch + periodic sweep.
// Returns a cancel function to stop the loop.
func startReconciler(rctx *reconcilerContext) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())

	// Periodic sweep
	go func() {
		ticker := time.NewTicker(time.Duration(rctx.config.ReconcileIntervalMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fullSweep(rctx)
			}
		}
	}()

	// KV watch for reactive reconciliation
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			startKvWatch(ctx, rctx)
			// If watch exits unexpectedly, retry after a delay
			if ctx.Err() != nil {
				return
			}
			rctx.log.Warn("reconcile: KV watch ended, retrying in 5s")
			time.Sleep(5 * time.Second)
		}
	}()

	// Initial sweep
	go fullSweep(rctx)

	return cancel
}

// startKvWatch watches the desired_services KV bucket for changes.
func startKvWatch(ctx context.Context, rctx *reconcilerContext) {
	done := make(chan struct{})

	sub, err := rctx.b.KVWatchAll(topics.BucketDesiredServices, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			rctx.log.Info("reconcile: module removed from desired_services, stopping and cleaning up", "moduleId", key)
			if rctx.mod != nil && rctx.mod.IsMonolith() {
				// Stop in-process module
				rctx.mod.mu.Lock()
				if rm, ok := rctx.mod.running[key]; ok {
					rm.mod.Stop()
					rm.cancel()
					delete(rctx.mod.running, key)
				}
				rctx.mod.mu.Unlock()
			} else {
				if getSystemdState(key) == "active" {
					systemctlStop(key, rctx.log)
				}
			}
			if err := rctx.b.KVDelete(topics.BucketServiceStatus, key); err != nil {
				rctx.log.Warn("reconcile: failed to delete service status", "moduleId", key, "error", err)
			}
			return
		}

		var desired otypes.DesiredServiceKV
		if err := json.Unmarshal(value, &desired); err != nil {
			rctx.log.Error("reconcile: failed to unmarshal desired service", "key", key, "error", err)
			return
		}

		rctx.log.Info("reconcile: desired state changed", "moduleId", desired.ModuleID, "version", desired.Version, "running", desired.Running)
		dispatchReconcile(desired, rctx)
	})
	if err != nil {
		rctx.log.Error("reconcile: failed to start KV watch", "error", err)
		return
	}
	defer sub.Unsubscribe()

	// Block until context is cancelled
	select {
	case <-ctx.Done():
	case <-done:
	}
}

// runMigration performs the full bootstrap migration on first boot.
func runMigration(b bus.Bus, config *OrchestratorConfig, log *slog.Logger) {
	if isMigrated(config) {
		log.Debug("migration: already migrated, skipping bootstrap")
		return
	}

	log.Info("migration: running bootstrap migration")
	migrated := 0

	for i := range moduleRegistry {
		entry := &moduleRegistry[i]
		if !isLegacyInstalled(entry, config) {
			continue
		}

		if !migrateModule(entry, config, log) {
			continue
		}

		state := getSystemdState(entry.ModuleID)
		running := state == "active"

		desired := otypes.DesiredServiceKV{
			ModuleID:  entry.ModuleID,
			Version:   "unknown",
			Running:   running,
			UpdatedAt: time.Now().UnixMilli(),
		}
		if err := putDesiredService(b, desired); err != nil {
			log.Error("migration: failed to put desired service", "moduleId", entry.ModuleID, "error", err)
			continue
		}
		log.Info("migration: populated desired_services", "moduleId", entry.ModuleID, "version", "unknown", "running", running)
		migrated++
	}

	if err := writeMigrationMarker(config); err != nil {
		log.Warn("migration: failed to write marker", "error", err)
	}
	log.Info("migration: bootstrap complete", "modulesAdopted", migrated)
}

const migrationMarker = ".orchestrator-migrated"

// isMigrated checks if bootstrap migration has already run.
func isMigrated(config *OrchestratorConfig) bool {
	_, err := os.Stat(config.ConfigDir + "/" + migrationMarker)
	return err == nil
}

// writeMigrationMarker writes the marker file to prevent re-migration.
func writeMigrationMarker(config *OrchestratorConfig) error {
	return os.WriteFile(
		config.ConfigDir+"/"+migrationMarker,
		[]byte(fmt.Sprintf("Migrated at %s\n", time.Now().Format(time.RFC3339))),
		0644,
	)
}

// isLegacyInstalled checks if a module exists at its pre-orchestrator path
// as a real file/directory (not a symlink).
func isLegacyInstalled(entry *otypes.ModuleRegistryEntry, config *OrchestratorConfig) bool {
	if entry.Runtime == "go" {
		info, err := os.Lstat(config.BinDir + "/" + entry.ModuleID)
		if err != nil {
			return false
		}
		return info.Mode().IsRegular()
	}

	info, err := os.Lstat(config.ServicesDir + "/" + entry.Repo)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// migrateModule moves a single module from legacy path to versioned storage.
func migrateModule(entry *otypes.ModuleRegistryEntry, config *OrchestratorConfig, log *slog.Logger) bool {
	versionDir := config.VersionsDir + "/" + entry.ModuleID + "/unknown"
	os.MkdirAll(versionDir, 0755)

	if entry.Runtime == "go" {
		legacyPath := config.BinDir + "/" + entry.ModuleID
		newPath := versionDir + "/" + entry.ModuleID

		if err := os.Rename(legacyPath, newPath); err != nil {
			log.Error("migration: failed to migrate", "moduleId", entry.ModuleID, "error", err)
			return false
		}
		if err := os.Symlink(newPath, legacyPath); err != nil {
			log.Error("migration: failed to create symlink", "moduleId", entry.ModuleID, "error", err)
			return false
		}
		log.Info("migration: migrated module", "moduleId", entry.ModuleID, "from", legacyPath, "to", newPath)
		return true
	}

	// Deno / deno-web
	legacyPath := config.ServicesDir + "/" + entry.Repo
	if err := os.Rename(legacyPath, versionDir); err != nil {
		log.Error("migration: failed to migrate", "moduleId", entry.ModuleID, "error", err)
		return false
	}
	if err := os.Symlink(versionDir, legacyPath); err != nil {
		log.Error("migration: failed to create symlink", "moduleId", entry.ModuleID, "error", err)
		return false
	}
	log.Info("migration: migrated module", "moduleId", entry.ModuleID, "from", legacyPath, "to", versionDir)
	return true
}
