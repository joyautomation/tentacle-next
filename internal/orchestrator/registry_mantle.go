//go:build mantle && !all

package orchestrator

import (
	otypes "github.com/joyautomation/tentacle/internal/types"
)

func init() {
	moduleRegistry = append(moduleRegistry,
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "history",
			Description:  "Centralized historian (TimescaleDB) — fleet aggregation",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
			RequiredConfig: []otypes.ConfigField{
				{EnvVar: "HISTORY_DB_HOST", Description: "PostgreSQL host", Default: "localhost"},
				{EnvVar: "HISTORY_DB_PORT", Description: "PostgreSQL port", Default: "5432"},
				{EnvVar: "HISTORY_DB_USER", Description: "PostgreSQL user", Default: "postgres"},
				{EnvVar: "HISTORY_DB_PASSWORD", Description: "PostgreSQL password", Default: "postgres"},
				{EnvVar: "HISTORY_DB_NAME", Description: "Database name", Default: "tentacle"},
			},
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "mqtt-broker",
			Description:  "Embedded MQTT broker (mochi-mqtt)",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
			RequiredConfig: []otypes.ConfigField{
				{EnvVar: "MQTT_BROKER_LISTEN", Description: "Listen address (host:port)", Default: ":1883"},
				{EnvVar: "MQTT_BROKER_ALLOW_ALL", Description: "Allow anonymous connections (true/false)", Default: "true"},
				{EnvVar: "MQTT_BROKER_USERNAME", Description: "Username (when allow-all is false)"},
				{EnvVar: "MQTT_BROKER_PASSWORD", Description: "Password (when allow-all is false)"},
			},
		},
		otypes.ModuleRegistryEntry{
			Repo:         "tentacle-next",
			ModuleID:     "sparkplug-host",
			Description:  "Sparkplug B Host Application — fleet ingestion to history/bus",
			Category:     "optional",
			Runtime:      "embedded",
			Experimental: true,
			RequiredConfig: []otypes.ConfigField{
				{EnvVar: "SPARKPLUG_HOST_BROKER_URL", Description: "MQTT broker URL (tcp:// or tls://)", Required: true, Default: "tcp://localhost:1883"},
				{EnvVar: "SPARKPLUG_HOST_CLIENT_ID", Description: "MQTT client ID base"},
				{EnvVar: "SPARKPLUG_HOST_USERNAME", Description: "Broker username"},
				{EnvVar: "SPARKPLUG_HOST_PASSWORD", Description: "Broker password"},
				{EnvVar: "SPARKPLUG_HOST_APP_ID", Description: "Primary Host Application ID", Default: "MantleHost"},
				{EnvVar: "SPARKPLUG_HOST_GROUP", Description: "Group filter (+ for all groups)", Default: "+"},
				{EnvVar: "SPARKPLUG_HOST_SHARED_GROUP", Description: "MQTT5 shared subscription group for HA (empty = no sharing)"},
			},
		},
	)
}
