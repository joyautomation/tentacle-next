//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
)

// handleBrowseTags sends a browse request to the protocol scanner.
// POST /api/v1/browse/{protocol}
func (m *Module) handleBrowseTags(w http.ResponseWriter, r *http.Request) {
	protocol := chi.URLParam(r, "protocol")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	resp, err := m.bus.Request(topics.Browse(protocol), body, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "browse request failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleStreamBrowseProgress streams browse progress events via SSE.
// GET /api/v1/browse/{browseId}/progress
func (m *Module) handleStreamBrowseProgress(w http.ResponseWriter, r *http.Request) {
	browseID := chi.URLParam(r, "browseId")

	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Subscribe with wildcard to catch any protocol's browse progress.
	subject := fmt.Sprintf("*.browse.progress.%s", browseID)

	sub, err := m.bus.Subscribe(subject, func(_ string, data []byte, _ bus.ReplyFunc) {
		sse.WriteEvent("progress", json.RawMessage(data))
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// handleStartGatewayBrowse initiates a browse for a specific gateway.
// POST /api/v1/gateways/{gatewayId}/browse
func (m *Module) handleStartGatewayBrowse(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	// Extract protocol from the request body.
	var params map[string]interface{}
	if err := json.Unmarshal(body, &params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	protocol, _ := params["protocol"].(string)
	if protocol == "" {
		writeError(w, http.StatusBadRequest, "missing required field: protocol")
		return
	}

	// Browse can be slow, use a longer timeout.
	resp, err := m.bus.Request(topics.Browse(protocol), body, 30*time.Second)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "browse request failed: "+err.Error())
		return
	}

	// Track browse state and cache result.
	deviceID, _ := params["deviceId"].(string)
	browseID, _ := params["browseId"].(string)
	async, _ := params["async"].(bool)

	cacheKey := gatewayID + ":" + deviceID

	m.browseMu.Lock()
	m.browseCache[cacheKey] = json.RawMessage(resp)
	if browseID != "" {
		m.browseStates[browseID] = &BrowseState{
			BrowseID:  browseID,
			GatewayID: gatewayID,
			DeviceID:  deviceID,
			Protocol:  protocol,
			Status:    "completed",
			StartedAt: time.Now().UnixMilli(),
			Result:    json.RawMessage(resp),
		}
	}
	m.browseMu.Unlock()

	if async {
		// For async browse, return just the browseId.
		writeJSON(w, http.StatusOK, map[string]string{"browseId": browseID})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleStreamGatewayBrowseProgress streams gateway browse progress events via SSE.
// GET /api/v1/gateways/{gatewayId}/browse/{browseId}/progress
func (m *Module) handleStreamGatewayBrowseProgress(w http.ResponseWriter, r *http.Request) {
	browseID := chi.URLParam(r, "browseId")

	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Subscribe with wildcard to catch any protocol's browse progress.
	subject := fmt.Sprintf("*.browse.progress.%s", browseID)

	sub, err := m.bus.Subscribe(subject, func(_ string, data []byte, _ bus.ReplyFunc) {
		sse.WriteEvent("progress", json.RawMessage(data))
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// handleScannerSubscribe subscribes to scanner data for a protocol.
// POST /api/v1/scanner/{protocol}/subscribe
func (m *Module) handleScannerSubscribe(w http.ResponseWriter, r *http.Request) {
	protocol := chi.URLParam(r, "protocol")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	resp, err := m.bus.Request(topics.ScannerSubscribe(protocol), body, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scanner subscribe failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleScannerUnsubscribe unsubscribes from scanner data for a protocol.
// POST /api/v1/scanner/{protocol}/unsubscribe
func (m *Module) handleScannerUnsubscribe(w http.ResponseWriter, r *http.Request) {
	protocol := chi.URLParam(r, "protocol")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	resp, err := m.bus.Request(topics.ScannerUnsubscribe(protocol), body, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scanner unsubscribe failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}
