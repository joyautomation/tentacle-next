//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	ttypes "github.com/joyautomation/tentacle/types"
)

// Ensure ttypes is used (DeadBandConfig is referenced transitively through itypes).
var _ *ttypes.DeadBandConfig

// ─── Gateway Config Helpers ─────────────────────────────────────────────────

func (m *Module) getGatewayConfig(t Target, gatewayID string) (*itypes.GatewayConfigKV, error) {
	if t.IsRemote() {
		return m.loadGatewayConfigForTarget(t, gatewayID)
	}
	data, _, err := m.bus.KVGet(topics.BucketGatewayConfig, gatewayID)
	if err != nil {
		return nil, err
	}
	var cfg itypes.GatewayConfigKV
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// putGatewayConfig persists the gateway config either to local KV or, when
// targeting a remote tentacle, to that tentacle's git repo on mantle. msg is
// the commit message used in remote mode and ignored in local mode.
func (m *Module) putGatewayConfig(t Target, cfg *itypes.GatewayConfigKV, msg string) error {
	if t.IsRemote() {
		return m.saveGatewayConfigForTarget(t, cfg, msg)
	}
	cfg.UpdatedAt = time.Now().UnixMilli()
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = m.bus.KVPut(topics.BucketGatewayConfig, cfg.GatewayID, data)
	return err
}

// ensureMaps initializes nil maps on a GatewayConfigKV so callers can safely write.
func ensureMaps(cfg *itypes.GatewayConfigKV) {
	if cfg.Devices == nil {
		cfg.Devices = make(map[string]itypes.GatewayDeviceConfig)
	}
	if cfg.Variables == nil {
		cfg.Variables = make(map[string]itypes.GatewayVariableConfig)
	}
	if cfg.UdtTemplates == nil {
		cfg.UdtTemplates = make(map[string]itypes.GatewayUdtTemplateConfig)
	}
	if cfg.UdtVariables == nil {
		cfg.UdtVariables = make(map[string]itypes.GatewayUdtVariableConfig)
	}
}

// removeOrphanedTemplates deletes any UdtTemplate whose name is not
// referenced by at least one remaining UdtVariable.
func removeOrphanedTemplates(cfg *itypes.GatewayConfigKV) {
	used := make(map[string]struct{})
	for _, uv := range cfg.UdtVariables {
		used[uv.TemplateName] = struct{}{}
	}
	for name := range cfg.UdtTemplates {
		if _, ok := used[name]; !ok {
			delete(cfg.UdtTemplates, name)
		}
	}
}

// ─── 1. List Gateways ──────────────────────────────────────────────────────

func (m *Module) handleListGateways(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketGatewayConfig)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list gateway keys: %v", err))
		return
	}
	gateways := make([]*itypes.GatewayConfigKV, 0, len(keys))
	for _, key := range keys {
		cfg, err := m.getGatewayConfig(Target{}, key)
		if err != nil {
			m.log.Warn("skipping gateway", "key", key, "err", err)
			continue
		}
		gateways = append(gateways, cfg)
	}
	writeJSON(w, http.StatusOK, gateways)
}

// ─── 2. Get Gateway ────────────────────────────────────────────────────────

// gatewayDeviceResponse is a device config with its ID included.
type gatewayDeviceResponse struct {
	DeviceID string `json:"deviceId"`
	itypes.GatewayDeviceConfig
}

// gatewayVariableResponse is a variable config with its ID included.
type gatewayVariableResponse struct {
	ID string `json:"id"`
	itypes.GatewayVariableConfig
}

// gatewayUdtTemplateResponse wraps a UDT template config for the frontend.
type gatewayUdtTemplateResponse struct {
	itypes.GatewayUdtTemplateConfig
}

// gatewayUdtVariableResponse wraps a UDT variable config for the frontend.
type gatewayUdtVariableResponse struct {
	itypes.GatewayUdtVariableConfig
}

// gatewayResponse transforms the map-based KV storage into arrays for the frontend.
type gatewayResponse struct {
	GatewayID          string                        `json:"gatewayId"`
	Devices            []gatewayDeviceResponse       `json:"devices"`
	Variables          []gatewayVariableResponse     `json:"variables"`
	UdtTemplates       []gatewayUdtTemplateResponse  `json:"udtTemplates"`
	UdtVariables       []gatewayUdtVariableResponse  `json:"udtVariables"`
	AvailableProtocols []string                      `json:"availableProtocols"`
	UpdatedAt          int64                         `json:"updatedAt"`
}

