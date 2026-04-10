//go:build orchestrator || all

package orchestrator

import (
	otypes "github.com/joyautomation/tentacle/internal/types"
)

// moduleRegistry describes all modules the orchestrator can manage.
// In monolith mode, modules are started as in-process goroutines.
// In bare-metal mode, modules are managed as systemd services.
var moduleRegistry = []otypes.ModuleRegistryEntry{
	{
		Repo:        "tentacle-next",
		ModuleID:    "gateway",
		Description: "Device gateway (RBE, UDT assembly)",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "ethernetip",
		Description: "EtherNet/IP scanner (Allen-Bradley, etc.)",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "opcua",
		Description: "OPC UA client",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "snmp",
		Description: "SNMP scanner & trap listener",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "modbus",
		Description: "Modbus TCP scanner",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "mqtt",
		Description: "MQTT Sparkplug B bridge",
		Category:    "optional",
		Runtime:     "embedded",
		RequiredConfig: []otypes.ConfigField{
			{EnvVar: "MQTT_BROKER_URL", Description: "MQTT broker URL (mqtt:// or mqtts://)", Required: true},
			{EnvVar: "MQTT_CLIENT_ID", Description: "MQTT client ID base", Default: "tentacle-mqtt"},
			{EnvVar: "MQTT_GROUP_ID", Description: "Sparkplug B group ID", Default: "TentacleGroup"},
			{EnvVar: "MQTT_EDGE_NODE", Description: "Sparkplug B edge node name", Default: "EdgeNode"},
			{EnvVar: "MQTT_USERNAME", Description: "MQTT broker username"},
			{EnvVar: "MQTT_PASSWORD", Description: "MQTT broker password"},
		},
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "ethernetip-server",
		Description: "EtherNet/IP server",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "modbus-server",
		Description: "Modbus TCP server",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "history",
		Description: "Edge historian (TimescaleDB)",
		Category:    "optional",
		Runtime:     "embedded",
		RequiredConfig: []otypes.ConfigField{
			{EnvVar: "HISTORY_DB_HOST", Description: "PostgreSQL host", Default: "localhost"},
			{EnvVar: "HISTORY_DB_PORT", Description: "PostgreSQL port", Default: "5432"},
			{EnvVar: "HISTORY_DB_NAME", Description: "Database name", Default: "tentacle"},
		},
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "network",
		Description: "Network interface manager",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "nftables",
		Description: "Firewall manager",
		Category:    "optional",
		Runtime:     "embedded",
	},
	{
		Repo:        "tentacle-next",
		ModuleID:    "gitops",
		Description: "GitOps config sync",
		Category:    "optional",
		Runtime:     "embedded",
		RequiredConfig: []otypes.ConfigField{
			{EnvVar: "GITOPS_REPO_URL", Description: "Git repository URL (SSH or HTTPS)", Required: true},
		},
	},
}

// getRegistryEntry looks up a module by its moduleId.
func getRegistryEntry(moduleID string) *otypes.ModuleRegistryEntry {
	for i := range moduleRegistry {
		if moduleRegistry[i].ModuleID == moduleID {
			return &moduleRegistry[i]
		}
	}
	return nil
}
