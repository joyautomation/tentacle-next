//go:build ethernetip || all

// Package ethernetip implements an EtherNet/IP (CIP) scanner using libplctag.
// It subscribes to tags on Allen-Bradley ControlLogix PLCs and publishes data via the Bus.
package ethernetip

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const defaultServiceType = "ethernetip"

// Module implements the module.Module interface for EtherNet/IP scanning.
type Module struct {
	moduleID  string
	scanner   *Scanner
	stopHB    func()
	subs      []bus.Subscription
	b         bus.Bus
}

// New creates a new EtherNet/IP module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "ethernetip"
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
			slog.Warn("eip: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Create and start scanner
	m.scanner = NewScanner(b, m.moduleID)
	m.scanner.Start()

	// Start heartbeat
	m.stopHB = heartbeat.Start(b, m.moduleID, defaultServiceType, func() map[string]interface{} {
		devices := m.scanner.ActiveDevices()
		devicesJSON, _ := json.Marshal(devices)
		return map[string]interface{}{
			"devices":     string(devicesJSON),
			"enabled":     m.scanner.IsEnabled(),
			"publishRate": fmt.Sprintf("%.1f", m.scanner.PollRate()),
		}
	})

	// Watch enabled state
	if data, _, err := b.KVGet(topics.BucketServiceEnabled, m.moduleID); err == nil {
		var state types.ServiceEnabledKV
		if json.Unmarshal(data, &state) == nil {
			m.scanner.SetEnabled(state.Enabled)
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
		slog.Warn("eip: failed to watch service_enabled KV", "error", err)
	} else {
		m.subs = append(m.subs, enabledSub)
	}

	// Listen for shutdown via Bus
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		slog.Info("eip: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.subs = append(m.subs, shutdownSub)

	slog.Info("eip: service running", "moduleId", m.moduleID)

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
