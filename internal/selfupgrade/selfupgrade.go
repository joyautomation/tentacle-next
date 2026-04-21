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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/paths"
	"github.com/joyautomation/tentacle/internal/version"
)

const (
	defaultBaseURL = "https://joyautomation.com"
	cacheTTL       = 1 * time.Hour
	binaryName     = "tentacle"
	httpTimeout    = 120 * time.Second
	unitName       = "tentacle.service"
	cacheFileName  = "releases-cache.json"
)

// UpgradeStatus tracks the progress of an in-flight upgrade.
type UpgradeStatus struct {
	State   string `json:"state"` // "idle", "downloading", "extracting", "replacing", "restarting", "failed"
	Error   string `json:"error,omitempty"`
	Version string `json:"version,omitempty"`
}

// ReleasesResponse wraps a release list with cache metadata.
type ReleasesResponse struct {
	Releases    []ReleaseInfo `json:"releases"`
	LastChecked int64         `json:"lastChecked"` // unix millis
}

// diskCache is the JSON structure persisted to disk.
type diskCache struct {
	Releases  []ReleaseInfo `json:"releases"`
	FetchedAt time.Time     `json:"fetchedAt"`
}

var (
	statusMu sync.Mutex
	status   = UpgradeStatus{State: "idle"}

	releasesMu sync.RWMutex
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

func cacheFilePath() string {
	return filepath.Join(paths.DataDir(), cacheFileName)
}

func readDiskCache() (*diskCache, error) {
	data, err := os.ReadFile(cacheFilePath())
	if err != nil {
		return nil, err
	}
	var dc diskCache
	if err := json.Unmarshal(data, &dc); err != nil {
		return nil, err
	}
	return &dc, nil
}

func writeDiskCache(dc *diskCache) error {
	dir := paths.DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(dc)
	if err != nil {
		return err
	}
	return os.WriteFile(cacheFilePath(), data, 0o644)
}

// OfflineError indicates that the release manifest could not be reached.
type OfflineError struct {
	Cause error
}

func (e *OfflineError) Error() string {
	return "unable to reach release manifest — check your internet connection"
}

func (e *OfflineError) Unwrap() error { return e.Cause }

// ReleaseInfo describes a single published release.
type ReleaseInfo struct {
	Version     string            `json:"version"`
	TagName     string            `json:"tagName"`
	Name        string            `json:"name"`
	ReleaseURL  string            `json:"releaseUrl"`
	PublishedAt string            `json:"publishedAt"`
	Current     bool              `json:"current"`
	Assets      map[string]string `json:"assets,omitempty"`
}

func resolveBaseURL(baseURL string) string {
	if baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return defaultBaseURL
}

// ListReleases returns all published releases from the joyautomation.com
// manifest. Results are cached to disk with a 1-hour TTL. On fetch failure,
// stale cached data is returned instead of an error.
func ListReleases(baseURL string) (*ReleasesResponse, error) {
	base := resolveBaseURL(baseURL)

	releasesMu.RLock()
	dc, _ := readDiskCache()
	releasesMu.RUnlock()

	// Return cached data if still fresh.
	if dc != nil && time.Since(dc.FetchedAt) < cacheTTL {
		return &ReleasesResponse{
			Releases:    stampCurrent(dc.Releases),
			LastChecked: dc.FetchedAt.UnixMilli(),
		}, nil
	}

	result, fetchErr := fetchReleases(base)
	if fetchErr != nil {
		// Serve stale cache on error instead of failing.
		if dc != nil && len(dc.Releases) > 0 {
			return &ReleasesResponse{
				Releases:    stampCurrent(dc.Releases),
				LastChecked: dc.FetchedAt.UnixMilli(),
			}, nil
		}
		return nil, fetchErr
	}

	now := time.Now()
	newCache := &diskCache{Releases: result, FetchedAt: now}
	releasesMu.Lock()
	if err := writeDiskCache(newCache); err != nil {
		slog.Warn("selfupgrade: failed to write release cache", "error", err)
	}
	releasesMu.Unlock()

	return &ReleasesResponse{
		Releases:    stampCurrent(result),
		LastChecked: now.UnixMilli(),
	}, nil
}

// stampCurrent sets the Current flag based on the running version.
func stampCurrent(releases []ReleaseInfo) []ReleaseInfo {
	current := strings.TrimPrefix(version.Version, "v")
	// Strip dev suffix (e.g. "0.0.8-7-gabcdef-dirty" → "0.0.8").
	if idx := strings.IndexByte(current, '-'); idx >= 0 {
		current = current[:idx]
	}
	out := make([]ReleaseInfo, len(releases))
	copy(out, releases)
	for i := range out {
		out[i].Current = out[i].Version == current
	}
	return out
}

func fetchReleases(baseURL string) ([]ReleaseInfo, error) {
	url := baseURL + "/api/releases/tentacle"
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "tentacle-selfupgrade")

	resp, err := client.Do(req)
	if err != nil {
		return nil, &OfflineError{Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest returned %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Releases []struct {
			Version      string            `json:"version"`
			TagName      string            `json:"tagName"`
			ReleasedAt   string            `json:"releasedAt"`
			Notes        string            `json:"notes"`
			Assets       map[string]string `json:"assets"`
			IsPrerelease bool              `json:"isPrerelease"`
		} `json:"releases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	releaseURLBase := baseURL + "/software/tentacle/releases"
	result := make([]ReleaseInfo, 0, len(payload.Releases))
	for _, r := range payload.Releases {
		if r.IsPrerelease {
			continue
		}
		result = append(result, ReleaseInfo{
			Version:     r.Version,
			TagName:     r.TagName,
			Name:        r.TagName,
			ReleaseURL:  releaseURLBase + "/" + r.Version,
			PublishedAt: r.ReleasedAt,
			Assets:      r.Assets,
		})
	}
	return result, nil
}

// PerformUpgrade downloads the target version and replaces the running binary.
// It must be called in a goroutine — after success it spawns a systemd restart script.
func PerformUpgrade(targetVersion, baseURL, binaryPath string, log *slog.Logger) {
	base := resolveBaseURL(baseURL)

	setStatus("downloading", targetVersion, "")
	log.Info("selfupgrade: starting", "version", targetVersion)

	arch := getArch()
	assetKey := fmt.Sprintf("linux_%s", arch)
	downloadURL := fmt.Sprintf("%s/downloads/tentacle/v%s/%s.tar.gz", base, targetVersion, assetKey)

	tmpArchive, err := os.CreateTemp("", "tentacle-upgrade-*.tar.gz")
	if err != nil {
		setStatus("failed", targetVersion, "create temp file: "+err.Error())
		return
	}
	defer os.Remove(tmpArchive.Name())
	defer tmpArchive.Close()

	if err := downloadFile(downloadURL, tmpArchive, log); err != nil {
		setStatus("failed", targetVersion, "download: "+err.Error())
		return
	}
	tmpArchive.Close()

	setStatus("extracting", targetVersion, "")

	tmpBinary, err := os.CreateTemp("/usr/local/bin", ".tentacle-upgrade-*")
	if err != nil {
		setStatus("failed", targetVersion, "create temp binary: "+err.Error())
		return
	}
	tmpBinaryPath := tmpBinary.Name()
	tmpBinary.Close()
	defer func() {
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

	setStatus("replacing", targetVersion, "")
	if err := os.Rename(tmpBinaryPath, binaryPath); err != nil {
		setStatus("failed", targetVersion, "replace binary: "+err.Error())
		return
	}

	releasesMu.Lock()
	os.Remove(cacheFilePath())
	releasesMu.Unlock()

	setStatus("restarting", targetVersion, "")
	if err := spawnRestartScript(log); err != nil {
		setStatus("failed", targetVersion, "restart: "+err.Error())
		return
	}

	log.Info("selfupgrade: restart script launched", "version", targetVersion)
}

func downloadFile(url string, dest *os.File, log *slog.Logger) error {
	log.Info("selfupgrade: downloading", "url", url)

	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "tentacle-selfupgrade")
	req.Header.Set("Accept", "application/octet-stream")

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