func (m *Module) handleGetGateway(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)
	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}

	// Auto-sync module sources: discover interfaces and module status
	// variables so they appear automatically in the Variables page.
	// Skip auto-sync when targeting a remote tentacle — the modules whose
	// status we'd be probing aren't running on this mantle process, so the
	// NATS requests would only timeout. Phase 3's Sparkplug RPC can replace
	// this with a remote browse if needed.
	if !target.IsRemote() {
		type syncResult struct {
			changed bool
		}
		var syncWg sync.WaitGroup
		syncResults := make([]syncResult, 3)

		syncWg.Add(3)
		go func() {
			defer syncWg.Done()
			syncResults[0].changed = m.syncNetworkInterfaces(cfg)
		}()
		go func() {
			defer syncWg.Done()
			syncResults[1].changed = m.syncModuleStatus(cfg, gatewayID, "gateway")
		}()
		go func() {
			defer syncWg.Done()
			syncResults[2].changed = m.syncModuleStatus(cfg, gatewayID, "mqtt")
		}()
		syncWg.Wait()

		configChanged := syncResults[0].changed || syncResults[1].changed || syncResults[2].changed
		if configChanged {
			if err := m.putGatewayConfig(target, cfg, ""); err != nil {
				m.log.Warn("api: failed to persist auto-sync", "error", err)
			}
		}
	}

	// Convert device map to array
	devices := make([]gatewayDeviceResponse, 0, len(cfg.Devices))
	for id, dev := range cfg.Devices {
		devices = append(devices, gatewayDeviceResponse{DeviceID: id, GatewayDeviceConfig: dev})
	}

	// Convert variable map to array
	variables := make([]gatewayVariableResponse, 0, len(cfg.Variables))
	for id, v := range cfg.Variables {
		variables = append(variables, gatewayVariableResponse{ID: id, GatewayVariableConfig: v})
	}

	// Convert UDT template map to array
	udtTemplates := make([]gatewayUdtTemplateResponse, 0, len(cfg.UdtTemplates))
	for _, t := range cfg.UdtTemplates {
		udtTemplates = append(udtTemplates, gatewayUdtTemplateResponse{GatewayUdtTemplateConfig: t})
	}

	// Convert UDT variable map to array
	udtVariables := make([]gatewayUdtVariableResponse, 0, len(cfg.UdtVariables))
	for _, uv := range cfg.UdtVariables {
		udtVariables = append(udtVariables, gatewayUdtVariableResponse{GatewayUdtVariableConfig: uv})
	}

	// Determine which protocol modules are running by checking heartbeats.
	// For remote targets we don't yet know which protocols the edge has;
	// list the common ones so the configurator UI lets the operator pick.
	// Phase 3 will replace this with the target's NBIRTH module advert.
	protocolTypes := []string{"ethernetip", "opcua", "snmp", "modbus", "network", "plc"}
	var available []string
	if target.IsRemote() {
		available = []string{"modbus", "ethernetip", "snmp", "opcua"}
	} else {
		keys, _ := m.bus.KVKeys(topics.BucketHeartbeats)
		for _, key := range keys {
			data, _, err := m.bus.KVGet(topics.BucketHeartbeats, key)
			if err != nil {
				continue
			}
			var hb ttypes.ServiceHeartbeat
			if err := json.Unmarshal(data, &hb); err != nil {
				continue
			}
			for _, pt := range protocolTypes {
				if hb.ServiceType == pt {
					available = append(available, pt)
					break
				}
			}
		}
	}

	resp := gatewayResponse{
		GatewayID:          cfg.GatewayID,
		Devices:            devices,
		Variables:          variables,
		UdtTemplates:       udtTemplates,
		UdtVariables:       udtVariables,
		AvailableProtocols: available,
		UpdatedAt:          cfg.UpdatedAt,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── 3. Set Gateway Device ─────────────────────────────────────────────────

func (m *Module) handleSetGatewayDevice(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)

	var body struct {
		DeviceID string `json:"deviceId"`
		itypes.GatewayDeviceConfig
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if body.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "deviceId is required")
		return
	}

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		// Config doesn't exist yet — create a new one.
		cfg = &itypes.GatewayConfigKV{
			GatewayID:    gatewayID,
			Devices:      make(map[string]itypes.GatewayDeviceConfig),
			Variables:    make(map[string]itypes.GatewayVariableConfig),
			UdtTemplates: make(map[string]itypes.GatewayUdtTemplateConfig),
			UdtVariables: make(map[string]itypes.GatewayUdtVariableConfig),
		}
	}
	ensureMaps(cfg)

	cfg.Devices[body.DeviceID] = body.GatewayDeviceConfig

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 4. Delete Gateway Device ──────────────────────────────────────────────

