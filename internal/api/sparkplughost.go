//go:build api || all

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/joyautomation/tentacle/internal/sparkplug"
)

// handleGetSparkplugHostNodes returns the sparkplug-host node inventory snapshot.
// GET /api/v1/sparkplug-host/nodes
func (m *Module) handleGetSparkplugHostNodes(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(sparkplug.SubjectHostNodes, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, "sparkplug-host module unavailable: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleStreamSparkplugHostNodes streams inventory changes via SSE, polling at 2.5s.
// GET /api/v1/sparkplug-host/nodes/stream
func (m *Module) handleStreamSparkplugHostNodes(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ticker := time.NewTicker(2500 * time.Millisecond)
	defer ticker.Stop()

	var prev []byte
	if resp, err := m.bus.Request(sparkplug.SubjectHostNodes, []byte("{}"), busTimeout); err == nil {
		prev = resp
		sse.WriteEvent("nodes", json.RawMessage(resp))
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			resp, err := m.bus.Request(sparkplug.SubjectHostNodes, []byte("{}"), busTimeout)
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
