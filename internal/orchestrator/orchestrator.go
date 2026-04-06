//go:build orchestrator || all

// Package orchestrator implements the bare-metal service orchestrator module.
// It manages systemd services for all tentacle modules: downloading binaries,
// installing versions, writing systemd units, and reconciling desired vs actual state.
package orchestrator

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
)

const defaultServiceType = "orchestrator"

// Module implements the module.Module interface for the orchestrator.
type Module struct {
	moduleID       string
	stopHB         func()
	stopReconciler context.CancelFunc
	subs           []bus.Subscription
	b              bus.Bus
}

// New creates a new orchestrator module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "orchestrator"
	}
	return &Module{moduleID: moduleID}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return defaultServiceType }

// Start initializes the orchestrator, heartbeat, command listener, and reconciler.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b

	slog.Info("orchestrator: starting bare-metal service orchestrator", "moduleId", m.moduleID)

	// Load config
	config := loadConfig()
	slog.Info("orchestrator: config loaded",
		"installDir", config.InstallDir,
		"reconcileIntervalMs", config.ReconcileIntervalMs,
	)

	// Ensure KV buckets exist
	for _, bucket := range []string{
		topics.BucketHeartbeats,
		topics.BucketServiceEnabled,
		topics.BucketServiceStatus,
		topics.BucketDesiredServices,
		topics.BucketTentacleConfig,
	} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			slog.Warn("orchestrator: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Ensure directories exist
	os.MkdirAll(config.VersionsDir, 0755)
	os.MkdirAll(config.CacheDir+"/deno/versions", 0755)

	// Run bootstrap migration if needed (first boot)
	runMigration(b, config)

	// Start heartbeat
	m.stopHB = heartbeat.Start(b, m.moduleID, defaultServiceType, func() map[string]interface{} {
		return map[string]interface{}{
			"reconcileIntervalMs": config.ReconcileIntervalMs,
		}
	})
	slog.Info("orchestrator: heartbeat started", "moduleId", m.moduleID)

	// Start command listener (request/reply)
	cmdSub, err := startCommandListener(b, config)
	if err != nil {
		slog.Warn("orchestrator: command listener failed to start", "error", err)
	} else {
		m.subs = append(m.subs, cmdSub)
	}

	// Start the reconciliation loop
	rctx := &reconcilerContext{
		b:      b,
		config: config,
	}
	m.stopReconciler = startReconciler(rctx)
	slog.Info("orchestrator: reconciler started")

	// Listen for shutdown via Bus
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		slog.Info("orchestrator: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.subs = append(m.subs, shutdownSub)

	slog.Info("orchestrator: running", "moduleId", m.moduleID)

	// Block until context cancelled or signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case sig := <-sigChan:
		slog.Info("orchestrator: received signal, shutting down", "signal", sig)
	}

	return nil
}

// Stop tears down reconciler, heartbeat, and subscriptions.
func (m *Module) Stop() error {
	if m.stopReconciler != nil {
		m.stopReconciler()
	}
	if m.stopHB != nil {
		m.stopHB()
	}
	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil
	slog.Info("orchestrator: stopped")
	return nil
}
