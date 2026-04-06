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
}

// reconcileModule ensures a single module's actual state matches desired.
func reconcileModule(desired otypes.DesiredServiceKV, rctx *reconcilerContext) {
	entry := getRegistryEntry(desired.ModuleID)
	if entry == nil {
		slog.Warn("reconcile: unknown module in desired_services", "moduleId", desired.ModuleID)
		return
	}

	// Self-update is handled specially
	if desired.ModuleID == selfModuleID {
		currentVersion := getActiveVersion(&selfEntry, rctx.config)
		resolvedVersion := resolveVersion(&selfEntry, desired.Version, rctx.config)
		if resolvedVersion != "" && resolvedVersion != currentVersion {
			slog.Info("reconcile: self-update requested", "from", currentVersion, "to", resolvedVersion)
			selfUpdate(resolvedVersion, rctx.config)
		}
		return
	}

	// Step 1: Resolve version
	resolvedVersion := resolveVersion(entry, desired.Version, rctx.config)
	if resolvedVersion == "" {
		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     getActiveVersion(entry, rctx.config),
			SystemdState:      getSystemdState(entry.ModuleID),
			ReconcileState:    "version_unavailable",
			LastError:         fmt.Sprintf("Cannot resolve version %q (offline?)", desired.Version),
		})
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
			})
			return
		}

		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     getActiveVersion(entry, rctx.config),
			SystemdState:      getSystemdState(entry.ModuleID),
			ReconcileState:    "downloading",
		})

		if !installVersion(entry, resolvedVersion, rctx.config) {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     getActiveVersion(entry, rctx.config),
				SystemdState:      getSystemdState(entry.ModuleID),
				ReconcileState:    "error",
				LastError:         fmt.Sprintf("Failed to download/install %s", resolvedVersion),
			})
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
		})
		if !ensureDeps(entry) {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     getActiveVersion(entry, rctx.config),
				SystemdState:      getSystemdState(entry.ModuleID),
				ReconcileState:    "error",
				LastError:         "Failed to install system dependencies",
			})
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
		})

		// Stop service if running before switching
		if getSystemdState(entry.ModuleID) == "active" {
			systemctlStop(entry.ModuleID)
		}

		if !updateSymlink(entry, resolvedVersion, rctx.config) {
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     currentActiveVersion,
				SystemdState:      getSystemdState(entry.ModuleID),
				ReconcileState:    "error",
				LastError:         "Failed to update symlink",
			})
			return
		}

		// Regenerate systemd unit (DENO_DIR path changes per version)
		writeSystemdUnit(entry, resolvedVersion, rctx.config)
		systemctlDaemonReload()
		systemctlEnable(entry.ModuleID)
	}

	// Step 3.5: Check required config is present before starting
	var moduleConfig map[string]string
	if len(entry.RequiredConfig) > 0 && desired.Running {
		var err error
		moduleConfig, err = getModuleConfig(rctx.b, entry.ModuleID)
		if err != nil {
			slog.Warn("reconcile: failed to read config", "moduleId", entry.ModuleID, "error", err)
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
			})
			return
		}
	}

	// Step 4: Ensure running state matches desired
	systemdState := getSystemdState(entry.ModuleID)

	if desired.Running && systemdState != "active" {
		// Always regenerate the unit before starting to pick up config/dependency changes
		writeSystemdUnit(entry, resolvedVersion, rctx.config, moduleConfig)
		systemctlDaemonReload()

		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     resolvedVersion,
			SystemdState:      systemdState,
			ReconcileState:    "starting",
		})
		if !systemctlStart(entry.ModuleID) {
			finalState := getSystemdState(entry.ModuleID)
			reportStatus(rctx.b, entry, statusOpts{
				InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
				ActiveVersion:     resolvedVersion,
				SystemdState:      finalState,
				ReconcileState:    "error",
				LastError:         fmt.Sprintf("Failed to start %s", unitName(entry.ModuleID)),
			})
			return
		}
	} else if !desired.Running && systemdState == "active" {
		reportStatus(rctx.b, entry, statusOpts{
			InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
			ActiveVersion:     resolvedVersion,
			SystemdState:      systemdState,
			ReconcileState:    "stopping",
		})
		systemctlStop(entry.ModuleID)
	}

	// Final status report
	reportStatus(rctx.b, entry, statusOpts{
		InstalledVersions: listInstalledVersions(entry.ModuleID, rctx.config),
		ActiveVersion:     resolvedVersion,
		SystemdState:      getSystemdState(entry.ModuleID),
		ReconcileState:    "ok",
	})
}

