//go:build profinet || profinetcontroller || all

package profinet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

// Manager coordinates bus subscriptions, tag value tracking, I/O buffer
// management, and the PROFINET device lifecycle.
type Manager struct {
	b        bus.Bus
	moduleID string
	log      *slog.Logger
	ctx      context.Context

	config *ProfinetConfig
	status ProfinetStatus
	device *Device

	// Current tag values for input (device -> controller) packing
	tagValues map[string]interface{}

	// Bus subscriptions
	dataSubs map[string]bus.Subscription // source subject -> subscription
	busSubs  []bus.Subscription

	mu sync.Mutex
}

// NewManager creates a new PROFINET manager.
func NewManager(b bus.Bus, moduleID string, log *slog.Logger) *Manager {
	return &Manager{
		b:         b,
		moduleID:  moduleID,
		log:       log,
		tagValues: make(map[string]interface{}),
		dataSubs:  make(map[string]bus.Subscription),
	}
}

// Start registers bus request handlers.
func (m *Manager) Start(ctx context.Context) {
	m.ctx = ctx
	m.registerHandler(topics.ProfinetConfigure, m.handleConfigure)
	m.registerHandler(topics.ProfinetVariables, m.handleVariables)
	m.registerHandler(topics.ProfinetStatus, m.handleStatus)
	m.registerHandler(topics.ProfinetGsdml, m.handleGsdml)

	// Watch for config changes in KV
	if err := m.b.KVCreate(topics.BucketProfinetConfig, topics.BucketConfigs()[topics.BucketProfinetConfig]); err != nil {
		m.log.Warn("profinet: failed to create config bucket", "error", err)
	}

	kvSub, err := m.b.KVWatch(topics.BucketProfinetConfig, m.moduleID, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			m.log.Info("profinet: config deleted, stopping device")
			m.unconfigure()
			return
		}
		var cfg ProfinetConfig
		if json.Unmarshal(value, &cfg) == nil {
			m.applyConfig(&cfg)
		}
	})
	if err != nil {
		m.log.Warn("profinet: failed to watch config KV", "error", err)
	} else {
		m.busSubs = append(m.busSubs, kvSub)
	}

	// Try loading existing config from KV
	if data, _, err := m.b.KVGet(topics.BucketProfinetConfig, m.moduleID); err == nil {
		var cfg ProfinetConfig
		if json.Unmarshal(data, &cfg) == nil {
			m.applyConfig(&cfg)
		}
	}

	m.log.Info("profinet: manager started", "moduleId", m.moduleID)
}

// Stop cleans up all subscriptions and stops the device.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.device != nil {
		m.device.Stop()
		m.device = nil
	}

	for subj, sub := range m.dataSubs {
		if err := sub.Unsubscribe(); err != nil {
			m.log.Warn("profinet: failed to unsubscribe", "subject", subj, "error", err)
		}
	}
	m.dataSubs = make(map[string]bus.Subscription)

	for _, sub := range m.busSubs {
		_ = sub.Unsubscribe()
	}
	m.busSubs = nil

	m.log.Info("profinet: manager stopped")
}

func (m *Manager) registerHandler(subject string, handler bus.MessageHandler) {
	sub, err := m.b.Subscribe(subject, handler)
	if err != nil {
		m.log.Error("profinet: failed to subscribe", "subject", subject, "error", err)
		return
	}
	m.busSubs = append(m.busSubs, sub)
}

