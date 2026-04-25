//go:build gitopsserver || mantle || all

// This file holds the embedded git smart-HTTP server that mantle uses to host
// the fleet configuration repos. Edge tentacles' gitops client (the rest of
// this package) clones/pulls from these repos. Implementation lands in
// Phase 1 — this scaffold establishes the build-tag boundary.
package gitops

import (
	"context"
	"errors"
	"net/http"
)

// Server hosts bare git repositories over HTTP smart protocol.
// One Server per mantle process; repos are stored under <DataDir>/gitops/server/<repo>.git.
type Server struct {
	rootDir string
}

// NewServer returns a Server rooted at the given directory. The directory is
// created on first repo write.
func NewServer(rootDir string) *Server {
	return &Server{rootDir: rootDir}
}

// Handler returns an http.Handler mounted under /git/. Phase 1 will implement
// info/refs and git-{upload,receive}-pack.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "git server not yet implemented", http.StatusNotImplemented)
	})
}

// CreateRepo initializes a new bare repository at <rootDir>/<name>.git.
func (s *Server) CreateRepo(_ context.Context, name string) error {
	if name == "" {
		return errors.New("repo name required")
	}
	return errors.New("not yet implemented")
}

// ListRepos returns all bare repos under rootDir.
func (s *Server) ListRepos(_ context.Context) ([]string, error) {
	return nil, nil
}
