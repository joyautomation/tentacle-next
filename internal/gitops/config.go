//go:build gitops || all

package gitops

import (
	"os"
	"strconv"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/paths"
	"github.com/joyautomation/tentacle/internal/topics"
)

// configSchema defines the GitOps module's configuration fields for the settings UI.
var configSchema = []config.FieldDef{
	// Repository group
	{EnvVar: "GITOPS_REPO_URL", Label: "Git Repository URL", Type: "string", Required: true, Description: "SSH or HTTPS URL of the git repository", Group: "Repository", GroupOrder: 0, SortOrder: 0},
	{EnvVar: "GITOPS_BRANCH", Label: "Branch", Type: "string", Default: "main", Description: "Git branch to sync with", Group: "Repository", GroupOrder: 0, SortOrder: 1},
	{EnvVar: "GITOPS_PATH", Label: "Config Path", Type: "string", Default: "config", Description: "Directory within the repo for manifest files", Group: "Repository", GroupOrder: 0, SortOrder: 2},
	// Authentication group
	{EnvVar: "GITOPS_SSH_KEY_PATH", Label: "SSH Key Path", Type: "string", Default: paths.SSHKeyPath(), Description: "Path to SSH private key for git auth", Group: "Authentication", GroupOrder: 1, SortOrder: 0},
	// Sync group
	{EnvVar: "GITOPS_POLL_INTERVAL_S", Label: "Poll Interval (seconds)", Type: "number", Default: "60", Description: "How often to check for remote changes", Group: "Sync", GroupOrder: 2, SortOrder: 0},
	{EnvVar: "GITOPS_AUTO_PUSH", Label: "Auto Push Changes", Type: "boolean", Default: "true", Description: "Automatically push local config changes to git", Group: "Sync", GroupOrder: 2, SortOrder: 1},
	{EnvVar: "GITOPS_AUTO_PULL", Label: "Auto Pull Changes", Type: "boolean", Default: "true", Description: "Automatically pull and apply remote changes", Group: "Sync", GroupOrder: 2, SortOrder: 2},
	{EnvVar: "GITOPS_DEBOUNCE_S", Label: "Debounce (seconds)", Type: "number", Default: "5", Description: "Wait this long after last local change before committing", Group: "Sync", GroupOrder: 2, SortOrder: 3},
}

// gitopsConfig holds the resolved configuration for the gitops module.
type gitopsConfig struct {
	RepoURL      string
	Branch       string
	Path         string
	SSHKeyPath   string
	PollInterval int
	AutoPush     bool
	AutoPull     bool
	DebounceS    int
}

// loadConfig resolves gitops config from KV → env → defaults.
func loadConfig(b bus.Bus) gitopsConfig {
	get := func(envVar, defaultVal string) string {
		if b != nil {
			if data, _, err := b.KVGet(topics.BucketTentacleConfig, "gitops."+envVar); err == nil && len(data) > 0 {
				return string(data)
			}
		}
		if v := os.Getenv(envVar); v != "" {
			return v
		}
		return defaultVal
	}

	getInt := func(envVar string, defaultVal int) int {
		s := get(envVar, "")
		if s == "" {
			return defaultVal
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		return defaultVal
	}

	getBool := func(envVar string, defaultVal bool) bool {
		s := get(envVar, "")
		if s == "" {
			return defaultVal
		}
		if b, err := strconv.ParseBool(s); err == nil {
			return b
		}
		return defaultVal
	}

	return gitopsConfig{
		RepoURL:      get("GITOPS_REPO_URL", ""),
		Branch:       get("GITOPS_BRANCH", "main"),
		Path:         get("GITOPS_PATH", "config"),
		SSHKeyPath:   get("GITOPS_SSH_KEY_PATH", paths.SSHKeyPath()),
		PollInterval: getInt("GITOPS_POLL_INTERVAL_S", 60),
		AutoPush:     getBool("GITOPS_AUTO_PUSH", true),
		AutoPull:     getBool("GITOPS_AUTO_PULL", true),
		DebounceS:    getInt("GITOPS_DEBOUNCE_S", 5),
	}
}
