//go:build opcua || all

// Package opcua implements an OPC UA scanner module.
// It subscribes to OPC UA server tags and publishes data via the Bus.
package opcua

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const defaultServiceType = "opcua"

// Module implements the module.Module interface for OPC UA scanning.
type Module struct {
	moduleID string
	scanner  *Scanner
	stopHB   func()
	subs     []bus.Subscription
	b        bus.Bus
}

// New creates a new OPC UA module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "opcua"
	}
	return &Module{moduleID: moduleID}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return defaultServiceType }

// Start initializes the scanner, heartbeat, and enabled state watcher.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b

	// Ensure KV buckets exist
	for _, bucket := range []string{topics.BucketHeartbeats, topics.BucketServiceEnabled} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			slog.Warn("opcua: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Certificate setup
	pkiDir := os.Getenv("OPCUA_PKI_DIR")
	if pkiDir == "" {
		pkiDir = "./pki"
	}
	autoAcceptCerts := os.Getenv("OPCUA_AUTO_ACCEPT_CERTS") != "false"

	certFile, keyFile, err := ensureCertificate(pkiDir)
	if err != nil {
		slog.Error("opcua: certificate setup failed", "error", err)
		return err
	}
	slog.Info("opcua: certificate loaded", "certFile", certFile, "keyFile", keyFile)

	// Create and start scanner
	m.scanner = NewScanner(b, m.moduleID, certFile, keyFile, pkiDir, autoAcceptCerts)
	m.scanner.Start()

	// Start heartbeat
	m.stopHB = heartbeat.Start(b, m.moduleID, defaultServiceType, func() map[string]interface{} {
		return map[string]interface{}{
			"enabled": m.scanner.IsEnabled(),
		}
	})

	// Watch enabled state
	if data, _, err := b.KVGet(topics.BucketServiceEnabled, m.moduleID); err == nil {
		var state types.ServiceEnabledKV
		if json.Unmarshal(data, &state) == nil {
			m.scanner.SetEnabled(state.Enabled)
			slog.Info("opcua: initial enabled state", "enabled", state.Enabled)
		}
	}

	enabledSub, err := b.KVWatch(topics.BucketServiceEnabled, m.moduleID, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			m.scanner.SetEnabled(true) // default to enabled
			return
		}
		var state types.ServiceEnabledKV
		if json.Unmarshal(value, &state) == nil {
			m.scanner.SetEnabled(state.Enabled)
		}
	})
	if err != nil {
		slog.Warn("opcua: failed to watch service_enabled KV", "error", err)
	} else {
		m.subs = append(m.subs, enabledSub)
	}

	// Listen for shutdown via Bus
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		slog.Info("opcua: received shutdown command via Bus")
		_ = m.Stop()
		os.Exit(0)
	})
	m.subs = append(m.subs, shutdownSub)

	slog.Info("opcua: service running", "moduleId", m.moduleID)

	// Block until context cancelled or signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down scanner, heartbeat, and subscriptions.
func (m *Module) Stop() error {
	if m.scanner != nil {
		m.scanner.Stop()
	}
	if m.stopHB != nil {
		m.stopHB()
	}
	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil
	return nil
}
