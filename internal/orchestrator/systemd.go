//go:build orchestrator || all

package orchestrator

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	otypes "github.com/joyautomation/tentacle/internal/types"
)

// unitName returns the systemd unit name for a module.
func unitName(modID string) string {
	return modID + ".service"
}

// runCmd executes a command and returns stdout, stderr, and success.
// Ensures a full PATH is available so system binaries are always found.
func runCmd(name string, args ...string) (stdout, stderr string, ok bool) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), err == nil
}

// unitExists checks whether a systemd unit is actually loaded (not just "inactive").
func unitExists(modID string) bool {
	stdout, _, _ := runCmd("systemctl", "show", "--property=LoadState", unitName(modID))
	// LoadState=loaded means it exists; LoadState=not-found means it doesn't
	return strings.Contains(stdout, "LoadState=loaded")
}

// getSystemdState checks systemctl is-active for a module.
func getSystemdState(modID string) string {
	stdout, _, _ := runCmd("systemctl", "is-active", unitName(modID))
	switch stdout {
	case "active", "inactive", "failed", "activating", "deactivating":
		return stdout
	default:
		return "not-found"
	}
}

// normalizeSystemdState maps transient states to their stable equivalents.
func normalizeSystemdState(state string) string {
	switch state {
	case "active", "inactive", "failed", "not-found":
		return state
	case "activating":
		return "active"
	case "deactivating":
		return "inactive"
	default:
		return "not-found"
	}
}

func systemctlStart(modID string) bool {
	slog.Info("systemd: starting unit", "unit", unitName(modID))
	_, stderr, ok := runCmd("systemctl", "start", unitName(modID))
	if !ok {
		slog.Error("systemd: failed to start", "unit", unitName(modID), "stderr", stderr)
	}
	return ok
}

func systemctlStop(modID string) bool {
	slog.Info("systemd: stopping unit", "unit", unitName(modID))
	_, stderr, ok := runCmd("systemctl", "stop", unitName(modID))
	if !ok {
		slog.Error("systemd: failed to stop", "unit", unitName(modID), "stderr", stderr)
	}
	return ok
}

func systemctlRestart(modID string) bool {
	slog.Info("systemd: restarting unit", "unit", unitName(modID))
	_, stderr, ok := runCmd("systemctl", "restart", unitName(modID))
	if !ok {
		slog.Error("systemd: failed to restart", "unit", unitName(modID), "stderr", stderr)
	}
	return ok
}

func systemctlDaemonReload() bool {
	slog.Debug("systemd: reloading daemon")
	_, stderr, ok := runCmd("systemctl", "daemon-reload")
	if !ok {
		slog.Error("systemd: failed to reload", "stderr", stderr)
	}
	return ok
}

func systemctlEnable(modID string) bool {
	_, stderr, ok := runCmd("systemctl", "enable", unitName(modID))
	if !ok {
		slog.Warn("systemd: failed to enable", "unit", unitName(modID), "stderr", stderr)
	}
	return ok
}

// writeSystemdUnit generates and writes a systemd unit file for a module.
func writeSystemdUnit(entry *otypes.ModuleRegistryEntry, version string, config *OrchestratorConfig, moduleConfig ...map[string]string) bool {
	unitPath := config.SystemdDir + "/" + unitName(entry.ModuleID)
	envFile := config.ConfigDir + "/tentacle.env"
	natsUnit := config.NatsUnitName

	// Dependencies -- only add if the nats unit actually exists on this system
	natsAvailable := natsUnit != "" && unitExists(natsUnit)

	var after, requires string
	if entry.ModuleID == "tentacle-web" {
		after = "tentacle-graphql.service"
		requires = "tentacle-graphql.service"
	} else if natsAvailable {
		after = natsUnit + ".service"
		requires = natsUnit + ".service"
	}

	var execStart, workingDir, denoDir string
	denoPath := findDeno(config)

	switch entry.Runtime {
	case "go":
		execStart = config.BinDir + "/" + entry.ModuleID
	case "deno":
		execStart = denoPath + " run -A main.ts"
		workingDir = config.ServicesDir + "/" + entry.Repo
		denoDir = fmt.Sprintf("%s/deno/versions/%s/%s", config.CacheDir, entry.ModuleID, version)
	case "deno-web":
		execStart = denoPath + " run -A build/index.js"
		workingDir = config.ServicesDir + "/" + entry.Repo
		denoDir = fmt.Sprintf("%s/deno/versions/%s/%s", config.CacheDir, entry.ModuleID, version)
	default:
		slog.Error("systemd: unknown runtime", "runtime", entry.Runtime, "moduleId", entry.ModuleID)
		return false
	}

	// Build environment lines
	var envLines []string
	if denoDir != "" {
		envLines = append(envLines, "Environment=DENO_DIR="+denoDir)
	}
	if entry.ExtraEnv != "" {
		envLines = append(envLines, "Environment="+entry.ExtraEnv)
	}

	// Add module config from KV as environment variables
	if len(moduleConfig) > 0 && moduleConfig[0] != nil {
		for envVar, value := range moduleConfig[0] {
			envLines = append(envLines, fmt.Sprintf("Environment=%s=%s", envVar, value))
		}
	}
	// Add defaults for any config fields not set in KV
	for _, cf := range entry.RequiredConfig {
		if cf.Default != "" {
			alreadySet := false
			if len(moduleConfig) > 0 && moduleConfig[0] != nil {
				if _, ok := moduleConfig[0][cf.EnvVar]; ok {
					alreadySet = true
				}
			}
			if !alreadySet {
				envLines = append(envLines, fmt.Sprintf("Environment=%s=%s", cf.EnvVar, cf.Default))
			}
		}
	}

	var envSection string
	if len(envLines) > 0 {
		envSection = strings.Join(envLines, "\n") + "\n"
	}

	var workingDirLine string
	if workingDir != "" {
		workingDirLine = "WorkingDirectory=" + workingDir + "\n"
	}

	var depLines string
	if after != "" {
		depLines += "After=" + after + "\n"
	}
	if requires != "" {
		depLines += "Requires=" + requires + "\n"
	}

	unit := fmt.Sprintf(`[Unit]
Description=Tentacle %s
%s
[Service]
Type=simple
EnvironmentFile=-%s
%s%sExecStart=%s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

[Install]
WantedBy=multi-user.target
`, entry.ModuleID, depLines, envFile, envSection, workingDirLine, execStart, entry.ModuleID)

	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		slog.Error("systemd: failed to write unit", "path", unitPath, "error", err)
		return false
	}
	slog.Debug("systemd: wrote unit file", "path", unitPath)
	return true
}
