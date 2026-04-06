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
	EnableHyper   bool   `json:"enableHyper"`   // Enable TimescaleDB features
	RetentionDays int    `json:"retentionDays"` // Data retention in days, 0 = unlimited
}

// HistoryRecord is a single row written to the history table.
type HistoryRecord struct {
	ModuleID    string   `json:"moduleId"`
	VariableID  string   `json:"variableId"`
	IntValue    *int64   `json:"intValue,omitempty"`
	FloatValue  *float64 `json:"floatValue,omitempty"`
	StringValue *string  `json:"stringValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
	Timestamp   int64    `json:"timestamp"`
}

// HistoryVariableRef identifies a variable for history queries.
type HistoryVariableRef struct {
	ModuleID   string `json:"moduleId"`
	VariableID string `json:"variableId"`
}

// HistoryQueryRequest is the request payload for history.query.
type HistoryQueryRequest struct {
	RequestID string               `json:"requestId"`
	Start     int64                `json:"start"`
	End       int64                `json:"end"`
	Variables []HistoryVariableRef `json:"variables"`
	Interval  string               `json:"interval,omitempty"`
	Samples   int                  `json:"samples,omitempty"`
	Raw       bool                 `json:"raw,omitempty"`
	Timestamp int64                `json:"timestamp"`
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

// HistoryVariableData contains historical data points for a single variable.
type HistoryVariableData struct {
	ModuleID   string         `json:"moduleId"`
	VariableID string         `json:"variableId"`
	Points     []HistoryPoint `json:"points"`
}

// HistoryQueryResponse is the reply to history.query.
type HistoryQueryResponse struct {
	RequestID string                `json:"requestId"`
	Success   bool                  `json:"success"`
	Error     string                `json:"error,omitempty"`
	Results   []HistoryVariableData `json:"results,omitempty"`
	Timestamp int64                 `json:"timestamp"`
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