// handleConfigure receives a ProfinetConfig, validates it, stores to KV, and applies it.
func (m *Manager) handleConfigure(_ string, data []byte, reply bus.ReplyFunc) {
	var cfg ProfinetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		m.sendReply(reply, map[string]interface{}{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	if err := cfg.Validate(); err != nil {
		m.sendReply(reply, map[string]interface{}{"error": fmt.Sprintf("validation failed: %v", err)})
		return
	}

	// Persist to KV
	cfgData, _ := json.Marshal(cfg)
	if _, err := m.b.KVPut(topics.BucketProfinetConfig, m.moduleID, cfgData); err != nil {
		m.sendReply(reply, map[string]interface{}{"error": fmt.Sprintf("failed to persist config: %v", err)})
		return
	}

	m.applyConfig(&cfg)

	m.sendReply(reply, map[string]interface{}{
		"success":     true,
		"stationName": cfg.StationName,
		"slots":       len(cfg.Slots),
	})
}

// handleVariables returns current tag state.
func (m *Manager) handleVariables(_ string, _ []byte, reply bus.ReplyFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]interface{}, len(m.tagValues))
	for k, v := range m.tagValues {
		result[k] = v
	}
	m.sendReply(reply, result)
}

// handleStatus returns the current PROFINET device status.
func (m *Manager) handleStatus(_ string, _ []byte, reply bus.ReplyFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendReply(reply, m.status)
}

// handleGsdml generates and returns the GSDML XML for the current config.
func (m *Manager) handleGsdml(_ string, _ []byte, reply bus.ReplyFunc) {
	m.mu.Lock()
	cfg := m.config
	m.mu.Unlock()

	if cfg == nil {
		m.sendReply(reply, map[string]interface{}{"error": "no configuration loaded"})
		return
	}

	data, err := GenerateGSDML(cfg)
	if err != nil {
		m.sendReply(reply, map[string]interface{}{"error": fmt.Sprintf("GSDML generation failed: %v", err)})
		return
	}

	m.sendReply(reply, map[string]interface{}{
		"filename": GSDMLFilename(cfg),
		"xml":      string(data),
	})
}

// applyConfig applies a new configuration, updating subscriptions, status, and the device.
func (m *Manager) applyConfig(cfg *ProfinetConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing device if running
	if m.device != nil {
		m.device.Stop()
		m.device = nil
	}

	// Tear down existing data subscriptions
	for subj, sub := range m.dataSubs {
		_ = sub.Unsubscribe()
		delete(m.dataSubs, subj)
	}
	m.tagValues = make(map[string]interface{})

	m.config = cfg

	// Subscribe to source subjects for all input tags
	inputSlots := 0
	outputSlots := 0
	for _, slot := range cfg.Slots {
		for _, sub := range slot.Subslots {
			if sub.Direction == DirectionInput || sub.Direction == DirectionInputOutput {
				inputSlots++
			}
			if sub.Direction == DirectionOutput || sub.Direction == DirectionInputOutput {
				outputSlots++
			}
			for _, tag := range sub.Tags {
				if tag.Source != "" {
					m.subscribeToSource(tag.TagID, tag.Source)
				}
			}
		}
	}

	// Subscribe to command subjects for output tag writebacks
	cmdSub, err := m.b.Subscribe(topics.CommandWildcard(m.moduleID), m.handleCommand)
	if err == nil {
		m.dataSubs[topics.CommandWildcard(m.moduleID)] = cmdSub
	}

	m.status = ProfinetStatus{
		Connected:     false,
		StationName:   cfg.StationName,
		InterfaceName: cfg.InterfaceName,
		InputSlots:    inputSlots,
		OutputSlots:   outputSlots,
	}

	// Start the PROFINET device with all protocol layers
	device := NewDevice(cfg, DeviceCallbacks{
		OnIPSet: func(ip, mask, gateway net.IP) {
			m.log.Info("profinet: controller assigned IP via DCP", "ip", ip, "mask", mask, "gateway", gateway)
		},
		OnNameSet: func(name string) {
			m.log.Info("profinet: controller assigned station name via DCP", "name", name)
		},
		OnConnected: func() {
			m.mu.Lock()
			m.status.Connected = true
			m.mu.Unlock()
			m.log.Info("profinet: connected to controller")
		},
		OnDisconnected: func() {
			m.mu.Lock()
			m.status.Connected = false
			m.mu.Unlock()
			m.log.Info("profinet: disconnected from controller")
		},
		GetInputData: func(sub *SubslotConfig) []byte {
			return m.GetInputBuffer(sub)
		},
		OnOutputData: func(sub *SubslotConfig, data []byte) {
			m.ProcessOutputBuffer(sub, data)
		},
	}, m.log)
	m.device = device

	go func() {
		if err := device.Start(m.ctx); err != nil && m.ctx.Err() == nil {
			m.log.Error("profinet: device stopped with error", "error", err)
		}
	}()

	m.log.Info("profinet: configuration applied",
		"stationName", cfg.StationName,
		"interface", cfg.InterfaceName,
		"inputSlots", inputSlots,
		"outputSlots", outputSlots,
	)
}

