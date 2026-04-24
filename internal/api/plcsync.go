//go:build api || all

package api

import (
	"encoding/json"
	"time"

	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	ttypes "github.com/joyautomation/tentacle/types"
)

// syncPlcVariables auto-discovers variables from each running PLC module
// and populates the browse cache so they appear in the Gateway variables
// page without requiring a manual browse. Each PLC instance becomes an
// auto-managed device with protocol="plc". Returns true if gateway config
// was changed (e.g. stale variable cleanup).
func (m *Module) syncPlcVariables(cfg *itypes.GatewayConfigKV, gatewayID string) bool {
	plcIDs := m.runningPlcIDs()
	if len(plcIDs) == 0 {
		return false
	}

	ensureMaps(cfg)
	changed := false

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

		// Ensure auto-managed device exists.
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

		// Build browse cache items. Skip struct parents — struct members
		// come in as separate VariableInfo entries with dot-path IDs.
		type browseCacheItem struct {
			Tag          string      `json:"tag"`
			Name         string      `json:"name"`
			Datatype     string      `json:"datatype"`
			Value        interface{} `json:"value"`
			ProtocolType string      `json:"protocolType"`
		}
		items := make([]browseCacheItem, 0, len(vars))
		discovered := make(map[string]bool, len(vars))
		for _, v := range vars {
			items = append(items, browseCacheItem{
				Tag:      v.VariableID,
				Name:     v.VariableID,
				Datatype: v.Datatype,
				Value:    v.Value,
			})
			discovered[v.VariableID] = true
		}
		discoveredPerDevice[plcID] = discovered

		cache := struct {
			DeviceID   string            `json:"deviceId"`
			Protocol   string            `json:"protocol"`
			Items      []browseCacheItem `json:"items"`
			Udts       []interface{}     `json:"udts"`
			StructTags map[string]string `json:"structTags"`
			CachedAt   string            `json:"cachedAt"`
		}{
			DeviceID:   plcID,
			Protocol:   "plc",
			Items:      items,
			Udts:       []interface{}{},
			StructTags: map[string]string{},
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

	// Clean up config variables for tags the PLC no longer reports.
	for id, v := range cfg.Variables {
		discovered, ok := discoveredPerDevice[v.DeviceID]
		if !ok {
			continue
		}
		if !discovered[v.Tag] {
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
