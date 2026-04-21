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
// Queries running scanner modules via bus request for live variable data.
// GET /api/v1/variables?moduleId=
func (m *Module) handleListVariables(w http.ResponseWriter, r *http.Request) {
	moduleID := r.URL.Query().Get("moduleId")

	// Determine which module IDs to query. Only scanner/protocol modules
	// respond to variable requests — skip infrastructure modules like
	// api, orchestrator, caddy, telemetry, etc.
	variableServiceTypes := map[string]bool{
		"ethernetip": true, "opcua": true, "snmp": true, "modbus": true,
		"gateway": true, "plc": true, "network": true,
	}

	var moduleIDs []string
	if moduleID != "" {
		moduleIDs = []string{moduleID}
	} else {
		keys, err := m.bus.KVKeys(topics.BucketHeartbeats)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list heartbeats: "+err.Error())
			return
		}
		for _, key := range keys {
			data, _, err := m.bus.KVGet(topics.BucketHeartbeats, key)
			if err != nil {
				continue
			}
			var hb ttypes.ServiceHeartbeat
			if json.Unmarshal(data, &hb) != nil {
				continue
			}
			if variableServiceTypes[hb.ServiceType] {
				moduleIDs = append(moduleIDs, key)
			}
		}
	}

	type varResult struct {
		vars []json.RawMessage
	}

	results := make([]varResult, len(moduleIDs))
	var wg sync.WaitGroup
	for i, mid := range moduleIDs {
		wg.Add(1)
		go func(idx int, moduleID string) {
			defer wg.Done()
			resp, err := m.bus.Request(topics.Variables(moduleID), nil, 500*time.Millisecond)
			if err != nil {
				return
			}
			var vars []json.RawMessage
			if json.Unmarshal(resp, &vars) == nil {
				results[idx] = varResult{vars: vars}
			}
		}(i, mid)
	}
	wg.Wait()

	allVars := make([]json.RawMessage, 0)
	for _, r := range results {
		allVars = append(allVars, r.vars...)
	}

	writeJSON(w, http.StatusOK, allVars)
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

			// Send as array — frontend iterates with for...of.
			vals := make([]json.RawMessage, 0, len(snapshot))
			for _, v := range snapshot {
				vals = append(vals, v)
			}
			sse.WriteEvent("batch", vals)
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
