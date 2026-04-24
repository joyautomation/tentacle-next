//go:build api || all

package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	ttypes "github.com/joyautomation/tentacle/types"
)

// syncPlcVariables auto-discovers variables from each running PLC module
// and populates the browse cache so they appear in the Gateway variables
// page without requiring a manual browse. Struct-typed variables are
// expanded into their member leaves and the template is exposed in the
// browse cache's udts/structTags so the UI shows them as UDT instances.
// Each PLC instance becomes an auto-managed device with protocol="plc".
// Returns true if gateway config was changed (stale variable cleanup).
func (m *Module) syncPlcVariables(cfg *itypes.GatewayConfigKV, gatewayID string) bool {
	plcIDs := m.runningPlcIDs()
	if len(plcIDs) == 0 {
		return false
	}

	ensureMaps(cfg)
	changed := false

	templates := m.loadPlcTemplateMap()
	discoveredPerDevice := make(map[string]map[string]bool, len(plcIDs))

	for _, plcID := range plcIDs {
		resp, err := m.bus.Request(topics.Variables(plcID), []byte("{}"), 500*time.Millisecond)
		if err != nil {
			continue
		}
		var vars []ttypes.VariableInfo
		if err := json.Unmarshal(resp, &vars); err != nil {
			continue
		}

		dev, devOK := m.getDevice(plcID)
		if !devOK {
			_ = m.putDevice(plcID, itypes.DeviceConfig{
				Protocol:    "plc",
				AutoManaged: true,
			})
		} else if !dev.AutoManaged || dev.Protocol != "plc" {
			dev.AutoManaged = true
			dev.Protocol = "plc"
			_ = m.putDevice(plcID, dev)
		}

		type browseCacheItem struct {
			Tag          string      `json:"tag"`
			Name         string      `json:"name"`
			Datatype     string      `json:"datatype"`
			Value        interface{} `json:"value"`
			ProtocolType string      `json:"protocolType"`
		}
		type udtMember struct {
			Name     string `json:"name"`
			Datatype string `json:"datatype"`
			CipType  string `json:"cipType"`
			UdtType  string `json:"udtType"`
			IsArray  bool   `json:"isArray"`
		}
		type udtExport struct {
			Name    string      `json:"name"`
			Members []udtMember `json:"members"`
		}

		items := make([]browseCacheItem, 0, len(vars))
		udts := []udtExport{}
		udtsEmitted := map[string]bool{}
		structTags := map[string]string{}
		discovered := make(map[string]bool, len(vars))

		emitTemplate := func(name string) {
			if udtsEmitted[name] {
				return
			}
			tmpl, ok := templates[name]
			if !ok {
				return
			}
			members := make([]udtMember, 0, len(tmpl.Fields))
			for _, f := range tmpl.Fields {
				base, isArray := stripArraySuffix(f.Type)
				members = append(members, udtMember{
					Name:     f.Name,
					Datatype: plcTypeToScannerDatatype(base),
					UdtType:  plcTemplateRefOrEmpty(base, templates),
					IsArray:  isArray,
				})
			}
			udts = append(udts, udtExport{Name: name, Members: members})
			udtsEmitted[name] = true
		}

		for _, v := range vars {
			discovered[v.VariableID] = true
			tmpl, isStruct := templates[v.Datatype]
			if !isStruct {
				items = append(items, browseCacheItem{
					Tag:      v.VariableID,
					Name:     v.VariableID,
					Datatype: v.Datatype,
					Value:    v.Value,
				})
				continue
			}
			// Struct instance: register in structTags + udts, emit the
			// root entry (filtered out of atomic count by structTags) and
			// expand each primitive member as its own leaf item.
			structTags[v.VariableID] = tmpl.Name
			emitTemplate(tmpl.Name)
			items = append(items, browseCacheItem{
				Tag:      v.VariableID,
				Name:     v.VariableID,
				Datatype: tmpl.Name,
			})
			fieldsValue, _ := v.Value.(map[string]interface{})
			for _, f := range tmpl.Fields {
				base, _ := stripArraySuffix(f.Type)
				if _, nested := templates[base]; nested {
					continue
				}
				var fv interface{}
				if fieldsValue != nil {
					fv = fieldsValue[f.Name]
				}
				items = append(items, browseCacheItem{
					Tag:      v.VariableID + "." + f.Name,
					Name:     v.VariableID + "." + f.Name,
					Datatype: plcTypeToScannerDatatype(base),
					Value:    fv,
				})
			}
		}
		discoveredPerDevice[plcID] = discovered

		cache := struct {
			DeviceID   string            `json:"deviceId"`
			Protocol   string            `json:"protocol"`
			Items      []browseCacheItem `json:"items"`
			Udts       []udtExport       `json:"udts"`
			StructTags map[string]string `json:"structTags"`
			CachedAt   string            `json:"cachedAt"`
		}{
			DeviceID:   plcID,
			Protocol:   "plc",
			Items:      items,
			Udts:       udts,
			StructTags: structTags,
			CachedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		if cacheJSON, err := json.Marshal(cache); err == nil {
			cacheKey := gatewayID + ":" + plcID
			m.browseMu.Lock()
			m.browseCache[cacheKey] = cacheJSON
			m.browseMu.Unlock()
			if _, err := m.bus.KVPut(topics.BucketBrowseCache, cacheKey, cacheJSON); err != nil {
				m.log.Warn("failed to persist plc browse cache", "key", cacheKey, "error", err)
			}
		}
	}

	for id, v := range cfg.Variables {
		discovered, ok := discoveredPerDevice[v.DeviceID]
		if !ok {
			continue
		}
		// Dot-path tags (struct members) are discovered as part of their
		// root — treat presence of the root as coverage for the member.
		rootTag := v.Tag
		if i := strings.Index(rootTag, "."); i > 0 {
			rootTag = rootTag[:i]
		}
		if !discovered[rootTag] {
			delete(cfg.Variables, id)
			changed = true
		}
	}

	return changed
}

// runningPlcIDs returns the moduleIDs of all running PLC instances.
func (m *Module) runningPlcIDs() []string {
	keys, err := m.bus.KVKeys(topics.BucketHeartbeats)
	if err != nil {
		return nil
	}
	var ids []string
	for _, key := range keys {
		data, _, err := m.bus.KVGet(topics.BucketHeartbeats, key)
		if err != nil {
			continue
		}
		var hb ttypes.ServiceHeartbeat
		if json.Unmarshal(data, &hb) != nil {
			continue
		}
		if hb.ServiceType == "plc" && hb.ModuleID != "" {
			ids = append(ids, hb.ModuleID)
		}
	}
	return ids
}

// loadPlcTemplateMap reads every PLC template from the plc_templates
// bucket. Returns a map keyed by template name so callers can quickly
// test whether a datatype string refers to a struct type.
func (m *Module) loadPlcTemplateMap() map[string]*itypes.PlcTemplate {
	out := map[string]*itypes.PlcTemplate{}
	keys, err := m.bus.KVKeys(topics.BucketPlcTemplates)
	if err != nil {
		return out
	}
	for _, k := range keys {
		data, _, err := m.bus.KVGet(topics.BucketPlcTemplates, k)
		if err != nil {
			continue
		}
		var tmpl itypes.PlcTemplate
		if err := json.Unmarshal(data, &tmpl); err != nil {
			continue
		}
		out[tmpl.Name] = &tmpl
	}
	return out
}

// stripArraySuffix peels a trailing "[]" or "{}" off a PLC field type
// and reports whether it was an array/record.
func stripArraySuffix(t string) (string, bool) {
	if strings.HasSuffix(t, "[]") {
		return strings.TrimSuffix(t, "[]"), true
	}
	if strings.HasSuffix(t, "{}") {
		return strings.TrimSuffix(t, "{}"), true
	}
	return t, false
}

// plcTypeToScannerDatatype maps a PlcTemplateField primitive type to the
// frontend datatype vocabulary used in browse caches. Falls back to the
// raw type string so UDT references survive untouched.
func plcTypeToScannerDatatype(t string) string {
	switch t {
	case "bool":
		return "boolean"
	case "number":
		return "number"
	case "string":
		return "string"
	case "bytes":
		return "bytes"
	default:
		return t
	}
}

// plcTemplateRefOrEmpty returns t if t names a known template, else "".
// Used to populate udtType on nested member fields.
func plcTemplateRefOrEmpty(t string, templates map[string]*itypes.PlcTemplate) string {
	if _, ok := templates[t]; ok {
		return t
	}
	return ""
}
