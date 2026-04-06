//go:build api || all

package api

import (
	"net/http"

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
