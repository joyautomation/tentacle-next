//go:build gitops || all

// Package gitops implements bidirectional sync between NATS KV configuration
// and a git repository. Local config changes are committed and pushed;
// remote git changes are pulled and applied.
package gitops

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/paths"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
)

const serviceType = "gitops"

// Module implements the module.Module interface for GitOps sync.
type Module struct {
	moduleID      string
	b             bus.Bus
	log           *slog.Logger
	repo          *gitRepo
	cfg           gitopsConfig
	stopHeartbeat func()
	subs          []bus.Subscription
}

// New creates a new GitOps module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "gitops"
	}
	return &Module{moduleID: moduleID}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceType }

// Start initializes the git repo and starts sync loops.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	// Ensure required KV buckets exist.
	for _, bucket := range []string{topics.BucketTentacleConfig, topics.BucketServiceEnabled, topics.BucketHeartbeats, topics.BucketConfigMetadata} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("gitops: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Load configuration.
	m.cfg = loadConfig(b)

	if m.cfg.RepoURL == "" {
		m.log.Error("gitops: GITOPS_REPO_URL is required")
		// Keep running so heartbeat reports the problem and config can be updated.
		<-ctx.Done()
		return nil
	}

	// Register config schema for the settings UI.
	if schemaSub, err := config.RegisterSchema(b, serviceType, configSchema); err == nil {
		m.subs = append(m.subs, schemaSub)
	}

	// Start heartbeat.
	m.stopHeartbeat = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		meta := map[string]interface{}{
			"repoUrl":  m.cfg.RepoURL,
			"branch":   m.cfg.Branch,
			"autoPush": m.cfg.AutoPush,
			"autoPull": m.cfg.AutoPull,
		}
		if m.repo != nil {
			if sha, err := m.repo.CurrentSHA(); err == nil {
				meta["commitSHA"] = sha[:min(len(sha), 8)]
			}
		}
		return meta
	})

	// Set up local clone directory.
	cloneDir := filepath.Join(paths.DataDir(), "gitops", "repo")

	m.repo = &gitRepo{
		dir:        cloneDir,
		remote:     m.cfg.RepoURL,
		branch:     m.cfg.Branch,
		sshKeyPath: m.cfg.SSHKeyPath,
		log:        m.log,
	}

	// Initialize repo (clone or fetch).
	if err := m.repo.Init(); err != nil {
		m.log.Error("gitops: repo init failed", "error", err)
		// Keep running — the poll loop will retry.
	} else {
		m.repo.EnsureIdentity()
		m.log.Info("gitops: repo initialized", "dir", cloneDir, "remote", m.cfg.RepoURL, "branch", m.cfg.Branch)

		// Initial reconciliation: always apply on-disk manifests to KV, even if
		// no "new" remote changes were pulled. After a fresh clone, KV may hold
		// stale local state from the persistence file while the repo holds the
		// authoritative desired state. Without this forced apply, the syncToGit
		// below would export stale KV and overwrite the just-cloned files.
		configPath := filepath.Join(cloneDir, m.cfg.Path)
		applyFromDisk(b, configPath, m.log)

		// Now export KV → disk to capture any local state not yet in git.
		// Since apply just converged KV with disk, this should typically be a no-op.
		syncToGit(b, m.repo, configPath, m.cfg.AutoPush, m.log)
	}

	// Set up KV change channel for debounced push-to-git.
	changes := make(chan kvChange, 100)

	// Watch config-relevant KV buckets.
	watchBuckets := []string{
		topics.BucketGatewayConfig,
		topics.BucketDesiredServices,
		topics.BucketTentacleConfig,
	}
	for _, bucket := range watchBuckets {
		bucket := bucket // capture
		sub, err := b.KVWatchAll(bucket, func(key string, value []byte, op bus.KVOperation) {
			// Skip tentacle_config keys for the gitops module itself to avoid loops.
			if bucket == topics.BucketTentacleConfig {
				prefix := m.moduleID + "."
				if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
					return
				}
			}
			select {
			case changes <- kvChange{bucket: bucket, key: key}:
			default:
				// Channel full — drop oldest by draining one.
				select {
				case <-changes:
				default:
				}
				changes <- kvChange{bucket: bucket, key: key}
			}
		})
		if err != nil {
			m.log.Warn("gitops: failed to watch bucket", "bucket", bucket, "error", err)
		} else {
			m.subs = append(m.subs, sub)
		}
	}

	// Start background loops.
	go debounceLoop(ctx, b, m.repo, m.cfg, changes, m.log)
	go pollLoop(ctx, b, m.repo, m.cfg, m.log)

	m.log.Info("gitops: module started",
		"autoPush", m.cfg.AutoPush,
		"autoPull", m.cfg.AutoPull,
		"pollInterval", m.cfg.PollInterval,
		"debounce", m.cfg.DebounceS,
	)

	// Block until shutdown.
	<-ctx.Done()
	return nil
}

// Stop cleans up subscriptions and heartbeat.
func (m *Module) Stop() error {
	m.log.Info("gitops: stopping")
	for _, sub := range m.subs {
		sub.Unsubscribe()
	}
	if m.stopHeartbeat != nil {
		m.stopHeartbeat()
	}
	return nil
}
