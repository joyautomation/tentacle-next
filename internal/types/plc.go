package types

import ttypes "github.com/joyautomation/tentacle/types"

// PlcConfigKV is the full PLC configuration stored in the plc_config
// NATS KV bucket, keyed by plcId.
type PlcConfigKV struct {
	PlcID        string                             `json:"plcId"`
	Devices      map[string]PlcDeviceConfigKV       `json:"devices"`
	Variables    map[string]PlcVariableConfigKV      `json:"variables"`
	UdtTemplates map[string]PlcUdtTemplateConfigKV  `json:"udtTemplates,omitempty"`
	Tasks        map[string]PlcTaskConfigKV          `json:"tasks"`
	UpdatedAt    int64                               `json:"updatedAt"`
}

// PlcDeviceConfigKV maps a scanner device that this PLC subscribes to.
type PlcDeviceConfigKV struct {
	Protocol    string `json:"protocol"`              // "ethernetip", "opcua", "modbus", "snmp"
	Host        string `json:"host,omitempty"`
	Port        *int   `json:"port,omitempty"`
	Slot        *int   `json:"slot,omitempty"`         // EtherNet/IP: chassis slot (default 0)
	EndpointURL string `json:"endpointUrl,omitempty"` // OPC UA
	Version     string `json:"version,omitempty"`     // SNMP: "1", "2c", "3"
	Community   string `json:"community,omitempty"`   // SNMP
	UnitID      *int   `json:"unitId,omitempty"`      // Modbus
	ScanRate    *int   `json:"scanRate,omitempty"`
}

// PlcVariableConfigKV defines a single PLC variable.
type PlcVariableConfigKV struct {
	ID          string                  `json:"id"`
	Description string                  `json:"description,omitempty"`
	Datatype    string                  `json:"datatype"`  // "number", "boolean", "string"
	Default     interface{}             `json:"default"`
	Direction   string                  `json:"direction"` // "input", "output", "internal"
	Source      *PlcVariableSourceKV    `json:"source,omitempty"`
	Deadband    *ttypes.DeadBandConfig  `json:"deadband,omitempty"`
	DisableRBE  bool                    `json:"disableRBE,omitempty"`
}

// PlcVariableSourceKV ties a PLC input variable to a scanner tag.
type PlcVariableSourceKV struct {
	Protocol       string `json:"protocol"`              // "ethernetip", "opcua", "modbus", "snmp"
	DeviceID       string `json:"deviceId"`
	Tag            string `json:"tag"`
	CipType        string `json:"cipType,omitempty"`     // EtherNet/IP CIP type hint
	FunctionCode   *int   `json:"functionCode,omitempty"`   // Modbus
	ModbusDatatype string `json:"modbusDatatype,omitempty"` // Modbus
	ByteOrder      string `json:"byteOrder,omitempty"`      // Modbus
	Address        *int   `json:"address,omitempty"`        // Modbus
}

// PlcUdtTemplateConfigKV defines a UDT template for variables produced by this PLC.
type PlcUdtTemplateConfigKV struct {
	Name    string                       `json:"name"`
	Version string                       `json:"version,omitempty"`
	Members []PlcUdtTemplateMemberConfig `json:"members"`
}

// PlcUdtTemplateMemberConfig describes a single field in a PLC UDT template.
type PlcUdtTemplateMemberConfig struct {
	Name     string `json:"name"`
	Datatype string `json:"datatype"` // "number", "boolean", "string"
}

// PlcTaskConfigKV defines a scan-loop task.
type PlcTaskConfigKV struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ScanRateMs  int    `json:"scanRateMs"`
	ProgramRef  string `json:"programRef"`         // key in plc_programs KV bucket
	EntryFn     string `json:"entryFn,omitempty"`  // top-level Starlark function (default "main")
	Enabled     bool   `json:"enabled"`
}

// PlcProgramKV stores a Starlark program in the plc_programs KV bucket.
// A program exposes one or more top-level callable functions; `Signature`
// documents the entry function's parameters and return type so the editor
// and LSP can offer completion/hover/diagnostics across the flat namespace.
type PlcProgramKV struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Language    string          `json:"language"`            // "ladder", "st", "starlark"
	Source      string          `json:"source"`              // Starlark source (for ladder, this IS the DSL)
	StSource    string          `json:"stSource,omitempty"`  // Original ST source (for ST programs only)
	Signature   *PlcFunctionSig `json:"signature,omitempty"` // entry-function signature for intellisense
	UpdatedAt   int64           `json:"updatedAt"`
	UpdatedBy   string          `json:"updatedBy,omitempty"` // "gui", "cli", "gitops"

	// Online-edit pending state. When PendingSource is non-empty the program
	// has an uncommitted edit that hasn't been swapped into the running
	// engine yet. Cleared by assemble (promoted to live) or cancel.
	PendingSource    string          `json:"pendingSource,omitempty"`
	PendingStSource  string          `json:"pendingStSource,omitempty"`
	PendingLanguage  string          `json:"pendingLanguage,omitempty"`
	PendingSignature *PlcFunctionSig `json:"pendingSignature,omitempty"`
	PendingUpdatedAt int64           `json:"pendingUpdatedAt,omitempty"`
	PendingUpdatedBy string          `json:"pendingUpdatedBy,omitempty"`
}

// HasPending reports whether the program carries an uncommitted online edit.
func (p *PlcProgramKV) HasPending() bool {
	return p != nil && p.PendingSource != ""
}

// PlcFunctionSig captures a callable's input/output shape. Types use the
// same vocabulary as variables (number, boolean, string, or a UDT name).
type PlcFunctionSig struct {
	Params  []PlcFunctionParam `json:"params,omitempty"`
	Returns *PlcFunctionReturn `json:"returns,omitempty"`
}

// PlcFunctionParam describes a single input parameter.
type PlcFunctionParam struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // "number", "boolean", "string", or UDT template name
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

// PlcFunctionReturn describes a function's return shape.
type PlcFunctionReturn struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}
