//go:build gitops || all

package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/manifest"
	"github.com/joyautomation/tentacle/internal/topics"
)

// kvChange represents a detected configuration change.
type kvChange struct {
	bucket string
	key    string
}

// syncToGit exports current config and commits/pushes to git.
//
// nodeID identifies which edge produced the commit; it goes into the commit
// trailer so a fleet operator can audit which side of the bidirectional sync
// originated each change.
func syncToGit(b bus.Bus, repo *gitRepo, configPath, nodeID string, autoPush bool, log *slog.Logger) {
	resources, err := manifest.Export(b, manifest.ExportOptions{})
	if err != nil {
		log.Error("gitops: export failed", "error", err)
		return
	}

	if err := writeManifestFiles(resources, configPath); err != nil {
		log.Error("gitops: write manifests failed", "error", err)
		return
	}

	hasChanges, err := repo.HasLocalChanges()
	if err != nil {
		log.Error("gitops: check changes failed", "error", err)
		return
	}
	if !hasChanges {
		log.Debug("gitops: no changes to commit")
		return
	}

	source := nodeID
	if source == "" {
		source = "edge"
	}
	msg := fmt.Sprintf("config: update via tentacle at %s\n\nSource: edge:%s", time.Now().Format(time.RFC3339), source)
	if err := repo.CommitAll(msg); err != nil {
		log.Error("gitops: commit failed", "error", err)
		return
	}
	log.Info("gitops: committed config changes")

	if autoPush {
		if err := repo.PushWithRetry(); err != nil {
			log.Error("gitops: push failed", "error", err)
			return
		}
		log.Info("gitops: pushed to remote")
	}
}

// applyFromDisk reads all manifests in configPath and applies them to KV
// unconditionally. Used at startup to force convergence of KV with the
// just-cloned repo, regardless of whether any "new" commits were pulled.
func applyFromDisk(b bus.Bus, configPath string, log *slog.Logger) {
	resources, err := readManifestFiles(configPath)
	if err != nil {
		log.Error("gitops: read manifests for initial apply failed", "error", err)
		return
	}
	if len(resources) == 0 {
		return
	}
	result, err := manifest.Apply(b, resources, "gitops")
	if err != nil {
		log.Error("gitops: initial apply from disk failed", "error", err)
		return
	}
	log.Info("gitops: initial apply from disk", "applied", len(result.Applied), "skipped", len(result.Skipped))
}

// syncFromGit pulls remote changes and applies them to KV.
func syncFromGit(b bus.Bus, repo *gitRepo, configPath string, log *slog.Logger) {
	hasNew, err := repo.HasRemoteChanges()
	if err != nil {
		log.Warn("gitops: check remote failed", "error", err)
		return
	}
	if !hasNew {
		return
	}

	pulled, err := repo.Pull()
	if err != nil {
		log.Error("gitops: pull failed, resetting to remote", "error", err)
		if resetErr := repo.ResetToRemote(); resetErr != nil {
			log.Error("gitops: reset to remote failed", "error", resetErr)
			return
		}
		pulled = true
	}
	if !pulled {
		return
	}

	log.Info("gitops: pulled new changes from remote")

	resources, err := readManifestFiles(configPath)
	if err != nil {
		log.Error("gitops: read manifests failed", "error", err)
		return
	}

	result, err := manifest.Apply(b, resources, "gitops")
	if err != nil {
		log.Error("gitops: apply from git failed", "error", err)
		return
	}

	log.Info("gitops: applied remote changes",
		"applied", len(result.Applied),
		"skipped", len(result.Skipped),
	)
}

