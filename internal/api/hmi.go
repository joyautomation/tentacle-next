//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// ─── HMI Config Helpers ────────────────────────────────────────────────────

func (m *Module) getHmiApp(appID string) (*itypes.HmiAppConfig, error) {
	data, _, err := m.bus.KVGet(topics.BucketHmiConfig, appID)
	if err != nil {
		return nil, err
	}
	var cfg itypes.HmiAppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	ensureHmiMaps(&cfg)
	return &cfg, nil
}

func (m *Module) putHmiApp(cfg *itypes.HmiAppConfig) error {
	ensureHmiMaps(cfg)
	cfg.UpdatedAt = time.Now().UnixMilli()
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = m.bus.KVPut(topics.BucketHmiConfig, cfg.AppID, data)
	return err
}

func ensureHmiMaps(cfg *itypes.HmiAppConfig) {
	if cfg.Screens == nil {
		cfg.Screens = make(map[string]itypes.HmiScreenConfig)
	}
	if cfg.Components == nil {
		cfg.Components = make(map[string]itypes.HmiComponentConfig)
	}
	if cfg.Classes == nil {
		cfg.Classes = make(map[string]string)
	}
}

// slugify produces a URL-safe id from a free-form name.
func hmiSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || r == ' ':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		out = "item"
	}
	return out
}

// ─── Apps ──────────────────────────────────────────────────────────────────

// GET /api/v1/hmi/apps
func (m *Module) handleListHmiApps(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketHmiConfig)
	if err != nil {
		// Bucket may not exist yet — return empty list.
		writeJSON(w, http.StatusOK, []*itypes.HmiAppConfig{})
		return
	}
	apps := make([]*itypes.HmiAppConfig, 0, len(keys))
	for _, key := range keys {
		cfg, err := m.getHmiApp(key)
		if err != nil {
			m.log.Warn("hmi: skipping app", "appId", key, "err", err)
			continue
		}
		apps = append(apps, cfg)
	}
	writeJSON(w, http.StatusOK, apps)
}

