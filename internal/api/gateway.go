//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	ttypes "github.com/joyautomation/tentacle/types"
)

// Ensure ttypes is used (DeadBandConfig is referenced transitively through itypes).
var _ *ttypes.DeadBandConfig

// ─── Gateway Config Helpers ─────────────────────────────────────────────────

func (m *Module) getGatewayConfig(gatewayID string) (*itypes.GatewayConfigKV, error) {
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

func (m *Module) putGatewayConfig(cfg *itypes.GatewayConfigKV) error {
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
		cfg, err := m.getGatewayConfig(key)
		if err != nil {
			m.log.Warn("skipping gateway", "key", key, "err", err)
			continue
		}
		gateways = append(gateways, cfg)
	}
	writeJSON(w, http.StatusOK, gateways)
}

// ─── 2. Get Gateway ────────────────────────────────────────────────────────

func (m *Module) handleGetGateway(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	cfg, err := m.getGatewayConfig(gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 3. Set Gateway Device ─────────────────────────────────────────────────

func (m *Module) handleSetGatewayDevice(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

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

	cfg, err := m.getGatewayConfig(gatewayID)
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

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 4. Delete Gateway Device ──────────────────────────────────────────────

func (m *Module) handleDeleteGatewayDevice(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	deviceID := chi.URLParam(r, "deviceId")

	cfg, err := m.getGatewayConfig(gatewayID)
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

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 5. Set Template Overrides ─────────────────────────────────────────────

func (m *Module) handleSetTemplateOverrides(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	deviceID := chi.URLParam(r, "deviceId")

	var body struct {
		Overrides map[string]string `json:"overrides"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(gatewayID)
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

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 6. Set Gateway Variable ───────────────────────────────────────────────

func (m *Module) handleSetGatewayVariable(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	variableID := chi.URLParam(r, "variableId")

	var v itypes.GatewayVariableConfig
	if err := readJSON(r, &v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	v.ID = variableID

	cfg, err := m.getGatewayConfig(gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	cfg.Variables[variableID] = v

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 7. Set Gateway Variables (bulk) ───────────────────────────────────────

func (m *Module) handleSetGatewayVariables(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

	var vars []itypes.GatewayVariableConfig
	if err := readJSON(r, &vars); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	for _, v := range vars {
		cfg.Variables[v.ID] = v
	}

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 8. Delete Gateway Variable ────────────────────────────────────────────

func (m *Module) handleDeleteGatewayVariable(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	variableID := chi.URLParam(r, "variableId")

	cfg, err := m.getGatewayConfig(gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	delete(cfg.Variables, variableID)

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 9. Delete Gateway Variables (bulk) ────────────────────────────────────

func (m *Module) handleDeleteGatewayVariables(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

	var body struct {
		VariableIDs []string `json:"variableIds"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	for _, id := range body.VariableIDs {
		delete(cfg.Variables, id)
	}

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 10. Delete Gateway UDT Variable ──────────────────────────────────────

func (m *Module) handleDeleteGatewayUdtVariable(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	udtVariableID := chi.URLParam(r, "udtVariableId")

	cfg, err := m.getGatewayConfig(gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	delete(cfg.UdtVariables, udtVariableID)
	removeOrphanedTemplates(cfg)

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 11. Delete Gateway UDT Variables (bulk) ──────────────────────────────

func (m *Module) handleDeleteGatewayUdtVariables(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

	var body struct {
		UdtVariableIDs []string `json:"udtVariableIds"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(gatewayID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway %q not found: %v", gatewayID, err))
		return
	}
	ensureMaps(cfg)

	for _, id := range body.UdtVariableIDs {
		delete(cfg.UdtVariables, id)
	}
	removeOrphanedTemplates(cfg)

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 12. Sync Gateway Device Variables ────────────────────────────────────

func (m *Module) handleSyncGatewayDeviceVariables(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
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

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 13. Import Gateway Browse ────────────────────────────────────────────

func (m *Module) handleImportGatewayBrowse(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

	var body struct {
		AtomicVariables []itypes.GatewayVariableConfig    `json:"atomicVariables"`
		UdtTemplates    []itypes.GatewayUdtTemplateConfig `json:"udtTemplates"`
		UdtVariables    []itypes.GatewayUdtVariableConfig `json:"udtVariables"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
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

	if err := m.putGatewayConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put gateway config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── 14. Update Gateway UDT Config ───────────────────────────────────────

func (m *Module) handleUpdateGatewayUdtConfig(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")
	templateName := chi.URLParam(r, "templateName")

	var body struct {
		// MemberUpdates maps member name → new default deadband.
		MemberUpdates map[string]*ttypes.DeadBandConfig `json:"memberUpdates"`
		// InstanceUpdates maps udtVariableId → member-level deadband overrides.
		InstanceUpdates map[string]map[string]ttypes.DeadBandConfig `json:"instanceUpdates"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	cfg, err := m.getGatewayConfig(gatewayID)
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

	// Update UDT variable instance overrides.
	for udtVarID, memberDeadbands := range body.InstanceUpdates {
		if uv, ok := cfg.UdtVariables[udtVarID]; ok {
			if uv.MemberDeadbands == nil {
				uv.MemberDeadbands = make(map[string]ttypes.DeadBandConfig)
			}
			for memberName, db := range memberDeadbands {
				uv.MemberDeadbands[memberName] = db
			}
			cfg.UdtVariables[udtVarID] = uv
		}
	}

	if err := m.putGatewayConfig(cfg); err != nil {
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

	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no browse cache for %s", cacheKey))
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
