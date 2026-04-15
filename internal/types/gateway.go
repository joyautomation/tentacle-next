package types

import ttypes "github.com/joyautomation/tentacle/types"

// GatewayConfigKV is the full gateway configuration stored in the gateway_config
// NATS KV bucket, keyed by gatewayId.
type GatewayConfigKV struct {
	GatewayID    string                              `json:"gatewayId"`
	Devices      map[string]GatewayDeviceConfig       `json:"devices"`
	Variables    map[string]GatewayVariableConfig      `json:"variables"`
	UdtTemplates map[string]GatewayUdtTemplateConfig  `json:"udtTemplates,omitempty"`
	UdtVariables map[string]GatewayUdtVariableConfig  `json:"udtVariables,omitempty"`
	UpdatedAt    int64                                `json:"updatedAt"`
}

// GatewayDeviceConfig is a protocol-specific device connection configuration.
type GatewayDeviceConfig struct {
	Protocol              string                   `json:"protocol"` // "ethernetip", "opcua", "snmp", "modbus"
	AutoManaged           bool                     `json:"autoManaged,omitempty"` // true for module-created devices (network, gateway status, etc.)
	Host                  string                   `json:"host,omitempty"`
	Port                  *int                     `json:"port,omitempty"`
	Slot                  *int                     `json:"slot,omitempty"`        // EtherNet/IP: chassis slot (default 0)
	EndpointURL           string                   `json:"endpointUrl,omitempty"` // OPC UA
	Version               string                   `json:"version,omitempty"`     // SNMP: "1", "2c", "3"
	Community             string                   `json:"community,omitempty"`   // SNMP
	V3Auth                *V3Auth                  `json:"v3Auth,omitempty"`      // SNMP v3
	UnitID                *int                     `json:"unitId,omitempty"`      // Modbus
	ByteOrder             string                   `json:"byteOrder,omitempty"`   // Modbus: "ABCD", "BADC", "CDAB", "DCBA"
	ScanRate              *int                     `json:"scanRate,omitempty"`
	Deadband              *ttypes.DeadBandConfig   `json:"deadband,omitempty"`
	DisableRBE            *bool                    `json:"disableRBE,omitempty"`
	TemplateNameOverrides map[string]string         `json:"templateNameOverrides,omitempty"`
}

// V3Auth holds SNMP v3 authentication credentials.
type V3Auth struct {
	Username      string `json:"username"`
	SecurityLevel string `json:"securityLevel"` // "noAuthNoPriv", "authNoPriv", "authPriv"
	AuthProtocol  string `json:"authProtocol,omitempty"`
	AuthPassword  string `json:"authPassword,omitempty"`
	PrivProtocol  string `json:"privProtocol,omitempty"`
	PrivPassword  string `json:"privPassword,omitempty"`
}

// GatewayVariableConfig maps a device tag/node/OID to a named gateway variable.
type GatewayVariableConfig struct {
	ID             string                  `json:"id"`
	Description    string                  `json:"description,omitempty"`
	Datatype       string                  `json:"datatype"` // "number", "boolean", "string"
	Default        interface{}             `json:"default"`
	DeviceID       string                  `json:"deviceId"`
	Tag            string                  `json:"tag"`
	CipType        string                  `json:"cipType,omitempty"`
	Bidirectional  bool                    `json:"bidirectional,omitempty"`
	Deadband       *ttypes.DeadBandConfig  `json:"deadband,omitempty"`
	DisableRBE     bool                    `json:"disableRBE,omitempty"`
	HistoryEnabled bool                    `json:"historyEnabled,omitempty"`
	FunctionCode   *int                    `json:"functionCode,omitempty"`
	ModbusDatatype string                  `json:"modbusDatatype,omitempty"`
	ByteOrder      string                  `json:"byteOrder,omitempty"`
	Address        *int                    `json:"address,omitempty"`
}

// GatewayUdtTemplateMemberConfig describes a single field in a UDT template.
type GatewayUdtTemplateMemberConfig struct {
	Name            string                 `json:"name"`
	Datatype        string                 `json:"datatype"`
	TemplateRef     string                 `json:"templateRef,omitempty"`
	DefaultDeadband *ttypes.DeadBandConfig `json:"defaultDeadband,omitempty"`
	// Modbus-specific structural fields (shared across all instances of a template).
	FunctionCode   string `json:"functionCode,omitempty"`   // "holding", "input", "coil", "discrete"
	ModbusDatatype string `json:"modbusDatatype,omitempty"` // "int16", "uint16", "int32", "uint32", "float32", "float64"
}

// GatewayUdtTemplateConfig is a UDT template definition stored in gateway config.
type GatewayUdtTemplateConfig struct {
	Name    string                           `json:"name"`
	Version string                           `json:"version,omitempty"`
	Members []GatewayUdtTemplateMemberConfig `json:"members"`
}

