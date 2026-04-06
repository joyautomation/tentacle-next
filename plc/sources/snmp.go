package sources

import (
	"github.com/joyautomation/tentacle/plc"
	ttypes "github.com/joyautomation/tentacle/types"
)

// SnmpDevice describes an SNMP agent target.
type SnmpDevice struct {
	DeviceID  string
	Host      string
	Port      int
	Version   string // "1", "2c", "3"
	Community string
	V3Auth    *plc.V3Auth
	ScanRate  int // ms
}

// ── Functional options ──────────────────────────────────────────────────────

type snmpOpts struct {
	description string
	deadband    *ttypes.DeadBandConfig
	disableRBE  bool
	datatype    string
}

// SnmpOption configures optional fields when creating an SNMP variable.
type SnmpOption func(*snmpOpts)

// WithSnmpDescription sets the variable description.
func WithSnmpDescription(desc string) SnmpOption {
	return func(o *snmpOpts) { o.description = desc }
}

// WithSnmpDeadband sets a per-variable deadband.
func WithSnmpDeadband(db ttypes.DeadBandConfig) SnmpOption {
	return func(o *snmpOpts) { o.deadband = &db }
}

// WithSnmpDisableRBE disables report-by-exception for the variable.
func WithSnmpDisableRBE() SnmpOption {
	return func(o *snmpOpts) { o.disableRBE = true }
}

// WithDatatype overrides the default String datatype for an SNMP variable.
func WithDatatype(dt string) SnmpOption {
	return func(o *snmpOpts) { o.datatype = dt }
}

func applySnmpOpts(opts []SnmpOption) snmpOpts {
	var o snmpOpts
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// ── Public API ──────────────────────────────────────────────────────────────

// SnmpTag creates a plc.Source for an SNMP OID.
func SnmpTag(device SnmpDevice, oid string) *plc.Source {
	return &plc.Source{
		SNMP: &plc.SNMPSource{
			DeviceID:  device.DeviceID,
			Host:      device.Host,
			Port:      device.Port,
			Version:   device.Version,
			Community: device.Community,
			V3Auth:    device.V3Auth,
			OID:       oid,
			ScanRate:  device.ScanRate,
		},
	}
}

// SnmpVar creates a plc.VariableConfig for a single SNMP OID.
// Defaults to String datatype; use WithDatatype to override.
func SnmpVar(device SnmpDevice, oid string, opts ...SnmpOption) plc.VariableConfig {
	o := applySnmpOpts(opts)

	datatype := plc.String
	if o.datatype != "" {
		datatype = o.datatype
	}

	src := SnmpTag(device, oid)

	vc := plc.VariableConfig{
		Description: o.description,
		Datatype:    datatype,
		Source:      src,
		DisableRBE:  o.disableRBE,
	}

	if o.deadband != nil {
		vc.Deadband = o.deadband
	}

	return vc
}

// SnmpVars creates plc.VariableConfig entries for multiple SNMP OIDs.
// The map is keyed by OID string.
func SnmpVars(device SnmpDevice, oids []string, opts ...SnmpOption) map[string]plc.VariableConfig {
	m := make(map[string]plc.VariableConfig, len(oids))
	for _, oid := range oids {
		m[oid] = SnmpVar(device, oid, opts...)
	}
	return m
}
