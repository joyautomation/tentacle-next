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

// handleGetNftablesConfig retrieves the current nftables NAT configuration.
// GET /api/v1/nftables/config
func (m *Module) handleGetNftablesConfig(w http.ResponseWriter, r *http.Request) {
	req := itypes.NftablesCommandRequest{
		RequestID: newRequestID(),
		Action:    "get-config",
		Timestamp: time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.NftablesCommand, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get nftables config: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleApplyNftablesConfig applies a new nftables NAT configuration.
// PUT /api/v1/nftables/config
func (m *Module) handleApplyNftablesConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NatRules []itypes.NatRule `json:"natRules"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	req := itypes.NftablesCommandRequest{
		RequestID: newRequestID(),
		Action:    "apply-config",
		NatRules:  body.NatRules,
		Timestamp: time.Now().UnixMilli(),
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal request: "+err.Error())
		return
	}

	resp, err := m.bus.Request(topics.NftablesCommand, payload, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply nftables config: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleStreamNftablesConfig streams nftables rule changes via SSE.
// GET /api/v1/nftables/stream
func (m *Module) handleStreamNftablesConfig(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	sub, err := m.bus.Subscribe(topics.NftablesRules, func(_ string, data []byte, _ bus.ReplyFunc) {
		sse.WriteEvent("nftables", json.RawMessage(data))
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}
