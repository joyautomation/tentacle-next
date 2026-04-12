//go:build api || all

package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/joyautomation/tentacle/internal/topics"
)

// handleGetProfinetConfig returns the current PROFINET IO Device configuration from KV.
// GET /api/v1/profinet/config
func (m *Module) handleGetProfinetConfig(w http.ResponseWriter, r *http.Request) {
	// Read config from KV bucket using the default module ID "profinet"
	keys, err := m.bus.KVKeys(topics.BucketProfinetConfig)
	if err != nil || len(keys) == 0 {
		writeJSON(w, http.StatusOK, nil)
		return
	}

	data, _, err := m.bus.KVGet(topics.BucketProfinetConfig, keys[0])
	if err != nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handlePutProfinetConfig saves a new PROFINET IO Device configuration.
// PUT /api/v1/profinet/config
func (m *Module) handlePutProfinetConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	// Forward to the PROFINET module via NATS request/reply for validation
	resp, err := m.bus.Request(topics.ProfinetConfigure, body, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profinet configure failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleGetProfinetStatus returns the current PROFINET IO Device status.
// GET /api/v1/profinet/status
func (m *Module) handleGetProfinetStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(topics.ProfinetStatus, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profinet status request failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleGetProfinetVariables returns current PROFINET IO Device tag values.
// GET /api/v1/profinet/variables
func (m *Module) handleGetProfinetVariables(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(topics.ProfinetVariables, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profinet variables request failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleGetProfinetGsdml generates and returns the GSDML XML for the IO Device.
// GET /api/v1/profinet/gsdml
func (m *Module) handleGetProfinetGsdml(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(topics.ProfinetGsdml, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profinet gsdml request failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleListPNControllerSubs lists all PROFINET Controller subscriptions from KV.
// GET /api/v1/profinetcontroller/subscriptions
func (m *Module) handleListPNControllerSubs(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketScannerProfinetController)
	if err != nil {
		// Bucket may not exist yet — return empty list
		writeJSON(w, http.StatusOK, []json.RawMessage{})
		return
	}

	subs := make([]json.RawMessage, 0, len(keys))
	for _, key := range keys {
		data, _, err := m.bus.KVGet(topics.BucketScannerProfinetController, key)
		if err != nil {
			continue
		}
		subs = append(subs, json.RawMessage(data))
	}

	writeJSON(w, http.StatusOK, subs)
}
