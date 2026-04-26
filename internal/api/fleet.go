//go:build (api || all) && (gitopsserver || mantle || all)

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/gitserver"
	"github.com/joyautomation/tentacle/internal/manifest"
	"github.com/joyautomation/tentacle/internal/sparkplug"
	"gopkg.in/yaml.v3"
)

// fleetModule is the desired-state view of a module, sourced from the edge's
// gitops repo on this mantle.
type fleetModule struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Running bool   `json:"running"`
}

// handleGetFleetNodes returns the sparkplug-host node inventory enriched with
// per-edge module manifests pulled from the gitops bare repo for that node.
// Modules are the desired set (config/services/*.yaml on `main`), so they
// reflect the operator's intended fleet state, not necessarily what's running.
//
// GET /api/v1/fleet/nodes
func (m *Module) handleGetFleetNodes(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(sparkplug.SubjectHostNodes, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, "sparkplug-host module unavailable: "+err.Error())
		return
	}

	var nodes []map[string]any
	if err := json.Unmarshal(resp, &nodes); err != nil {
		writeError(w, http.StatusInternalServerError, "decode sparkplug-host snapshot: "+err.Error())
		return
	}

	srv := gitserver.Server()
	for _, n := range nodes {
		group, _ := n["groupId"].(string)
		node, _ := n["nodeId"].(string)
		if srv == nil || group == "" || node == "" {
			n["modules"] = []fleetModule{}
			continue
		}
		mods, err := readNodeModules(group, node)
		if err != nil {
			n["modules"] = []fleetModule{}
			n["modulesError"] = err.Error()
			continue
		}
		n["modules"] = mods
	}

	writeJSON(w, http.StatusOK, nodes)
}

