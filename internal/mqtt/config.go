//go:build mqtt || all

package mqtt

import (
	"encoding/json"
	"log/slog"
	"os"
	"strconv"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// loadConfig loads the MQTT bridge config from NATS KV, falling back to env vars.
func loadConfig(b bus.Bus) itypes.MqttBridgeConfig {
	cfg := itypes.MqttBridgeConfig{
		BrokerURL:        envOrDefault("MQTT_BROKER_URL", "tcp://localhost:1883"),
		ClientID:         envOrDefault("MQTT_CLIENT_ID", "tentacle-mqtt"),
		GroupID:          envOrDefault("MQTT_GROUP_ID", "TentacleGroup"),
		EdgeNode:         envOrDefault("MQTT_EDGE_NODE", "EdgeNode1"),
		DeviceID:         envOrDefault("MQTT_DEVICE_ID", ""),
		Username:         envOrDefault("MQTT_USERNAME", ""),
		Password:         envOrDefault("MQTT_PASSWORD", ""),
		Keepalive:        envOrDefaultInt("MQTT_KEEPALIVE", 30),
		PrimaryHostID:    envOrDefault("MQTT_PRIMARY_HOST_ID", ""),
		UseTemplates:     envOrDefaultBool("MQTT_USE_TEMPLATES", true),
		StoreForwardMax:  envOrDefaultInt("MQTT_SF_MAX_RECORDS", 10000),
		StoreForwardSize: int64(envOrDefaultInt("MQTT_SF_MAX_MB", 50)) * 1024 * 1024,
		DrainRate:        envOrDefaultInt("MQTT_SF_DRAIN_RATE", 100),
		TLSEnabled:       envOrDefaultBool("MQTT_TLS_ENABLED", false),
		TLSCertPath:      envOrDefault("MQTT_TLS_CERT_PATH", ""),
		TLSKeyPath:       envOrDefault("MQTT_TLS_KEY_PATH", ""),
		TLSCaPath:        envOrDefault("MQTT_TLS_CA_PATH", ""),
	}

	// Try to load from NATS KV (overrides env vars)
	if b != nil {
		if data, _, err := b.KVGet(topics.BucketTentacleConfig, "mqtt.config"); err == nil {
			var kvCfg itypes.MqttBridgeConfig
			if err := json.Unmarshal(data, &kvCfg); err == nil {
				slog.Info("mqtt: loaded config from KV")
				return kvCfg
			}
			slog.Warn("mqtt: failed to parse KV config, using env vars", "error", err)
		}
	}

	return cfg
}

// saveConfig persists the current config to NATS KV.
func saveConfig(b bus.Bus, cfg *itypes.MqttBridgeConfig) {
	data, err := json.Marshal(cfg)
	if err != nil {
		slog.Warn("mqtt: failed to marshal config", "error", err)
		return
	}
	if _, err := b.KVPut(topics.BucketTentacleConfig, "mqtt.config", data); err != nil {
		slog.Warn("mqtt: failed to save config to KV", "error", err)
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envOrDefaultInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func envOrDefaultBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}
