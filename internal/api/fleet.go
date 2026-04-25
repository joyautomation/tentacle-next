//go:build api || all

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/joyautomation/tentacle/internal/topics"
)

// handleGetFleetNodes returns the fleet module's node inventory snapshot.
// GET /api/v1/fleet/nodes
func (m *Module) handleGetFleetNodes(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(topics.FleetNodes, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fleet module unavailable: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleStreamFleetNodes streams fleet inventory changes via SSE, polling at 2.5s.
// GET /api/v1/fleet/nodes/stream
func (m *Module) handleStreamFleetNodes(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ticker := time.NewTicker(2500 * time.Millisecond)
	defer ticker.Stop()

	var prev []byte
	if resp, err := m.bus.Request(topics.FleetNodes, []byte("{}"), busTimeout); err == nil {
		prev = resp
		sse.WriteEvent("nodes", json.RawMessage(resp))
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			resp, err := m.bus.Request(topics.FleetNodes, []byte("{}"), busTimeout)
			if err != nil {
				continue
			}
			if bytes.Equal(resp, prev) {
				continue
			}
			prev = resp
			sse.WriteEvent("nodes", json.RawMessage(resp))
		}
	}
}
