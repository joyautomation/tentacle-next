//go:build api || all

package api

import (
	"net/http"
	"os"

	"github.com/joyautomation/tentacle/internal/selfupgrade"
	"github.com/joyautomation/tentacle/internal/service"
	"github.com/joyautomation/tentacle/internal/version"
)

// handleGetVersion returns the running binary's version info.
// GET /api/v1/system/version
func (m *Module) handleGetVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": version.Version,
		"commit":  version.Commit,
		"date":    version.Date,
	})
}

// handleCheckUpdates queries GitHub for a newer release.
// GET /api/v1/system/updates
func (m *Module) handleCheckUpdates(w http.ResponseWriter, _ *http.Request) {
	ghOrg := os.Getenv("TENTACLE_GH_ORG")
	ghToken := os.Getenv("GITHUB_TOKEN")

	info, err := selfupgrade.CheckForUpdate(ghOrg, ghToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to check for updates: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, info)
}

// handleUpgrade downloads a new release and restarts via systemd.
// POST /api/v1/system/upgrade
func (m *Module) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if m.mode != "systemd" {
		writeError(w, http.StatusBadRequest, "upgrade requires running as a systemd service")
		return
	}

	current := selfupgrade.GetStatus()
	if current.State != "idle" && current.State != "failed" {
		writeError(w, http.StatusConflict, "upgrade already in progress: "+current.State)
		return
	}

	var body struct {
		Version string `json:"version"`
	}
	if r.ContentLength > 0 {
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	// If no version specified, resolve latest.
	if body.Version == "" {
		ghOrg := os.Getenv("TENTACLE_GH_ORG")
		ghToken := os.Getenv("GITHUB_TOKEN")
		info, err := selfupgrade.CheckForUpdate(ghOrg, ghToken)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to resolve latest version: "+err.Error())
			return
		}
		if !info.UpdateAvailable {
			writeError(w, http.StatusBadRequest, "already running the latest version")
			return
		}
		body.Version = info.LatestVersion
	}

	ghOrg := os.Getenv("TENTACLE_GH_ORG")
	ghToken := os.Getenv("GITHUB_TOKEN")

	go selfupgrade.PerformUpgrade(body.Version, ghOrg, ghToken, service.BinaryPath, m.log)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "upgrading",
		"version": body.Version,
	})
}

// handleUpgradeStatus returns the current upgrade progress.
// GET /api/v1/system/upgrade/status
func (m *Module) handleUpgradeStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, selfupgrade.GetStatus())
}
