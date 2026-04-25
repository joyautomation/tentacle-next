//go:build fleet || mantle || all

package fleet

import (
	"os"
	"strconv"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/topics"
)

type Config struct {
	BrokerURL    string
	ClientID     string
	Username     string
	Password     string
	GroupFilter  string
	KeepAlive    int
	CleanSession bool
	StaleSeconds int // node considered stale if LastSeen older than this
}

var configSchema = []config.FieldDef{
	// Connection
	{EnvVar: "FLEET_BROKER_URL", Label: "Broker URL", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 0, Default: "tcp://localhost:1883"},
	{EnvVar: "FLEET_CLIENT_ID", Label: "Client ID", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 1, Description: "Defaults to the module ID if blank."},
	{EnvVar: "FLEET_USERNAME", Label: "Username", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 2},
	{EnvVar: "FLEET_PASSWORD", Label: "Password", Type: "password", Group: "Connection", GroupOrder: 0, SortOrder: 3},
	{EnvVar: "FLEET_KEEPALIVE", Label: "Keep Alive (seconds)", Type: "number", Group: "Connection", GroupOrder: 0, SortOrder: 4, Default: "30"},
	{EnvVar: "FLEET_CLEAN_SESSION", Label: "Clean Session", Type: "boolean", Group: "Connection", GroupOrder: 0, SortOrder: 5, Default: "true"},

	// Inventory
	{EnvVar: "FLEET_GROUP", Label: "Group Filter", Type: "string", Group: "Inventory", GroupOrder: 1, SortOrder: 0, Default: "+", Description: "Sparkplug group to track. Use '+' to watch all groups."},
	{EnvVar: "FLEET_STALE_SECONDS", Label: "Stale Threshold (seconds)", Type: "number", Group: "Inventory", GroupOrder: 1, SortOrder: 1, Default: "90", Description: "Nodes with no activity for this long are marked stale."},
}

func loadConfig(b bus.Bus, moduleID string) Config {
	get := func(envVar, def string) string {
		if b != nil {
			if data, _, err := b.KVGet(topics.BucketTentacleConfig, "fleet."+envVar); err == nil && len(data) > 0 {
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
		BrokerURL:    get("FLEET_BROKER_URL", "tcp://localhost:1883"),
		ClientID:     get("FLEET_CLIENT_ID", moduleID),
		Username:     get("FLEET_USERNAME", ""),
		Password:     get("FLEET_PASSWORD", ""),
		GroupFilter:  get("FLEET_GROUP", "+"),
		KeepAlive:    getInt("FLEET_KEEPALIVE", 30),
		CleanSession: getBool("FLEET_CLEAN_SESSION", true),
		StaleSeconds: getInt("FLEET_STALE_SECONDS", 90),
	}
}

func saveConfig(b bus.Bus, cfg Config) {
	put := func(envVar, value string) {
		_, _ = b.KVPut(topics.BucketTentacleConfig, "fleet."+envVar, []byte(value))
	}
	put("FLEET_BROKER_URL", cfg.BrokerURL)
	put("FLEET_CLIENT_ID", cfg.ClientID)
	put("FLEET_USERNAME", cfg.Username)
	put("FLEET_PASSWORD", cfg.Password)
	put("FLEET_GROUP", cfg.GroupFilter)
	put("FLEET_KEEPALIVE", strconv.Itoa(cfg.KeepAlive))
	put("FLEET_CLEAN_SESSION", strconv.FormatBool(cfg.CleanSession))
	put("FLEET_STALE_SECONDS", strconv.Itoa(cfg.StaleSeconds))
}
