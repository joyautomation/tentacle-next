//go:build ethernetipserver || all

// Package ethernetipserver implements an EtherNet/IP (CIP) server using gologix.
// It exposes PLC tag values over CIP for external EtherNet/IP clients to read/write,
// bridging bus data into a CIP tag database.
package ethernetipserver

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

const defaultServiceType = "ethernetip-server"

// Module implements the module.Module interface for the EtherNet/IP CIP server.
type Module struct {
	moduleID string
	manager  *Manager
	stopHB   func()
	subs     []bus.Subscription
	b        bus.Bus
	log      *slog.Logger
}

// New creates a new EtherNet/IP server module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "ethernetip-server"
	}
	return &Module{moduleID: moduleID}
}

// ModuleID returns the unique identifier for this module instance.
func (m *Module) ModuleID() string { return m.moduleID }

// ServiceType returns the service type identifier.
func (m *Module) ServiceType() string { return defaultServiceType }

// Start initializes the manager, heartbeat, and enabled state watcher.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	// Ensure KV buckets exist
	for _, bucket := range []string{topics.BucketHeartbeats, topics.BucketServiceEnabled} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("eipserver: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Create and start manager
	m.manager = NewManager(b, m.moduleID, m.log)
	m.manager.Start()

	// Start heartbeat
	m.stopHB = heartbeat.Start(b, m.moduleID, defaultServiceType, func() map[string]interface{} {
		return map[string]interface{}{}
	})

	// Watch enabled state
	if data, _, err := b.KVGet(topics.BucketServiceEnabled, m.moduleID); err == nil {
		var state types.ServiceEnabledKV
		if json.Unmarshal(data, &state) == nil {
			if !state.Enabled {
				m.log.Info("eipserver: service disabled via KV", "moduleId", m.moduleID)
			}
		}
	}

	enabledSub, err := b.KVWatch(topics.BucketServiceEnabled, m.moduleID, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			m.log.Info("eipserver: enabled key deleted, defaulting to enabled")
			return
		}
		var state types.ServiceEnabledKV
		if json.Unmarshal(value, &state) == nil {
			m.log.Info("eipserver: enabled state changed", "enabled", state.Enabled)
		}
	})
	if err != nil {
		m.log.Warn("eipserver: failed to watch service_enabled KV", "error", err)
	} else {
		m.subs = append(m.subs, enabledSub)
	}

	// Listen for shutdown via bus
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("eipserver: received shutdown command via bus")
		_ = m.Stop()
		os.Exit(0)
	})
	m.subs = append(m.subs, shutdownSub)

	m.log.Info("eipserver: service running", "moduleId", m.moduleID)

	// Block until context cancelled or signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down the manager, heartbeat, and subscriptions.
func (m *Module) Stop() error {
	if m.manager != nil {
		m.manager.Stop()
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
