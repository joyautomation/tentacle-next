package types

// PlcDataMessage is published when a monitored variable changes value.
// Subject: {moduleId}.data.{deviceId}.{sanitizedVariableId}
type PlcDataMessage struct {
	ModuleID        string                    `json:"moduleId"`
	DeviceID        string                    `json:"deviceId"`
	VariableID      string                    `json:"variableId"`
	Value           interface{}               `json:"value"`
	Timestamp       int64                     `json:"timestamp"`
	Datatype        string                    `json:"datatype"` // "number", "boolean", "string", "udt"
	Description     string                    `json:"description,omitempty"`
	Deadband        *DeadBandConfig           `json:"deadband,omitempty"`
	DisableRBE      bool                      `json:"disableRBE,omitempty"`
	HistoryEnabled  bool                      `json:"historyEnabled,omitempty"`
	// MqttEnabled gates downstream MQTT forwarding. Pointer so legacy
	// publishers (no field) are treated as enabled, preserving prior behavior
	// where being in the gateway aggregate implied MQTT.
	MqttEnabled     *bool                     `json:"mqttEnabled,omitempty"`
	UdtTemplate     *UdtTemplateDefinition    `json:"udtTemplate,omitempty"`
	MemberDeadbands map[string]DeadBandConfig `json:"memberDeadbands,omitempty"`
}

// PlcVariableKV is stored in the plc_variables KV bucket.
type PlcVariableKV struct {
	ModuleID    string          `json:"moduleId"`
	DeviceID    string          `json:"deviceId,omitempty"`
	VariableID  string          `json:"variableId"`
	Value       interface{}     `json:"value"`
	Datatype    string          `json:"datatype"`
	LastUpdated int64           `json:"lastUpdated"`
	Origin      string          `json:"origin"` // "mqtt", "graphql", "plc", "field", "manual"
	Quality     string          `json:"quality"` // "good", "uncertain", "bad"
	Source      string          `json:"source,omitempty"` // NATS topic for this variable
	Deadband    *DeadBandConfig `json:"deadband,omitempty"`
	DisableRBE  bool            `json:"disableRBE,omitempty"`
}

// PlcStatusMessage represents a module status change.
type PlcStatusMessage struct {
	ModuleID     string `json:"moduleId"`
	Status       string `json:"status"` // "running", "stopped", "error", "paused"
	Timestamp    int64  `json:"timestamp"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// DeviceRegistryKV is stored in the device_registry KV bucket.
type DeviceRegistryKV struct {
	DeviceID      string                 `json:"deviceId"`
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Status        string                 `json:"status"` // "online", "offline", "error"
	LastHeartbeat int64                  `json:"lastHeartbeat"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}
