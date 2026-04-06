//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	ttypes "github.com/joyautomation/tentacle/types"
)

// ServiceInfo is the combined service response returned by handleListServices.
type ServiceInfo struct {
	ModuleID    string                 `json:"moduleId"`
	ServiceType string                `json:"serviceType"`
	Enabled     bool                   `json:"enabled"`
	LastSeen    int64                  `json:"lastSeen"`
	StartedAt   int64                  `json:"startedAt"`
	Version     string                 `json:"version,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// handleListServices returns all known services with heartbeat and enabled state.
// GET /api/v1/services
func (m *Module) handleListServices(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketHeartbeats)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list heartbeat keys: "+err.Error())
		return
	}

	result := make([]ServiceInfo, 0, len(keys))

	for _, key := range keys {
		data, _, err := m.bus.KVGet(topics.BucketHeartbeats, key)
		if err != nil {
			continue
		}
		var hb ttypes.ServiceHeartbeat
		if err := json.Unmarshal(data, &hb); err != nil {
			continue
		}

		info := ServiceInfo{
			ModuleID:    hb.ModuleID,
			ServiceType: hb.ServiceType,
			Enabled:     true, // default to enabled
			LastSeen:    hb.LastSeen,
			StartedAt:   hb.StartedAt,
			Version:     hb.Version,
			Metadata:    hb.Metadata,
		}

		// Check if we have an explicit enabled state.
		if enabledData, _, err := m.bus.KVGet(topics.BucketServiceEnabled, hb.ModuleID); err == nil {
			var se ttypes.ServiceEnabledKV
			if json.Unmarshal(enabledData, &se) == nil {
				info.Enabled = se.Enabled
			}
		}

		result = append(result, info)
	}

	writeJSON(w, http.StatusOK, result)
}

// handleSetServiceEnabled updates the enabled state for a service.
// PUT /api/v1/services/{moduleId}/enabled
func (m *Module) handleSetServiceEnabled(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	entry := ttypes.ServiceEnabledKV{
		ModuleID:  moduleID,
		Enabled:   body.Enabled,
		UpdatedAt: time.Now().UnixMilli(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal entry: "+err.Error())
		return
	}

	if _, err := m.bus.KVPut(topics.BucketServiceEnabled, moduleID, data); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store enabled state: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// handleRestartService sends a restart command to the orchestrator.
// POST /api/v1/services/{moduleId}/restart
func (m *Module) handleRestartService(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")

	cmd := itypes.OrchestratorCommandRequest{
		RequestID: newRequestID(),
		Action:    "restart-service",
		ModuleID:  moduleID,
		Timestamp: time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(cmd)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal command: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.OrchestratorCommand, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "orchestrator request failed: "+err.Error())
		return
	}

	var result json.RawMessage
	if json.Unmarshal(resp, &result) != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleGetServiceLogs returns buffered log entries for a service type.
// GET /api/v1/services/{serviceType}/logs?limit=
func (m *Module) handleGetServiceLogs(w http.ResponseWriter, r *http.Request) {
	serviceType := chi.URLParam(r, "serviceType")

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	m.logsMu.RLock()
	// Filter by serviceType.
	filtered := make([]ttypes.ServiceLogEntry, 0)
	for _, entry := range m.logBuf {
		if entry.ServiceType == serviceType {
			filtered = append(filtered, entry)
		}
	}
	m.logsMu.RUnlock()

	// Apply limit from the end (most recent).
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}

	writeJSON(w, http.StatusOK, filtered)
}

// handleStreamServiceLogs streams log entries for a service type via SSE.
// GET /api/v1/services/{serviceType}/logs/stream
func (m *Module) handleStreamServiceLogs(w http.ResponseWriter, r *http.Request) {
	serviceType := chi.URLParam(r, "serviceType")

	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	subject := "service.logs." + serviceType + ".>"

	sub, err := m.bus.Subscribe(subject, func(_ string, data []byte, _ bus.ReplyFunc) {
		var entry ttypes.ServiceLogEntry
		if json.Unmarshal(data, &entry) != nil {
			return
		}
		sse.WriteEvent("log", entry)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}
