//go:build mqtt || all

package mqtt

import (
	"os"
	"strconv"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// configSchema defines the MQTT module's configuration fields for the settings UI.
var configSchema = []config.FieldDef{
	// Connection group
	{EnvVar: "MQTT_BROKER_URL", Label: "Broker URL", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 0, Default: "tcp://localhost:1883"},
	{EnvVar: "MQTT_CLIENT_ID", Label: "Client ID", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 1, Default: "tentacle-mqtt"},
	{EnvVar: "MQTT_GROUP_ID", Label: "Group ID", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 2, Default: "TentacleGroup"},
	{EnvVar: "MQTT_EDGE_NODE", Label: "Node ID", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 3, Default: "EdgeNode1"},
	{EnvVar: "MQTT_USERNAME", Label: "Username", Type: "string", Group: "Connection", GroupOrder: 0, SortOrder: 4},
	{EnvVar: "MQTT_PASSWORD", Label: "Password", Type: "password", Group: "Connection", GroupOrder: 0, SortOrder: 5},
	{EnvVar: "MQTT_KEEPALIVE", Label: "Keep Alive (seconds)", Type: "number", Group: "Connection", GroupOrder: 0, SortOrder: 6, Default: "30"},
	// Sparkplug B group
	{EnvVar: "MQTT_USE_TEMPLATES", Label: "Use Templates", Type: "boolean", Group: "Sparkplug B", GroupOrder: 1, SortOrder: 0, Default: "true"},
	// Store & Forward group
	{EnvVar: "MQTT_PRIMARY_HOST_ID", Label: "Primary Host ID", Type: "string", Group: "Store & Forward", GroupOrder: 2, SortOrder: 0},
	{EnvVar: "MQTT_SF_MAX_MB", Label: "Max Buffer (MB)", Type: "number", Group: "Store & Forward", GroupOrder: 2, SortOrder: 1, Default: "50"},
	{EnvVar: "MQTT_SF_MAX_RECORDS", Label: "Max Records", Type: "number", Group: "Store & Forward", GroupOrder: 2, SortOrder: 2, Default: "10000"},
	{EnvVar: "MQTT_SF_DRAIN_RATE", Label: "Drain Rate (records/sec)", Type: "number", Group: "Store & Forward", GroupOrder: 2, SortOrder: 3, Default: "100"},
	// TLS group
	{EnvVar: "MQTT_TLS_ENABLED", Label: "TLS Enabled", Type: "boolean", Group: "TLS", GroupOrder: 3, SortOrder: 0, Default: "false"},
	{EnvVar: "MQTT_TLS_CERT_PATH", Label: "TLS Certificate Path", Type: "string", Group: "TLS", GroupOrder: 3, SortOrder: 1, DependsOn: "MQTT_TLS_ENABLED"},
	{EnvVar: "MQTT_TLS_KEY_PATH", Label: "TLS Key Path", Type: "string", Group: "TLS", GroupOrder: 3, SortOrder: 2, DependsOn: "MQTT_TLS_ENABLED"},
	{EnvVar: "MQTT_TLS_CA_PATH", Label: "TLS CA Path", Type: "string", Group: "TLS", GroupOrder: 3, SortOrder: 3, DependsOn: "MQTT_TLS_ENABLED"},
}

// saveConfig persists the current config as individual KV keys (mqtt.MQTT_BROKER_URL etc.)
// so the web UI settings page can read and display them.
func saveConfig(b bus.Bus, cfg *itypes.MqttBridgeConfig) {
	put := func(envVar, value string) {
		if _, err := b.KVPut(topics.BucketTentacleConfig, "mqtt."+envVar, []byte(value)); err != nil {
			// Ignore errors — best effort
		}
	}
	put("MQTT_BROKER_URL", cfg.BrokerURL)
	put("MQTT_CLIENT_ID", cfg.ClientID)
	put("MQTT_GROUP_ID", cfg.GroupID)
	put("MQTT_EDGE_NODE", cfg.EdgeNode)
	put("MQTT_PER_SOURCE_DEVICE", strconv.FormatBool(cfg.PerSourceDevice))
	put("MQTT_USERNAME", cfg.Username)
	put("MQTT_PASSWORD", cfg.Password)
	put("MQTT_KEEPALIVE", strconv.Itoa(cfg.Keepalive))
	put("MQTT_PRIMARY_HOST_ID", cfg.PrimaryHostID)
	put("MQTT_USE_TEMPLATES", strconv.FormatBool(cfg.UseTemplates))
	put("MQTT_SF_MAX_RECORDS", strconv.Itoa(cfg.StoreForwardMax))
	put("MQTT_SF_MAX_MB", strconv.Itoa(int(cfg.StoreForwardSize/(1024*1024))))
	put("MQTT_SF_DRAIN_RATE", strconv.Itoa(cfg.DrainRate))
	put("MQTT_TLS_ENABLED", strconv.FormatBool(cfg.TLSEnabled))
	put("MQTT_TLS_CERT_PATH", cfg.TLSCertPath)
	put("MQTT_TLS_KEY_PATH", cfg.TLSKeyPath)
	put("MQTT_TLS_CA_PATH", cfg.TLSCaPath)
}

// loadConfig loads the MQTT bridge config.
// Priority: KV individual keys (mqtt.MQTT_BROKER_URL etc.) > env vars > defaults.
func loadConfig(b bus.Bus) itypes.MqttBridgeConfig {
	get := func(envVar, defaultVal string) string {
		// Try KV first (written by the web UI settings page)
		if b != nil {
			if data, _, err := b.KVGet(topics.BucketTentacleConfig, "mqtt."+envVar); err == nil && len(data) > 0 {
				return string(data)
			}
		}
		// Fall back to env var
		if v := os.Getenv(envVar); v != "" {
			return v
		}
		return defaultVal
	}

	getInt := func(envVar string, defaultVal int) int {
		s := get(envVar, "")
		if s == "" {
			return defaultVal
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		return defaultVal
	}

	getBool := func(envVar string, defaultVal bool) bool {
		s := get(envVar, "")
		if s == "" {
			return defaultVal
		}
		if b, err := strconv.ParseBool(s); err == nil {
			return b
		}
		return defaultVal
	}

	return itypes.MqttBridgeConfig{
		BrokerURL:        get("MQTT_BROKER_URL", "tcp://localhost:1883"),
		ClientID:         get("MQTT_CLIENT_ID", "tentacle-mqtt"),
		GroupID:          get("MQTT_GROUP_ID", "TentacleGroup"),
		EdgeNode:         get("MQTT_EDGE_NODE", "EdgeNode1"),
		DeviceID:         "", // Always use source device IDs
		PerSourceDevice:  getBool("MQTT_PER_SOURCE_DEVICE", false),
		Username:         get("MQTT_USERNAME", ""),
		Password:         get("MQTT_PASSWORD", ""),
		Keepalive:        getInt("MQTT_KEEPALIVE", 30),
		PrimaryHostID:    get("MQTT_PRIMARY_HOST_ID", ""),
		UseTemplates:     getBool("MQTT_USE_TEMPLATES", true),
		StoreForwardMax:  getInt("MQTT_SF_MAX_RECORDS", 10000),
		StoreForwardSize: int64(getInt("MQTT_SF_MAX_MB", 50)) * 1024 * 1024,
		DrainRate:        getInt("MQTT_SF_DRAIN_RATE", 100),
		TLSEnabled:      getBool("MQTT_TLS_ENABLED", false),
		TLSCertPath:     get("MQTT_TLS_CERT_PATH", ""),
		TLSKeyPath:      get("MQTT_TLS_KEY_PATH", ""),
		TLSCaPath:       get("MQTT_TLS_CA_PATH", ""),
		BdSeqFile:       get("MQTT_BDSEQ_FILE", ""),
	}
}
