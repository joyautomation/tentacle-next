//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	ttypes "github.com/joyautomation/tentacle/types"
)

// handleListVariables returns all PLC variables, optionally filtered by moduleId.
// GET /api/v1/variables?moduleId=
func (m *Module) handleListVariables(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketPlcVariables)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list variable keys: "+err.Error())
		return
	}

	moduleID := r.URL.Query().Get("moduleId")
	result := make([]ttypes.PlcVariableKV, 0, len(keys))

	for _, key := range keys {
		data, _, err := m.bus.KVGet(topics.BucketPlcVariables, key)
		if err != nil {
			continue
		}
		var v ttypes.PlcVariableKV
		if err := json.Unmarshal(data, &v); err != nil {
			continue
		}
		if moduleID != "" && v.ModuleID != moduleID {
			continue
		}
		result = append(result, v)
	}

	writeJSON(w, http.StatusOK, result)
}

// handleGetVariable returns a single PLC variable by variableId.
// GET /api/v1/variables/{variableId}
func (m *Module) handleGetVariable(w http.ResponseWriter, r *http.Request) {
	variableID := chi.URLParam(r, "variableId")

	data, _, err := m.bus.KVGet(topics.BucketPlcVariables, variableID)
	if err != nil {
		writeError(w, http.StatusNotFound, "variable not found: "+err.Error())
		return
	}

	var v ttypes.PlcVariableKV
	if err := json.Unmarshal(data, &v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmarshal variable: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, v)
}

// handleWriteVariable publishes a write command for a variable.
// PUT /api/v1/variables/{moduleId}/{variableId}/value
func (m *Module) handleWriteVariable(w http.ResponseWriter, r *http.Request) {
	moduleID := chi.URLParam(r, "moduleId")
	variableID := chi.URLParam(r, "variableId")

	var body struct {
		Value interface{} `json:"value"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	payload, err := json.Marshal(map[string]interface{}{"value": body.Value})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal command: "+err.Error())
		return
	}

	if err := m.bus.Publish(topics.Command(moduleID, variableID), payload); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to publish command: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleStreamVariables streams all variable data changes via SSE.
// GET /api/v1/variables/stream?moduleId=
func (m *Module) handleStreamVariables(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	moduleID := r.URL.Query().Get("moduleId")
	subject := topics.AllData()
	if moduleID != "" {
		subject = topics.DataWildcard(moduleID)
	}

	sub, err := m.bus.Subscribe(subject, func(_ string, data []byte, _ bus.ReplyFunc) {
		var msg ttypes.PlcDataMessage
		if json.Unmarshal(data, &msg) != nil {
			return
		}
		sse.WriteEvent("variable", msg)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// handleStreamVariableBatch streams batched variable data changes via SSE.
// Collects changes over 2.5s intervals and emits them as a single event.
// GET /api/v1/variables/stream/batch?moduleId=
func (m *Module) handleStreamVariableBatch(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	moduleID := r.URL.Query().Get("moduleId")
	subject := topics.AllData()
	if moduleID != "" {
		subject = topics.DataWildcard(moduleID)
	}

	var mu sync.Mutex
	batch := make(map[string]json.RawMessage)

	sub, err := m.bus.Subscribe(subject, func(subj string, data []byte, _ bus.ReplyFunc) {
		mu.Lock()
		batch[subj] = json.RawMessage(data)
		mu.Unlock()
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	ticker := time.NewTicker(2500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			mu.Lock()
			if len(batch) == 0 {
				mu.Unlock()
				continue
			}
			snapshot := batch
			batch = make(map[string]json.RawMessage)
			mu.Unlock()

			sse.WriteEvent("batch", snapshot)
		}
	}
}

// handleStreamVariable streams data changes for a single variable via SSE.
// GET /api/v1/variables/{variableId}/stream
func (m *Module) handleStreamVariable(w http.ResponseWriter, r *http.Request) {
	variableID := chi.URLParam(r, "variableId")

	// Look up the variable to find moduleId and deviceId.
	data, _, err := m.bus.KVGet(topics.BucketPlcVariables, variableID)
	if err != nil {
		writeError(w, http.StatusNotFound, "variable not found: "+err.Error())
		return
	}

	var v ttypes.PlcVariableKV
	if err := json.Unmarshal(data, &v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmarshal variable: "+err.Error())
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	subject := topics.Data(v.ModuleID, v.DeviceID, variableID)

	sub, err := m.bus.Subscribe(subject, func(_ string, data []byte, _ bus.ReplyFunc) {
		var msg ttypes.PlcDataMessage
		if json.Unmarshal(data, &msg) != nil {
			return
		}
		sse.WriteEvent("variable", msg)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}
