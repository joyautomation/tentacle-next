package sources

import (
	"github.com/joyautomation/tentacle/plc"
	ttypes "github.com/joyautomation/tentacle/types"
)

// OpcuaDevice describes an OPC UA server target.
type OpcuaDevice struct {
	DeviceID    string
	EndpointURL string
	ScanRate    int // ms
}

// ── Functional options ──────────────────────────────────────────────────────

type opcuaOpts struct {
	description   string
	deadband      *ttypes.DeadBandConfig
	disableRBE    bool
	bidirectional bool
}

// OpcuaOption configures optional fields when creating an OPC UA variable.
type OpcuaOption func(*opcuaOpts)

// WithOpcuaDescription sets the variable description.
func WithOpcuaDescription(desc string) OpcuaOption {
	return func(o *opcuaOpts) { o.description = desc }
}

// WithOpcuaDeadband sets a per-variable deadband.
func WithOpcuaDeadband(db ttypes.DeadBandConfig) OpcuaOption {
	return func(o *opcuaOpts) { o.deadband = &db }
}

// WithOpcuaDisableRBE disables report-by-exception for the variable.
func WithOpcuaDisableRBE() OpcuaOption {
	return func(o *opcuaOpts) { o.disableRBE = true }
}

// WithOpcuaBidirectional marks the source as bidirectional (allows writes).
func WithOpcuaBidirectional() OpcuaOption {
	return func(o *opcuaOpts) { o.bidirectional = true }
}

func applyOpcuaOpts(opts []OpcuaOption) opcuaOpts {
	var o opcuaOpts
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// ── Public API ──────────────────────────────────────────────────────────────

// OpcuaTag creates a plc.Source for an OPC UA node.
func OpcuaTag(device OpcuaDevice, nodeID string) *plc.Source {
	return &plc.Source{
		OpcUA: &plc.OpcUASource{
			DeviceID:    device.DeviceID,
			EndpointURL: device.EndpointURL,
			NodeID:      nodeID,
			ScanRate:    device.ScanRate,
		},
	}
}

// OpcuaVar creates a plc.VariableConfig for a single OPC UA node.
// Defaults to Number datatype.
func OpcuaVar(device OpcuaDevice, nodeID string, opts ...OpcuaOption) plc.VariableConfig {
	o := applyOpcuaOpts(opts)

	src := OpcuaTag(device, nodeID)
	src.Bidirectional = o.bidirectional

	vc := plc.VariableConfig{
		Description: o.description,
		Datatype:    plc.Number,
		Source:      src,
		DisableRBE:  o.disableRBE,
	}

	if o.deadband != nil {
		vc.Deadband = o.deadband
	}

	return vc
}

// OpcuaVars creates plc.VariableConfig entries for multiple OPC UA nodes.
// The map is keyed by node ID.
func OpcuaVars(device OpcuaDevice, nodeIDs []string, opts ...OpcuaOption) map[string]plc.VariableConfig {
	m := make(map[string]plc.VariableConfig, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		m[nodeID] = OpcuaVar(device, nodeID, opts...)
	}
	return m
}

// OpcuaAll creates plc.VariableConfig entries for OPC UA nodes.
// OPC UA has no UDT distinction, so this is equivalent to OpcuaVars.
func OpcuaAll(device OpcuaDevice, nodeIDs []string, opts ...OpcuaOption) map[string]plc.VariableConfig {
	return OpcuaVars(device, nodeIDs, opts...)
}
