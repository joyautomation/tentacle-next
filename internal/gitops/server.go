//go:build gitopsserver || mantle || all

// This file holds the embedded git smart-HTTP server that mantle uses to host
// the fleet configuration repos. Edge tentacles' gitops client (the rest of
// this package) clones/pulls from these repos. The server delegates smart-HTTP
// protocol handling to the system `git http-backend` CGI binary so we get the
// full upload-pack/receive-pack semantics for free.
package gitops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Server hosts bare git repositories over HTTP smart protocol.
// One Server per mantle process; repos are stored under <RootDir>/<repo>.git.
type Server struct {
	rootDir string
}

// NewServer returns a Server rooted at the given directory. The directory is
// created on demand by CreateRepo.
func NewServer(rootDir string) *Server {
	return &Server{rootDir: rootDir}
}

// RootDir is the on-disk directory holding all bare repositories.
func (s *Server) RootDir() string { return s.rootDir }

// Handler returns an http.Handler that serves git smart-HTTP for any repo
// under RootDir. The caller is expected to mount it under a path prefix
// (typically "/git/") and to strip that prefix before delegation.
//
// We expect URLs of the form "/<repo>.git/info/refs?service=git-upload-pack"
// or "/<repo>.git/git-upload-pack" / ".../git-receive-pack". Authentication
// is the caller's responsibility — wrap this handler with whatever auth the
// outer HTTP mux uses (basic auth + admin token, etc.).
func (s *Server) Handler() http.Handler {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		// We can't fail at construction time because callers wire the
		// handler unconditionally. Surface the error per-request instead.
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "git binary not found on PATH: "+err.Error(), http.StatusInternalServerError)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject path traversal up front. The repo name appears in the URL
		// path; refuse anything containing ".." or absolute components.
		if strings.Contains(r.URL.Path, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(s.rootDir, 0o755); err != nil {
			http.Error(w, "init root: "+err.Error(), http.StatusInternalServerError)
			return
		}
		h := &cgi.Handler{
			Path: gitBin,
			Args: []string{"http-backend"},
			Env: []string{
				"GIT_PROJECT_ROOT=" + s.rootDir,
				"GIT_HTTP_EXPORT_ALL=1",
				// Allow push over HTTP. http.receivepack on the repo
				// itself can also gate this; we permit globally and rely
				// on the outer auth middleware.
				"GIT_HTTP_RECEIVE_PACK=1",
			},
		}
		h.ServeHTTP(w, r)
	})
}

// CreateRepo initializes a new bare repository at <rootDir>/<name>.git.
// Idempotent: returns nil if the repo already exists. The repo is configured
// to allow push over HTTP (http.receivepack=true) and to accept the modern
// default branch.
func (s *Server) CreateRepo(_ context.Context, name string) error {
	if name == "" {
		return errors.New("repo name required")
	}
	if !validRepoName(name) {
		return errors.New("invalid repo name")
	}
	if err := os.MkdirAll(s.rootDir, 0o755); err != nil {
		return fmt.Errorf("create root dir: %w", err)
	}
	repoPath := filepath.Join(s.rootDir, name+".git")
	if _, err := os.Stat(repoPath); err == nil {
		return nil
	}
	if out, err := exec.Command("git", "init", "--bare", "--initial-branch=main", repoPath).CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", repoPath, "config", "http.receivepack", "true").CombinedOutput(); err != nil {
		return fmt.Errorf("git config: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ListRepos returns all bare repos under rootDir, sorted, without the
// trailing ".git" suffix.
func (s *Server) ListRepos(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".git") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".git"))
	}
	sort.Strings(out)
	return out, nil
}

// DeleteRepo removes a bare repo. Used by the API for fleet provisioning.
func (s *Server) DeleteRepo(_ context.Context, name string) error {
	if !validRepoName(name) {
		return errors.New("invalid repo name")
	}
	return os.RemoveAll(filepath.Join(s.rootDir, name+".git"))
}

// validRepoName accepts simple repo names: letters, digits, dash, underscore.
// Avoids any path-injection or shell-metachar surprises.
func validRepoName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}
