//go:build gitops || all

package gitops

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRepo wraps git CLI operations for a local clone.
type gitRepo struct {
	dir        string // local clone directory
	remote     string // git remote URL
	branch     string // branch to track
	sshKeyPath string // SSH key for authentication
	log        *slog.Logger
}

// Init clones the repo if it doesn't exist, or fetches if it does.
func (r *gitRepo) Init() error {
	if _, err := os.Stat(filepath.Join(r.dir, ".git")); err == nil {
		// Already cloned — configure remote and fetch.
		if err := r.run("remote", "set-url", "origin", r.remote); err != nil {
			return fmt.Errorf("set remote: %w", err)
		}
		// Fetch without branch name — branch-specific fetch fails on empty remotes.
		_ = r.run("fetch", "origin")
		return nil
	}

	// Clone fresh.
	if err := os.MkdirAll(filepath.Dir(r.dir), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	// Try branch-specific clone first (faster for non-empty repos).
	if err := r.runInDir(filepath.Dir(r.dir), "clone", "--branch", r.branch, "--single-branch", r.remote, filepath.Base(r.dir)); err == nil {
		return nil
	}

	// Fall back to plain clone (handles empty repos).
	if err := r.runInDir(filepath.Dir(r.dir), "clone", r.remote, filepath.Base(r.dir)); err != nil {
		return fmt.Errorf("clone: %w", err)
	}

	// Ensure we're on the desired branch (empty repo default may differ).
	currentBranch, _ := r.output("symbolic-ref", "--short", "HEAD")
	if strings.TrimSpace(currentBranch) != r.branch {
		_ = r.run("checkout", "-b", r.branch)
	}

	return nil
}

// EnsureIdentity configures git user.name and user.email in the repo if not already set.
func (r *gitRepo) EnsureIdentity() {
	if name, _ := r.output("config", "user.name"); strings.TrimSpace(name) == "" {
		hostname, _ := os.Hostname()
		_ = r.run("config", "user.name", "tentacle")
		_ = r.run("config", "user.email", fmt.Sprintf("tentacle@%s", hostname))
	}
}

// HasRemoteChanges returns true if the remote branch has commits not in the local branch.
func (r *gitRepo) HasRemoteChanges() (bool, error) {
	if err := r.run("fetch", "origin"); err != nil {
		return false, err
	}

	localSHA, err := r.output("rev-parse", "HEAD")
	if err != nil {
		// No local commits yet. Check if remote has any.
		_, remoteErr := r.output("rev-parse", "origin/"+r.branch)
		return remoteErr == nil, nil
	}

	remoteSHA, err := r.output("rev-parse", "origin/"+r.branch)
	if err != nil {
		// Remote branch doesn't exist yet.
		return false, nil
	}

	return strings.TrimSpace(localSHA) != strings.TrimSpace(remoteSHA), nil
}

// Pull fast-forwards the local branch to match remote.
// Returns true if new commits were pulled.
func (r *gitRepo) Pull() (bool, error) {
	before, _ := r.output("rev-parse", "HEAD")

	if err := r.run("pull", "--ff-only", "origin", r.branch); err != nil {
		return false, fmt.Errorf("pull: %w", err)
	}

	after, _ := r.output("rev-parse", "HEAD")
	return strings.TrimSpace(before) != strings.TrimSpace(after), nil
}

// HasLocalChanges returns true if the working tree has uncommitted changes.
func (r *gitRepo) HasLocalChanges() (bool, error) {
	out, err := r.output("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// CommitAll stages all changes and creates a commit.
func (r *gitRepo) CommitAll(msg string) error {
	if err := r.run("add", "-A"); err != nil {
		return fmt.Errorf("add: %w", err)
	}

	// Check if there's anything to commit after staging.
	out, err := r.output("diff", "--cached", "--quiet")
	_ = out
	if err == nil {
		// No staged changes.
		return nil
	}

	return r.run("commit", "-m", msg)
}

// Push pushes the local branch to the remote.
func (r *gitRepo) Push() error {
	return r.run("push", "origin", r.branch)
}

// CurrentSHA returns the current HEAD commit SHA (empty string for repos with no commits).
func (r *gitRepo) CurrentSHA() (string, error) {
	out, err := r.output("rev-parse", "HEAD")
	if err != nil {
		// Empty repo — no commits yet.
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// ResetToRemote hard-resets the local branch to match the remote.
func (r *gitRepo) ResetToRemote() error {
	return r.run("reset", "--hard", "origin/"+r.branch)
}

// run executes a git command in the repo directory.
func (r *gitRepo) run(args ...string) error {
	_, err := r.execGit(r.dir, args...)
	return err
}

// runInDir executes a git command in the specified directory.
func (r *gitRepo) runInDir(dir string, args ...string) error {
	_, err := r.execGit(dir, args...)
	return err
}

// output executes a git command and returns stdout.
func (r *gitRepo) output(args ...string) (string, error) {
	return r.execGit(r.dir, args...)
}

func (r *gitRepo) execGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	// Set up SSH key if specified.
	if r.sshKeyPath != "" {
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %s -o StrictHostKeyChecking=accept-new", r.sshKeyPath),
		)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		r.log.Debug("git command failed",
			"args", args,
			"stderr", strings.TrimSpace(stderr.String()),
			"error", err,
		)
		return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}
