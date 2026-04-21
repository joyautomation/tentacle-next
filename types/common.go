// Package types provides shared wire-format types used across all tentacle modules.
// These types are public so that external programs (e.g., user PLC programs
// importing the plc/ library) can produce messages compatible with internal modules.
//
// All types produce canonical JSON when marshaled with encoding/json.
package types

import "strings"

// DeadBandConfig defines RBE (Report By Exception) thresholds for a variable.
type DeadBandConfig struct {
	Value   float64 `json:"value"`             // only publish if change exceeds this amount
	MinTime int64   `json:"minTime,omitempty"` // ms — suppress publishes more frequent than this
	MaxTime int64   `json:"maxTime,omitempty"` // ms — force publish if exceeded, 0 = disabled
}

// DeadBandOverride stores sparse per-field overrides for instance-level deadband
// configuration. Nil pointer means "inherit from template default."
type DeadBandOverride struct {
	Value   *float64 `json:"value,omitempty"`
	MinTime *int64   `json:"minTime,omitempty"`
	MaxTime *int64   `json:"maxTime,omitempty"`
}

// Merge applies non-nil override fields on top of a base DeadBandConfig.
func (o DeadBandOverride) Merge(base DeadBandConfig) DeadBandConfig {
	result := base
	if o.Value != nil {
		result.Value = *o.Value
	}
	if o.MinTime != nil {
		result.MinTime = *o.MinTime
	}
	if o.MaxTime != nil {
		result.MaxTime = *o.MaxTime
	}
	return result
}

// UdtMemberDefinition describes a single field in a UDT template.
type UdtMemberDefinition struct {
	Name        string `json:"name"`
	Datatype    string `json:"datatype"`             // "number", "boolean", "string"
	Description string `json:"description,omitempty"` // human-readable
	TemplateRef string `json:"templateRef,omitempty"` // nested UDT reference
	IsArray     bool   `json:"isArray,omitempty"`
}

// UdtTemplateDefinition is a Sparkplug B UDT template definition.
type UdtTemplateDefinition struct {
	Name    string                `json:"name"`
	Version string                `json:"version,omitempty"`
	Members []UdtMemberDefinition `json:"members"`
}

// ServiceHeartbeat is published every 10s to the service_heartbeats KV bucket.
type ServiceHeartbeat struct {
	ServiceType string                 `json:"serviceType"`
	ModuleID    string                 `json:"moduleId"`
	LastSeen    int64                  `json:"lastSeen"`
	StartedAt   int64                  `json:"startedAt"`
	Version     string                 `json:"version,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ServiceEnabledKV is the value stored in the service_enabled KV bucket.
type ServiceEnabledKV struct {
	ModuleID  string `json:"moduleId"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ServiceLogEntry is published to service.logs.{serviceType}.{moduleId}.
type ServiceLogEntry struct {
	Timestamp   int64  `json:"timestamp"`
	Level       string `json:"level"`
	Message     string `json:"message"`
	ServiceType string `json:"serviceType"`
	ModuleID    string `json:"moduleId"`
	Logger      string `json:"logger,omitempty"`
}

// BrowseProgressMessage is published during async browse operations
// to {protocol}.browse.progress.{browseId}.
type BrowseProgressMessage struct {
	BrowseID        string `json:"browseId"`
	ModuleID        string `json:"moduleId"`
	DeviceID        string `json:"deviceId"`
	Phase           string `json:"phase"` // "discovering", "expanding", "reading", "caching", "completed", "failed"
	TotalCount      int    `json:"totalCount"`
	DiscoveredCount int    `json:"discoveredCount"`
	ErrorCount      int    `json:"errorCount"`
	Message         string `json:"message,omitempty"`
	Timestamp       string `json:"timestamp"`
}

// HealthCheckMessage is published for service health monitoring.
type HealthCheckMessage struct {
	Service   string          `json:"service"`
	Status    string          `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp int64           `json:"timestamp"`
	Uptime    int64           `json:"uptime"` // ms
	Checks    map[string]bool `json:"checks,omitempty"`
}

// CommunicationEvent represents an error, warning, info, or debug event.
type CommunicationEvent struct {
	ModuleID  string   `json:"moduleId"`
	Type      string   `json:"type"`     // "error", "warning", "info", "debug"
	Message   string   `json:"message"`
	Severity  string   `json:"severity"` // "critical", "high", "medium", "low"
	Timestamp int64    `json:"timestamp"`
	Source    string   `json:"source"`
	Tags      []string `json:"tags,omitempty"`
}

// SanitizeForSubject replaces NATS-invalid characters in an identifier.
func SanitizeForSubject(s string) string {
	r := strings.NewReplacer(" ", "_", ".", "_", ":", "_", "*", "_", ">", "_", ";", "_", "=", "_")
	return r.Replace(s)
}
