//go:build history || all

package history

import (
	"fmt"
	"os"
	"strconv"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// loadConfigFromEnv populates a HistoryConfig from environment variables.
func loadConfigFromEnv() itypes.HistoryConfig {
	cfg := itypes.HistoryConfig{
		DBHost:        envOrDefault("TENTACLE_DB_HOST", "localhost"),
		DBPort:        envIntOrDefault("TENTACLE_DB_PORT", 5432),
		DBUser:        envOrDefault("TENTACLE_DB_USER", "postgres"),
		DBPassword:    envOrDefault("TENTACLE_DB_PASSWORD", "postgres"),
		DBName:        envOrDefault("TENTACLE_DB_NAME", "tentacle"),
		DBSSLMode:     "disable",
		EnableHyper:   envBoolOrDefault("TENTACLE_HISTORIAN_ENABLED", true),
		RetentionDays: envIntOrDefault("TENTACLE_RETENTION_DAYS", 30),
	}

	if envBoolOrDefault("TENTACLE_DB_SSL", false) {
		cfg.DBSSLMode = "require"
	}

	return cfg
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
