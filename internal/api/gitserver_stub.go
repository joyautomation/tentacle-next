//go:build (api || all) && !gitopsserver && !mantle && !all

package api

import "github.com/go-chi/chi/v5"

// Stubs: gitops server is only available in mantle / gitopsserver / all builds.
// In other configurations these methods compile to no-ops so api.go can call
// them unconditionally.
func (m *Module) mountGitServer(_ chi.Router)    {}
func (m *Module) mountGitServerAPI(_ chi.Router) {}
