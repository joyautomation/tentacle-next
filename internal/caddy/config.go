//go:build all

package caddy

import (
	"os"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/topics"
)

// configSchema defines the Caddy module's configuration fields for the settings UI.
var configSchema = []config.FieldDef{
	// Mode toggle
	{EnvVar: "CADDY_ADVANCED_MODE", Label: "Advanced Mode", Type: "boolean",
		Group: "Mode", GroupOrder: 0, SortOrder: 0, Default: "false",
		Description: "Edit the raw Caddyfile instead of using simple fields"},

	// Simple mode fields (hidden when advanced mode is on)
	{EnvVar: "CADDY_DOMAIN", Label: "Domain / Address", Type: "string",
		Group: "Simple Configuration", GroupOrder: 1, SortOrder: 0,
		Default: ":80", DependsOn: "!CADDY_ADVANCED_MODE",
		Description: "Domain name for auto-HTTPS, or :port for HTTP-only"},
	{EnvVar: "CADDY_UPSTREAM_PORT", Label: "Upstream Port", Type: "number",
		Group: "Simple Configuration", GroupOrder: 1, SortOrder: 1,
		Default: "4000", DependsOn: "!CADDY_ADVANCED_MODE",
		Description: "Port tentacle is listening on"},

	// Advanced mode field (shown when advanced mode is on)
	{EnvVar: "CADDY_CADDYFILE", Label: "Caddyfile", Type: "textarea",
		Group: "Advanced Configuration", GroupOrder: 2, SortOrder: 0,
		DependsOn: "CADDY_ADVANCED_MODE",
		Description: "Raw Caddyfile content"},
}

// caddyConfig holds the resolved Caddy configuration.
type caddyConfig struct {
	AdvancedMode bool
	Domain       string
	UpstreamPort string
	Caddyfile    string
}

// loadConfig loads Caddy config from KV → env → defaults.
func loadConfig(b bus.Bus) caddyConfig {
	get := func(envVar, defaultVal string) string {
		if b != nil {
			if data, _, err := b.KVGet(topics.BucketTentacleConfig, "caddy."+envVar); err == nil && len(data) > 0 {
				return string(data)
			}
		}
		if v := os.Getenv(envVar); v != "" {
			return v
		}
		return defaultVal
	}

	return caddyConfig{
		AdvancedMode: get("CADDY_ADVANCED_MODE", "false") == "true",
		Domain:       get("CADDY_DOMAIN", ":80"),
		UpstreamPort: get("CADDY_UPSTREAM_PORT", "4000"),
		Caddyfile:    get("CADDY_CADDYFILE", ""),
	}
}

// saveConfig persists the current config as individual KV keys.
func saveConfig(b bus.Bus, cfg *caddyConfig) {
	put := func(envVar, value string) {
		if _, err := b.KVPut(topics.BucketTentacleConfig, "caddy."+envVar, []byte(value)); err != nil {
			// best effort
		}
	}
	advancedStr := "false"
	if cfg.AdvancedMode {
		advancedStr = "true"
	}
	put("CADDY_ADVANCED_MODE", advancedStr)
	put("CADDY_DOMAIN", cfg.Domain)
	put("CADDY_UPSTREAM_PORT", cfg.UpstreamPort)
	put("CADDY_CADDYFILE", cfg.Caddyfile)
}