// fullSweep reads all desired_services entries and reconciles each one,
// then cleans up orphaned service_status entries.
func fullSweep(rctx *reconcilerContext) {
	desired, err := getAllDesiredServices(rctx.b)
	if err != nil {
		slog.Error("reconcile: failed to read desired services", "error", err)
		return
	}
	slog.Debug("reconcile: sweep", "count", len(desired))

	desiredSet := make(map[string]bool, len(desired))
	for _, d := range desired {
		desiredSet[d.ModuleID] = true
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("reconcile: panic", "moduleId", d.ModuleID, "recover", r)
				}
			}()
			reconcileModule(d, rctx)
		}()
	}

	// Clean up orphaned service_status entries
	statusKeys, err := rctx.b.KVKeys(topics.BucketServiceStatus)
	if err == nil {
		for _, key := range statusKeys {
			if !desiredSet[key] {
				slog.Info("reconcile: cleaning up orphaned status", "moduleId", key)
				if err := rctx.b.KVDelete(topics.BucketServiceStatus, key); err != nil {
					slog.Warn("reconcile: failed to delete orphaned status", "moduleId", key, "error", err)
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
			slog.Warn("reconcile: KV watch ended, retrying in 5s")
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
			slog.Info("reconcile: module removed from desired_services, stopping and cleaning up", "moduleId", key)
			if getSystemdState(key) == "active" {
				systemctlStop(key)
			}
			if err := rctx.b.KVDelete(topics.BucketServiceStatus, key); err != nil {
				slog.Warn("reconcile: failed to delete service status", "moduleId", key, "error", err)
			}
			return
		}

		var desired otypes.DesiredServiceKV
		if err := json.Unmarshal(value, &desired); err != nil {
			slog.Error("reconcile: failed to unmarshal desired service", "key", key, "error", err)
			return
		}

		slog.Info("reconcile: desired state changed", "moduleId", desired.ModuleID, "version", desired.Version, "running", desired.Running)
		reconcileModule(desired, rctx)
	})
	if err != nil {
		slog.Error("reconcile: failed to start KV watch", "error", err)
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
func runMigration(b bus.Bus, config *OrchestratorConfig) {
	if isMigrated(config) {
		slog.Debug("migration: already migrated, skipping bootstrap")
		return
	}

	slog.Info("migration: running bootstrap migration")
	migrated := 0

	for i := range moduleRegistry {
		entry := &moduleRegistry[i]
		if !isLegacyInstalled(entry, config) {
			continue
		}

		if !migrateModule(entry, config) {
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
			slog.Error("migration: failed to put desired service", "moduleId", entry.ModuleID, "error", err)
			continue
		}
		slog.Info("migration: populated desired_services", "moduleId", entry.ModuleID, "version", "unknown", "running", running)
		migrated++
	}

	if err := writeMigrationMarker(config); err != nil {
		slog.Warn("migration: failed to write marker", "error", err)
	}
	slog.Info("migration: bootstrap complete", "modulesAdopted", migrated)
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
func migrateModule(entry *otypes.ModuleRegistryEntry, config *OrchestratorConfig) bool {
	versionDir := config.VersionsDir + "/" + entry.ModuleID + "/unknown"
	os.MkdirAll(versionDir, 0755)

	if entry.Runtime == "go" {
		legacyPath := config.BinDir + "/" + entry.ModuleID
		newPath := versionDir + "/" + entry.ModuleID

		if err := os.Rename(legacyPath, newPath); err != nil {
			slog.Error("migration: failed to migrate", "moduleId", entry.ModuleID, "error", err)
			return false
		}
		if err := os.Symlink(newPath, legacyPath); err != nil {
			slog.Error("migration: failed to create symlink", "moduleId", entry.ModuleID, "error", err)
			return false
		}
		slog.Info("migration: migrated module", "moduleId", entry.ModuleID, "from", legacyPath, "to", newPath)
		return true
	}

	// Deno / deno-web
	legacyPath := config.ServicesDir + "/" + entry.Repo
	if err := os.Rename(legacyPath, versionDir); err != nil {
		slog.Error("migration: failed to migrate", "moduleId", entry.ModuleID, "error", err)
		return false
	}
	if err := os.Symlink(versionDir, legacyPath); err != nil {
		slog.Error("migration: failed to create symlink", "moduleId", entry.ModuleID, "error", err)
		return false
	}
	slog.Info("migration: migrated module", "moduleId", entry.ModuleID, "from", legacyPath, "to", versionDir)
	return true
}
