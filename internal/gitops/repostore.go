//go:build gitopsserver || mantle || all

package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// RepoStore is a thin file-level interface over the bare repos hosted by
// Server. Each (group, node) edge tentacle gets its own bare repo by default;
// mantle-side configurator endpoints use this to read and write tracked
// manifest YAML files (e.g. config/gateways/<id>.yaml) on behalf of the operator.
//
// Internally we maintain a server-side working clone next to each bare repo
// so we can do file operations with plain filesystem reads + git plumbing.
// The work-clones live at <rootDir>/_work/<repoName>; bare repos at
// <rootDir>/<repoName>.git.
type RepoStore struct {
	server *Server

	mu       sync.Mutex
	repoLock map[string]*sync.Mutex
}

// NewRepoStore returns a RepoStore that uses the given Server for repo
// creation and lookup.
func NewRepoStore(s *Server) *RepoStore {
	return &RepoStore{server: s, repoLock: map[string]*sync.Mutex{}}
}

// repoNameForTarget is the default mapping (group, node) → repo name. Operators
// will eventually be able to remap multiple targets onto a shared repo, but the
// initial release is one-repo-per-tentacle.
func repoNameForTarget(group, node string) string {
	return sanitize(group) + "--" + sanitize(node)
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// lockFor returns a per-repo mutex so concurrent writes to the same repo
// serialize on a single working clone.
func (rs *RepoStore) lockFor(name string) *sync.Mutex {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if l, ok := rs.repoLock[name]; ok {
		return l
	}
	l := &sync.Mutex{}
	rs.repoLock[name] = l
	return l
}

func (rs *RepoStore) barePath(name string) string {
	return filepath.Join(rs.server.RootDir(), name+".git")
}

func (rs *RepoStore) workPath(name string) string {
	return filepath.Join(rs.server.RootDir(), "_work", name)
}

// ensureRepoForTarget creates the bare repo for (group, node) if it doesn't
// exist, and ensures the work-clone next to it is initialized.
func (rs *RepoStore) ensureRepoForTarget(group, node string) (string, error) {
	name := repoNameForTarget(group, node)
	if name == "--" {
		return "", errors.New("group and node required")
	}

	bare := rs.barePath(name)
	if _, err := os.Stat(bare); os.IsNotExist(err) {
		if err := rs.server.CreateRepo(nil, name); err != nil {
			return "", fmt.Errorf("create bare repo: %w", err)
		}
	}

	work := rs.workPath(name)
	if _, err := os.Stat(filepath.Join(work, ".git")); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(work), 0o755); err != nil {
			return "", fmt.Errorf("create work parent: %w", err)
		}
		// clone --local from the bare. -l avoids hardlinks to keep things
		// safe across filesystems but is fine for a small config repo.
		if out, err := exec.Command("git", "clone", bare, work).CombinedOutput(); err != nil {
			return "", fmt.Errorf("clone work: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		// First-clone of an empty bare leaves us in a detached state with
		// no branch; create main so subsequent commits land somewhere.
		if _, err := runGit(work, "rev-parse", "--verify", "HEAD"); err != nil {
			if out, err := exec.Command("git", "-C", work, "checkout", "-b", "main").CombinedOutput(); err != nil {
				return "", fmt.Errorf("init main: %w (%s)", err, strings.TrimSpace(string(out)))
			}
		}
		// Configure identity for the mantle-side commits.
		_, _ = runGit(work, "config", "user.email", "mantle@tentacle.local")
		_, _ = runGit(work, "config", "user.name", "Tentacle Mantle")
	} else {
		// Pull latest in case operator pushed externally.
		_, _ = runGit(work, "fetch", "origin")
		_, _ = runGit(work, "merge", "--ff-only", "origin/main")
	}

	return name, nil
}

// ReadFile returns the bytes of <path> in the (group, node) target's repo on
// the main branch. Returns os.ErrNotExist (wrapped) if the file does not
// exist yet.
func (rs *RepoStore) ReadFile(group, node, path string) ([]byte, error) {
	name, err := rs.ensureRepoForTarget(group, node)
	if err != nil {
		return nil, err
	}
	lk := rs.lockFor(name)
	lk.Lock()
	defer lk.Unlock()

	full := filepath.Join(rs.workPath(name), path)
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// WriteFile writes <path> in the (group, node) target's repo, commits with
// the given message, and pushes back to the bare. Creates parent directories
// as needed. The commit author identity is set when the work-clone is first
// created (see ensureRepoForTarget).
func (rs *RepoStore) WriteFile(group, node, path string, data []byte, msg string) error {
	name, err := rs.ensureRepoForTarget(group, node)
	if err != nil {
		return err
	}
	lk := rs.lockFor(name)
	lk.Lock()
	defer lk.Unlock()

	work := rs.workPath(name)
	full := filepath.Join(work, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	if _, err := runGit(work, "add", path); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	// If nothing changed, status --porcelain returns empty. Skip the commit
	// in that case so we don't pollute history with no-op commits.
	out, err := runGit(work, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	if _, err := runGit(work, "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if _, err := runGit(work, "push", "origin", "HEAD:main"); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// DeleteFile removes <path> in the (group, node) target's repo, commits with
// the given message, and pushes back to the bare. No-op if the file does not
// exist (or is already untracked).
func (rs *RepoStore) DeleteFile(group, node, path, msg string) error {
	name, err := rs.ensureRepoForTarget(group, node)
	if err != nil {
		return err
	}
	lk := rs.lockFor(name)
	lk.Lock()
	defer lk.Unlock()

	work := rs.workPath(name)
	full := filepath.Join(work, path)
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return nil
	}
	if _, err := runGit(work, "rm", "-f", path); err != nil {
		return fmt.Errorf("git rm: %w", err)
	}
	out, err := runGit(work, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	if _, err := runGit(work, "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if _, err := runGit(work, "push", "origin", "HEAD:main"); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// runGit runs `git -C <dir> <args...>` and returns combined output.
func runGit(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(buf.String()))
	}
	return buf.String(), nil
}
