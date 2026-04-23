//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/scanner"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// ─── Source Helpers ────────────────────────────────────────────────────────

// getSource reads a single SourceConfig from the shared sources bucket.
func (m *Module) getSource(deviceID string) (itypes.SourceConfig, bool) {
	data, _, err := m.bus.KVGet(topics.BucketSources, deviceID)
	if err != nil {
		return itypes.SourceConfig{}, false
	}
	var cfg itypes.SourceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return itypes.SourceConfig{}, false
	}
	return cfg, true
}

// putSource writes a SourceConfig to the shared sources bucket.
func (m *Module) putSource(deviceID string, cfg itypes.SourceConfig) error {
	return scanner.Put(m.bus, deviceID, cfg)
}

// deleteSource removes a SourceConfig from the shared sources bucket.
func (m *Module) deleteSource(deviceID string) error {
	return scanner.Delete(m.bus, deviceID)
}

// listSources returns all SourceConfig entries from the shared sources bucket.
func (m *Module) listSources() (map[string]itypes.SourceConfig, error) {
	return scanner.List(m.bus)
}

// ─── HTTP Handlers ─────────────────────────────────────────────────────────

// sourceEntry pairs a deviceId with its SourceConfig for list/get responses.
type sourceEntry struct {
	DeviceID string `json:"deviceId"`
	itypes.SourceConfig
}

// handleListSources returns all sources.
// GET /api/v1/sources
func (m *Module) handleListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := m.listSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list sources: %v", err))
		return
	}
	out := make([]sourceEntry, 0, len(sources))
	for id, cfg := range sources {
		out = append(out, sourceEntry{DeviceID: id, SourceConfig: cfg})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	writeJSON(w, http.StatusOK, out)
}

// handleGetSource returns a single source.
// GET /api/v1/sources/{deviceId}
func (m *Module) handleGetSource(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "deviceId")
	src, ok := m.getSource(deviceID)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("source %q not found", deviceID))
		return
	}
	writeJSON(w, http.StatusOK, sourceEntry{DeviceID: deviceID, SourceConfig: src})
}

// handleSetSource creates or replaces a source.
// PUT /api/v1/sources/{deviceId}
func (m *Module) handleSetSource(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "deviceId")

	var body itypes.SourceConfig
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if body.Protocol == "" {
		writeError(w, http.StatusBadRequest, "protocol is required")
		return
	}

	if err := m.putSource(deviceID, body); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put source: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, sourceEntry{DeviceID: deviceID, SourceConfig: body})
}

// handleDeleteSource removes a source.
// DELETE /api/v1/sources/{deviceId}
func (m *Module) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "deviceId")
	if err := m.deleteSource(deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete source: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