// readNodeModules enumerates config/services/*.yaml in the bare repo for the
// (group, node) target and parses each as a Service manifest. Returns an empty
// slice if the repo doesn't exist yet.
func readNodeModules(group, node string) ([]fleetModule, error) {
	srv := gitserver.Server()
	if srv == nil {
		return nil, errors.New("git server module not enabled")
	}
	repoName := repoNameForFleet(group, node)
	files, err := srv.RepoTree(nil, repoName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []fleetModule{}, nil
		}
		return nil, err
	}

	out := make([]fleetModule, 0)
	for _, f := range files {
		dir, name := path.Split(f)
		if dir != "config/services/" {
			continue
		}
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		data, err := srv.ReadFileFromBare(repoName, f)
		if err != nil {
			continue
		}
		resources, err := manifest.ParseBytes(data)
		if err != nil {
			continue
		}
		for _, res := range resources {
			svc, ok := res.(*manifest.ServiceResource)
			if !ok {
				continue
			}
			out = append(out, fleetModule{
				ID:      svc.Metadata.Name,
				Version: svc.Spec.Version,
				Running: svc.Spec.Running,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// repoNameForFleet mirrors gitops.repoNameForTarget. Kept local to avoid
// exporting it just for this caller.
func repoNameForFleet(group, node string) string {
	return sanitizeRepoSegment(group) + "--" + sanitizeRepoSegment(node)
}

// handleGetFleetNodeServices returns the parsed Service manifests in a node's
// gitops repo. This is the same data the list endpoint includes inline, but
// scoped to a single node so the detail page doesn't have to filter the whole
// fleet array.
//
// GET /api/v1/fleet/nodes/{group}/{node}/services
func (m *Module) handleGetFleetNodeServices(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	node := chi.URLParam(r, "node")
	if group == "" || node == "" {
		writeError(w, http.StatusBadRequest, "group and node are required")
		return
	}
	mods, err := readNodeModules(group, node)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": mods})
}

// handlePutFleetNodeService upserts a Service manifest at
// config/services/{name}.yaml in the node's gitops repo. The body may set any
// subset of {running, version}; missing fields preserve existing values when
// the file already exists, or take sensible defaults when creating.
//
// PUT /api/v1/fleet/nodes/{group}/{node}/services/{name}
func (m *Module) handlePutFleetNodeService(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	node := chi.URLParam(r, "node")
	name := chi.URLParam(r, "name")
	if group == "" || node == "" || name == "" {
		writeError(w, http.StatusBadRequest, "group, node and name are required")
		return
	}
	if !validServiceName(name) {
		writeError(w, http.StatusBadRequest, "invalid service name (use a-z 0-9 - _)")
		return
	}

	var body struct {
		Running *bool   `json:"running,omitempty"`
		Version *string `json:"version,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	store := gitserver.Store()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "git server module not enabled")
		return
	}

	relPath := "config/services/" + name + ".yaml"
	res := manifest.ServiceResource{
		ResourceHeader: manifest.ResourceHeader{
			APIVersion: manifest.APIVersion,
			Kind:       manifest.KindService,
			Metadata:   manifest.Metadata{Name: name},
		},
		Spec: manifest.ServiceSpec{Version: "latest", Running: true},
	}

	existing, err := store.ReadFile(group, node, relPath)
	if err == nil {
		var prev manifest.ServiceResource
		if uerr := yaml.Unmarshal(existing, &prev); uerr == nil {
			res.Spec = prev.Spec
		}
	} else if !os.IsNotExist(err) && !strings.Contains(err.Error(), "no such file") {
		writeError(w, http.StatusInternalServerError, "read existing: "+err.Error())
		return
	}

	if body.Running != nil {
		res.Spec.Running = *body.Running
	}
	if body.Version != nil {
		res.Spec.Version = *body.Version
	}

	out, err := yaml.Marshal(&res)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal manifest: "+err.Error())
		return
	}

	msg := fmt.Sprintf("fleet: update service %s (running=%v version=%s)", name, res.Spec.Running, res.Spec.Version)
	if err := store.WriteFile(group, node, relPath, out, msg); err != nil {
		writeError(w, http.StatusInternalServerError, "write manifest: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, fleetModule{ID: name, Version: res.Spec.Version, Running: res.Spec.Running})
}

// handleDeleteFleetNodeService removes config/services/{name}.yaml from the
// node's gitops repo. The edge orchestrator's reconciler will tear down the
// module on the next sync.
//
// DELETE /api/v1/fleet/nodes/{group}/{node}/services/{name}
func (m *Module) handleDeleteFleetNodeService(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	node := chi.URLParam(r, "node")
	name := chi.URLParam(r, "name")
	if group == "" || node == "" || name == "" {
		writeError(w, http.StatusBadRequest, "group, node and name are required")
		return
	}
	if !validServiceName(name) {
		writeError(w, http.StatusBadRequest, "invalid service name")
		return
	}
	store := gitserver.Store()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "git server module not enabled")
		return
	}
	relPath := "config/services/" + name + ".yaml"
	msg := fmt.Sprintf("fleet: remove service %s", name)
	if err := store.DeleteFile(group, node, relPath, msg); err != nil {
		writeError(w, http.StatusInternalServerError, "delete manifest: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validServiceName mirrors what we accept as a manifest metadata.name segment
// AND as a filename — keep this conservative.
func validServiceName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// handleDeleteFleetNode evicts a node from the sparkplug-host inventory map and
// deletes its bare gitops repo. If the edge keeps publishing NBIRTH it will
// reappear in inventory, but its `git pull` against this mantle will then 404,
// which is the natural way to signal "you're no longer adopted here."
//
// DELETE /api/v1/fleet/nodes/{group}/{node}
func (m *Module) handleDeleteFleetNode(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	node := chi.URLParam(r, "node")
	if group == "" || node == "" {
		writeError(w, http.StatusBadRequest, "group and node are required")
		return
	}

	reqBody, _ := json.Marshal(map[string]string{"groupId": group, "nodeId": node})
	if _, err := m.bus.Request(sparkplug.SubjectHostNodesDelete, reqBody, busTimeout); err != nil {
		writeError(w, http.StatusBadGateway, "sparkplug-host module unavailable: "+err.Error())
		return
	}

	var repoErr string
	if srv := gitserver.Server(); srv != nil {
		if err := srv.DeleteRepo(r.Context(), repoNameForFleet(group, node)); err != nil {
			repoErr = err.Error()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"groupId": group,
		"nodeId":  node,
		"repoError": repoErr,
	})
}

func sanitizeRepoSegment(s string) string {
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
