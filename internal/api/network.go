//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
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

// handleDiscoverNetworkInterfaces returns available network interfaces for
// gateway configuration. Each interface can be added as a gateway device.
// GET /api/v1/gateways/{gatewayId}/network/discover
func (m *Module) handleDiscoverNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

	// Call network.browse to discover interfaces.
	resp, err := m.bus.Request(topics.Browse("network"), []byte("{}"), busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "network module not running or failed to discover: "+err.Error())
		return
	}

	// Parse the browse result to extract interface names.
	var browseResult struct {
		Instances []struct {
			Name         string            `json:"name"`
			TemplateName string            `json:"templateName"`
			MemberTags   map[string]string `json:"memberTags"`
		} `json:"instances"`
		Udts map[string]json.RawMessage `json:"udts"`
	}
	if err := json.Unmarshal(resp, &browseResult); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse browse result: "+err.Error())
		return
	}

	// Check which interfaces are already configured as gateway devices.
	var configuredInterfaces map[string]bool
	cfg, err := m.getGatewayConfig(gatewayID)
	if err == nil {
		configuredInterfaces = make(map[string]bool)
		for deviceID, dev := range cfg.Devices {
			if dev.Protocol == "network" {
				configuredInterfaces[deviceID] = true
			}
		}
	}

	type discoveredInterface struct {
		Name       string `json:"name"`
		Configured bool   `json:"configured"`
	}
	interfaces := make([]discoveredInterface, 0, len(browseResult.Instances))
	for _, inst := range browseResult.Instances {
		interfaces = append(interfaces, discoveredInterface{
			Name:       inst.Name,
			Configured: configuredInterfaces[inst.Name],
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"interfaces": interfaces,
	})
}

// handleAddNetworkInterfaces adds selected network interfaces as gateway
// devices with UDT variables.
// POST /api/v1/gateways/{gatewayId}/network/add
func (m *Module) handleAddNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

	var body struct {
		Interfaces []string `json:"interfaces"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if len(body.Interfaces) == 0 {
		writeError(w, http.StatusBadRequest, "at least one interface is required")
		return
	}

	cfg, err := m.getGatewayConfig(gatewayID)
	if err != nil {
		cfg = &itypes.GatewayConfigKV{
			GatewayID:    gatewayID,
			Devices:      make(map[string]itypes.GatewayDeviceConfig),
			Variables:    make(map[string]itypes.GatewayVariableConfig),
			UdtTemplates: make(map[string]itypes.GatewayUdtTemplateConfig),
			UdtVariables: make(map[string]itypes.GatewayUdtVariableConfig),
		}
	}
	ensureMaps(cfg)

	// Ensure the NetworkInterface UDT template exists.
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

	memberNames := []string{
		"operstate", "carrier", "speed", "mtu", "mac",
		"rxBytes", "txBytes", "rxPackets", "txPackets",
		"rxErrors", "txErrors", "rxDropped", "txDropped",
	}

	for _, ifaceName := range body.Interfaces {
		// Create device.
		cfg.Devices[ifaceName] = itypes.GatewayDeviceConfig{
			Protocol: "network",
		}

		// Create UDT variable with member tags mapping member name → member name
		// (network module uses the member name directly as the tag).
		memberTags := make(map[string]string, len(memberNames))
		for _, name := range memberNames {
			memberTags[name] = name
		}

		cfg.UdtVariables[ifaceName] = itypes.GatewayUdtVariableConfig{
			ID:           ifaceName,
			DeviceID:     ifaceName,
			Tag:          ifaceName,
			TemplateName: "NetworkInterface",
			MemberTags:   memberTags,
		}
	}

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
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
