package sources

import (
	"strings"

	"github.com/joyautomation/tentacle/plc"
	ttypes "github.com/joyautomation/tentacle/types"
)

// ModbusDevice describes a Modbus TCP target.
type ModbusDevice struct {
	DeviceID string
	Host     string
	Port     int
	UnitID   int
	ScanRate int // ms
}

// ModbusTagDef describes a single Modbus register to poll.
type ModbusTagDef struct {
	Tag            string // human-readable name
	Address        int    // register address
	FunctionCode   int    // 1-4
	ModbusDatatype string // "boolean","int16","uint16","int32","uint32","float32","float64"
	ByteOrder      string // "ABCD","BADC","CDAB","DCBA"
}

// ── Functional options ──────────────────────────────────────────────────────

type modbusOpts struct {
	description   string
	deadband      *ttypes.DeadBandConfig
	disableRBE    bool
	bidirectional bool
}

// ModbusOption configures optional fields when creating a Modbus variable.
type ModbusOption func(*modbusOpts)

// WithModbusDescription sets the variable description.
func WithModbusDescription(desc string) ModbusOption {
	return func(o *modbusOpts) { o.description = desc }
}

// WithModbusDeadband sets a per-variable deadband.
func WithModbusDeadband(db ttypes.DeadBandConfig) ModbusOption {
	return func(o *modbusOpts) { o.deadband = &db }
}

// WithModbusDisableRBE disables report-by-exception for the variable.
func WithModbusDisableRBE() ModbusOption {
	return func(o *modbusOpts) { o.disableRBE = true }
}

// WithModbusBidirectional marks the source as bidirectional (allows writes).
func WithModbusBidirectional() ModbusOption {
	return func(o *modbusOpts) { o.bidirectional = true }
}

func applyModbusOpts(opts []ModbusOption) modbusOpts {
	var o modbusOpts
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// modbusDataypeToDatatype maps a Modbus data type to a plc datatype constant.
func modbusDatatypeToDatatype(mdt string) string {
	if strings.ToLower(mdt) == "boolean" {
		return plc.Boolean
	}
	return plc.Number
}

// ── Public API ──────────────────────────────────────────────────────────────

// ModbusTag creates a plc.Source for a Modbus register.
func ModbusTag(device ModbusDevice, def ModbusTagDef) *plc.Source {
	return &plc.Source{
		Modbus: &plc.ModbusSource{
			DeviceID:       device.DeviceID,
			Host:           device.Host,
			Port:           device.Port,
			UnitID:         device.UnitID,
			Tag:            def.Tag,
			Address:        def.Address,
			FunctionCode:   def.FunctionCode,
			ModbusDatatype: def.ModbusDatatype,
			ByteOrder:      def.ByteOrder,
			ScanRate:       device.ScanRate,
		},
	}
}

// ModbusVar creates a plc.VariableConfig for a single Modbus register.
// The datatype is inferred from the ModbusDatatype: "boolean" maps to Boolean,
// everything else maps to Number.
func ModbusVar(device ModbusDevice, def ModbusTagDef, opts ...ModbusOption) plc.VariableConfig {
	o := applyModbusOpts(opts)

	src := ModbusTag(device, def)
	src.Bidirectional = o.bidirectional

	vc := plc.VariableConfig{
		Description: o.description,
		Datatype:    modbusDatatypeToDatatype(def.ModbusDatatype),
		Source:      src,
		DisableRBE:  o.disableRBE,
	}

	if o.deadband != nil {
		vc.Deadband = o.deadband
	}

	return vc
}

// ModbusVars creates plc.VariableConfig entries for multiple Modbus registers.
// The map is keyed by the Tag field of each ModbusTagDef.
func ModbusVars(device ModbusDevice, defs []ModbusTagDef, opts ...ModbusOption) map[string]plc.VariableConfig {
	m := make(map[string]plc.VariableConfig, len(defs))
	for _, def := range defs {
		m[def.Tag] = ModbusVar(device, def, opts...)
	}
	return m
}
