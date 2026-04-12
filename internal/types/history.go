package types

// HistoryConfig holds the configuration for the history/TimescaleDB module.
type HistoryConfig struct {
	DBHost        string `json:"dbHost"`
	DBPort        int    `json:"dbPort"`
	DBUser        string `json:"dbUser"`
	DBPassword    string `json:"dbPassword"`
	DBName        string `json:"dbName"`
	DBSSLMode     string `json:"dbSslMode,omitempty"`
	DBSSLCaPath   string `json:"dbSslCaPath,omitempty"`
	GroupID       string `json:"groupId"`       // Sparkplug group ID for history records
	EnableHyper   bool   `json:"enableHyper"`   // Enable TimescaleDB features
	RetentionDays int    `json:"retentionDays"` // Data retention in days, 0 = unlimited
}

// HistoryRecord is a single row written to the history table.
// Schema matches mantle: (group_id, node_id, device_id, metric_id).
type HistoryRecord struct {
	GroupID     string   `json:"groupId"`
	NodeID      string   `json:"nodeId"`
	DeviceID    string   `json:"deviceId"`
	MetricID    string   `json:"metricId"`
	IntValue    *int64   `json:"intValue,omitempty"`
	FloatValue  *float64 `json:"floatValue,omitempty"`
	StringValue *string  `json:"stringValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
	Timestamp   int64    `json:"timestamp"`
}

// HistoryPropertyRecord is a single row written to the history_properties table.
type HistoryPropertyRecord struct {
	GroupID     string   `json:"groupId"`
	NodeID      string   `json:"nodeId"`
	DeviceID    string   `json:"deviceId"`
	MetricID    string   `json:"metricId"`
	PropertyID  string   `json:"propertyId"`
	IntValue    *int64   `json:"intValue,omitempty"`
	FloatValue  *float64 `json:"floatValue,omitempty"`
	StringValue *string  `json:"stringValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
	Timestamp   int64    `json:"timestamp"`
}

// MetricProperties is the current property set for a metric, stored as JSONB.
type MetricProperties struct {
	GroupID    string                 `json:"groupId"`
	NodeID     string                 `json:"nodeId"`
	DeviceID   string                 `json:"deviceId"`
	MetricID   string                 `json:"metricId"`
	Properties map[string]interface{} `json:"properties"`
}

// HistoryMetricRef identifies a metric for history queries.
type HistoryMetricRef struct {
	GroupID  string `json:"groupId"`
	NodeID   string `json:"nodeId"`
	DeviceID string `json:"deviceId"`
	MetricID string `json:"metricId"`
}

// HistoryQueryRequest is the request payload for history.query.
type HistoryQueryRequest struct {
	RequestID string             `json:"requestId"`
	Start     int64              `json:"start"`
	End       int64              `json:"end"`
	Metrics   []HistoryMetricRef `json:"metrics"`
	Interval  string             `json:"interval,omitempty"`
	Samples   int                `json:"samples,omitempty"`
	Raw       bool               `json:"raw,omitempty"`
	Timestamp int64              `json:"timestamp"`
}

// HistoryPoint is a single data point in a history query result.
type HistoryPoint struct {
	Timestamp   int64    `json:"timestamp"`
	Avg         *float64 `json:"avg,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	IntValue    *int64   `json:"intValue,omitempty"`
	FloatValue  *float64 `json:"floatValue,omitempty"`
	StringValue *string  `json:"stringValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

// HistoryMetricData contains historical data points for a single metric.
type HistoryMetricData struct {
	GroupID  string         `json:"groupId"`
	NodeID   string         `json:"nodeId"`
	DeviceID string         `json:"deviceId"`
	MetricID string         `json:"metricId"`
	Points   []HistoryPoint `json:"points"`
}

// HistoryQueryResponse is the reply to history.query.
type HistoryQueryResponse struct {
	RequestID string              `json:"requestId"`
	Success   bool                `json:"success"`
	Error     string              `json:"error,omitempty"`
	Results   []HistoryMetricData `json:"results,omitempty"`
	Timestamp int64               `json:"timestamp"`
}

// HistoryUsageStats contains storage usage statistics.
type HistoryUsageStats struct {
	TotalSize    int64               `json:"totalSize"`
	RowCount     int64               `json:"rowCount"`
	OldestRecord int64               `json:"oldestRecord"`
	ByMonth      []HistoryMonthUsage `json:"byMonth,omitempty"`
}

// HistoryMonthUsage is per-month storage usage.
type HistoryMonthUsage struct {
	Month    string `json:"month"`
	RowCount int64  `json:"rowCount"`
	Size     int64  `json:"size"`
}

// HistoryUsageResponse is the reply to history.usage.
type HistoryUsageResponse struct {
	RequestID string             `json:"requestId"`
	Success   bool               `json:"success"`
	Error     string             `json:"error,omitempty"`
	Usage     *HistoryUsageStats `json:"usage,omitempty"`
	Timestamp int64              `json:"timestamp"`
}

// HistoryEnabledResponse is the reply to history.enabled.
type HistoryEnabledResponse struct {
	RequestID string `json:"requestId"`
	Enabled   bool   `json:"enabled"`
	Timestamp int64  `json:"timestamp"`
}
