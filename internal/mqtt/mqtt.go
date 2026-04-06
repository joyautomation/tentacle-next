//go:build mqtt || all

// Package mqtt implements the Sparkplug B MQTT bridge module.
// It subscribes to PLC data on the Bus, converts it to Sparkplug B metrics,
// and publishes via MQTT. It also handles DCMD (device commands) from MQTT
// back to the appropriate source modules on the Bus.
package mqtt

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

const serviceType = "mqtt"

// Module implements the module.Module interface for the MQTT bridge.
type Module struct {
	moduleID      string
	b             bus.Bus
	bridge        *Bridge
	stopHeartbeat func()
	subs          []bus.Subscription
}

// New creates a new MQTT bridge module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "mqtt"
	}
	return &Module{moduleID: moduleID}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceType }

// Start initializes and runs the MQTT bridge module.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b

	// Ensure required KV buckets exist
	for _, bucket := range []string{topics.BucketTentacleConfig, topics.BucketServiceEnabled, topics.BucketHeartbeats} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			slog.Warn("mqtt: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Load configuration
	cfg := loadConfig(b)

	// Save config to KV for persistence
	saveConfig(b, &cfg)

	// Start heartbeat
	m.stopHeartbeat = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		meta := map[string]interface{}{
			"brokerUrl": cfg.BrokerURL,
			"clientId":  cfg.ClientID,
		}
		if m.bridge != nil && m.bridge.sf != nil {
			meta["storeForward"] = m.bridge.sf.State() != SFOnline
		}
		return meta
	})

	// Create and start the bridge
	m.bridge = NewBridge(b, m.moduleID, cfg)
	if err := m.bridge.Start(); err != nil {
		slog.Error("mqtt: failed to start bridge", "error", err)
		// Don't return error — continue running so heartbeat/config updates work
	} else {
		slog.Info("mqtt: bridge started",
			"broker", cfg.BrokerURL,
			"group", cfg.GroupID,
			"edgeNode", cfg.EdgeNode,
			"device", cfg.DeviceID,
			"useTemplates", cfg.UseTemplates)
	}

	// Watch enabled state
	enabledSub, err := b.KVWatch(topics.BucketServiceEnabled, m.moduleID, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			m.bridge.SetEnabled(true)
			return
		}
		var kv types.ServiceEnabledKV
		if err := json.Unmarshal(value, &kv); err != nil {
			return
		}
		m.bridge.SetEnabled(kv.Enabled)
	})
	if err == nil {
		m.subs = append(m.subs, enabledSub)
	}

	// Watch for config changes
	configSub, err := b.KVWatch(topics.BucketTentacleConfig, "mqtt.config", func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			return
		}
		slog.Info("mqtt: config changed, restarting bridge")
		m.bridge.Stop()

		var newCfg = loadConfig(b)
		m.bridge = NewBridge(b, m.moduleID, newCfg)
		if err := m.bridge.Start(); err != nil {
			slog.Error("mqtt: failed to restart bridge", "error", err)
		}
	})
	if err == nil {
		m.subs = append(m.subs, configSub)
	}

	// Listen for shutdown
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		slog.Info("mqtt: received shutdown command")
		m.Stop()
		os.Exit(0)
	})
	m.subs = append(m.subs, shutdownSub)

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
	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil

	if m.bridge != nil {
		m.bridge.Stop()
	}
	if m.stopHeartbeat != nil {
		m.stopHeartbeat()
	}
	return nil
}
