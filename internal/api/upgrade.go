//go:build api || all

package api

import (
	"errors"
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

// handleListReleases returns all available releases from GitHub (cached).
// GET /api/v1/system/releases
func (m *Module) handleListReleases(w http.ResponseWriter, _ *http.Request) {
	ghOrg := os.Getenv("TENTACLE_GH_ORG")
	ghToken := os.Getenv("GITHUB_TOKEN")

	resp, err := selfupgrade.ListReleases(ghOrg, ghToken)
	if err != nil {
		status := http.StatusBadGateway
		var offline *selfupgrade.OfflineError
		if errors.As(err, &offline) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
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

	// If no version specified, resolve latest from release list.
	if body.Version == "" {
		ghOrg := os.Getenv("TENTACLE_GH_ORG")
		ghToken := os.Getenv("GITHUB_TOKEN")
		resp, err := selfupgrade.ListReleases(ghOrg, ghToken)
		if err != nil || len(resp.Releases) == 0 {
			msg := "failed to resolve latest version"
			if err != nil {
				msg += ": " + err.Error()
			}
			writeError(w, http.StatusBadGateway, msg)
			return
		}
		body.Version = resp.Releases[0].Version
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
