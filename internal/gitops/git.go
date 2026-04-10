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
		return r.run("fetch", "origin", r.branch)
	}

	// Clone fresh.
	if err := os.MkdirAll(filepath.Dir(r.dir), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	return r.runInDir(filepath.Dir(r.dir), "clone", "--branch", r.branch, "--single-branch", r.remote, filepath.Base(r.dir))
}

// HasRemoteChanges returns true if the remote branch has commits not in the local branch.
func (r *gitRepo) HasRemoteChanges() (bool, error) {
	if err := r.run("fetch", "origin", r.branch); err != nil {
		return false, err
	}

	localSHA, err := r.output("rev-parse", "HEAD")
	if err != nil {
		return false, err
	}

	remoteSHA, err := r.output("rev-parse", "origin/"+r.branch)
	if err != nil {
		return false, err
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

// CurrentSHA returns the current HEAD commit SHA.
func (r *gitRepo) CurrentSHA() (string, error) {
	out, err := r.output("rev-parse", "HEAD")
	if err != nil {
		return "", err
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
