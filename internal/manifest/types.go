package manifest

import (
	itypes "github.com/joyautomation/tentacle/internal/types"
)

const APIVersion = "tentacle.joyautomation.com/v1"

// Resource kinds.
const (
	KindGateway      = "Gateway"
	KindService      = "Service"
	KindModuleConfig = "ModuleConfig"
	KindNftables     = "Nftables"
	KindNetwork      = "Network"
	KindPlc          = "Plc"
)

// AllKinds lists every known resource kind.
var AllKinds = []string{
	KindGateway,
	KindService,
	KindModuleConfig,
	KindNftables,
	KindNetwork,
	KindPlc,
}

// ResourceHeader is the common envelope for all manifest resources.
type ResourceHeader struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
}

// Metadata identifies a resource.
type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

// ─── Gateway ────────────────────────────────────────────────────────────────

// GatewayResource is the manifest representation of a gateway configuration.
type GatewayResource struct {
	ResourceHeader `yaml:",inline"`
	Spec           GatewaySpec `yaml:"spec" json:"spec"`
}

// GatewaySpec is the gateway config without runtime fields (gatewayId, updatedAt).
type GatewaySpec struct {
	Devices      map[string]itypes.GatewayDeviceConfig      `yaml:"devices" json:"devices"`
	Variables    map[string]itypes.GatewayVariableConfig     `yaml:"variables" json:"variables"`
	UdtTemplates map[string]itypes.GatewayUdtTemplateConfig `yaml:"udtTemplates,omitempty" json:"udtTemplates,omitempty"`
	UdtVariables map[string]itypes.GatewayUdtVariableConfig `yaml:"udtVariables,omitempty" json:"udtVariables,omitempty"`
}

// ─── Service ────────────────────────────────────────────────────────────────

// ServiceResource is the manifest representation of a desired service.
type ServiceResource struct {
	ResourceHeader `yaml:",inline"`
	Spec           ServiceSpec `yaml:"spec" json:"spec"`
}

// ServiceSpec mirrors DesiredServiceKV without moduleId/updatedAt.
type ServiceSpec struct {
	Version string `yaml:"version" json:"version"`
	Running bool   `yaml:"running" json:"running"`
	Enabled *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// ─── Module Config ──────────────────────────────────────────────────────────

// ModuleConfigResource is the manifest representation of a module's configuration.
type ModuleConfigResource struct {
	ResourceHeader `yaml:",inline"`
	Spec           ModuleConfigSpec `yaml:"spec" json:"spec"`
}

// ModuleConfigSpec holds the key-value config for a module.
type ModuleConfigSpec struct {
	Values map[string]string `yaml:"values" json:"values"`
}

// ─── Nftables ───────────────────────────────────────────────────────────────

// NftablesResource is the manifest representation of nftables NAT configuration.
type NftablesResource struct {
	ResourceHeader `yaml:",inline"`
	Spec           NftablesSpec `yaml:"spec" json:"spec"`
}

// NftablesSpec wraps the NAT rules.
type NftablesSpec struct {
	NatRules []itypes.NatRule `yaml:"natRules" json:"natRules"`
}

// ─── Network ────────────────────────────────────────────────────────────────

// NetworkResource is the manifest representation of network interface configuration.
type NetworkResource struct {
	ResourceHeader `yaml:",inline"`
	Spec           NetworkSpec `yaml:"spec" json:"spec"`
}

// NetworkSpec wraps the interface configurations.
type NetworkSpec struct {
	Interfaces []itypes.NetworkInterfaceConfig `yaml:"interfaces" json:"interfaces"`
}

// ─── Plc ────────────────────────────────────────────────────────────────────

// PlcResource is the manifest representation of a PLC configuration.
type PlcResource struct {
	ResourceHeader `yaml:",inline"`
	Spec           PlcSpec `yaml:"spec" json:"spec"`
}

// PlcSpec is the PLC config for GitOps.
type PlcSpec struct {
	Devices      map[string]itypes.PlcDeviceConfigKV      `yaml:"devices" json:"devices"`
	Variables    map[string]itypes.PlcVariableConfigKV     `yaml:"variables" json:"variables"`
	UdtTemplates map[string]itypes.PlcUdtTemplateConfigKV `yaml:"udtTemplates,omitempty" json:"udtTemplates,omitempty"`
	Tasks        map[string]itypes.PlcTaskConfigKV         `yaml:"tasks" json:"tasks"`
	Programs     map[string]PlcProgramSpec                 `yaml:"programs,omitempty" json:"programs,omitempty"`
}

// PlcProgramSpec is the manifest representation of a PLC program.
type PlcProgramSpec struct {
	Language string `yaml:"language" json:"language"` // "ladder", "st", "starlark"
	Source   string `yaml:"source" json:"source"`
	StSource string `yaml:"stSource,omitempty" json:"stSource,omitempty"`
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// NewHeader creates a ResourceHeader with the standard API version.
func NewHeader(kind, name string) ResourceHeader {
	return ResourceHeader{
		APIVersion: APIVersion,
		Kind:       kind,
		Metadata:   Metadata{Name: name},
	}
}

// KnownKind returns true if the kind is recognized.
func KnownKind(kind string) bool {
	for _, k := range AllKinds {
		if k == kind {
			return true
		}
	}
	return false
}
