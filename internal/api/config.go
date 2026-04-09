//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/topics"
)

// configEntry represents a single configuration key-value entry.
type configEntry struct {
	ModuleID string `json:"moduleId"`
	EnvVar   string `json:"envVar"`
	Value    string `json:"value"`
}

// handleGetAllConfig returns all configuration entries from the tentacle_config KV bucket.
// GET /api/v1/config
func (m *Module) handleGetAllConfig(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketTentacleConfig)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list config keys: "+err.Error())
		return
	}

	entries := make([]configEntry, 0, len(keys))
	for _, key := range keys {
		parts := strings.SplitN(key, ".", 2)
		if len(parts) != 2 {
			continue
		}
		data, _, err := m.bus.KVGet(topics.BucketTentacleConfig, key)
		if err != nil {
			continue
		}
		entries = append(entries, configEntry{
			ModuleID: parts[0],
			EnvVar:   parts[1],
			Value:    string(data),
		})
	}

	writeJSON(w, http.StatusOK, entries)
}

// handleGetServiceConfig returns configuration entries for a specific module.
// GET /api/v1/config/{moduleId}
func (m *Module) handleGetServiceConfig(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")
	prefix := moduleID + "."

	keys, err := m.bus.KVKeys(topics.BucketTentacleConfig)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list config keys: "+err.Error())
		return
	}

	entries := make([]configEntry, 0)
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		data, _, err := m.bus.KVGet(topics.BucketTentacleConfig, key)
		if err != nil {
			continue
		}
		envVar := strings.TrimPrefix(key, prefix)
		entries = append(entries, configEntry{
			ModuleID: moduleID,
			EnvVar:   envVar,
			Value:    string(data),
		})
	}

	writeJSON(w, http.StatusOK, entries)
}

// handleUpdateServiceConfig updates a single configuration entry.
// PUT /api/v1/config/{moduleId}/{envVar}
func (m *Module) handleUpdateServiceConfig(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")
	envVar := chi.URLParam(r, "envVar")

	var body struct {
		Value string `json:"value"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	key := moduleID + "." + envVar
	if _, err := m.bus.KVPut(topics.BucketTentacleConfig, key, []byte(body.Value)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update config: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, configEntry{
		ModuleID: moduleID,
		EnvVar:   envVar,
		Value:    body.Value,
	})
}

// handleGetConfigSchema returns the config field definitions for a module by
// sending a bus request to "{moduleId}.config.schema".
// GET /api/v1/config/{moduleId}/schema
func (m *Module) handleGetConfigSchema(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")
	subject := topics.ConfigSchema(moduleID)

	resp, err := m.bus.Request(subject, nil, 3*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, []config.FieldDef{})
		return
	}

	var fields []config.FieldDef
	if err := json.Unmarshal(resp, &fields); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse schema: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, fields)
}
