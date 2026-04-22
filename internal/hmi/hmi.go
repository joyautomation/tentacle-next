//go:build hmi || all

// Package hmi implements the HMI module. The module owns the hmi_config KV
// bucket where HMI apps, screens, and reusable UDT-bound components are
// stored. Live tag values are streamed to the web UI via the existing
// /api/v1/variables/* endpoints; this module does not produce data of its own.
package hmi

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

const defaultServiceType = "hmi"

// Module implements the module.Module interface for the HMI service.
type Module struct {
	moduleID string
	stopHB   func()
	subs     []bus.Subscription
	b        bus.Bus
	log      *slog.Logger
	enabled  bool
}

// New creates a new HMI module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "hmi"
	}
	return &Module{moduleID: moduleID, enabled: true}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return defaultServiceType }

// Start initializes the HMI config bucket, heartbeat, and shutdown listener.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	for _, bucket := range []string{topics.BucketHeartbeats, topics.BucketServiceEnabled, topics.BucketHmiConfig} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("hmi: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	m.stopHB = heartbeat.Start(b, m.moduleID, defaultServiceType, func() map[string]interface{} {
		appCount := 0
		if keys, err := b.KVKeys(topics.BucketHmiConfig); err == nil {
			appCount = len(keys)
		}
		return map[string]interface{}{
			"apps":    appCount,
			"enabled": m.enabled,
		}
	})

	if data, _, err := b.KVGet(topics.BucketServiceEnabled, m.moduleID); err == nil {
		var state types.ServiceEnabledKV
		if json.Unmarshal(data, &state) == nil {
			m.enabled = state.Enabled
		}
	}

	enabledSub, err := b.KVWatch(topics.BucketServiceEnabled, m.moduleID, func(_ string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			m.enabled = true
			return
		}
		var state types.ServiceEnabledKV
		if json.Unmarshal(value, &state) == nil {
			m.enabled = state.Enabled
		}
	})
	if err != nil {
		m.log.Warn("hmi: failed to watch service_enabled KV", "error", err)
	} else {
		m.subs = append(m.subs, enabledSub)
	}

	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(_ string, _ []byte, _ bus.ReplyFunc) {
		m.log.Info("hmi: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.subs = append(m.subs, shutdownSub)

	m.log.Info("hmi: service running", "moduleId", m.moduleID)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down the heartbeat and subscriptions.
func (m *Module) Stop() error {
	if m.stopHB != nil {
		m.stopHB()
	}
	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil
	if m.log != nil {
		m.log.Info("hmi: shutdown complete")
	}
	return nil
}
