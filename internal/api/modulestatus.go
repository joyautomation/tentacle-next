//go:build api || all

package api

import (
	"encoding/json"
	"time"

	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// syncModuleStatus auto-discovers status variables from a module via its
// status.browse handler and ensures they exist as atomic gateway variables
// under an auto-managed device. Returns true if config was changed.
func (m *Module) syncModuleStatus(cfg *itypes.GatewayConfigKV, moduleType string) bool {
	resp, err := m.bus.Request(topics.StatusBrowse(moduleType), []byte("{}"), 2*time.Second)
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

	// Ensure auto-managed device exists.
	if dev, ok := cfg.Devices[moduleType]; !ok {
		cfg.Devices[moduleType] = itypes.GatewayDeviceConfig{
			Protocol:    moduleType,
			AutoManaged: true,
		}
		changed = true
	} else if !dev.AutoManaged {
		dev.AutoManaged = true
		cfg.Devices[moduleType] = dev
		changed = true
	}

	// Build set of discovered variable names.
	discovered := make(map[string]bool, len(browseResult.Variables))
	for _, v := range browseResult.Variables {
		discovered[v.Name] = true
	}

	// Remove variables for this device that are no longer reported.
	for id, v := range cfg.Variables {
		if v.DeviceID == moduleType && !discovered[v.Tag] {
			delete(cfg.Variables, id)
			changed = true
		}
	}

	// Add or update variables.
	for _, v := range browseResult.Variables {
		varID := moduleType + "_" + v.Name
		if existing, ok := cfg.Variables[varID]; ok {
			if existing.Datatype != v.Datatype {
				existing.Datatype = v.Datatype
				cfg.Variables[varID] = existing
				changed = true
			}
			continue
		}
		cfg.Variables[varID] = itypes.GatewayVariableConfig{
			ID:       varID,
			DeviceID: moduleType,
			Tag:      v.Name,
			Datatype: v.Datatype,
		}
		changed = true
	}

	return changed
}
