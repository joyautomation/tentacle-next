// Package sources provides helper functions that build plc.VariableConfig and
// plc.Source values for each supported protocol.  Users import this package to
// reduce the boilerplate of wiring devices to variables.
package sources

import (
	"strings"

	"github.com/joyautomation/tentacle/plc"
	ttypes "github.com/joyautomation/tentacle/types"
)

// EipDevice describes an EtherNet/IP target.
type EipDevice struct {
	DeviceID   string
	Host       string
	Port       int
	ScanRate   int                   // ms
	Deadband   *ttypes.DeadBandConfig // device-level default deadband
	DisableRBE bool
}

// ── Functional options ──────────────────────────────────────────────────────

type eipOpts struct {
	cipType       string
	description   string
	deadband      *ttypes.DeadBandConfig
	disableRBE    bool
	bidirectional bool
}

// EipOption configures optional fields when creating an EtherNet/IP variable.
type EipOption func(*eipOpts)

// WithCipType sets the CIP data type (e.g. "REAL", "DINT", "BOOL", "STRING").
func WithCipType(cipType string) EipOption {
	return func(o *eipOpts) { o.cipType = cipType }
}

// WithDescription sets the variable description.
func WithDescription(desc string) EipOption {
	return func(o *eipOpts) { o.description = desc }
}

// WithDeadband sets a per-variable deadband.
func WithDeadband(db ttypes.DeadBandConfig) EipOption {
	return func(o *eipOpts) { o.deadband = &db }
}

// WithDisableRBE disables report-by-exception for the variable.
func WithDisableRBE() EipOption {
	return func(o *eipOpts) { o.disableRBE = true }
}

// WithBidirectional marks the source as bidirectional (allows writes).
func WithBidirectional() EipOption {
	return func(o *eipOpts) { o.bidirectional = true }
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// cipTypeToDatatype maps a CIP type string to a plc datatype constant.
func cipTypeToDatatype(cipType string) string {
	switch strings.ToUpper(cipType) {
	case "BOOL":
		return plc.Boolean
	case "STRING":
		return plc.String
	default:
		return plc.Number
	}
}

func applyEipOpts(opts []EipOption) eipOpts {
	var o eipOpts
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// ── Public API ──────────────────────────────────────────────────────────────

// EipTag creates a plc.Source for an EtherNet/IP tag.
func EipTag(device EipDevice, tag string) *plc.Source {
	return &plc.Source{
		EthernetIP: &plc.EthernetIPSource{
			DeviceID: device.DeviceID,
			Host:     device.Host,
			Port:     device.Port,
			Tag:      tag,
			ScanRate: device.ScanRate,
		},
	}
}

// EipVar creates a plc.VariableConfig for a single EtherNet/IP tag.
// The datatype is inferred from the CIP type when provided via WithCipType;
// it defaults to Number otherwise.
func EipVar(device EipDevice, tag string, opts ...EipOption) plc.VariableConfig {
	o := applyEipOpts(opts)

	datatype := plc.Number
	if o.cipType != "" {
		datatype = cipTypeToDatatype(o.cipType)
	}

	src := EipTag(device, tag)
	src.EthernetIP.CipType = o.cipType
	src.Bidirectional = o.bidirectional

	vc := plc.VariableConfig{
		Description: o.description,
		Datatype:    datatype,
		Source:      src,
		DisableRBE:  o.disableRBE || device.DisableRBE,
	}

	// Per-variable deadband takes precedence over device-level.
	if o.deadband != nil {
		vc.Deadband = o.deadband
	} else if device.Deadband != nil {
		vc.Deadband = device.Deadband
	}

	return vc
}

// EipUdtVar creates a plc.VariableConfig for a UDT EtherNet/IP tag.
func EipUdtVar(device EipDevice, tag string, template ttypes.UdtTemplateDefinition, opts ...EipOption) plc.VariableConfig {
	o := applyEipOpts(opts)

	src := EipTag(device, tag)
	src.Bidirectional = o.bidirectional

	vc := plc.VariableConfig{
		Description: o.description,
		Datatype:    plc.Udt,
		Source:      src,
		UdtTemplate: &template,
		DisableRBE:  o.disableRBE || device.DisableRBE,
	}

	if o.deadband != nil {
		vc.Deadband = o.deadband
	} else if device.Deadband != nil {
		vc.Deadband = device.Deadband
	}

	return vc
}

// EipVars creates plc.VariableConfig entries for multiple atomic tags.
// The map is keyed by tag name.
func EipVars(device EipDevice, tags []string, opts ...EipOption) map[string]plc.VariableConfig {
	m := make(map[string]plc.VariableConfig, len(tags))
	for _, tag := range tags {
		m[tag] = EipVar(device, tag, opts...)
	}
	return m
}

// EipAll creates plc.VariableConfig entries for both atomic tags and UDT tags.
// Tags whose names appear as keys in the templates map are created as UDTs;
// all others are created as atomic variables.  The returned map is keyed by
// tag name.
func EipAll(device EipDevice, tags []string, templates map[string]ttypes.UdtTemplateDefinition, opts ...EipOption) map[string]plc.VariableConfig {
	m := make(map[string]plc.VariableConfig, len(tags))
	for _, tag := range tags {
		if tmpl, ok := templates[tag]; ok {
			m[tag] = EipUdtVar(device, tag, tmpl, opts...)
		} else {
			m[tag] = EipVar(device, tag, opts...)
		}
	}
	return m
}
