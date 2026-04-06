//go:build snmp || all

package snmp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
)

// TrapListener listens for incoming SNMP traps and publishes them to the Bus.
type TrapListener struct {
	b        bus.Bus
	moduleID string
	port     int
	listener *gosnmp.TrapListener
	mu       sync.RWMutex
	running  bool
}

// NewTrapListener creates a new trap listener.
func NewTrapListener(b bus.Bus, moduleID string, port int) *TrapListener {
	return &TrapListener{
		b:        b,
		moduleID: moduleID,
		port:     port,
	}
}

// Start begins listening for SNMP traps on the configured port.
func (t *TrapListener) Start() error {
	t.listener = gosnmp.NewTrapListener()
	t.listener.OnNewTrap = t.handleTrap
	t.listener.Params = gosnmp.Default
	t.listener.Params.Logger = gosnmp.NewLogger(&trapLogAdapter{})

	addr := fmt.Sprintf("0.0.0.0:%d", t.port)
	slog.Info("snmp: starting trap listener", "addr", addr)

	t.mu.Lock()
	t.running = true
	t.mu.Unlock()

	err := t.listener.Listen(addr)
	if err != nil {
		t.mu.Lock()
		t.running = false
		t.mu.Unlock()
		slog.Error("snmp: trap listener error", "error", err)
		return err
	}
	return nil
}

// Stop shuts down the trap listener.
func (t *TrapListener) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running && t.listener != nil {
		t.listener.Close()
		t.running = false
		slog.Info("snmp: trap listener stopped")
	}
}

// handleTrap processes an incoming SNMP trap.
func (t *TrapListener) handleTrap(packet *gosnmp.SnmpPacket, addr *net.UDPAddr) {
	deviceID := addr.IP.String()
	slog.Info("snmp: received trap", "device", deviceID, "version", versionToString(packet.Version))

	// Extract trap OID (SNMPv2-MIB::snmpTrapOID.0 = .1.3.6.1.6.3.1.1.4.1.0)
	trapOID := ""
	var variables []TrapVariable

	for _, pdu := range packet.Variables {
		// Check if this is the snmpTrapOID
		if pdu.Name == ".1.3.6.1.6.3.1.1.4.1.0" {
			if val, ok := pdu.Value.(string); ok {
				trapOID = val
			}
			continue
		}

		snmpType := pduTypeToString(pdu.Type)
		value := pduToValue(pdu)

		variables = append(variables, TrapVariable{
			OID:      pdu.Name,
			Value:    value,
			SnmpType: snmpType,
		})
	}

	// For v1 traps, construct the trap OID from enterprise + specific-trap
	if packet.Version == gosnmp.Version1 && trapOID == "" {
		trapOID = packet.Enterprise
		if packet.GenericTrap == 6 { // enterprise-specific
			trapOID = fmt.Sprintf("%s.0.%d", packet.Enterprise, packet.SpecificTrap)
		}
	}

	trapMsg := TrapMessage{
		ModuleID:  t.moduleID,
		DeviceID:  deviceID,
		TrapOID:   trapOID,
		Variables: variables,
		Timestamp: time.Now().UnixMilli(),
		Version:   versionToString(packet.Version),
		Community: packet.Community,
	}

	data, err := json.Marshal(trapMsg)
	if err != nil {
		slog.Error("snmp: failed to marshal trap message", "error", err)
		return
	}

	subject := topics.SnmpTrap(sanitizeOidForSubject(deviceID))
	if err := t.b.Publish(subject, data); err != nil {
		slog.Error("snmp: failed to publish trap", "error", err)
		return
	}

	slog.Info("snmp: published trap", "device", deviceID, "trapOid", trapOID, "varbinds", len(variables))
}

// trapLogAdapter suppresses gosnmp's internal logging.
type trapLogAdapter struct{}

func (t *trapLogAdapter) Print(v ...interface{})                 {}
func (t *trapLogAdapter) Printf(format string, v ...interface{}) {}
