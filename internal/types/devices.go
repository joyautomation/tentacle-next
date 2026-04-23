package types

import ttypes "github.com/joyautomation/tentacle/types"

// DeviceConfig describes a device scanner connection. Lives in the shared
// `devices` KV bucket, keyed by deviceId. Consumed by gateway (MQTT
// bridging, variable binding) and PLC (input variables, ad-hoc reads).
//
// All fields are a superset — protocol-specific fields (V3Auth, UnitID,
// ByteOrder, etc.) are only populated for the relevant protocol.
type DeviceConfig struct {
	Protocol              string                 `json:"protocol"`
	AutoManaged           bool                   `json:"autoManaged,omitempty"`
	Host                  string                 `json:"host,omitempty"`
	Port                  *int                   `json:"port,omitempty"`
	Slot                  *int                   `json:"slot,omitempty"`
	EndpointURL           string                 `json:"endpointUrl,omitempty"`
	Version               string                 `json:"version,omitempty"`
	Community             string                 `json:"community,omitempty"`
	V3Auth                *V3Auth                `json:"v3Auth,omitempty"`
	UnitID                *int                   `json:"unitId,omitempty"`
	ByteOrder             string                 `json:"byteOrder,omitempty"`
	ScanRate              *int                   `json:"scanRate,omitempty"`
	Deadband              *ttypes.DeadBandConfig `json:"deadband,omitempty"`
	DisableRBE            *bool                  `json:"disableRBE,omitempty"`
	TemplateNameOverrides map[string]string      `json:"templateNameOverrides,omitempty"`
}
