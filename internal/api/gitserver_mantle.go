//go:build (api || all) && (gitopsserver || mantle || all)

package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/gitserver"
)

// The git server is owned by the gitserver orchestrator module. These
// routes look it up at request time; if the module isn't enabled they
// return 503 so the operator gets a clear "module disabled" error rather
// than a partially-working page.

// mountGitServer wires the smart-HTTP git protocol under /git/. The
// underlying handler is fetched per-request so enable/disable through the
// orchestrator takes effect without restarting the API listener.
func (m *Module) mountGitServer(r chi.Router) {
	r.Mount("/git", http.StripPrefix("/git", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		srv := gitserver.Server()
		if srv == nil {
			writeError(w, http.StatusServiceUnavailable, "git server module not enabled")
			return
		}
		srv.Handler().ServeHTTP(w, req)
	})))
}

// mountGitServerAPI is called inside the /api/v1 sub-route to add the
// repo CRUD endpoints in the same versioned namespace as the rest of the API.
func (m *Module) mountGitServerAPI(r chi.Router) {
	r.Get("/gitops/repos", m.handleListGitopsRepos)
	r.Post("/gitops/repos", m.handleCreateGitopsRepo)
	r.Delete("/gitops/repos/{name}", m.handleDeleteGitopsRepo)
	r.Get("/gitops/repos/{name}/tree", m.handleGitopsRepoTree)
	r.Get("/gitops/tree", m.handleGitopsAllTrees)

	r.Get("/fleet/nodes", m.handleGetFleetNodes)
}

func (m *Module) handleListGitopsRepos(w http.ResponseWriter, r *http.Request) {
	srv := gitserver.Server()
	if srv == nil {
		writeError(w, http.StatusServiceUnavailable, "git server module not enabled")
		return
	}
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
	srv := gitserver.Server()
	if srv == nil {
		writeError(w, http.StatusServiceUnavailable, "git server module not enabled")
		return
	}
	if err := srv.CreateRepo(context.Background(), body.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": body.Name})
}

func (m *Module) handleGitopsRepoTree(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	srv := gitserver.Server()
	if srv == nil {
		writeError(w, http.StatusServiceUnavailable, "git server module not enabled")
		return
	}
	files, err := srv.RepoTree(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "files": files})
}

func (m *Module) handleGitopsAllTrees(w http.ResponseWriter, r *http.Request) {
	srv := gitserver.Server()
	if srv == nil {
		writeError(w, http.StatusServiceUnavailable, "git server module not enabled")
		return
	}
	repos, err := srv.ListRepos(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(repos))
	for _, name := range repos {
		files, err := srv.RepoTree(r.Context(), name)
		if err != nil {
			out = append(out, map[string]any{"name": name, "error": err.Error(), "files": []string{}})
			continue
		}
		out = append(out, map[string]any{"name": name, "files": files})
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": out})
}

func (m *Module) handleDeleteGitopsRepo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	srv := gitserver.Server()
	if srv == nil {
		writeError(w, http.StatusServiceUnavailable, "git server module not enabled")
		return
	}
	if err := srv.DeleteRepo(r.Context(), name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