// isGitOpsSource checks the config_metadata bucket to see if a change was
// made by the gitops module itself (feedback loop prevention).
func isGitOpsSource(b bus.Bus, bucket, key string) bool {
	metaKey := bucket + "." + key
	data, _, err := b.KVGet(topics.BucketConfigMetadata, metaKey)
	if err != nil || len(data) == 0 {
		return false
	}
	var meta struct {
		Source    string `json:"source"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return false
	}
	// Consider "gitops" source fresh within the last 10 seconds.
	if meta.Source == "gitops" && time.Since(time.UnixMilli(meta.Timestamp)) < 10*time.Second {
		return true
	}
	return false
}

// debounceLoop collects KV changes and triggers syncToGit after a quiet period.
func debounceLoop(ctx context.Context, b bus.Bus, repo *gitRepo, cfg gitopsConfig, nodeID string, changes <-chan kvChange, log *slog.Logger) {
	configPath := filepath.Join(repo.dir, cfg.Path)
	debounce := time.Duration(cfg.DebounceS) * time.Second

	timer := time.NewTimer(0)
	timer.Stop() // Start dormant.
	pending := false

	for {
		select {
		case <-ctx.Done():
			if pending {
				syncToGit(b, repo, configPath, nodeID, cfg.AutoPush, log)
			}
			return

		case change := <-changes:
			// Skip changes made by gitops itself.
			if isGitOpsSource(b, change.bucket, change.key) {
				continue
			}
			pending = true
			timer.Reset(debounce)

		case <-timer.C:
			if pending {
				syncToGit(b, repo, configPath, nodeID, cfg.AutoPush, log)
				pending = false
			}
		}
	}
}

// pollLoop periodically checks for remote git changes and applies them.
func pollLoop(ctx context.Context, b bus.Bus, repo *gitRepo, cfg gitopsConfig, log *slog.Logger) {
	configPath := filepath.Join(repo.dir, cfg.Path)
	ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if cfg.AutoPull {
				syncFromGit(b, repo, configPath, log)
			}
		}
	}
}

// ─── File I/O ───────────────────────────────────────────────────────────────

// writeManifestFiles writes resources to the config directory, one file per resource.
func writeManifestFiles(resources []any, configPath string) error {
	// Group resources by kind for directory structure.
	kindDir := map[string]string{
		manifest.KindGateway:      "gateways",
		manifest.KindService:      "services",
		manifest.KindModuleConfig: "config",
		manifest.KindNftables:     "infrastructure",
		manifest.KindNetwork:      "infrastructure",
		manifest.KindPlc:          "plc",
	}

	// Ensure all directories exist.
	for _, dir := range kindDir {
		if err := os.MkdirAll(filepath.Join(configPath, dir), 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Write each resource to its own file.
	for _, res := range resources {
		kind := resourceKind(res)
		name := resourceName(res)
		dir, ok := kindDir[kind]
		if !ok {
			continue
		}

		yamlBytes, err := manifest.Serialize([]any{res})
		if err != nil {
			return fmt.Errorf("serialize %s/%s: %w", kind, name, err)
		}

		filename := filepath.Join(configPath, dir, name+".yaml")
		if err := os.WriteFile(filename, yamlBytes, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
		}
	}

	return nil
}

// readManifestFiles reads all YAML files from the config directory tree.
func readManifestFiles(configPath string) ([]any, error) {
	var allResources []any

	err := filepath.Walk(configPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		resources, err := manifest.ParseBytes(data)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		allResources = append(allResources, resources...)
		return nil
	})

	return allResources, err
}

// resourceKind and resourceName extract header fields from typed resources.
func resourceKind(res any) string {
	switch r := res.(type) {
	case *manifest.GatewayResource:
		return r.Kind
	case *manifest.ServiceResource:
		return r.Kind
	case *manifest.ModuleConfigResource:
		return r.Kind
	case *manifest.NftablesResource:
		return r.Kind
	case *manifest.NetworkResource:
		return r.Kind
	case *manifest.PlcResource:
		return r.Kind
	default:
		return ""
	}
}

func resourceName(res any) string {
	switch r := res.(type) {
	case *manifest.GatewayResource:
		return r.Metadata.Name
	case *manifest.ServiceResource:
		return r.Metadata.Name
	case *manifest.ModuleConfigResource:
		return r.Metadata.Name
	case *manifest.NftablesResource:
		return r.Metadata.Name
	case *manifest.NetworkResource:
		return r.Metadata.Name
	case *manifest.PlcResource:
		return r.Metadata.Name
	default:
		return ""
	}
}
