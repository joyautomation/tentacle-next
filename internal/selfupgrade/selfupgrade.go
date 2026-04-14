package selfupgrade

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/version"
)

const (
	defaultGhOrg  = "joyautomation"
	defaultRepo   = "tentacle-next"
	cacheTTL      = 5 * time.Minute
	binaryName    = "tentacle"
	httpTimeout   = 120 * time.Second
	unitName      = "tentacle.service"
)

// UpdateInfo describes an available update.
type UpdateInfo struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	CheckedAt       int64  `json:"checkedAt"`
}

// UpgradeStatus tracks the progress of an in-flight upgrade.
type UpgradeStatus struct {
	State   string `json:"state"`   // "idle", "downloading", "extracting", "replacing", "restarting", "failed"
	Error   string `json:"error,omitempty"`
	Version string `json:"version,omitempty"`
}

var (
	statusMu sync.Mutex
	status   = UpgradeStatus{State: "idle"}

	cacheMu    sync.RWMutex
	cachedInfo *UpdateInfo
	cacheTime  time.Time
)

// GetStatus returns the current upgrade status.
func GetStatus() UpgradeStatus {
	statusMu.Lock()
	defer statusMu.Unlock()
	return status
}

func setStatus(state, version, errMsg string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	status = UpgradeStatus{State: state, Version: version, Error: errMsg}
}

// CheckForUpdate queries the GitHub releases API for the latest version.
func CheckForUpdate(ghOrg, ghToken string) (*UpdateInfo, error) {
	if ghOrg == "" {
		ghOrg = defaultGhOrg
	}

	cacheMu.RLock()
	if cachedInfo != nil && time.Since(cacheTime) < cacheTTL {
		info := *cachedInfo
		cacheMu.RUnlock()
		return &info, nil
	}
	cacheMu.RUnlock()

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", ghOrg, defaultRepo)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "tentacle-selfupgrade")
	if ghToken != "" {
		req.Header.Set("Authorization", "Bearer "+ghToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(body))
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(version.Version, "v")

	info := &UpdateInfo{
		CurrentVersion:  version.Version,
		LatestVersion:   latest,
		UpdateAvailable: compareSemver(latest, current) > 0,
		ReleaseURL:      release.HTMLURL,
		CheckedAt:       time.Now().UnixMilli(),
	}

	cacheMu.Lock()
	cachedInfo = info
	cacheTime = time.Now()
	cacheMu.Unlock()

	return info, nil
}

// PerformUpgrade downloads the target version and replaces the running binary.
// It must be called in a goroutine — after success it spawns a systemd restart script.
func PerformUpgrade(targetVersion, ghOrg, ghToken, binaryPath string, log *slog.Logger) {
	if ghOrg == "" {
		ghOrg = defaultGhOrg
	}

	setStatus("downloading", targetVersion, "")
	log.Info("selfupgrade: starting", "version", targetVersion)

	// Build download URL for the tar.gz archive.
	arch := getArch()
	archiveName := fmt.Sprintf("%s_%s_linux_%s.tar.gz", binaryName, targetVersion, arch)
	downloadURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/v%s/%s",
		ghOrg, defaultRepo, targetVersion, archiveName)

	// Download to a temp file.
	tmpArchive, err := os.CreateTemp("", "tentacle-upgrade-*.tar.gz")
	if err != nil {
		setStatus("failed", targetVersion, "create temp file: "+err.Error())
		return
	}
	defer os.Remove(tmpArchive.Name())
	defer tmpArchive.Close()

	if err := downloadFile(downloadURL, ghToken, tmpArchive, log); err != nil {
		setStatus("failed", targetVersion, "download: "+err.Error())
		return
	}
	tmpArchive.Close()

	// Extract the tentacle binary from the archive.
	setStatus("extracting", targetVersion, "")

	// Create temp file in the same directory as binaryPath for atomic rename.
	tmpBinary, err := os.CreateTemp("/usr/local/bin", ".tentacle-upgrade-*")
	if err != nil {
		setStatus("failed", targetVersion, "create temp binary: "+err.Error())
		return
	}
	tmpBinaryPath := tmpBinary.Name()
	tmpBinary.Close()
	defer func() {
		// Clean up temp binary on failure — on success it's been renamed.
		os.Remove(tmpBinaryPath)
	}()

	if err := extractBinaryFromTarGz(tmpArchive.Name(), binaryName, tmpBinaryPath); err != nil {
		setStatus("failed", targetVersion, "extract: "+err.Error())
		return
	}

	if err := os.Chmod(tmpBinaryPath, 0755); err != nil {
		setStatus("failed", targetVersion, "chmod: "+err.Error())
		return
	}

	// Atomic replace.
	setStatus("replacing", targetVersion, "")
	if err := os.Rename(tmpBinaryPath, binaryPath); err != nil {
		setStatus("failed", targetVersion, "replace binary: "+err.Error())
		return
	}

	// Invalidate the update cache so the next check shows the new version.
	cacheMu.Lock()
	cachedInfo = nil
	cacheMu.Unlock()

	// Spawn restart script.
	setStatus("restarting", targetVersion, "")
	if err := spawnRestartScript(log); err != nil {
		setStatus("failed", targetVersion, "restart: "+err.Error())
		return
	}

	log.Info("selfupgrade: restart script launched", "version", targetVersion)
}

func downloadFile(url, ghToken string, dest *os.File, log *slog.Logger) error {
	log.Info("selfupgrade: downloading", "url", url)

	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "tentacle-selfupgrade")
	req.Header.Set("Accept", "application/octet-stream")
	if ghToken != "" {
		req.Header.Set("Authorization", "Bearer "+ghToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	n, err := io.Copy(dest, resp.Body)
	if err != nil {
		return err
	}

	log.Info("selfupgrade: download complete", "bytes", n)
	return nil
}

func extractBinaryFromTarGz(archivePath, targetName, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("binary %q not found in archive", targetName)
		}
		if err != nil {
			return err
		}

		// The archive may contain paths like "tentacle" or "./tentacle".
		name := header.Name
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if name != targetName || header.Typeflag != tar.TypeReg {
			continue
		}

		out, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
}

func spawnRestartScript(log *slog.Logger) error {
	script := fmt.Sprintf(`#!/bin/bash
set -e
sleep 1
systemctl daemon-reload
systemctl restart %s
rm -f "$0"
`, unitName)

	scriptPath := "/tmp/tentacle-selfupgrade.sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return err
	}

	log.Info("selfupgrade: restart script spawned")
	return nil
}

func getArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

func compareSemver(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}
	for i := 0; i < maxLen; i++ {
		var va, vb int
		if i < len(partsA) {
			va, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			vb, _ = strconv.Atoi(partsB[i])
		}
		if va != vb {
			return va - vb
		}
	}
	return 0
}
