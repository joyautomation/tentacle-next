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
	"sync"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const serviceType = "mqtt"

// Module implements the module.Module interface for the MQTT bridge.
type Module struct {
	moduleID      string
	b             bus.Bus
	log           *slog.Logger
	bridge        *Bridge
	startedAt     time.Time
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
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	// Ensure required KV buckets exist
	for _, bucket := range []string{topics.BucketTentacleConfig, topics.BucketServiceEnabled, topics.BucketHeartbeats} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("mqtt: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Load configuration
	cfg := loadConfig(b)

	// Save config to KV for persistence
	saveConfig(b, &cfg)

	// Register config schema for the settings UI
	if schemaSub, err := config.RegisterSchema(b, serviceType, configSchema); err == nil {
		m.subs = append(m.subs, schemaSub)
	}

	m.startedAt = time.Now()

	// Start heartbeat
	m.stopHeartbeat = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		meta := map[string]interface{}{
			"brokerUrl":       cfg.BrokerURL,
			"clientId":        cfg.ClientID,
			"brokerReachable": false,
			"connected":       false,
		}
		if m.bridge != nil {
			m.bridge.mu.RLock()
			meta["brokerUrl"] = m.bridge.config.BrokerURL
			meta["clientId"] = m.bridge.config.ClientID
			m.bridge.mu.RUnlock()
			if m.bridge.node != nil {
				meta["brokerReachable"] = m.bridge.node.IsBrokerReachable()
				meta["connected"] = m.bridge.node.State() == StateBorn
			}
			if m.bridge.sf != nil {
				meta["storeForward"] = m.bridge.sf.State() != SFOnline
			}
		}
		return meta
	})

	// Subscribe to status browse for module status variables.
	statusVars := []module.StatusVar{
		{Name: "uptime", Datatype: "number"},
		{Name: "connected", Datatype: "boolean"},
		{Name: "publishRate", Datatype: "number"},
		{Name: "storeForward", Datatype: "boolean"},
	}
	statusBrowseSub, _ := b.Subscribe(topics.StatusBrowse(serviceType), func(_ string, _ []byte, reply bus.ReplyFunc) {
		module.HandleStatusBrowse(statusVars, reply)
	})
	m.subs = append(m.subs, statusBrowseSub)

	// Publish status data every 10s.
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

	// Create and start the bridge. Start() sets up bus subscriptions and
	// data handling regardless of broker connectivity — it only returns
	// an error for non-recoverable failures.
	m.bridge = NewBridge(b, m.moduleID, cfg, m.log)
	if err := m.bridge.Start(); err != nil {
		m.log.Error("mqtt: failed to start bridge", "error", err)
	} else {
		m.log.Info("mqtt: bridge started",
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

	// Watch for config changes (any mqtt.* key in tentacle_config).
	// Debounce: the web UI writes multiple keys at once, so wait 1s of quiet before restarting.
	var configDebounce *time.Timer
	var configMu sync.Mutex
	configSub, err := b.KVWatchAll(topics.BucketTentacleConfig, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			return
		}
		if len(key) < 5 || key[:5] != "mqtt." {
			return
		}
		configMu.Lock()
		defer configMu.Unlock()
		if configDebounce != nil {
			configDebounce.Stop()
		}
		configDebounce = time.AfterFunc(1*time.Second, func() {
			newCfg := loadConfig(b)
			// Skip the restart on no-op writes (initial KV replay,
			// re-saving identical values). Without this guard the
			// watcher fires on startup as soon as it replays the
			// keys we just wrote in saveConfig — which would force
			// a Sparkplug NDEATH/NBIRTH cycle and a bdSeq increment
			// inside every test the TCK runs.
			m.bridge.mu.RLock()
			currentCfg := m.bridge.config
			m.bridge.mu.RUnlock()
			if newCfg == currentCfg {
				return
			}
			m.log.Info("mqtt: config changed, restarting bridge")
			m.bridge.Stop()

			m.bridge = NewBridge(b, m.moduleID, newCfg, m.log)
			if err := m.bridge.Start(); err != nil {
				m.log.Error("mqtt: failed to restart bridge", "error", err)
			}
		})
	})
	if err == nil {
		m.subs = append(m.subs, configSub)
	}

	// Listen for shutdown
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("mqtt: received shutdown command")
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

// publishStatus publishes MQTT bridge status variables via the bus.
func (m *Module) publishStatus() {
	uptimeSeconds := int64(time.Since(m.startedAt).Seconds())

	connected := false
	storeForward := false
	publishRate := float64(0)

	if m.bridge != nil {
		if m.bridge.node != nil {
			connected = m.bridge.node.State() == StateBorn
		}
		if m.bridge.sf != nil {
			storeForward = m.bridge.sf.State() != SFOnline
			publishRate = m.bridge.sf.PublishRate()
		}
	}

	module.PublishStatus(m.b, serviceType, map[string]module.StatusValue{
		"uptime":       {Value: uptimeSeconds, Datatype: "number"},
		"connected":    {Value: connected, Datatype: "boolean"},
		"publishRate":  {Value: publishRate, Datatype: "number"},
		"storeForward": {Value: storeForward, Datatype: "boolean"},
	})
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
