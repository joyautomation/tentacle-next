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

// syncNetworkInterfaces auto-discovers network interfaces via the network
// module and ensures they exist as gateway devices with UDT variables.
// Called during gateway config GET so interfaces appear automatically.
func (m *Module) syncNetworkInterfaces(cfg *itypes.GatewayConfigKV) bool {
	// Ask the network module for discovered interfaces.
	resp, err := m.bus.Request(topics.Browse("network"), []byte("{}"), 2*time.Second)
	if err != nil {
		return false // network module not running — nothing to sync
	}

	var browseResult struct {
		Instances []struct {
			Name string `json:"name"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(resp, &browseResult); err != nil || len(browseResult.Instances) == 0 {
		return false
	}

	ensureMaps(cfg)

	// Build set of discovered interface names.
	discovered := make(map[string]bool, len(browseResult.Instances))
	for _, inst := range browseResult.Instances {
		discovered[inst.Name] = true
	}

	// Remove network devices that no longer exist.
	changed := false
	for deviceID, dev := range cfg.Devices {
		if dev.Protocol == "network" && !discovered[deviceID] {
			delete(cfg.Devices, deviceID)
			delete(cfg.UdtVariables, deviceID)
			changed = true
		}
	}

	memberNames := []string{
		"operstate", "carrier", "speed", "mtu", "mac",
		"rxBytes", "txBytes", "rxPackets", "txPackets",
		"rxErrors", "txErrors", "rxDropped", "txDropped",
	}

	// Add any missing interfaces.
	for _, inst := range browseResult.Instances {
		if _, ok := cfg.Devices[inst.Name]; ok {
			continue // already configured
		}

		// Ensure the template exists.
		if _, ok := cfg.UdtTemplates["NetworkInterface"]; !ok {
			cfg.UdtTemplates["NetworkInterface"] = itypes.GatewayUdtTemplateConfig{
				Name:    "NetworkInterface",
				Version: "1.0",
				Members: []itypes.GatewayUdtTemplateMemberConfig{
					{Name: "operstate", Datatype: "string"},
					{Name: "carrier", Datatype: "boolean"},
					{Name: "speed", Datatype: "number"},
					{Name: "mtu", Datatype: "number"},
					{Name: "mac", Datatype: "string"},
					{Name: "rxBytes", Datatype: "number"},
					{Name: "txBytes", Datatype: "number"},
					{Name: "rxPackets", Datatype: "number"},
					{Name: "txPackets", Datatype: "number"},
					{Name: "rxErrors", Datatype: "number"},
					{Name: "txErrors", Datatype: "number"},
					{Name: "rxDropped", Datatype: "number"},
					{Name: "txDropped", Datatype: "number"},
				},
			}
		}

		cfg.Devices[inst.Name] = itypes.GatewayDeviceConfig{
			Protocol: "network",
		}

		memberTags := make(map[string]string, len(memberNames))
		for _, name := range memberNames {
			memberTags[name] = name
		}
		cfg.UdtVariables[inst.Name] = itypes.GatewayUdtVariableConfig{
			ID:           inst.Name,
			DeviceID:     inst.Name,
			Tag:          inst.Name,
			TemplateName: "NetworkInterface",
			MemberTags:   memberTags,
		}
		changed = true
	}

	return changed
}

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
