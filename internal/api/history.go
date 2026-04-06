//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/internal/topics"
)

// handleQueryHistory queries historical variable data.
// GET /api/v1/history?start=&end=&variables=&interval=&samples=&raw=
func (m *Module) handleQueryHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	startStr := q.Get("start")
	if startStr == "" {
		writeError(w, http.StatusBadRequest, "start parameter is required")
		return
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid start parameter: "+err.Error())
		return
	}

	endStr := q.Get("end")
	if endStr == "" {
		writeError(w, http.StatusBadRequest, "end parameter is required")
		return
	}
	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid end parameter: "+err.Error())
		return
	}

	variablesStr := q.Get("variables")
	if variablesStr == "" {
		writeError(w, http.StatusBadRequest, "variables parameter is required")
		return
	}
	var variables []itypes.HistoryVariableRef
	if err := json.Unmarshal([]byte(variablesStr), &variables); err != nil {
		writeError(w, http.StatusBadRequest, "invalid variables parameter: "+err.Error())
		return
	}

	req := itypes.HistoryQueryRequest{
		RequestID: newRequestID(),
		Start:     start,
		End:       end,
		Variables: variables,
		Interval:  q.Get("interval"),
		Timestamp: time.Now().UnixMilli(),
	}

	if samplesStr := q.Get("samples"); samplesStr != "" {
		samples, err := strconv.Atoi(samplesStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid samples parameter: "+err.Error())
			return
		}
		req.Samples = samples
	}

	if rawStr := q.Get("raw"); rawStr != "" {
		raw, err := strconv.ParseBool(rawStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid raw parameter: "+err.Error())
			return
		}
		req.Raw = raw
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.HistoryQuery, payload, 30*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, "history module unavailable: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleGetHistoryUsage returns history storage usage statistics.
// GET /api/v1/history/usage
func (m *Module) handleGetHistoryUsage(w http.ResponseWriter, r *http.Request) {
	req := map[string]interface{}{
		"requestId": newRequestID(),
		"timestamp": time.Now().UnixMilli(),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.HistoryUsage, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, "history module unavailable: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleGetHistoryEnabled checks whether the history module is running.
// GET /api/v1/history/enabled
func (m *Module) handleGetHistoryEnabled(w http.ResponseWriter, r *http.Request) {
	req := map[string]interface{}{
		"requestId": newRequestID(),
		"timestamp": time.Now().UnixMilli(),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.HistoryEnabled, payload, busTimeout)
	if err != nil {
		// History module is not running — return enabled: false.
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}