// GatewayUdtVariableConfig maps a UDT instance to a named variable with member tags.
type GatewayUdtVariableConfig struct {
	ID              string                            `json:"id"`
	DeviceID        string                            `json:"deviceId"`
	Tag             string                            `json:"tag"`
	TemplateName    string                            `json:"templateName"`
	MemberTags      map[string]string                 `json:"memberTags"`
	MemberCipTypes  map[string]string                 `json:"memberCipTypes,omitempty"`
	MemberDeadbands map[string]ttypes.DeadBandOverride `json:"memberDeadbands,omitempty"`
	Deadband        *ttypes.DeadBandConfig            `json:"deadband,omitempty"`
	DisableRBE      bool                              `json:"disableRBE,omitempty"`
	HistoryEnabled  bool                              `json:"historyEnabled,omitempty"`
	// Modbus-specific per-instance fields.
	MemberAddresses  map[string]int    `json:"memberAddresses,omitempty"`  // member name → register address
	MemberByteOrders map[string]string `json:"memberByteOrders,omitempty"` // member name → byte order override
}

// ─── Scanner Subscribe Requests ─────────────────────────────────────────────

// EthernetIPSubscribeRequest matches tentacle-ethernetip-go's subscribe format.
type EthernetIPSubscribeRequest struct {
	SubscriberID string                           `json:"subscriberId"`
	DeviceID     string                           `json:"deviceId"`
	Host         string                           `json:"host"`
	Port         int                              `json:"port,omitempty"`
	Slot         int                              `json:"slot"`
	Tags         []string                         `json:"tags"`
	ScanRate     int                              `json:"scanRate,omitempty"`
	CipTypes     map[string]string                `json:"cipTypes,omitempty"`
	StructTypes  map[string]string                `json:"structTypes,omitempty"`
	Deadbands    map[string]ttypes.DeadBandConfig `json:"deadbands,omitempty"`
	DisableRBE   map[string]bool                  `json:"disableRBE,omitempty"`
}

// OpcUASubscribeRequest matches tentacle-opcua-go's subscribe format.
type OpcUASubscribeRequest struct {
	SubscriberID string   `json:"subscriberId"`
	DeviceID     string   `json:"deviceId"`
	EndpointURL  string   `json:"endpointUrl"`
	NodeIDs      []string `json:"nodeIds"`
	ScanRate     int      `json:"scanRate,omitempty"`
}

// SNMPSubscribeRequest matches tentacle-snmp's subscribe format.
type SNMPSubscribeRequest struct {
	SubscriberID string   `json:"subscriberId"`
	DeviceID     string   `json:"deviceId"`
	Host         string   `json:"host"`
	Port         int      `json:"port,omitempty"`
	Version      string   `json:"version"`
	Community    string   `json:"community,omitempty"`
	V3Auth       *V3Auth  `json:"v3Auth,omitempty"`
	OIDs         []string `json:"oids"`
	ScanRate     int      `json:"scanRate,omitempty"`
}


// ProfinetControllerSubscribeRequest asks the PROFINET IO Controller scanner
// to connect to a device. Mirrors profinetcontroller.SubscribeRequest.
type ProfinetControllerSubscribeRequest struct {
	SubscriberID  string                       `json:"subscriberId"`
	DeviceID      string                       `json:"deviceId"`
	StationName   string                       `json:"stationName"`
	IP            string                       `json:"ip,omitempty"`
	InterfaceName string                       `json:"interfaceName"`
	VendorID      uint16                       `json:"vendorId,omitempty"`
	DeviceIDPN    uint16                       `json:"deviceIdPn,omitempty"`
	CycleTimeMs   int                          `json:"cycleTimeMs,omitempty"`
	Slots         []ProfinetControllerSlot     `json:"slots"`
}

// ProfinetControllerSlot describes a module slot.
type ProfinetControllerSlot struct {
	SlotNumber    uint16                          `json:"slotNumber"`
	ModuleIdentNo uint32                         `json:"moduleIdentNo"`
	Subslots      []ProfinetControllerSubslot    `json:"subslots"`
}

// ProfinetControllerSubslot describes a submodule and its I/O data.
type ProfinetControllerSubslot struct {
	SubslotNumber    uint16                     `json:"subslotNumber"`
	SubmoduleIdentNo uint32                     `json:"submoduleIdentNo"`
	InputSize        uint16                     `json:"inputSize"`
	OutputSize       uint16                     `json:"outputSize"`
	Tags             []ProfinetControllerTag    `json:"tags"`
}

// ProfinetControllerTag maps a PROFINET I/O byte position to a Tentacle tag.
type ProfinetControllerTag struct {
	TagID      string `json:"tagId"`
	ByteOffset uint16 `json:"byteOffset"`
	BitOffset  uint8  `json:"bitOffset,omitempty"`
	Datatype   string `json:"datatype"`
	Direction  string `json:"direction"` // "input" or "output"
}

// FunctionCodeToString converts a numeric Modbus function code to its string name.
func FunctionCodeToString(fc int) string {
	switch fc {
	case 1:
		return "coil"
	case 2:
		return "discrete"
	case 3:
		return "holding"
	case 4:
		return "input"
	default:
		return "holding"
	}
}

// CipToNatsDatatype normalizes CIP type names to tentacle datatypes.
func CipToNatsDatatype(cipType string) string {
	switch cipType {
	case "BOOL":
		return "boolean"
	case "STRING":
		return "string"
	default:
		return "number"
	}
}
