//go:build sparkplughost || mantle || all

package sparkplughost

import (
	"os"
	"strconv"
)

type Config struct {
	BrokerURL    string // e.g. tcp://localhost:1883
	ClientID     string
	Username     string
	Password     string
	HostAppID    string // Primary Host Application ID (used in birth/death certificate topic)
	GroupFilter  string // "+" matches all groups; specific group otherwise
	SharedGroup  string // if set, use $share/<group>/spBv1.0/... for HA. Empty = no sharing.
	KeepAlive    int    // seconds
	CleanSession bool
}

func loadConfig(moduleID string) Config {
	return Config{
		BrokerURL:    envOr("SPARKPLUG_HOST_BROKER_URL", "tcp://localhost:1883"),
		ClientID:     envOr("SPARKPLUG_HOST_CLIENT_ID", moduleID),
		Username:     os.Getenv("SPARKPLUG_HOST_USERNAME"),
		Password:     os.Getenv("SPARKPLUG_HOST_PASSWORD"),
		HostAppID:    envOr("SPARKPLUG_HOST_APP_ID", "MantleHost"),
		GroupFilter:  envOr("SPARKPLUG_HOST_GROUP", "+"),
		SharedGroup:  os.Getenv("SPARKPLUG_HOST_SHARED_GROUP"), // e.g. "mantle" → $share/mantle/...
		KeepAlive:    envInt("SPARKPLUG_HOST_KEEPALIVE", 30),
		CleanSession: envBool("SPARKPLUG_HOST_CLEAN_SESSION", true),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
