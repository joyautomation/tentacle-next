//go:build orchestrator || all

package orchestrator

import (
	"os"
	"strconv"
)

// OrchestratorConfig holds all configuration, loaded from environment variables.
type OrchestratorConfig struct {
	NatsServers         string
	NatsUser            string
	NatsPass            string
	NatsToken           string
	InstallDir          string
	BinDir              string
	ServicesDir         string
	VersionsDir         string
	CacheDir            string
	ConfigDir           string
	SystemdDir          string
	NatsUnitName        string
	GhOrg               string
	GhToken             string
	ReconcileIntervalMs int
	LatestCacheTtlMs    int
}

func loadConfig() *OrchestratorConfig {
	installDir := envOrDefault("TENTACLE_INSTALL_DIR", "/opt/tentacle")

	return &OrchestratorConfig{
		NatsServers:         envOrDefault("NATS_SERVERS", "nats://localhost:4222"),
		NatsUser:            os.Getenv("NATS_USER"),
		NatsPass:            os.Getenv("NATS_PASS"),
		NatsToken:           os.Getenv("NATS_TOKEN"),
		InstallDir:          installDir,
		BinDir:              installDir + "/bin",
		ServicesDir:         installDir + "/services",
		VersionsDir:         installDir + "/versions",
		CacheDir:            installDir + "/cache",
		ConfigDir:           installDir + "/config",
		SystemdDir:          envOrDefault("TENTACLE_SYSTEMD_DIR", "/etc/systemd/system"),
		NatsUnitName:        envOrDefault("TENTACLE_NATS_UNIT", "tentacle-nats"),
		GhOrg:               envOrDefault("TENTACLE_GH_ORG", "joyautomation"),
		GhToken:             os.Getenv("GITHUB_TOKEN"),
		ReconcileIntervalMs: envOrDefaultInt("TENTACLE_RECONCILE_INTERVAL", 30000),
		LatestCacheTtlMs:    envOrDefaultInt("TENTACLE_LATEST_CACHE_TTL", 300000),
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

// statusOpts holds options for reporting module status.
type statusOpts struct {
	InstalledVersions []string
	ActiveVersion     string
	SystemdState      string
	ReconcileState    string
	LastError         string
}
