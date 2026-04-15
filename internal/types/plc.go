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
	Protocol    string `json:"protocol"`              // "ethernetip", "opcua", "modbus", "snmp", "profinetcontroller"
	Host        string `json:"host,omitempty"`
	Port        *int   `json:"port,omitempty"`
	Slot        *int   `json:"slot,omitempty"`         // EtherNet/IP: chassis slot (default 0)
	EndpointURL string `json:"endpointUrl,omitempty"` // OPC UA
	Version     string `json:"version,omitempty"`     // SNMP: "1", "2c", "3"
	Community   string `json:"community,omitempty"`   // SNMP
	UnitID      *int   `json:"unitId,omitempty"`      // Modbus
	ScanRate    *int   `json:"scanRate,omitempty"`

	// PROFINET IO Controller fields
	StationName   string `json:"stationName,omitempty"`   // PROFINET: station name for DCP discovery
	InterfaceName string `json:"interfaceName,omitempty"` // PROFINET: network interface for raw L2
	CycleTimeMs   *int   `json:"cycleTimeMs,omitempty"`   // PROFINET: RT cycle time in ms
	VendorID      *int   `json:"vendorId,omitempty"`      // PROFINET: vendor ID for verification
	DeviceIDPN    *int   `json:"deviceIdPn,omitempty"`    // PROFINET: device ID for verification
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
	Protocol       string `json:"protocol"`              // "ethernetip", "opcua", "modbus", "snmp", "profinetcontroller"
	DeviceID       string `json:"deviceId"`
	Tag            string `json:"tag"`
	CipType        string `json:"cipType,omitempty"`     // EtherNet/IP CIP type hint
	FunctionCode   *int   `json:"functionCode,omitempty"`   // Modbus
	ModbusDatatype string `json:"modbusDatatype,omitempty"` // Modbus
	ByteOrder      string `json:"byteOrder,omitempty"`      // Modbus
	Address        *int   `json:"address,omitempty"`        // Modbus

	// PROFINET IO Controller fields
	PnSlotNumber       *int   `json:"pnSlotNumber,omitempty"`       // PROFINET: slot number
	PnSubslotNumber    *int   `json:"pnSubslotNumber,omitempty"`    // PROFINET: subslot number
	PnModuleIdentNo    *int   `json:"pnModuleIdentNo,omitempty"`    // PROFINET: module ident number
	PnSubmoduleIdentNo *int   `json:"pnSubmoduleIdentNo,omitempty"` // PROFINET: submodule ident number
	PnInputSize        *int   `json:"pnInputSize,omitempty"`        // PROFINET: input data size (bytes) for this subslot
	PnOutputSize       *int   `json:"pnOutputSize,omitempty"`       // PROFINET: output data size (bytes) for this subslot
	PnByteOffset       *int   `json:"pnByteOffset,omitempty"`       // PROFINET: byte offset in cyclic I/O data
	PnBitOffset        *int   `json:"pnBitOffset,omitempty"`        // PROFINET: bit offset (for bool)
	PnDatatype         string `json:"pnDatatype,omitempty"`         // PROFINET: binary datatype (float32, uint16, etc.)
	PnDirection        string `json:"pnDirection,omitempty"`        // PROFINET: "input" (from device) or "output" (to device)
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
	ProgramRef  string `json:"programRef"` // key in plc_programs KV bucket
	Enabled     bool   `json:"enabled"`
}

// PlcProgramKV stores a Starlark program in the plc_programs KV bucket.
type PlcProgramKV struct {
	Name      string `json:"name"`
	Language  string `json:"language"`            // "ladder", "st", "starlark"
	Source    string `json:"source"`              // Starlark source (for ladder, this IS the DSL)
	StSource  string `json:"stSource,omitempty"`  // Original ST source (for ST programs only)
	UpdatedAt int64  `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy,omitempty"` // "gui", "cli", "gitops"
}
