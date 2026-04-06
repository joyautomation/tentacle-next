//go:build orchestrator || all

package orchestrator

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	otypes "github.com/joyautomation/tentacle/internal/types"
)

// getAllDesiredServices reads all entries from the desired_services KV bucket.
func getAllDesiredServices(b bus.Bus) ([]otypes.DesiredServiceKV, error) {
	keys, err := b.KVKeys(topics.BucketDesiredServices)
	if err != nil {
		// No keys yet
		return nil, nil
	}

	var results []otypes.DesiredServiceKV
	for _, key := range keys {
		data, _, err := b.KVGet(topics.BucketDesiredServices, key)
		if err != nil {
			continue
		}
		var d otypes.DesiredServiceKV
		if json.Unmarshal(data, &d) == nil {
			results = append(results, d)
		}
	}
	return results, nil
}

// putDesiredService writes a desired service entry to the KV bucket.
func putDesiredService(b bus.Bus, desired otypes.DesiredServiceKV) error {
	data, err := json.Marshal(desired)
	if err != nil {
		return err
	}
	_, err = b.KVPut(topics.BucketDesiredServices, desired.ModuleID, data)
	return err
}

// reportStatus builds and publishes a ServiceStatusKV entry.
func reportStatus(b bus.Bus, entry *otypes.ModuleRegistryEntry, opts statusOpts) {
	status := otypes.ServiceStatusKV{
		ModuleID:          entry.ModuleID,
		InstalledVersions: opts.InstalledVersions,
		ActiveVersion:     opts.ActiveVersion,
		SystemdState:      normalizeSystemdState(opts.SystemdState),
		ReconcileState:    opts.ReconcileState,
		LastError:         opts.LastError,
		Runtime:           entry.Runtime,
		Category:          entry.Category,
		Repo:              entry.Repo,
		UpdatedAt:         time.Now().UnixMilli(),
	}
	data, err := json.Marshal(status)
	if err != nil {
		slog.Warn("status: failed to marshal", "moduleId", entry.ModuleID, "error", err)
		return
	}
	if _, err := b.KVPut(topics.BucketServiceStatus, status.ModuleID, data); err != nil {
		slog.Warn("status: failed to report", "moduleId", entry.ModuleID, "error", err)
	}
}

// getModuleConfig reads all config entries for a module from tentacle_config KV.
// Keys follow the pattern: {moduleId}.{ENVVAR}
func getModuleConfig(b bus.Bus, moduleID string) (map[string]string, error) {
	config := make(map[string]string)

	keys, err := b.KVKeys(topics.BucketTentacleConfig)
	if err != nil {
		return config, nil // no keys yet
	}

	prefix := moduleID + "."
	for _, key := range keys {
		if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
			continue
		}
		data, _, err := b.KVGet(topics.BucketTentacleConfig, key)
		if err != nil {
			continue
		}
		envVar := key[len(prefix):]
		config[envVar] = string(data)
	}
	return config, nil
}
