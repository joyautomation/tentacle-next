//go:build orchestrator || all

package orchestrator

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	otypes "github.com/joyautomation/tentacle/internal/types"
)

// latestCacheEntry stores a cached "latest" version resolution.
type latestCacheEntry struct {
	version    string
	resolvedAt time.Time
}

var (
	latestCache   = make(map[string]latestCacheEntry)
	latestCacheMu sync.RWMutex
)

// getArch returns the architecture string used in download URLs.
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

// checkInternet tests connectivity to the GitHub API.
func checkInternet() bool {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("HEAD", "https://api.github.com", nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "tentacle-orchestrator")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
}

// resolveLatestVersion fetches the latest release tag from GitHub.
// Results are cached for config.LatestCacheTtlMs.
func resolveLatestVersion(entry *otypes.ModuleRegistryEntry, config *OrchestratorConfig) string {
	latestCacheMu.RLock()
	cached, ok := latestCache[entry.Repo]
	latestCacheMu.RUnlock()

	ttl := time.Duration(config.LatestCacheTtlMs) * time.Millisecond
	if ok && time.Since(cached.resolvedAt) < ttl {
		return cached.version
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", config.GhOrg, entry.Repo)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Warn("download: failed to create request", "repo", entry.Repo, "error", err)
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "tentacle-orchestrator")
	if config.GhToken != "" {
		req.Header.Set("Authorization", "Bearer "+config.GhToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("download: failed to resolve latest version", "repo", entry.Repo, "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		slog.Warn("download: failed to resolve latest version", "repo", entry.Repo, "status", resp.StatusCode)
		return ""
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		slog.Warn("download: failed to parse release", "repo", entry.Repo, "error", err)
		return ""
	}

	version := strings.TrimPrefix(release.TagName, "v")
	latestCacheMu.Lock()
	latestCache[entry.Repo] = latestCacheEntry{version: version, resolvedAt: time.Now()}
	latestCacheMu.Unlock()

	slog.Debug("download: resolved latest version", "repo", entry.Repo, "version", version)
	return version
}

// resolveVersion resolves the desired version string.
// If "latest", tries GitHub then falls back to the highest local version.
func resolveVersion(entry *otypes.ModuleRegistryEntry, desiredVersion string, config *OrchestratorConfig) string {
	if desiredVersion != "latest" {
		return desiredVersion
	}

	// Try GitHub first
	if resolved := resolveLatestVersion(entry, config); resolved != "" {
		return resolved
	}

	// Offline fallback: highest local version
	versionsDir := config.VersionsDir + "/" + entry.ModuleID
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return ""
	}

	var versions []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "unknown" {
			versions = append(versions, e.Name())
		}
	}
	if len(versions) == 0 {
		return ""
	}

	sort.Slice(versions, func(i, j int) bool {
		return compareSemver(versions[i], versions[j]) < 0
	})

	highest := versions[len(versions)-1]
	slog.Debug("download: offline fallback", "moduleId", entry.ModuleID, "version", highest)
	return highest
}

// compareSemver compares two semver-ish strings. Returns <0, 0, >0.
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

// getDownloadURL builds the download URL for a module version.
func getDownloadURL(entry *otypes.ModuleRegistryEntry, version string, config *OrchestratorConfig) string {
	tag := "v" + version
	base := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s", config.GhOrg, entry.Repo, tag)

	switch entry.Runtime {
	case "go":
		return fmt.Sprintf("%s/%s-linux-%s", base, entry.ModuleID, getArch())
	case "deno":
		return fmt.Sprintf("%s/%s-src.tar.gz", base, entry.Repo)
	case "deno-web":
		return fmt.Sprintf("%s/%s-build.tar.gz", base, entry.Repo)
	default:
		return ""
	}
}

