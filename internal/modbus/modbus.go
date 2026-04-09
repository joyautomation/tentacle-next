//go:build modbus || all

// Package modbus implements a Modbus TCP scanner module for tentacle.
// It polls Modbus devices over TCP, reading coils, discrete inputs, holding
// registers, and input registers. Data is published via the Bus interface;
// write commands are accepted for writable tags.
package modbus

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

const serviceTypeName = "modbus"

// Module implements the module.Module interface for the Modbus TCP scanner.
type Module struct {
	moduleID      string
	b             bus.Bus
	log           *slog.Logger
	scanner       *Scanner
	stopHeartbeat func()
	enabledSub    bus.Subscription
	shutdownSub   bus.Subscription
	enabled       bool
}

// New creates a new Modbus scanner module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "modbus"
	}
	return &Module{
		moduleID: moduleID,
		enabled:  true,
	}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceTypeName }

// Start initializes the Modbus scanner module with the given Bus.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	// Ensure required KV buckets exist
	for _, bucket := range []string{topics.BucketHeartbeats, topics.BucketServiceEnabled} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("modbus: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Start heartbeat
	m.stopHeartbeat = heartbeat.Start(b, m.moduleID, serviceTypeName, func() map[string]interface{} {
		meta := map[string]interface{}{
			"enabled": m.enabled,
		}
		if m.scanner != nil {
			m.scanner.mu.Lock()
			meta["deviceCount"] = len(m.scanner.devices)
			tagCount := 0
			for _, dev := range m.scanner.devices {
				dev.mu.Lock()
				tagCount += len(dev.allTags)
				dev.mu.Unlock()
			}
			meta["tagCount"] = tagCount
			m.scanner.mu.Unlock()
		}
		return meta
	})

	// Create and start the scanner
	m.scanner = newScanner(b, m.moduleID, m.log)
	if err := m.scanner.start(); err != nil {
		return err
	}

	// Watch enabled state from KV
	m.watchEnabled()

	// Listen for shutdown via Bus
	shutdownSub, err := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("modbus: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	if err != nil {
		m.log.Warn("modbus: failed to subscribe to shutdown", "error", err)
	} else {
		m.shutdownSub = shutdownSub
	}

	// Block until context cancelled or signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop gracefully shuts down the module.
func (m *Module) Stop() error {
	if m.scanner != nil {
		m.scanner.stop()
	}
	if m.enabledSub != nil {
		_ = m.enabledSub.Unsubscribe()
		m.enabledSub = nil
	}
	if m.shutdownSub != nil {
		_ = m.shutdownSub.Unsubscribe()
		m.shutdownSub = nil
	}
	if m.stopHeartbeat != nil {
		m.stopHeartbeat()
	}
	m.log.Info("modbus: module stopped", "moduleId", m.moduleID)
	return nil
}

// watchEnabled watches the service_enabled KV bucket for this module's enabled state.
func (m *Module) watchEnabled() {
	sub, err := m.b.KVWatch(topics.BucketServiceEnabled, m.moduleID, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			m.log.Info("modbus: enabled key deleted, defaulting to enabled")
			m.enabled = true
			return
		}
		var kv types.ServiceEnabledKV
		if err := json.Unmarshal(value, &kv); err != nil {
			m.log.Warn("modbus: failed to parse enabled state", "error", err)
			return
		}
		m.enabled = kv.Enabled
		m.log.Info("modbus: enabled state changed", "enabled", m.enabled)
	})
	if err != nil {
		m.log.Warn("modbus: failed to watch enabled state", "error", err)
		return
	}
	m.enabledSub = sub
}