// GET /api/v1/hmi/apps/{appId}
func (m *Module) handleGetHmiApp(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	cfg, err := m.getHmiApp(appID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("app not found: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

type createHmiAppRequest struct {
	AppID       string `json:"appId,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// POST /api/v1/hmi/apps
func (m *Module) handleCreateHmiApp(w http.ResponseWriter, r *http.Request) {
	var req createHmiAppRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	appID := req.AppID
	if appID == "" {
		appID = hmiSlug(req.Name)
	}
	if _, _, err := m.bus.KVGet(topics.BucketHmiConfig, appID); err == nil {
		writeError(w, http.StatusConflict, "app already exists")
		return
	}
	cfg := &itypes.HmiAppConfig{
		AppID:       appID,
		Name:        req.Name,
		Description: req.Description,
	}
	if err := m.putHmiApp(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, cfg)
}

// PUT /api/v1/hmi/apps/{appId}
// Replaces the entire app config. Useful for bulk import / save.
func (m *Module) handlePutHmiApp(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	var cfg itypes.HmiAppConfig
	if err := readJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	cfg.AppID = appID
	if err := m.putHmiApp(&cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, &cfg)
}

// DELETE /api/v1/hmi/apps/{appId}
func (m *Module) handleDeleteHmiApp(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	if err := m.bus.KVDelete(topics.BucketHmiConfig, appID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete failed: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Screens ───────────────────────────────────────────────────────────────

type upsertHmiScreenRequest struct {
	ScreenID string                `json:"screenId,omitempty"`
	Name     string                `json:"name"`
	Width    int                   `json:"width,omitempty"`
	Height   int                   `json:"height,omitempty"`
	Widgets  []itypes.HmiWidget    `json:"widgets,omitempty"`
}

// PUT /api/v1/hmi/apps/{appId}/screens/{screenId}
func (m *Module) handlePutHmiScreen(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	screenID := chi.URLParam(r, "screenId")
	var req upsertHmiScreenRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	cfg, err := m.getHmiApp(appID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if req.Widgets == nil {
		req.Widgets = []itypes.HmiWidget{}
	}
	cfg.Screens[screenID] = itypes.HmiScreenConfig{
		ScreenID: screenID,
		Name:     req.Name,
		Width:    req.Width,
		Height:   req.Height,
		Widgets:  req.Widgets,
	}
	if err := m.putHmiApp(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg.Screens[screenID])
}

// POST /api/v1/hmi/apps/{appId}/screens
type createHmiScreenRequest struct {
	ScreenID string `json:"screenId,omitempty"`
	Name     string `json:"name"`
}

func (m *Module) handleCreateHmiScreen(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	var req createHmiScreenRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	cfg, err := m.getHmiApp(appID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	screenID := req.ScreenID
	if screenID == "" {
		screenID = hmiSlug(req.Name)
	}
	if _, exists := cfg.Screens[screenID]; exists {
		writeError(w, http.StatusConflict, "screen already exists")
		return
	}
	scr := itypes.HmiScreenConfig{
		ScreenID: screenID,
		Name:     req.Name,
		Widgets:  []itypes.HmiWidget{},
	}
	cfg.Screens[screenID] = scr
	if err := m.putHmiApp(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, scr)
}

// DELETE /api/v1/hmi/apps/{appId}/screens/{screenId}
func (m *Module) handleDeleteHmiScreen(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	screenID := chi.URLParam(r, "screenId")
	cfg, err := m.getHmiApp(appID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	delete(cfg.Screens, screenID)
	if err := m.putHmiApp(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Components ────────────────────────────────────────────────────────────

type createHmiComponentRequest struct {
	ComponentID string `json:"componentId,omitempty"`
	Name        string `json:"name"`
	UdtTemplate string `json:"udtTemplate,omitempty"`
}

// POST /api/v1/hmi/apps/{appId}/components
func (m *Module) handleCreateHmiComponent(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	var req createHmiComponentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	cfg, err := m.getHmiApp(appID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	componentID := req.ComponentID
	if componentID == "" {
		componentID = hmiSlug(req.Name)
	}
	if _, exists := cfg.Components[componentID]; exists {
		writeError(w, http.StatusConflict, "component already exists")
		return
	}
	comp := itypes.HmiComponentConfig{
		ComponentID: componentID,
		Name:        req.Name,
		UdtTemplate: req.UdtTemplate,
		Widgets:     []itypes.HmiWidget{},
	}
	cfg.Components[componentID] = comp
	if err := m.putHmiApp(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, comp)
}

// PUT /api/v1/hmi/apps/{appId}/components/{componentId}
func (m *Module) handlePutHmiComponent(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	componentID := chi.URLParam(r, "componentId")
	var comp itypes.HmiComponentConfig
	if err := readJSON(r, &comp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	comp.ComponentID = componentID
	if comp.Widgets == nil {
		comp.Widgets = []itypes.HmiWidget{}
	}
	cfg, err := m.getHmiApp(appID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	cfg.Components[componentID] = comp
	if err := m.putHmiApp(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, comp)
}

// DELETE /api/v1/hmi/apps/{appId}/components/{componentId}
func (m *Module) handleDeleteHmiComponent(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	componentID := chi.URLParam(r, "componentId")
	cfg, err := m.getHmiApp(appID)
	if err != nil {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	delete(cfg.Components, componentID)
	if err := m.putHmiApp(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── UDT Discovery ─────────────────────────────────────────────────────────

// hmiUdtTemplate is a flattened view of a UDT template across all gateways,
// with the list of instances that use it.
type hmiUdtTemplate struct {
	Name      string                                  `json:"name"`
	Version   string                                  `json:"version,omitempty"`
	Members   []itypes.GatewayUdtTemplateMemberConfig `json:"members"`
	Gateways  []string                                `json:"gateways"`
	Instances []hmiUdtInstance                        `json:"instances"`
}

type hmiUdtInstance struct {
	GatewayID string `json:"gatewayId"`
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	DeviceID  string `json:"deviceId"`
}

// GET /api/v1/hmi/udts
//
// Returns all UDT templates declared by any gateway, with the instances
// (UDT variables) that reference them. This is the discovery surface for
// the HMI builder when authoring UDT-bound components.
func (m *Module) handleListHmiUdts(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketGatewayConfig)
	if err != nil {
		writeJSON(w, http.StatusOK, []hmiUdtTemplate{})
		return
	}
	byName := make(map[string]*hmiUdtTemplate)
	for _, key := range keys {
		data, _, err := m.bus.KVGet(topics.BucketGatewayConfig, key)
		if err != nil {
			continue
		}
		var cfg itypes.GatewayConfigKV
		if json.Unmarshal(data, &cfg) != nil {
			continue
		}
		for name, tmpl := range cfg.UdtTemplates {
			t, ok := byName[name]
			if !ok {
				t = &hmiUdtTemplate{
					Name:    name,
					Version: tmpl.Version,
					Members: tmpl.Members,
				}
				byName[name] = t
			}
			t.Gateways = appendUnique(t.Gateways, cfg.GatewayID)
		}
		for _, uv := range cfg.UdtVariables {
			t, ok := byName[uv.TemplateName]
			if !ok {
				continue
			}
			t.Instances = append(t.Instances, hmiUdtInstance{
				GatewayID: cfg.GatewayID,
				ID:        uv.ID,
				Tag:       uv.Tag,
				DeviceID:  uv.DeviceID,
			})
		}
	}
	out := make([]hmiUdtTemplate, 0, len(byName))
	for _, t := range byName {
		out = append(out, *t)
	}
	writeJSON(w, http.StatusOK, out)
}

func appendUnique(xs []string, x string) []string {
	for _, v := range xs {
		if v == x {
			return xs
		}
	}
	return append(xs, x)
}