// downloadFile downloads a URL to a local path.
func downloadFile(url, destPath string, config *OrchestratorConfig) bool {
	slog.Info("download: downloading", "url", url)

	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Error("download: failed to create request", "error", err)
		return false
	}
	req.Header.Set("User-Agent", "tentacle-orchestrator")
	req.Header.Set("Accept", "application/octet-stream")
	if config.GhToken != "" {
		req.Header.Set("Authorization", "Bearer "+config.GhToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("download: failed", "url", url, "error", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		slog.Error("download: HTTP error", "status", resp.StatusCode, "url", url)
		return false
	}

	f, err := os.Create(destPath)
	if err != nil {
		slog.Error("download: failed to create file", "path", destPath, "error", err)
		return false
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		slog.Error("download: failed to write file", "path", destPath, "error", err)
		os.Remove(destPath)
		return false
	}

	slog.Debug("download: complete", "bytes", n, "path", destPath)
	return true
}

// findDeno locates the deno binary -- prefers install dir, falls back to PATH.
func findDeno(config *OrchestratorConfig) string {
	installDeno := config.BinDir + "/deno"
	if _, err := os.Stat(installDeno); err == nil {
		return installDeno
	}
	return "deno"
}

// isVersionInstalled checks if a version directory exists on disk.
func isVersionInstalled(modID, version string, config *OrchestratorConfig) bool {
	versionDir := config.VersionsDir + "/" + modID + "/" + version
	info, err := os.Stat(versionDir)
	return err == nil && info.IsDir()
}

// listInstalledVersions returns all version subdirectories for a module.
func listInstalledVersions(modID string, config *OrchestratorConfig) []string {
	versionsDir := config.VersionsDir + "/" + modID
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return []string{}
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	return versions
}

// getActiveVersion reads the symlink and extracts the version from the path.
func getActiveVersion(entry *otypes.ModuleRegistryEntry, config *OrchestratorConfig) string {
	var linkPath string
	if entry.Runtime == "go" {
		linkPath = config.BinDir + "/" + entry.ModuleID
	} else {
		linkPath = config.ServicesDir + "/" + entry.Repo
	}

	target, err := os.Readlink(linkPath)
	if err != nil {
		return ""
	}

	// Extract version from path like .../versions/tentacle-mqtt/0.0.6/
	parts := strings.Split(filepath.ToSlash(target), "/")
	for i, p := range parts {
		if p == "versions" && i+2 < len(parts) {
			return parts[i+2]
		}
	}
	return ""
}

// installVersion downloads and installs a specific version of a module.
func installVersion(entry *otypes.ModuleRegistryEntry, version string, config *OrchestratorConfig) bool {
	versionDir := config.VersionsDir + "/" + entry.ModuleID + "/" + version
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		slog.Error("install: failed to create version dir", "path", versionDir, "error", err)
		return false
	}

	url := getDownloadURL(entry, version, config)
	if url == "" {
		slog.Error("install: unknown runtime", "runtime", entry.Runtime)
		return false
	}

	switch entry.Runtime {
	case "go":
		binaryPath := versionDir + "/" + entry.ModuleID
		if !downloadFile(url, binaryPath, config) {
			os.RemoveAll(versionDir)
			return false
		}
		if err := os.Chmod(binaryPath, 0755); err != nil {
			slog.Warn("install: failed to chmod", "path", binaryPath, "error", err)
		}
		return true

	case "deno", "deno-web":
		tmpDir, err := os.MkdirTemp("", "tentacle-install-*")
		if err != nil {
			slog.Error("install: failed to create temp dir", "error", err)
			return false
		}
		defer os.RemoveAll(tmpDir)

		tarPath := tmpDir + "/pkg.tar.gz"
		if !downloadFile(url, tarPath, config) {
			os.RemoveAll(versionDir)
			return false
		}

		// Extract tarball
		cmd := exec.Command("tar", "xzf", tarPath, "-C", tmpDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			slog.Error("install: failed to extract tarball", "output", string(out))
			os.RemoveAll(versionDir)
			return false
		}

		// Source tarballs contain repo-name/ dir; build tarballs contain build/ dir
		repoDir := tmpDir + "/" + entry.Repo
		buildDir := tmpDir + "/build"

		if info, err := os.Stat(repoDir); err == nil && info.IsDir() {
			if !moveContents(repoDir, versionDir) {
				os.RemoveAll(versionDir)
				return false
			}
		} else if info, err := os.Stat(buildDir); err == nil && info.IsDir() {
			buildDest := versionDir + "/build"
			os.MkdirAll(buildDest, 0755)
			if !moveContents(buildDir, buildDest) {
				os.RemoveAll(versionDir)
				return false
			}
		} else {
			slog.Error("install: unexpected tarball layout", "repo", entry.Repo)
			os.RemoveAll(versionDir)
			return false
		}

		precacheDenoDeps(entry, version, config)
		return true

	default:
		slog.Error("install: unknown runtime", "runtime", entry.Runtime)
		return false
	}
}

// moveContents copies all files from src to dest using cp -a.
func moveContents(src, dest string) bool {
	cmd := exec.Command("bash", "-c",
		`cp -a "`+src+`/"* "`+dest+`/" 2>/dev/null; cp -a "`+src+`/".[^.]* "`+dest+`/" 2>/dev/null; true`)
	if err := cmd.Run(); err != nil {
		slog.Error("install: failed to move contents", "src", src, "dest", dest, "error", err)
		return false
	}
	return true
}

// precacheDenoDeps runs deno install --entrypoint to cache dependencies.
func precacheDenoDeps(entry *otypes.ModuleRegistryEntry, version string, config *OrchestratorConfig) {
	versionDir := config.VersionsDir + "/" + entry.ModuleID + "/" + version
	denoDir := config.CacheDir + "/deno/versions/" + entry.ModuleID + "/" + version
	os.MkdirAll(denoDir, 0755)

	// Check for deno.json
	if _, err := os.Stat(versionDir + "/deno.json"); err != nil {
		return // No deno.json, skip
	}

	entrypoint := versionDir + "/main.ts"
	if entry.Runtime == "deno-web" {
		entrypoint = versionDir + "/build/index.js"
	}

	slog.Debug("install: pre-caching deps", "moduleId", entry.ModuleID, "version", version)
	denoPath := findDeno(config)
	cmd := exec.Command(denoPath, "install", "--entrypoint", entrypoint)
	cmd.Env = append(os.Environ(), "DENO_DIR="+denoDir)
	cmd.Dir = versionDir
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("install: failed to cache deps", "moduleId", entry.ModuleID, "version", version, "output", string(out))
	}
}

