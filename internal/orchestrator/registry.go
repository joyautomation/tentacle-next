//go:build orchestrator || all

package orchestrator

import (
	otypes "github.com/joyautomation/tentacle/internal/types"
)

// moduleRegistry mirrors install.sh's MODULES array.
// The orchestrator uses this to know how to download, install, and
// create systemd units for each module.
var moduleRegistry = []otypes.ModuleRegistryEntry{
	{
		Repo:        "tentacle-graphql",
		ModuleID:    "tentacle-graphql",
		Description: "GraphQL API gateway",
		Category:    "core",
		Runtime:     "deno",
	},
	{
		Repo:        "tentacle-web",
		ModuleID:    "tentacle-web",
		Description: "Web dashboard",
		Category:    "core",
		Runtime:     "deno-web",
	},
	{
		Repo:        "tentacle-ethernetip-go",
		ModuleID:    "tentacle-ethernetip",
		Description: "EtherNet/IP scanner (Allen-Bradley, etc.)",
		Category:    "optional",
		Runtime:     "go",
		AptDeps:     []string{"cmake", "build-essential"},
		BuildDeps: []otypes.BuildDep{
			{
				Name:    "libplctag",
				Version: "v2.6.15",
				Repo:    "https://github.com/libplctag/libplctag.git",
				TestCmd: "ldconfig -p | grep libplctag",
			},
		},
	},
	{
		Repo:        "tentacle-opcua-go",
		ModuleID:    "tentacle-opcua",
		Description: "OPC UA client",
		Category:    "optional",
		Runtime:     "go",
		ExtraEnv:    "OPCUA_PKI_DIR=/opt/tentacle/data/opcua/pki",
	},
	{
		Repo:        "tentacle-snmp",
		ModuleID:    "tentacle-snmp",
		Description: "SNMP scanner & trap listener",
		Category:    "optional",
		Runtime:     "go",
	},
	{
		Repo:        "tentacle-mqtt",
		ModuleID:    "tentacle-mqtt",
		Description: "MQTT Sparkplug B bridge",
		Category:    "optional",
		Runtime:     "deno",
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
		Repo:        "tentacle-history",
		ModuleID:    "tentacle-history",
		Description: "Edge historian (TimescaleDB)",
		Category:    "optional",
		Runtime:     "deno",
	},
	{
		Repo:        "tentacle-modbus",
		ModuleID:    "tentacle-modbus",
		Description: "Modbus TCP scanner",
		Category:    "optional",
		Runtime:     "deno",
	},
	{
		Repo:        "tentacle-modbus-server",
		ModuleID:    "tentacle-modbus-server",
		Description: "Modbus TCP server",
		Category:    "optional",
		Runtime:     "deno",
	},
	{
		Repo:        "tentacle-network",
		ModuleID:    "tentacle-network",
		Description: "Network interface manager",
		Category:    "optional",
		Runtime:     "deno",
	},
	{
		Repo:        "tentacle-nftables",
		ModuleID:    "tentacle-nftables",
		Description: "Firewall manager",
		Category:    "optional",
		Runtime:     "deno",
	},
	{
		Repo:        "tentacle-gateway-go",
		ModuleID:    "tentacle-gateway",
		Description: "Device gateway (RBE, UDT)",
		Category:    "optional",
		Runtime:     "go",
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
