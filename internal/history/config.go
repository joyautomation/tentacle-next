//go:build history || all

package history

import (
	"fmt"
	"os"
	"strconv"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// loadConfigFromEnv populates a HistoryConfig from environment variables.
// Prefers HISTORY_DB_* (canonical) and falls back to TENTACLE_DB_* for backward compatibility.
func loadConfigFromEnv() itypes.HistoryConfig {
	cfg := itypes.HistoryConfig{
		DBHost:        firstEnv([]string{"HISTORY_DB_HOST", "TENTACLE_DB_HOST"}, "localhost"),
		DBPort:        firstEnvInt([]string{"HISTORY_DB_PORT", "TENTACLE_DB_PORT"}, 5432),
		DBUser:        firstEnv([]string{"HISTORY_DB_USER", "TENTACLE_DB_USER"}, "postgres"),
		DBPassword:    firstEnv([]string{"HISTORY_DB_PASSWORD", "TENTACLE_DB_PASSWORD"}, "postgres"),
		DBName:        firstEnv([]string{"HISTORY_DB_NAME", "TENTACLE_DB_NAME"}, "tentacle"),
		DBSSLMode:     "disable",
		GroupID:       envOrDefault("MQTT_GROUP_ID", "TentacleGroup"),
		EnableHyper:   envBoolOrDefault("TENTACLE_HISTORIAN_ENABLED", true),
		RetentionDays: envIntOrDefault("TENTACLE_RETENTION_DAYS", 30),
	}

	if envBoolOrDefault("HISTORY_DB_SSL", envBoolOrDefault("TENTACLE_DB_SSL", false)) {
		cfg.DBSSLMode = "require"
	}

	return cfg
}

// firstEnv returns the first non-empty env var value from keys, or default.
func firstEnv(keys []string, defaultVal string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return defaultVal
}

// firstEnvInt returns the first parseable int env var value from keys, or default.
func firstEnvInt(keys []string, defaultVal int) int {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	return defaultVal
}

// connString builds a PostgreSQL connection string from the config.
func connString(cfg itypes.HistoryConfig) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode,
	)
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envIntOrDefault(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func envBoolOrDefault(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}
