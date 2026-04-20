//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/topics"
)

// handleGetPlcTaskStats returns per-task scan-time statistics for a
// single PLC module. Queries the PLC module via bus request/reply.
// GET /api/v1/plcs/{plcId}/tasks/stats
func (m *Module) handleGetPlcTaskStats(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	if plcID == "" {
		writeError(w, http.StatusBadRequest, "plcId is required")
		return
	}
	resp, err := m.bus.Request(topics.PlcTaskStats(plcID), nil, 500*time.Millisecond)
	if err != nil {
		writeError(w, http.StatusBadGateway, "plc task stats request failed: "+err.Error())
		return
	}
	var payload json.RawMessage = resp
	writeJSON(w, http.StatusOK, payload)
}
