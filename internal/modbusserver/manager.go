//go:build modbusserver || all

package modbusserver

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

// ServerManager manages virtual Modbus devices created via subscribe requests.
type ServerManager struct {
	b        bus.Bus
	moduleID string
	log      *slog.Logger

	mu      sync.Mutex
	devices map[string]*VirtualDevice // deviceId -> VirtualDevice
	busSubs []bus.Subscription        // request handler subscriptions
}

// NewServerManager creates a new server manager.
func NewServerManager(b bus.Bus, moduleID string, log *slog.Logger) *ServerManager {
	return &ServerManager{
		b:        b,
		moduleID: moduleID,
		log:      log,
		devices:  make(map[string]*VirtualDevice),
	}
}

// Start registers bus request handlers for subscribe/unsubscribe/variables.
func (m *ServerManager) Start() {
	m.registerHandler(topics.ModbusServerSubscribe, m.handleSubscribe)
	m.registerHandler(topics.ModbusServerUnsubscribe, m.handleUnsubscribe)
	m.registerHandler(topics.ModbusServerVariables, m.handleVariables)

	m.log.Info("modbusserver: manager started, listening for requests")
}

// Stop cleans up all virtual devices and bus subscriptions.
func (m *ServerManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id := range m.devices {
		m.stopDeviceLocked(id)
	}

	for _, sub := range m.busSubs {
		_ = sub.Unsubscribe()
	}
	m.busSubs = nil

	m.log.Info("modbusserver: manager stopped")
}

// DeviceCount returns the number of active virtual devices.
func (m *ServerManager) DeviceCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.devices)
}

// registerHandler subscribes to a bus subject and tracks the subscription.
func (m *ServerManager) registerHandler(subject string, handler bus.MessageHandler) {
	sub, err := m.b.Subscribe(subject, handler)
	if err != nil {
		m.log.Error("modbusserver: failed to subscribe", "subject", subject, "error", err)
		return
	}
	m.busSubs = append(m.busSubs, sub)
}

