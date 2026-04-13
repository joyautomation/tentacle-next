//go:build all

// Package caddy manages a Caddy reverse proxy instance.
// It auto-installs Caddy, writes the Caddyfile (from structured config or raw
// advanced mode), and manages the caddy systemd service.
package caddy

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/topics"
)

const serviceType = "caddy"

// Module implements module.Module for the Caddy reverse proxy manager.
type Module struct {
	moduleID      string
	b             bus.Bus
	log           *slog.Logger
	stopHeartbeat func()
	subs          []bus.Subscription
	mu            sync.Mutex

	installed bool
	running   bool
	cfg       caddyConfig
}

// New creates a new Caddy module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "caddy"
	}
	return &Module{moduleID: moduleID}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceType }

// Start installs Caddy (if needed), writes the Caddyfile, starts the caddy
// systemd service, and watches for config changes.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	// Ensure required KV buckets exist.
	for _, bucket := range []string{topics.BucketTentacleConfig, topics.BucketServiceEnabled, topics.BucketHeartbeats} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("caddy: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Install Caddy if not present.
	if err := ensureInstalled(m.log); err != nil {
		m.log.Error("caddy: installation failed", "error", err)
		// Continue — the module can still register schema and accept config.
		// Once the user resolves the install issue and restarts, it will work.
	} else {
		m.installed = true
	}

	// Load and persist config.
	m.cfg = loadConfig(b)
	saveConfig(b, &m.cfg)

	// If advanced mode has no Caddyfile yet, seed it from simple config.
	if m.cfg.AdvancedMode && m.cfg.Caddyfile == "" {
		m.cfg.Caddyfile = generateCaddyfile(m.cfg.Domain, m.cfg.UpstreamPort)
		saveConfig(b, &m.cfg)
	}

	// Register config schema for the settings UI.
	if schemaSub, err := config.RegisterSchema(b, serviceType, configSchema); err == nil {
		m.subs = append(m.subs, schemaSub)
	}

	// Start heartbeat.
	m.stopHeartbeat = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		m.mu.Lock()
		defer m.mu.Unlock()
		mode := "simple"
		if m.cfg.AdvancedMode {
			mode = "advanced"
		}
		return map[string]interface{}{
			"installed": m.installed,
			"running":   m.running,
			"domain":    m.cfg.Domain,
			"mode":      mode,
		}
	})

	// Write initial Caddyfile and start service.
	if m.installed {
		m.applyCaddyfile()
		m.startCaddy()
	}

	// Register status browse handler.
	statusVars := []module.StatusVar{
		{Name: "installed", Datatype: "boolean"},
		{Name: "running", Datatype: "boolean"},
		{Name: "domain", Datatype: "string"},
		{Name: "mode", Datatype: "string"},
	}
	if sub, err := b.Subscribe(topics.StatusBrowse(serviceType), func(_ string, _ []byte, reply bus.ReplyFunc) {
		module.HandleStatusBrowse(statusVars, reply)
	}); err == nil {
		m.subs = append(m.subs, sub)
	}

	// Publish status periodically.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.publishStatus()
			}
		}
	}()

	// Watch for config changes — debounce to 1s.
	var configDebounce *time.Timer
	var configMu sync.Mutex
	configSub, err := b.KVWatchAll(topics.BucketTentacleConfig, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			return
		}
		prefix := "caddy."
		if len(key) < len(prefix) || key[:len(prefix)] != prefix {
			return
		}
		configMu.Lock()
		defer configMu.Unlock()
		if configDebounce != nil {
			configDebounce.Stop()
		}
		configDebounce = time.AfterFunc(1*time.Second, func() {
			m.log.Info("caddy: config changed, updating Caddyfile")
			m.mu.Lock()
			m.cfg = loadConfig(b)
			// Seed advanced Caddyfile if switching to advanced mode for the first time.
			if m.cfg.AdvancedMode && m.cfg.Caddyfile == "" {
				m.cfg.Caddyfile = generateCaddyfile(m.cfg.Domain, m.cfg.UpstreamPort)
				saveConfig(b, &m.cfg)
			}
			m.mu.Unlock()
			if m.installed {
				m.applyCaddyfile()
				m.reloadOrStart()
			}
		})
	})
	if err == nil {
		m.subs = append(m.subs, configSub)
	}

	// Shutdown handler.
	if sub, err := b.Subscribe(topics.Shutdown(m.moduleID), func(_ string, _ []byte, _ bus.ReplyFunc) {
		m.log.Info("caddy: received shutdown command")
		m.Stop()
		os.Exit(0)
	}); err == nil {
		m.subs = append(m.subs, sub)
	}

	// Block until context cancelled or signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down subscriptions and heartbeat. Caddy itself keeps running —
// it's a reverse proxy that should outlive module restarts.
func (m *Module) Stop() error {
	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil
	if m.stopHeartbeat != nil {
		m.stopHeartbeat()
	}
	return nil
}

// applyCaddyfile writes the resolved Caddyfile to disk.
func (m *Module) applyCaddyfile() {
	m.mu.Lock()
	content := resolveCaddyfile(m.cfg)
	m.mu.Unlock()

	if err := writeCaddyfile(content, m.log); err != nil {
		m.log.Error("caddy: failed to write Caddyfile", "error", err)
	}
}

// startCaddy enables and starts the caddy systemd service.
func (m *Module) startCaddy() {
	if out, err := exec.Command("systemctl", "enable", "--now", "caddy").CombinedOutput(); err != nil {
		m.log.Error("caddy: failed to start service", "error", err, "output", string(out))
		return
	}
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()
	m.log.Info("caddy: service started")
}

// reloadOrStart reloads Caddy if running, otherwise starts it.
func (m *Module) reloadOrStart() {
	// Check if caddy is active.
	out, err := exec.Command("systemctl", "is-active", "caddy").CombinedOutput()
	if err != nil || string(out) != "active\n" {
		m.startCaddy()
		return
	}
	if err := reloadCaddy(m.log); err != nil {
		m.log.Error("caddy: reload failed, restarting", "error", err)
		exec.Command("systemctl", "restart", "caddy").Run()
	}
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()
}

// publishStatus publishes caddy status variables via the bus.
func (m *Module) publishStatus() {
	m.mu.Lock()
	cfg := m.cfg
	installed := m.installed
	running := m.running
	m.mu.Unlock()

	mode := "simple"
	if cfg.AdvancedMode {
		mode = "advanced"
	}

	module.PublishStatus(m.b, serviceType, map[string]module.StatusValue{
		"installed": {Value: installed, Datatype: "boolean"},
		"running":   {Value: running, Datatype: "boolean"},
		"domain":    {Value: cfg.Domain, Datatype: "string"},
		"mode":      {Value: mode, Datatype: "string"},
	})
}
