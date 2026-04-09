//go:build api || all

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/joyautomation/tentacle/internal/topics"
)

// handleGetMqttMetrics returns MQTT Sparkplug metrics from the mqtt module.
// GET /api/v1/mqtt/metrics
func (m *Module) handleGetMqttMetrics(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(topics.MqttMetrics, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, "mqtt module unavailable: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleStreamMqttMetrics streams MQTT Sparkplug metric changes via SSE.
// Polls the MQTT module at 2.5s intervals and only pushes when data changes.
// GET /api/v1/mqtt/metrics/stream
func (m *Module) handleStreamMqttMetrics(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ticker := time.NewTicker(2500 * time.Millisecond)
	defer ticker.Stop()

	// Send initial snapshot immediately.
	var prev []byte
	if resp, err := m.bus.Request(topics.MqttMetrics, []byte("{}"), busTimeout); err == nil {
		prev = resp
		sse.WriteEvent("metrics", json.RawMessage(resp))
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			resp, err := m.bus.Request(topics.MqttMetrics, []byte("{}"), busTimeout)
			if err != nil {
				continue
			}
			if bytes.Equal(resp, prev) {
				continue
			}
			prev = resp
			sse.WriteEvent("metrics", json.RawMessage(resp))
		}
	}
}

// handleGetStoreForwardStatus returns MQTT store-and-forward status.
// GET /api/v1/mqtt/store-forward
func (m *Module) handleGetStoreForwardStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(topics.MqttStoreForward, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, "mqtt module unavailable: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}