// handleSubscribe processes modbus-server.subscribe requests.
func (m *ServerManager) handleSubscribe(_ string, data []byte, reply bus.ReplyFunc) {
	var req itypes.ModbusServerSubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		m.sendReply(reply, itypes.ModbusServerSubscribeResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if req.DeviceID == "" {
		m.sendReply(reply, itypes.ModbusServerSubscribeResponse{
			Success: false,
			Error:   "deviceId is required",
		})
		return
	}

	port := req.Port
	if port == 0 {
		port = 5020
	}
	unitID := req.UnitID
	if unitID == 0 {
		unitID = 1
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// If device already exists, tear it down first
	if _, exists := m.devices[req.DeviceID]; exists {
		m.log.Info("modbusserver: replacing existing device", "deviceId", req.DeviceID)
		m.stopDeviceLocked(req.DeviceID)
	}

	// Create register store and register all tags
	store := NewRegisterStore()
	for _, tag := range req.Tags {
		store.RegisterTag(tag)
	}

	// Write callback: when a Modbus client writes, decode and publish to bus
	sourceModuleID := req.SourceModuleID
	onWrite := func(fc string, address int) {
		result := store.ReadTypedValue(fc, address)
		if result == nil {
			return
		}
		if !result.Writable {
			m.log.Warn("modbusserver: write rejected, tag is read-only",
				"variableId", result.VariableID, "address", address)
			return
		}

		// Publish command to the source module
		cmdSubject := topics.Command(sourceModuleID, types.SanitizeForSubject(result.VariableID))
		cmdMsg := types.PlcDataMessage{
			ModuleID:   m.moduleID,
			DeviceID:   req.DeviceID,
			VariableID: result.VariableID,
			Value:      result.Value,
			Timestamp:  time.Now().UnixMilli(),
			Datatype:   modbusToNatsDatatype(fc),
		}
		payload, err := json.Marshal(cmdMsg)
		if err != nil {
			m.log.Warn("modbusserver: failed to marshal write command", "error", err)
			return
		}
		if err := m.b.Publish(cmdSubject, payload); err != nil {
			m.log.Warn("modbusserver: failed to publish write command",
				"subject", cmdSubject, "error", err)
		} else {
			m.log.Debug("modbusserver: Modbus write -> bus",
				"variableId", result.VariableID, "value", result.Value, "subject", cmdSubject)
		}
	}

	// Start TCP server
	server := NewTCPServer(port, unitID, store, onWrite, m.log)
	if err := server.Start(); err != nil {
		m.log.Error("modbusserver: failed to start TCP server",
			"deviceId", req.DeviceID, "port", port, "error", err)
		m.sendReply(reply, itypes.ModbusServerSubscribeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to start TCP server: %v", err),
		})
		return
	}

	// Subscribe to source module data topics: plc.data.{sourceModuleId}.*
	dataSubject := fmt.Sprintf("plc.data.%s.*", types.SanitizeForSubject(sourceModuleID))
	dataSub, err := m.b.Subscribe(dataSubject, func(_ string, msgData []byte, _ bus.ReplyFunc) {
		var msg types.PlcDataMessage
		if err := json.Unmarshal(msgData, &msg); err != nil {
			return
		}
		if msg.VariableID != "" && msg.Value != nil {
			store.UpdateFromVariable(msg.VariableID, msg.Value)
		}
	})
	if err != nil {
		m.log.Warn("modbusserver: failed to subscribe to data topic",
			"subject", dataSubject, "error", err)
	}

	device := &VirtualDevice{
		DeviceID:       req.DeviceID,
		Store:          store,
		Server:         server,
		SourceModuleID: sourceModuleID,
		DataSub:        dataSub,
	}
	m.devices[req.DeviceID] = device

	m.log.Info("modbusserver: device active",
		"deviceId", req.DeviceID, "port", port, "unitId", unitID,
		"tags", len(req.Tags), "source", sourceModuleID)

	m.sendReply(reply, itypes.ModbusServerSubscribeResponse{
		Success: true,
		Port:    port,
	})
}

// handleUnsubscribe processes modbus-server.unsubscribe requests.
func (m *ServerManager) handleUnsubscribe(_ string, data []byte, reply bus.ReplyFunc) {
	var req struct {
		DeviceID string `json:"deviceId"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		m.sendReply(reply, map[string]interface{}{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.devices[req.DeviceID]; !ok {
		m.sendReply(reply, map[string]interface{}{"error": "device not found"})
		return
	}

	m.stopDeviceLocked(req.DeviceID)
	m.sendReply(reply, map[string]interface{}{"success": true})
}

// handleVariables returns current state of all virtual devices.
func (m *ServerManager) handleVariables(_ string, _ []byte, reply bus.ReplyFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]interface{})
	for id, dev := range m.devices {
		result[id] = map[string]interface{}{
			"deviceId":       dev.DeviceID,
			"port":           dev.Server.Port(),
			"sourceModuleId": dev.SourceModuleID,
		}
	}
	m.sendReply(reply, result)
}

// stopDeviceLocked tears down a single virtual device. Caller must hold m.mu.
func (m *ServerManager) stopDeviceLocked(deviceID string) {
	dev, ok := m.devices[deviceID]
	if !ok {
		return
	}

	if dev.DataSub != nil {
		_ = dev.DataSub.Unsubscribe()
	}
	dev.Server.Stop()
	delete(m.devices, deviceID)

	m.log.Info("modbusserver: device stopped", "deviceId", deviceID)
}

// sendReply marshals a payload and sends it via the reply function.
func (m *ServerManager) sendReply(reply bus.ReplyFunc, payload interface{}) {
	if reply == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		m.log.Warn("modbusserver: failed to marshal reply", "error", err)
		return
	}
	if err := reply(data); err != nil {
		m.log.Warn("modbusserver: failed to send reply", "error", err)
	}
}

// modbusToNatsDatatype maps a Modbus function code category to a NATS datatype.
func modbusToNatsDatatype(fc string) string {
	switch fc {
	case "coil", "discrete":
		return "boolean"
	default:
		return "number"
	}
}
