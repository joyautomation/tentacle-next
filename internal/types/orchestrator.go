package types

// DesiredServiceKV is the desired state for a module.
// Stored in the desired_services KV bucket, keyed by moduleId.
type DesiredServiceKV struct {
	ModuleID  string `json:"moduleId"`
	Version   string `json:"version"`
	Running   bool   `json:"running"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ServiceStatusKV is the runtime status of a module as reported by the orchestrator.
// Stored in the service_status KV bucket (TTL 120s), keyed by moduleId.
type ServiceStatusKV struct {
	ModuleID          string   `json:"moduleId"`
	InstalledVersions []string `json:"installedVersions"`
	ActiveVersion     string   `json:"activeVersion,omitempty"`
	SystemdState      string   `json:"systemdState"`      // "active", "inactive", "failed", "not-found"
	ReconcileState    string   `json:"reconcileState"`     // "ok", "pending", "downloading", etc.
	LastError         string   `json:"lastError,omitempty"`
	Runtime           string   `json:"runtime"`  // "go", "deno", "deno-web", "binary"
	Category          string   `json:"category"` // "core", "optional"
	Repo              string   `json:"repo"`
	UpdatedAt         int64    `json:"updatedAt"`
}

// ModuleRegistryInfo describes a module from the orchestrator's registry.
type ModuleRegistryInfo struct {
	ModuleID       string              `json:"moduleId"`
	Repo           string              `json:"repo"`
	Description    string              `json:"description"`
	Category       string              `json:"category"`
	Runtime        string              `json:"runtime"`
	Experimental   bool                `json:"experimental,omitempty"`
	RequiredConfig []ModuleConfigField `json:"requiredConfig,omitempty"`
}

// ModuleConfigField describes a configuration field for a module.
type ModuleConfigField struct {
	EnvVar      string `json:"envVar"`
	Description string `json:"description"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required"`
}

// ModuleVersionInfo holds version info for a specific module.
type ModuleVersionInfo struct {
	ModuleID          string   `json:"moduleId"`
	InstalledVersions []string `json:"installedVersions"`
	LatestVersion     string   `json:"latestVersion,omitempty"`
	ActiveVersion     string   `json:"activeVersion,omitempty"`
}

// OrchestratorCommandRequest is sent to orchestrator.command (request/reply).
type OrchestratorCommandRequest struct {
	RequestID string `json:"requestId"`
	Action    string `json:"action"` // "get-registry", "check-internet", "get-module-versions", "restart-service"
	ModuleID  string `json:"moduleId,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// OrchestratorCommandResponse is the reply to an orchestrator command.
type OrchestratorCommandResponse struct {
	RequestID string              `json:"requestId"`
	Success   bool                `json:"success"`
	Error     string              `json:"error,omitempty"`
	Modules   []ModuleRegistryInfo `json:"modules,omitempty"`
	Online    *bool               `json:"online,omitempty"`
	Versions  *ModuleVersionInfo  `json:"versions,omitempty"`
	Timestamp int64               `json:"timestamp"`
}

// ModuleRegistryEntry describes a module the orchestrator knows how to manage.
// This is the internal registry entry, not the wire format.
type ModuleRegistryEntry struct {
	Repo           string
	ModuleID       string
	Description    string
	Category       string     // "core" or "optional"
	Runtime        string     // "go", "deno", "deno-web"
	Experimental   bool       // true = only included in dev/experimental builds
	ExtraEnv       string     // Extra systemd Environment lines
	AptDeps        []string   // Apt packages to install
	BuildDeps      []BuildDep // Libraries built from source
	RequiredConfig []ConfigField
}

// ConfigField describes a configuration field for a module.
type ConfigField struct {
	EnvVar      string
	Description string
	Default     string
	Required    bool
}

// BuildDep describes a library that must be built from source.
type BuildDep struct {
	Name    string // Human-readable name
	Version string // Git tag
	Repo    string // Clone URL
	TestCmd string // Command to test if installed
}
