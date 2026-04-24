//go:build mqttbroker || mantle || all

package mqttbroker

import (
	"os"
	"strconv"
)

type Config struct {
	ListenAddr string // e.g. ":1883"
	ID         string // listener ID
	AllowAll   bool   // if true, accept any client (no auth)
	Username   string // used when !AllowAll
	Password   string // used when !AllowAll
}

func loadConfig(moduleID string) Config {
	cfg := Config{
		ListenAddr: envOr("MQTT_BROKER_LISTEN", ":1883"),
		ID:         envOr("MQTT_BROKER_LISTENER_ID", moduleID+"-tcp"),
		AllowAll:   envBool("MQTT_BROKER_ALLOW_ALL", true),
		Username:   os.Getenv("MQTT_BROKER_USERNAME"),
		Password:   os.Getenv("MQTT_BROKER_PASSWORD"),
	}
	return cfg
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
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
