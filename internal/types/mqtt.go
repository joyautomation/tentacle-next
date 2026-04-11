package types

// MqttBridgeConfig holds MQTT/Sparkplug B bridge configuration.
type MqttBridgeConfig struct {
	BrokerURL        string `json:"brokerUrl"`
	ClientID         string `json:"clientId"`
	GroupID          string `json:"groupId"`
	EdgeNode         string `json:"edgeNode"`
	DeviceID         string `json:"deviceId,omitempty"`
	PerSourceDevice  bool   `json:"perSourceDevice,omitempty"` // When true, each source device becomes its own Sparkplug device
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	Keepalive        int    `json:"keepalive,omitempty"`
	PrimaryHostID    string `json:"primaryHostId,omitempty"`
	UseTemplates     bool   `json:"useTemplates"`
	StoreForwardMax  int    `json:"storeForwardMaxRecords,omitempty"`
	StoreForwardSize int64  `json:"storeForwardMaxBytes,omitempty"`
	DrainRate        int    `json:"drainRate,omitempty"`
	TLSEnabled       bool   `json:"tlsEnabled,omitempty"`
	TLSCertPath      string `json:"tlsCertPath,omitempty"`
	TLSKeyPath       string `json:"tlsKeyPath,omitempty"`
	TLSCaPath        string `json:"tlsCaPath,omitempty"`
}

// MqttMetricInfo describes a single metric published by the Sparkplug bridge.
type MqttMetricInfo struct {
	Name         string      `json:"name"`
	SparkplugType string     `json:"sparkplugType"` // "double", "boolean", "string", "template"
	Value        interface{} `json:"value,omitempty"`
	ModuleID     string      `json:"moduleId"`
	DeviceID     string      `json:"deviceId"`
	Datatype     string      `json:"datatype"` // "number", "boolean", "string", "udt"
	TemplateRef  string      `json:"templateRef,omitempty"`
	LastUpdated  int64       `json:"lastUpdated,omitempty"`
}

// MqttMetricsResponse is the reply to mqtt.metrics request.
type MqttMetricsResponse struct {
	Metrics   []MqttMetricInfo `json:"metrics"`
	Templates []MqttTemplateInfo `json:"templates"`
	DeviceID  string `json:"deviceId"`
	Timestamp int64  `json:"timestamp"`
}

// MqttTemplateInfo describes a UDT template known to the MQTT bridge.
type MqttTemplateInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Members []struct {
		Name        string `json:"name"`
		Datatype    string `json:"datatype"`
		TemplateRef string `json:"templateRef,omitempty"`
	} `json:"members"`
}

// MqttBridgeMessage wraps an inbound/outbound MQTT message.
type MqttBridgeMessage struct {
	OriginalTopic string `json:"originalTopic"`
	Payload       string `json:"payload"`
	Retained      bool   `json:"retained"`
	QoS           int    `json:"qos"`
	Timestamp     int64  `json:"timestamp"`
}
