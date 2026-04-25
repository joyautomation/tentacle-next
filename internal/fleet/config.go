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
	GroupFilter  string
	StaleSeconds int // node considered stale if LastSeen older than this
}

var configSchema = []config.FieldDef{
	{EnvVar: "FLEET_GROUP", Label: "Group Filter", Type: "string", Group: "Inventory", GroupOrder: 0, SortOrder: 0, Default: "+", Description: "Sparkplug group to track. Use '+' to watch all groups."},
	{EnvVar: "FLEET_STALE_SECONDS", Label: "Stale Threshold (seconds)", Type: "number", Group: "Inventory", GroupOrder: 0, SortOrder: 1, Default: "90", Description: "Nodes with no activity for this long are marked stale."},
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

	return Config{
		GroupFilter:  get("FLEET_GROUP", "+"),
		StaleSeconds: getInt("FLEET_STALE_SECONDS", 90),
	}
}

func saveConfig(b bus.Bus, cfg Config) {
	put := func(envVar, value string) {
		_, _ = b.KVPut(topics.BucketTentacleConfig, "fleet."+envVar, []byte(value))
	}
	put("FLEET_GROUP", cfg.GroupFilter)
	put("FLEET_STALE_SECONDS", strconv.Itoa(cfg.StaleSeconds))
}
