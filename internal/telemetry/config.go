//go:build telemetry || all

package telemetry

import (
	"os"
	"strconv"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/topics"
)

const (
	defaultEndpoint = "https://telemetry.joyautomation.com"
	defaultInterval = "3600"
	defaultAPIKey   = "c71a01c68f9d3c0fca4e5c44bf8d16872706cabafda00aa47571f9cbe54d7e61"
)

// configSchema defines the telemetry module's configuration fields for the settings UI.
// Endpoint, interval, and API key are hard-coded and not user-configurable.
var configSchema = []config.FieldDef{
	{EnvVar: "TELEMETRY_ENABLED", Label: "Enable Telemetry", Type: "boolean", Group: "General", GroupOrder: 0, SortOrder: 0, Default: "false", Description: "Send anonymous usage data to help improve Tentacle"},
	{EnvVar: "TELEMETRY_ERRORS_ENABLED", Label: "Error Reporting", Type: "boolean", Group: "General", GroupOrder: 0, SortOrder: 1, Default: "true", DependsOn: "TELEMETRY_ENABLED", Description: "Automatically report errors when they occur"},
}

// Config holds telemetry module configuration.
type Config struct {
	Enabled       bool
	Endpoint      string
	Interval      int
	ErrorsEnabled bool
	ConfigHash    bool
	APIKey        string
}

// loadConfig loads telemetry config with priority: KV → env var → default.
func loadConfig(b bus.Bus) Config {
	get := func(envVar, defaultVal string) string {
		if b != nil {
			if data, _, err := b.KVGet(topics.BucketTentacleConfig, "telemetry."+envVar); err == nil && len(data) > 0 {
				return string(data)
			}
		}
		if v := os.Getenv(envVar); v != "" {
			return v
		}
		return defaultVal
	}

	getBool := func(envVar string, defaultVal bool) bool {
		v := get(envVar, strconv.FormatBool(defaultVal))
		b, err := strconv.ParseBool(v)
		if err != nil {
			return defaultVal
		}
		return b
	}

	getInt := func(envVar string, defaultVal int) int {
		v := get(envVar, strconv.Itoa(defaultVal))
		n, err := strconv.Atoi(v)
		if err != nil {
			return defaultVal
		}
		return n
	}

	return Config{
		Enabled:       getBool("TELEMETRY_ENABLED", false),
		Endpoint:      get("TELEMETRY_ENDPOINT", defaultEndpoint),
		Interval:      getInt("TELEMETRY_INTERVAL", 3600),
		ErrorsEnabled: getBool("TELEMETRY_ERRORS_ENABLED", true),
		ConfigHash:    getBool("TELEMETRY_CONFIG_HASH", false),
		APIKey:        get("TELEMETRY_API_KEY", defaultAPIKey),
	}
}

// saveConfig persists the current config to KV so the web UI settings page can read it.
func saveConfig(b bus.Bus, cfg *Config) {
	put := func(envVar, value string) {
		if _, err := b.KVPut(topics.BucketTentacleConfig, "telemetry."+envVar, []byte(value)); err != nil {
			// Ignore — best effort
		}
	}
	put("TELEMETRY_ENABLED", strconv.FormatBool(cfg.Enabled))
	put("TELEMETRY_ENDPOINT", cfg.Endpoint)
	put("TELEMETRY_INTERVAL", strconv.Itoa(cfg.Interval))
	put("TELEMETRY_ERRORS_ENABLED", strconv.FormatBool(cfg.ErrorsEnabled))
	put("TELEMETRY_API_KEY", cfg.APIKey)
}
