// Package plc provides a library for building PLC programs that integrate
// with the tentacle ecosystem. Users import this package, define variables
// and tasks, and compile their own binary.
//
// Example:
//
//	plc, err := plc.Create(plc.Config{
//	    ProjectID: "my-plc",
//	    Variables: map[string]plc.VariableConfig{
//	        "temperature": {Datatype: plc.Number, Default: 20.0},
//	    },
//	    Tasks: map[string]plc.TaskConfig{
//	        "main": {Name: "Main", ScanRate: 100 * time.Millisecond, Program: myProgram},
//	    },
//	    NatsURL: "nats://localhost:4222",
//	})
package plc

import (
	"sync"

	ttypes "github.com/joyautomation/tentacle/types"
)

// Datatype constants.
const (
	Number  = "number"
	Boolean = "boolean"
	String  = "string"
	Udt     = "udt"
)

// VariableConfig defines a PLC variable.
type VariableConfig struct {
	Description     string
	Datatype        string // Number, Boolean, String, or Udt
	Default         interface{}
	Source          *Source
	Deadband        *ttypes.DeadBandConfig
	DisableRBE      bool
	UdtTemplate     *ttypes.UdtTemplateDefinition
	MemberSources   map[string]*Source
	MemberDeadbands map[string]ttypes.DeadBandConfig
}

// Source defines where a variable gets its value from an external system.
type Source struct {
	// Subject overrides the default NATS subject derived from the protocol source.
	Subject string
	// Bidirectional allows external writes back through the source.
	Bidirectional bool
	// OnResponse transforms incoming values before updating the variable.
	OnResponse func(interface{}) interface{}
	// OnSend transforms outgoing values before publishing.
	OnSend func(interface{}) interface{}

	// Protocol-specific source (use at most one).
	EthernetIP *EthernetIPSource
	OpcUA      *OpcUASource
	Modbus     *ModbusSource
	SNMP       *SNMPSource
}

// EthernetIPSource configures an EtherNet/IP tag data source.
type EthernetIPSource struct {
	DeviceID string
	Host     string
	Port     int    // 0 = default 44818
	Tag      string
	CipType  string // "REAL", "DINT", "BOOL", "STRING", etc.
	ScanRate int    // ms, 0 = device default
}

// OpcUASource configures an OPC UA node data source.
type OpcUASource struct {
	DeviceID    string
	EndpointURL string
	NodeID      string
	ScanRate    int // ms
}

// ModbusSource configures a Modbus register data source.
type ModbusSource struct {
	DeviceID       string
	Host           string
	Port           int
	UnitID         int
	Tag            string
	Address        int
	FunctionCode   int    // 1–4
	ModbusDatatype string // "boolean","int16","uint16","int32","uint32","float32","float64"
	ByteOrder      string // "ABCD","BADC","CDAB","DCBA"
	ScanRate       int    // ms
}

// SNMPSource configures an SNMP OID data source.
type SNMPSource struct {
	DeviceID  string
	Host      string
	Port      int
	Version   string // "1", "2c", "3"
	Community string
	V3Auth    *V3Auth
	OID       string
	ScanRate  int // ms
}

// V3Auth holds SNMPv3 authentication credentials.
type V3Auth struct {
	Username      string
	SecurityLevel string // "noAuthNoPriv", "authNoPriv", "authPriv"
	AuthProtocol  string
	AuthPassword  string
	PrivProtocol  string
	PrivPassword  string
}

// UpdateFunc is the callback for updating a variable value from a task program.
type UpdateFunc func(variableID string, value interface{})

// ProgramFunc is the signature for task programs.
type ProgramFunc func(vars *Variables, update UpdateFunc)

// ─── Runtime types ──────────────────────────────────────────────────────────

// Variable holds the runtime state of a single PLC variable.
type Variable struct {
	mu       sync.RWMutex
	value    interface{}
	datatype string
}

// Value returns the current value.
func (v *Variable) Value() interface{} {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.value
}

// NumberValue returns the value as float64 (0 if not numeric).
func (v *Variable) NumberValue() float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	switch n := v.value.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// BoolValue returns the value as bool.
func (v *Variable) BoolValue() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	b, _ := v.value.(bool)
	return b
}

// StringValue returns the value as string.
func (v *Variable) StringValue() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	s, _ := v.value.(string)
	return s
}

// UdtValue returns the value as a map (nil if not a UDT).
func (v *Variable) UdtValue() map[string]interface{} {
	v.mu.RLock()
	defer v.mu.RUnlock()
	m, _ := v.value.(map[string]interface{})
	return m
}

// Datatype returns the declared datatype.
func (v *Variable) Datatype() string { return v.datatype }

func (v *Variable) set(val interface{}) {
	v.mu.Lock()
	v.value = val
	v.mu.Unlock()
}

// Variables provides read access to all runtime PLC variables.
type Variables struct {
	vars map[string]*Variable
}

// Get returns a variable by ID, or nil if not found.
func (vs *Variables) Get(id string) *Variable { return vs.vars[id] }

// GetNumber returns the numeric value (0 if missing).
func (vs *Variables) GetNumber(id string) float64 {
	if v := vs.vars[id]; v != nil {
		return v.NumberValue()
	}
	return 0
}

// GetBool returns the boolean value (false if missing).
func (vs *Variables) GetBool(id string) bool {
	if v := vs.vars[id]; v != nil {
		return v.BoolValue()
	}
	return false
}

// GetString returns the string value ("" if missing).
func (vs *Variables) GetString(id string) string {
	if v := vs.vars[id]; v != nil {
		return v.StringValue()
	}
	return ""
}

// GetUdt returns the UDT map value (nil if missing).
func (vs *Variables) GetUdt(id string) map[string]interface{} {
	if v := vs.vars[id]; v != nil {
		return v.UdtValue()
	}
	return nil
}

// All returns every variable keyed by ID.
func (vs *Variables) All() map[string]*Variable { return vs.vars }