// unconfigure removes the current configuration and unsubscribes from all data sources.
func (m *Manager) unconfigure() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for subj, sub := range m.dataSubs {
		_ = sub.Unsubscribe()
		delete(m.dataSubs, subj)
	}
	m.config = nil
	m.tagValues = make(map[string]interface{})
	m.status = ProfinetStatus{}
}

// subscribeToSource subscribes to a bus data subject and updates the tag value cache.
func (m *Manager) subscribeToSource(tagID, source string) {
	if _, ok := m.dataSubs[source]; ok {
		return
	}

	sub, err := m.b.Subscribe(source, func(_ string, data []byte, _ bus.ReplyFunc) {
		var msg types.PlcDataMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}
		m.mu.Lock()
		m.tagValues[tagID] = msg.Value
		m.mu.Unlock()
	})
	if err != nil {
		m.log.Warn("profinet: failed to subscribe to source", "source", source, "tag", tagID, "error", err)
		return
	}
	m.dataSubs[source] = sub
}

// handleCommand processes write commands directed at this module's output tags.
func (m *Manager) handleCommand(subject string, data []byte, _ bus.ReplyFunc) {
	// Extract variable ID from subject: profinet.command.{variableId}
	parts := strings.SplitN(subject, ".", 3)
	if len(parts) < 3 {
		return
	}
	variableID := parts[2]

	var msg types.PlcDataMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	m.mu.Lock()
	m.tagValues[variableID] = msg.Value
	m.mu.Unlock()

	m.log.Debug("profinet: received command", "variable", variableID, "value", msg.Value)
}

// GetInputBuffer returns the packed input data for a specific subslot.
// Called by the cyclic I/O loop (future p-net integration).
func (m *Manager) GetInputBuffer(sub *SubslotConfig) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return PackInputBuffer(sub, m.tagValues)
}

// ProcessOutputBuffer unpacks output data from the controller and publishes
// changed values to the bus. Called by the cyclic I/O loop (future p-net integration).
func (m *Manager) ProcessOutputBuffer(sub *SubslotConfig, data []byte) {
	values := UnpackOutputBuffer(sub, data)

	m.mu.Lock()
	for tagID, val := range values {
		prev, exists := m.tagValues[tagID]
		m.tagValues[tagID] = val

		// Publish only on change
		if !exists || prev != val {
			msg := types.PlcDataMessage{
				ModuleID:   m.moduleID,
				DeviceID:   m.moduleID,
				VariableID: tagID,
				Value:      val,
				Timestamp:  time.Now().UnixMilli(),
			}
			msgData, _ := json.Marshal(msg)
			m.mu.Unlock()
			_ = m.b.Publish(topics.Data(m.moduleID, m.moduleID, tagID), msgData)
			m.mu.Lock()
		}
	}
	m.mu.Unlock()
}

func (m *Manager) sendReply(reply bus.ReplyFunc, payload interface{}) {
	if reply == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		m.log.Warn("profinet: failed to marshal reply", "error", err)
		return
	}
	if err := reply(data); err != nil {
		m.log.Warn("profinet: failed to send reply", "error", err)
	}
}
