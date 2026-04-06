package types

// VariableInfo is returned in response to {moduleId}.variables requests.
type VariableInfo struct {
	ModuleID    string                 `json:"moduleId"`
	DeviceID    string                 `json:"deviceId"`
	VariableID  string                 `json:"variableId"`
	Value       interface{}            `json:"value"`
	Datatype    string                 `json:"datatype"`
	Description string                 `json:"description,omitempty"`
	Deadband    *DeadBandConfig        `json:"deadband,omitempty"`
	DisableRBE  bool                   `json:"disableRBE,omitempty"`
	UdtTemplate *UdtTemplateDefinition `json:"udtTemplate,omitempty"`
}

// ScannerBatchMessage is the batch format used by some scanners (modbus, snmp).
type ScannerBatchMessage struct {
	ModuleID  string              `json:"moduleId"`
	DeviceID  string              `json:"deviceId"`
	Timestamp int64               `json:"timestamp"`
	Values    []ScannerBatchValue `json:"values"`
}

// ScannerBatchValue is a single value within a batch message.
type ScannerBatchValue struct {
	VariableID string      `json:"variableId"`
	Value      interface{} `json:"value"`
	Datatype   string      `json:"datatype"`
}
