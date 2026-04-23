//go:build api || all

package api

import (
	"encoding/json"
	"time"

	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// syncModuleStatus auto-discovers status variables from a module via its
// status.browse handler, ensures an auto-managed device exists, populates
// the browse cache so variables appear in the UI for selection, and cleans
// up any config variables that the module no longer reports.
// Returns true if config was changed.
func (m *Module) syncModuleStatus(cfg *itypes.GatewayConfigKV, gatewayID, moduleType string) bool {
	if !m.isModuleRunning(moduleType) {
		return false
	}
	resp, err := m.bus.Request(topics.StatusBrowse(moduleType), []byte("{}"), 500*time.Millisecond)
	if err != nil {
		return false // module not running or doesn't support status browse
	}

	var browseResult struct {
		Variables []struct {
			Name     string `json:"name"`
			Datatype string `json:"datatype"`
		} `json:"variables"`
	}
	if err := json.Unmarshal(resp, &browseResult); err != nil || len(browseResult.Variables) == 0 {
		return false
	}

	ensureMaps(cfg)

	changed := false

	// Ensure auto-managed source exists in the shared sources bucket.
	// AutoManaged sources are self-publishing — the scanner never writes
	// a subscribe config for them.
	src, srcOK := m.getSource(moduleType)
	if !srcOK {
		_ = m.putSource(moduleType, itypes.SourceConfig{
			Protocol:    moduleType,
			AutoManaged: true,
		})
	} else if !src.AutoManaged {
		src.AutoManaged = true
		_ = m.putSource(moduleType, src)
	}

	// Populate browse cache so variables appear in the Variables page for
	// user selection, without being auto-published to MQTT.
	type browseCacheItem struct {
		Tag          string      `json:"tag"`
		Name         string      `json:"name"`
		Datatype     string      `json:"datatype"`
		Value        interface{} `json:"value"`
		ProtocolType string      `json:"protocolType"`
	}
	items := make([]browseCacheItem, 0, len(browseResult.Variables))
	for _, v := range browseResult.Variables {
		items = append(items, browseCacheItem{
			Tag:      v.Name,
			Name:     v.Name,
			Datatype: v.Datatype,
		})
	}
	cache := struct {
		DeviceID   string            `json:"deviceId"`
		Protocol   string            `json:"protocol"`
		Items      []browseCacheItem `json:"items"`
		Udts       []interface{}     `json:"udts"`
		StructTags map[string]string `json:"structTags"`
		CachedAt   string            `json:"cachedAt"`
	}{
		DeviceID:   moduleType,
		Protocol:   moduleType,
		Items:      items,
		Udts:       []interface{}{},
		StructTags: map[string]string{},
		CachedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if cacheJSON, err := json.Marshal(cache); err == nil {
		cacheKey := gatewayID + ":" + moduleType
		m.browseMu.Lock()
		m.browseCache[cacheKey] = cacheJSON
		m.browseMu.Unlock()
		if _, err := m.bus.KVPut(topics.BucketBrowseCache, cacheKey, cacheJSON); err != nil {
			m.log.Warn("failed to persist module browse cache", "key", cacheKey, "error", err)
		}
	}

	// Clean up config variables for tags the module no longer reports.
	discovered := make(map[string]bool, len(browseResult.Variables))
	for _, v := range browseResult.Variables {
		discovered[v.Name] = true
	}
	for id, v := range cfg.Variables {
		if v.DeviceID == moduleType && !discovered[v.Tag] {
			delete(cfg.Variables, id)
			changed = true
		}
	}

	return changed
}
