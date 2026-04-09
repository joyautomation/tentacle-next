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
	"sync"
	"syscall"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	modpkg "github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/topics"
)

// ModuleFactory is a constructor function for creating module instances.
// Used in monolith mode to start/stop modules as goroutines.
type ModuleFactory func(moduleID string) modpkg.Module

// Option configures the orchestrator module.
type Option func(*Module)

// WithModuleFactories enables monolith mode with the given module constructors.
// When set, the orchestrator manages modules as in-process goroutines
// instead of systemd services.
func WithModuleFactories(factories map[string]ModuleFactory) Option {
	return func(m *Module) {
		m.factories = factories
		m.running = make(map[string]*runningModule)
	}
}

// runningModule tracks an in-process module goroutine.
type runningModule struct {
	mod    modpkg.Module
	cancel context.CancelFunc
}

const defaultServiceType = "orchestrator"

// Module implements the module.Module interface for the orchestrator.
type Module struct {
	moduleID       string
	stopHB         func()
	stopReconciler context.CancelFunc
	subs           []bus.Subscription
	b              bus.Bus
	log            *slog.Logger

	// Monolith mode: in-process module management
	factories map[string]ModuleFactory   // nil = bare-metal mode
	running   map[string]*runningModule
	mu        sync.Mutex
}

// IsMonolith returns true if the orchestrator is running in monolith mode.
func (m *Module) IsMonolith() bool {
	return m.factories != nil
}

// New creates a new orchestrator module.
func New(moduleID string, opts ...Option) *Module {
	if moduleID == "" {
		moduleID = "orchestrator"
	}
	mod := &Module{moduleID: moduleID}
	for _, opt := range opts {
		opt(mod)
	}
	return mod
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return defaultServiceType }

// Start initializes the orchestrator, heartbeat, command listener, and reconciler.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	mode := "bare-metal"
	if m.IsMonolith() {
		mode = "monolith"
	}
	m.log.Info("orchestrator: starting service orchestrator", "moduleId", m.moduleID, "mode", mode)

	// Load config
	config := loadConfig()
	m.log.Info("orchestrator: config loaded",
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
			m.log.Warn("orchestrator: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Bare-metal only: directories and migration
	if !m.IsMonolith() {
		os.MkdirAll(config.VersionsDir, 0755)
		os.MkdirAll(config.CacheDir+"/deno/versions", 0755)
		runMigration(b, config, m.log)
	}

	// Start heartbeat
	m.stopHB = heartbeat.Start(b, m.moduleID, defaultServiceType, func() map[string]interface{} {
		meta := map[string]interface{}{
			"reconcileIntervalMs": config.ReconcileIntervalMs,
			"mode":                mode,
		}
		if m.IsMonolith() {
			m.mu.Lock()
			meta["runningModules"] = len(m.running)
			m.mu.Unlock()
		}
		return meta
	})
	m.log.Info("orchestrator: heartbeat started", "moduleId", m.moduleID)

	// Start command listener (request/reply)
	cmdSub, err := startCommandListener(b, config, m)
	if err != nil {
		m.log.Warn("orchestrator: command listener failed to start", "error", err)
	} else {
		m.subs = append(m.subs, cmdSub)
	}

	// Start the reconciliation loop
	rctx := &reconcilerContext{
		b:      b,
		config: config,
		mod:    m,
		log:    m.log,
	}
	m.stopReconciler = startReconciler(rctx)
	m.log.Info("orchestrator: reconciler started")

	// Listen for shutdown via Bus
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("orchestrator: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.subs = append(m.subs, shutdownSub)

	m.log.Info("orchestrator: running", "moduleId", m.moduleID)

	// Block until context cancelled or signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case sig := <-sigChan:
		m.log.Info("orchestrator: received signal, shutting down", "signal", sig)
	}

	return nil
}

// Stop tears down reconciler, heartbeat, subscriptions, and running modules.
func (m *Module) Stop() error {
	if m.stopReconciler != nil {
		m.stopReconciler()
	}

	// Stop all in-process modules (monolith mode)
	if m.IsMonolith() {
		m.mu.Lock()
		for id, rm := range m.running {
			m.log.Info("orchestrator: stopping in-process module", "moduleId", id)
			if err := rm.mod.Stop(); err != nil {
				m.log.Warn("orchestrator: module stop error", "moduleId", id, "error", err)
			}
			rm.cancel()
		}
		m.running = make(map[string]*runningModule)
		m.mu.Unlock()
	}

	if m.stopHB != nil {
		m.stopHB()
	}
	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil
	m.log.Info("orchestrator: stopped")
	return nil
}