func (m *Module) handleDeleteGatewayDevice(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)
	deviceID := chi.URLParam(r, "deviceId")

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	delete(cfg.Devices, deviceID)

	// Remove all Variables belonging to this device.
	for id, v := range cfg.Variables {
		if v.DeviceID == deviceID {
			delete(cfg.Variables, id)
		}
	}

	// Remove all UdtVariables belonging to this device.
	for id, uv := range cfg.UdtVariables {
		if uv.DeviceID == deviceID {
			delete(cfg.UdtVariables, id)
		}
	}

	// Clean up orphaned templates.
	removeOrphanedTemplates(cfg)

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 5. Set Template Overrides ─────────────────────────────────────────────

func (m *Module) handleSetTemplateOverrides(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)
	deviceID := chi.URLParam(r, "deviceId")

	var body struct {
		Overrides map[string]string `json:"overrides"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	dev, ok := cfg.Devices[deviceID]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("device %q not found", deviceID))
		return
	}
	dev.TemplateNameOverrides = body.Overrides
	cfg.Devices[deviceID] = dev

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 6. Set Gateway Variable ───────────────────────────────────────────────

func (m *Module) handleSetGatewayVariable(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)
	variableID := chi.URLParam(r, "variableId")

	var v itypes.GatewayVariableConfig
	if err := readJSON(r, &v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	v.ID = variableID

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	cfg.Variables[variableID] = v

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 7. Set Gateway Variables (bulk) ───────────────────────────────────────

func (m *Module) handleSetGatewayVariables(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)

	var vars []itypes.GatewayVariableConfig
	if err := readJSON(r, &vars); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	for _, v := range vars {
		cfg.Variables[v.ID] = v
	}

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 8. Delete Gateway Variable ────────────────────────────────────────────

func (m *Module) handleDeleteGatewayVariable(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)
	variableID := chi.URLParam(r, "variableId")

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	delete(cfg.Variables, variableID)

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 9. Delete Gateway Variables (bulk) ────────────────────────────────────

func (m *Module) handleDeleteGatewayVariables(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)

	var body struct {
		VariableIDs []string `json:"variableIds"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	for _, id := range body.VariableIDs {
		delete(cfg.Variables, id)
	}

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 10. Delete Gateway UDT Variable ──────────────────────────────────────

func (m *Module) handleDeleteGatewayUdtVariable(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)
	udtVariableID := chi.URLParam(r, "udtVariableId")

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	delete(cfg.UdtVariables, udtVariableID)
	removeOrphanedTemplates(cfg)

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 11. Delete Gateway UDT Variables (bulk) ──────────────────────────────

func (m *Module) handleDeleteGatewayUdtVariables(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)

	var body struct {
		UdtVariableIDs []string `json:"udtVariableIds"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	for _, id := range body.UdtVariableIDs {
		delete(cfg.UdtVariables, id)
	}
	removeOrphanedTemplates(cfg)

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 12. Sync Gateway Device Variables ────────────────────────────────────

func (m *Module) handleSyncGatewayDeviceVariables(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)
	deviceID := chi.URLParam(r, "deviceId")

	var body struct {
		AtomicVariables []itypes.GatewayVariableConfig    `json:"atomicVariables"`
		UdtTemplates    []itypes.GatewayUdtTemplateConfig `json:"udtTemplates"`
		UdtVariables    []itypes.GatewayUdtVariableConfig `json:"udtVariables"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(target, gatewayID)
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

	// Remove all existing variables for this device.
	for id, v := range cfg.Variables {
		if v.DeviceID == deviceID {
			delete(cfg.Variables, id)
		}
	}

	// Remove all existing UDT variables for this device.
	for id, uv := range cfg.UdtVariables {
		if uv.DeviceID == deviceID {
			delete(cfg.UdtVariables, id)
		}
	}

	// Add the new atomic variables.
	for _, v := range body.AtomicVariables {
		cfg.Variables[v.ID] = v
	}

	// Add the new UDT templates.
	for _, t := range body.UdtTemplates {
		cfg.UdtTemplates[t.Name] = t
	}

	// Add the new UDT variables.
	for _, uv := range body.UdtVariables {
		cfg.UdtVariables[uv.ID] = uv
	}

	// Clean up orphaned templates (from other devices that may have been removed).
	removeOrphanedTemplates(cfg)

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 13. Import Gateway Browse ────────────────────────────────────────────

func (m *Module) handleImportGatewayBrowse(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)

	var body struct {
		AtomicVariables []itypes.GatewayVariableConfig    `json:"atomicVariables"`
		UdtTemplates    []itypes.GatewayUdtTemplateConfig `json:"udtTemplates"`
		UdtVariables    []itypes.GatewayUdtVariableConfig `json:"udtVariables"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(target, gatewayID)
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

	// Add/update — do NOT remove existing entries.
	for _, v := range body.AtomicVariables {
		cfg.Variables[v.ID] = v
	}
	for _, t := range body.UdtTemplates {
		cfg.UdtTemplates[t.Name] = t
	}
	for _, uv := range body.UdtVariables {
		cfg.UdtVariables[uv.ID] = uv
	}

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 14. Update Gateway UDT Config ───────────────────────────────────────

func (m *Module) handleUpdateGatewayUdtConfig(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	target := parseTarget(r)
	templateName := chi.URLParam(r, "templateName")

	var body struct {
		// MemberUpdates maps member name → new default deadband.
		MemberUpdates map[string]*ttypes.DeadBandConfig `json:"memberUpdates"`
		// InstanceUpdates maps udtVariableId → member-level deadband overrides (sparse).
		InstanceUpdates map[string]map[string]ttypes.DeadBandOverride `json:"instanceUpdates"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(target, gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	// Update template member defaults.
	if tmpl, ok := cfg.UdtTemplates[templateName]; ok {
		for memberName, db := range body.MemberUpdates {
			for i := range tmpl.Members {
				if tmpl.Members[i].Name == memberName {
					tmpl.Members[i].DefaultDeadband = db
					break
				}
			}
		}
		cfg.UdtTemplates[templateName] = tmpl
	}

	// Update UDT variable instance overrides — replace entire map so cleared
	// overrides (sent as empty {}) actually remove old entries.
	for udtVarID, memberDeadbands := range body.InstanceUpdates {
		if uv, ok := cfg.UdtVariables[udtVarID]; ok {
			uv.MemberDeadbands = memberDeadbands
			cfg.UdtVariables[udtVarID] = uv
		}
	}

	if err := m.putGatewayConfig(target, cfg, ""); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 15. Get Gateway Browse Cache ─────────────────────────────────────────

func (m *Module) handleGetGatewayBrowseCache(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	deviceID := chi.URLParam(r, "deviceId")

	cacheKey := gatewayID + ":" + deviceID

	m.browseMu.RLock()
	result, ok := m.browseCache[cacheKey]
	m.browseMu.RUnlock()

	// Fall back to KV if not in memory (e.g. after restart).
	if !ok {
		if data, _, err := m.bus.KVGet(topics.BucketBrowseCache, cacheKey); err == nil && len(data) > 0 {
			result = data
			ok = true
			// Repopulate in-memory cache.
			m.browseMu.Lock()
			m.browseCache[cacheKey] = data
			m.browseMu.Unlock()
		}
	}

	if !ok {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(result)
}

// ─── 16. Get Gateway Browse States ────────────────────────────────────────

func (m *Module) handleGetGatewayBrowseStates(w http.ResponseWriter, r *http.Request) {
	m.browseMu.RLock()
	states := make([]*BrowseState, 0, len(m.browseStates))
	for _, s := range m.browseStates {
		states = append(states, s)
	}
	m.browseMu.RUnlock()

	writeJSON(w, http.StatusOK, states)
}

// ─── 17. Get Gateway Browse State ─────────────────────────────────────────

func (m *Module) handleGetGatewayBrowseState(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	deviceID := chi.URLParam(r, "deviceId")

	m.browseMu.RLock()
	var found *BrowseState
	for _, s := range m.browseStates {
		if s.GatewayID == gatewayID && s.DeviceID == deviceID {
			found = s
			break
		}
	}
	m.browseMu.RUnlock()

	if found == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no browse state for gateway %q device %q", gatewayID, deviceID))
		return
	}
	writeJSON(w, http.StatusOK, found)
}
