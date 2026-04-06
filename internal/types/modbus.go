package types

// ModbusTagConfig describes a single Modbus register tag for scanning.
type ModbusTagConfig struct {
	ID           string `json:"id"`
	Description  string `json:"description,omitempty"`
	Address      int    `json:"address"`
	FunctionCode string `json:"functionCode"` // "coil", "discrete", "holding", "input"
	Datatype     string `json:"datatype"`     // "boolean", "int16", "uint16", "int32", "uint32", "float32", "float64"
	ByteOrder    string `json:"byteOrder,omitempty"` // "ABCD", "BADC", "CDAB", "DCBA"
	Writable     bool   `json:"writable,omitempty"`
}

// ModbusScannerSubscribeRequest is sent to modbus.subscribe.
type ModbusScannerSubscribeRequest struct {
	SubscriberID string           `json:"subscriberId"`
	DeviceID     string           `json:"deviceId"`
	Host         string           `json:"host"`
	Port         int              `json:"port,omitempty"`
	UnitID       int              `json:"unitId,omitempty"`
	ByteOrder    string           `json:"byteOrder,omitempty"` // Device-level default
	ScanRate     int              `json:"scanRate,omitempty"`
	Tags         []ModbusTagConfig `json:"tags"`
}

// ModbusScannerUnsubscribeRequest is sent to modbus.unsubscribe.
type ModbusScannerUnsubscribeRequest struct {
	SubscriberID string   `json:"subscriberId"`
	DeviceID     string   `json:"deviceId"`
	TagIDs       []string `json:"tagIds,omitempty"`
}

// ModbusServerSubscribeRequest is sent to modbus-server.subscribe.
type ModbusServerSubscribeRequest struct {
	DeviceID       string            `json:"deviceId"`
	Port           int               `json:"port,omitempty"`
	UnitID         int               `json:"unitId,omitempty"`
	Tags           []ModbusTagConfig `json:"tags"`
	SubscriberID   string            `json:"subscriberId"`
	SourceModuleID string            `json:"sourceModuleId"`
}

// ModbusServerSubscribeResponse is the reply to modbus-server.subscribe.
type ModbusServerSubscribeResponse struct {
	Success bool   `json:"success"`
	Port    int    `json:"port,omitempty"`
	Error   string `json:"error,omitempty"`
}
