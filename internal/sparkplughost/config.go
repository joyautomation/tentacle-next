//go:build sparkplughost || mantle || all

package sparkplughost

import (
	"os"
	"strconv"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/topics"
)

type Config struct {
	BrokerURL     string // e.g. tcp://localhost:1883
	ClientID      string
	Username      string
	Password      string
	PrimaryHostID string // Sparkplug B Host Application ID; published via spBv1.0/STATE/<id>
	GroupFilter   string // "+" matches all groups; specific group otherwise
	SharedGroup   string // if set, use $share/<group>/spBv1.0/... for HA. Empty = no sharing.
	KeepAlive     int    // seconds
	CleanSession  bool
	StaleSeconds  int    // node considered stale if LastSeen older than this
}

// configSchema defines the sparkplug-host module's configuration fields for the
// settings UI. Mirrors the structure of the mqtt module's schema.
var configSchema = []config.FieldDef{
	// Connection
	{EnvVar: "SPARKPLUG_HOST_BROKER_URL", Label: "Broker URL", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 0, Default: "tcp://localhost:1883"},
	{EnvVar: "SPARKPLUG_HOST_CLIENT_ID", Label: "Client ID", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 1, Description: "Defaults to the module ID if blank."},
	{EnvVar: "SPARKPLUG_HOST_USERNAME", Label: "Username", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 2},
	{EnvVar: "SPARKPLUG_HOST_PASSWORD", Label: "Password", Type: "password", Group: "Connection", GroupOrder: 0, SortOrder: 3},
	{EnvVar: "SPARKPLUG_HOST_KEEPALIVE", Label: "Keep Alive (seconds)", Type: "number", Group: "Connection", GroupOrder: 0, SortOrder: 4, Default: "30"},
	{EnvVar: "SPARKPLUG_HOST_CLEAN_SESSION", Label: "Clean Session", Type: "boolean", Group: "Connection", GroupOrder: 0, SortOrder: 5, Default: "true"},

	// Sparkplug B
	{EnvVar: "SPARKPLUG_HOST_PRIMARY_HOST_ID", Label: "Primary Host ID", Type: "string", Group: "Sparkplug B", GroupOrder: 1, SortOrder: 0, Default: "MantleHost", Description: "Published as a retained STATE message so EoN nodes can do store-and-forward."},
	{EnvVar: "SPARKPLUG_HOST_GROUP", Label: "Group Filter", Type: "string", Group: "Sparkplug B", GroupOrder: 1, SortOrder: 1, Default: "+", Description: "Sparkplug group to subscribe to. Use '+' to consume all groups."},
	{EnvVar: "SPARKPLUG_HOST_SHARED_GROUP", Label: "Shared Subscription Group", Type: "string", Group: "Sparkplug B", GroupOrder: 1, SortOrder: 2, Description: "Optional MQTT 5 shared-subscription name for HA fan-out across multiple hosts."},

	// Inventory
	{EnvVar: "SPARKPLUG_HOST_STALE_SECONDS", Label: "Stale Threshold (seconds)", Type: "number", Group: "Inventory", GroupOrder: 2, SortOrder: 0, Default: "90", Description: "Nodes with no activity for this long are reported as stale in heartbeat metadata."},
}

// loadConfig resolves config in priority order:
// KV (sparkplug-host.<ENVVAR>) > env var > default.
func loadConfig(b bus.Bus, moduleID string) Config {
	get := func(envVar, def string) string {
		if b != nil {
			if data, _, err := b.KVGet(topics.BucketTentacleConfig, "sparkplug-host."+envVar); err == nil && len(data) > 0 {
				return string(data)
			}
		}
		if v := os.Getenv(envVar); v != "" {
			return v
		}
		return def
	}
	getInt := func(envVar string, def int) int {
		s := get(envVar, "")
		if s == "" {
			return def
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		return def
	}
	getBool := func(envVar string, def bool) bool {
		s := get(envVar, "")
		if s == "" {
			return def
		}
		if v, err := strconv.ParseBool(s); err == nil {
			return v
		}
		return def
	}

	return Config{
		BrokerURL:     get("SPARKPLUG_HOST_BROKER_URL", "tcp://localhost:1883"),
		ClientID:      get("SPARKPLUG_HOST_CLIENT_ID", moduleID),
		Username:      get("SPARKPLUG_HOST_USERNAME", ""),
		Password:      get("SPARKPLUG_HOST_PASSWORD", ""),
		PrimaryHostID: get("SPARKPLUG_HOST_PRIMARY_HOST_ID", "MantleHost"),
		GroupFilter:   get("SPARKPLUG_HOST_GROUP", "+"),
		SharedGroup:   get("SPARKPLUG_HOST_SHARED_GROUP", ""),
		KeepAlive:     getInt("SPARKPLUG_HOST_KEEPALIVE", 30),
		CleanSession:  getBool("SPARKPLUG_HOST_CLEAN_SESSION", true),
		StaleSeconds:  getInt("SPARKPLUG_HOST_STALE_SECONDS", 90),
	}
}

// saveConfig persists current config to KV under sparkplug-host.<ENVVAR> so the
// settings UI can read and edit it.
func saveConfig(b bus.Bus, cfg Config) {
	put := func(envVar, value string) {
		_, _ = b.KVPut(topics.BucketTentacleConfig, "sparkplug-host."+envVar, []byte(value))
	}
	put("SPARKPLUG_HOST_BROKER_URL", cfg.BrokerURL)
	put("SPARKPLUG_HOST_CLIENT_ID", cfg.ClientID)
	put("SPARKPLUG_HOST_USERNAME", cfg.Username)
	put("SPARKPLUG_HOST_PASSWORD", cfg.Password)
	put("SPARKPLUG_HOST_PRIMARY_HOST_ID", cfg.PrimaryHostID)
	put("SPARKPLUG_HOST_GROUP", cfg.GroupFilter)
	put("SPARKPLUG_HOST_SHARED_GROUP", cfg.SharedGroup)
	put("SPARKPLUG_HOST_KEEPALIVE", strconv.Itoa(cfg.KeepAlive))
	put("SPARKPLUG_HOST_CLEAN_SESSION", strconv.FormatBool(cfg.CleanSession))
	put("SPARKPLUG_HOST_STALE_SECONDS", strconv.Itoa(cfg.StaleSeconds))
}
