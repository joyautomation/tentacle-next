//go:build (api || all) && (gitopsserver || mantle || all)

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/gitops"
	"github.com/joyautomation/tentacle/internal/paths"
)

// gitServer is a single mantle-process Server, lazily constructed on first
// use. RootDir lives under <DataDir>/gitops/server.
var (
	gitServerInst *gitops.Server
	repoStoreInst *gitops.RepoStore
)

func gitServerRootDir() string {
	return filepath.Join(paths.DataDir(), "gitops", "server")
}

func ensureGitServer() *gitops.Server {
	if gitServerInst == nil {
		gitServerInst = gitops.NewServer(gitServerRootDir())
	}
	return gitServerInst
}

func ensureRepoStore() *gitops.RepoStore {
	if repoStoreInst == nil {
		repoStoreInst = gitops.NewRepoStore(ensureGitServer())
	}
	return repoStoreInst
}

// mountGitServer wires the smart-HTTP git protocol under /git/ and the repo
// management REST endpoints under /api/v1/gitops/repos.
func (m *Module) mountGitServer(r chi.Router) {
	srv := ensureGitServer()
	r.Mount("/git", http.StripPrefix("/git", srv.Handler()))
}

// mountGitServerAPI is called inside the /api/v1 sub-route to add the
// repo CRUD endpoints in the same versioned namespace as the rest of the API.
func (m *Module) mountGitServerAPI(r chi.Router) {
	r.Get("/gitops/repos", m.handleListGitopsRepos)
	r.Post("/gitops/repos", m.handleCreateGitopsRepo)
	r.Delete("/gitops/repos/{name}", m.handleDeleteGitopsRepo)
}

func (m *Module) handleListGitopsRepos(w http.ResponseWriter, r *http.Request) {
	srv := ensureGitServer()
	repos, err := srv.ListRepos(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repos == nil {
		repos = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

func (m *Module) handleCreateGitopsRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	srv := ensureGitServer()
	if err := srv.CreateRepo(context.Background(), body.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": body.Name})
}

func (m *Module) handleDeleteGitopsRepo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	srv := ensureGitServer()
	if err := srv.DeleteRepo(r.Context(), name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