// updateSymlink creates or updates the symlink for a module version.
func updateSymlink(entry *otypes.ModuleRegistryEntry, version string, config *OrchestratorConfig) bool {
	var linkPath, target string

	if entry.Runtime == "go" {
		linkPath = config.BinDir + "/" + entry.ModuleID
		target = config.VersionsDir + "/" + entry.ModuleID + "/" + version + "/" + entry.ModuleID
	} else {
		linkPath = config.ServicesDir + "/" + entry.Repo
		target = config.VersionsDir + "/" + entry.ModuleID + "/" + version
	}

	// Remove existing symlink or file
	os.Remove(linkPath)

	if err := os.Symlink(target, linkPath); err != nil {
		slog.Error("install: failed to create symlink", "link", linkPath, "target", target, "error", err)
		return false
	}
	slog.Debug("install: symlink created", "link", linkPath, "target", target)
	return true
}

// ensureDeps installs apt packages and builds source dependencies for a module.
// Returns true if all deps are satisfied (already present or newly installed).
func ensureDeps(entry *otypes.ModuleRegistryEntry) bool {
	if len(entry.AptDeps) == 0 && len(entry.BuildDeps) == 0 {
		return true
	}

	aptDeps := make([]string, len(entry.AptDeps))
	copy(aptDeps, entry.AptDeps)
	// BuildDeps require git to clone source repos
	if len(entry.BuildDeps) > 0 {
		hasGit := false
		for _, d := range aptDeps {
			if d == "git" {
				hasGit = true
				break
			}
		}
		if !hasGit {
			aptDeps = append(aptDeps, "git")
		}
	}

	if len(aptDeps) > 0 {
		if !installAptDeps(aptDeps) {
			return false
		}
	}

	for _, dep := range entry.BuildDeps {
		if !ensureBuildDep(dep) {
			return false
		}
	}

	return true
}

// installAptDeps installs one or more apt packages if not already present.
func installAptDeps(packages []string) bool {
	// Check which packages are missing
	var missing []string
	for _, pkg := range packages {
		_, _, ok := runCmd("dpkg", "-s", pkg)
		if !ok {
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		return true
	}

	slog.Info("deps: installing apt packages", "packages", missing)
	_, stderr, ok := runCmd("apt-get", "update", "-qq")
	if !ok {
		slog.Error("deps: apt-get update failed", "stderr", stderr)
		return false
	}

	args := append([]string{"install", "-y", "-qq"}, missing...)
	_, stderr, ok = runCmd("apt-get", args...)
	if !ok {
		slog.Error("deps: apt-get install failed", "stderr", stderr)
		return false
	}
	return true
}

// ensureBuildDep checks if a source-built library is present, and builds it if not.
func ensureBuildDep(dep otypes.BuildDep) bool {
	// Test if already installed
	if dep.TestCmd != "" {
		_, _, ok := runCmd("bash", "-c", dep.TestCmd)
		if ok {
			slog.Debug("deps: already installed", "name", dep.Name)
			return true
		}
	}

	slog.Info("deps: building from source", "name", dep.Name, "version", dep.Version)

	buildDir := fmt.Sprintf("/tmp/%s-build", dep.Name)
	os.RemoveAll(buildDir)

	// Clone
	_, stderr, ok := runCmd("git", "clone", "--depth=1", "--branch", dep.Version, dep.Repo, buildDir)
	if !ok {
		slog.Error("deps: failed to clone", "name", dep.Name, "stderr", stderr)
		return false
	}

	// CMake configure
	cmakeBuildDir := buildDir + "/build"
	os.MkdirAll(cmakeBuildDir, 0755)
	_, stderr, ok = runCmd("cmake", "-S", buildDir, "-B", cmakeBuildDir,
		"-DCMAKE_BUILD_TYPE=Release", "-DCMAKE_INSTALL_PREFIX=/usr/local")
	if !ok {
		slog.Error("deps: cmake configure failed", "name", dep.Name, "stderr", stderr)
		os.RemoveAll(buildDir)
		return false
	}

	// Build
	_, stderr, ok = runCmd("cmake", "--build", cmakeBuildDir, "--parallel")
	if !ok {
		slog.Error("deps: cmake build failed", "name", dep.Name, "stderr", stderr)
		os.RemoveAll(buildDir)
		return false
	}

	// Install
	_, stderr, ok = runCmd("cmake", "--install", cmakeBuildDir)
	if !ok {
		slog.Error("deps: cmake install failed", "name", dep.Name, "stderr", stderr)
		os.RemoveAll(buildDir)
		return false
	}

	// Update linker cache
	runCmd("ldconfig")

	os.RemoveAll(buildDir)
	slog.Info("deps: successfully installed", "name", dep.Name, "version", dep.Version)
	return true
}
