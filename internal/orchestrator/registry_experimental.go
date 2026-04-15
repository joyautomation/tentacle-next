//go:build all

package orchestrator

import (
	otypes "github.com/joyautomation/tentacle/internal/types"
)

func init() {
	moduleRegistry = append(moduleRegistry,
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "opcua",
			Description:  "OPC UA client",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "modbus",
			Description:  "Modbus TCP scanner",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "profinet",
			Description:  "PROFINET IO Device",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "profinetcontroller",
			Description:  "PROFINET IO Controller",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "ethernetip-server",
			Description:  "EtherNet/IP server",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "modbus-server",
			Description:  "Modbus TCP server",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "history",
			Description:  "Edge historian (TimescaleDB)",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
			RequiredConfig: []otypes.ConfigField{
				{EnvVar: "HISTORY_DB_HOST", Description: "PostgreSQL host", Default: "localhost"},
				{EnvVar: "HISTORY_DB_PORT", Description: "PostgreSQL port", Default: "5432"},
				{EnvVar: "HISTORY_DB_NAME", Description: "Database name", Default: "tentacle"},
			},
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "nftables",
			Description:  "Firewall manager",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "plc",
			Description:  "Soft PLC engine",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
		},
	)
}
