//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// handleGetNetworkInterfaces returns the current network interface state.
// GET /api/v1/network/interfaces
func (m *Module) handleGetNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	resp, err := m.bus.Request(topics.NetworkState, []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get network interfaces: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleGetNetworkConfig retrieves the current network configuration.
// GET /api/v1/network/config
func (m *Module) handleGetNetworkConfig(w http.ResponseWriter, r *http.Request) {
	req := itypes.NetworkCommandRequest{
		RequestID: newRequestID(),
		Action:    "get-config",
		Timestamp: time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.NetworkCommand, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get network config: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleApplyNetworkConfig applies a new network configuration.
// PUT /api/v1/network/config
func (m *Module) handleApplyNetworkConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interfaces []itypes.NetworkInterfaceConfig `json:"interfaces"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	req := itypes.NetworkCommandRequest{
		RequestID:  newRequestID(),
		Action:     "apply-config",
		Interfaces: body.Interfaces,
		Timestamp:  time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.NetworkCommand, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply network config: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleStreamNetworkState streams network interface state changes via SSE.
// GET /api/v1/network/stream
func (m *Module) handleStreamNetworkState(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	sub, err := m.bus.Subscribe(topics.NetworkInterfaces, func(_ string, data []byte, _ bus.ReplyFunc) {
		sse.WriteEvent("network", json.RawMessage(data))
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}
