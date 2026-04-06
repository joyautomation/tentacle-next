//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// handleListDesiredServices returns all desired service entries from KV.
// GET /api/v1/orchestrator/desired-services
func (m *Module) handleListDesiredServices(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketDesiredServices)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list desired service keys: "+err.Error())
		return
	}

	result := make([]itypes.DesiredServiceKV, 0, len(keys))

	for _, key := range keys {
		data, _, err := m.bus.KVGet(topics.BucketDesiredServices, key)
		if err != nil {
			continue
		}
		var kv itypes.DesiredServiceKV
		if err := json.Unmarshal(data, &kv); err != nil {
			continue
		}
		result = append(result, kv)
	}

	writeJSON(w, http.StatusOK, result)
}

// handleSetDesiredService creates or updates a desired service entry.
// PUT /api/v1/orchestrator/desired-services/{moduleId}
func (m *Module) handleSetDesiredService(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")

	var body struct {
		Version string `json:"version"`
		Running bool   `json:"running"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	entry := itypes.DesiredServiceKV{
		ModuleID:  moduleID,
		Version:   body.Version,
		Running:   body.Running,
		UpdatedAt: time.Now().UnixMilli(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal entry: "+err.Error())
		return
	}

	if _, err := m.bus.KVPut(topics.BucketDesiredServices, moduleID, data); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store desired service: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// handleDeleteDesiredService removes a desired service entry.
// DELETE /api/v1/orchestrator/desired-services/{moduleId}
func (m *Module) handleDeleteDesiredService(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")

	if err := m.bus.KVDelete(topics.BucketDesiredServices, moduleID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete desired service: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleListServiceStatuses returns all service status entries from KV.
// GET /api/v1/orchestrator/service-statuses
func (m *Module) handleListServiceStatuses(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketServiceStatus)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list service status keys: "+err.Error())
		return
	}

	result := make([]itypes.ServiceStatusKV, 0, len(keys))

	for _, key := range keys {
		data, _, err := m.bus.KVGet(topics.BucketServiceStatus, key)
		if err != nil {
			continue
		}
		var kv itypes.ServiceStatusKV
		if err := json.Unmarshal(data, &kv); err != nil {
			continue
		}
		result = append(result, kv)
	}

	writeJSON(w, http.StatusOK, result)
}

// handleListModules requests the module registry from the orchestrator.
// GET /api/v1/orchestrator/modules
func (m *Module) handleListModules(w http.ResponseWriter, r *http.Request) {
	req := itypes.OrchestratorCommandRequest{
		RequestID: newRequestID(),
		Action:    "get-registry",
		Timestamp: time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal command: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.OrchestratorCommand, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "orchestrator request failed: "+err.Error())
		return
	}

	var response itypes.OrchestratorCommandResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmarshal response: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response.Modules)
}

// handleGetModuleVersions requests version info for a specific module.
// GET /api/v1/orchestrator/modules/{moduleId}/versions
func (m *Module) handleGetModuleVersions(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")

	req := itypes.OrchestratorCommandRequest{
		RequestID: newRequestID(),
		Action:    "get-module-versions",
		ModuleID:  moduleID,
		Timestamp: time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal command: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.OrchestratorCommand, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "orchestrator request failed: "+err.Error())
		return
	}

	var response itypes.OrchestratorCommandResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmarshal response: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response.Versions)
}

// handleCheckInternet checks internet connectivity via the orchestrator.
// GET /api/v1/orchestrator/internet
func (m *Module) handleCheckInternet(w http.ResponseWriter, r *http.Request) {
	req := itypes.OrchestratorCommandRequest{
		RequestID: newRequestID(),
		Action:    "check-internet",
		Timestamp: time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal command: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.OrchestratorCommand, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "orchestrator request failed: "+err.Error())
		return
	}

	var response itypes.OrchestratorCommandResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmarshal response: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]*bool{"online": response.Online})
}
