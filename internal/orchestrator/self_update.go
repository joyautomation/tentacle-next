//go:build orchestrator || all

package orchestrator

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	otypes "github.com/joyautomation/tentacle/internal/types"
)

const selfModuleID = "tentacle-orchestrator"

var selfEntry = otypes.ModuleRegistryEntry{
	Repo:        "tentacle-orchestrator-go",
	ModuleID:    selfModuleID,
	Description: "Service orchestrator",
	Category:    "core",
	Runtime:     "go",
}

// selfUpdate downloads a new version and spawns a restart script.
// Since we're a Go binary, the download is a single binary, and the
// restart script atomically swaps the symlink and restarts via systemd.
func selfUpdate(version string, config *OrchestratorConfig, log *slog.Logger) bool {
	// Download the new version if not already present
	if !isVersionInstalled(selfModuleID, version, config) {
		log.Info("self-update: downloading new version", "version", version)
		if !installVersion(&selfEntry, version, config, log) {
			log.Error("self-update: failed to download", "version", version)
			return false
		}
	}

	// Update the symlink
	if !updateSymlink(&selfEntry, version, config, log) {
		log.Error("self-update: failed to update symlink")
		return false
	}

	// Write and execute an updater script that restarts us via systemd
	scriptPath := config.BinDir + "/update-orchestrator.sh"
	script := fmt.Sprintf(`#!/bin/bash
set -e
sleep 1
systemctl daemon-reload
systemctl restart %s
rm -f "$0"
`, unitName(selfModuleID))

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		log.Error("self-update: failed to write update script", "error", err)
		return false
	}

	log.Info("self-update: spawning restart script", "version", version)

	// Fire-and-forget
	cmd := exec.Command("bash", scriptPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		log.Error("self-update: failed to execute script", "error", err)
		return false
	}

	return true
}
