package scanner

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// Registry is an in-memory view of the shared `devices` KV bucket. It
// maintains a local deviceId → DeviceConfig map kept in sync with the bucket
// via a watch, and notifies a callback whenever the set of devices changes.
//
// Gateway and PLC each create their own Registry. Neither owns the bucket —
// edits come from the HTTP API, gitops, or manifest imports.
type Registry struct {
	b   bus.Bus
	log *slog.Logger

	mu       sync.RWMutex
	devices  map[string]itypes.DeviceConfig
	onChange func()

	sub bus.Subscription
}

// NewRegistry constructs an empty Registry. Call Start to begin watching.
func NewRegistry(b bus.Bus, log *slog.Logger) *Registry {
	return &Registry{
		b:       b,
		log:     log,
		devices: make(map[string]itypes.DeviceConfig),
	}
}

// Start subscribes to the devices KV bucket and populates the local map.
// onChange is invoked (on the watcher goroutine) whenever a device is
// added, updated, or deleted — callers typically use this to rebuild
// scanner subscribe requests.
func (r *Registry) Start(onChange func()) error {
	r.mu.Lock()
	r.onChange = onChange
	r.mu.Unlock()

	sub, err := r.b.KVWatchAll(topics.BucketDevices, func(key string, value []byte, op bus.KVOperation) {
		r.handle(key, value, op)
	})
	if err != nil {
		return err
	}
	r.sub = sub
	return nil
}

// Stop cancels the watch.
func (r *Registry) Stop() {
	if r.sub != nil {
		_ = r.sub.Unsubscribe()
		r.sub = nil
	}
}

func (r *Registry) handle(deviceID string, value []byte, op bus.KVOperation) {
	r.mu.Lock()
	if op == bus.KVOpDelete {
		delete(r.devices, deviceID)
	} else {
		var cfg itypes.DeviceConfig
		if err := json.Unmarshal(value, &cfg); err != nil {
			r.log.Error("scanner.Registry: failed to parse device", "deviceId", deviceID, "error", err)
			r.mu.Unlock()
			return
		}
		r.devices[deviceID] = cfg
	}
	cb := r.onChange
	r.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// Get returns a copy of the DeviceConfig for a deviceId.
func (r *Registry) Get(deviceID string) (itypes.DeviceConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.devices[deviceID]
	return cfg, ok
}

// All returns a copy of the full deviceId → DeviceConfig map.
func (r *Registry) All() map[string]itypes.DeviceConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]itypes.DeviceConfig, len(r.devices))
	for k, v := range r.devices {
		out[k] = v
	}
	return out
}

// Count returns the number of known devices.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.devices)
}

// Put writes a DeviceConfig to the devices KV bucket. Used by API handlers
// and migration logic.
func Put(b bus.Bus, deviceID string, cfg itypes.DeviceConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = b.KVPut(topics.BucketDevices, deviceID, data)
	return err
}

// Delete removes a DeviceConfig from the devices KV bucket.
func Delete(b bus.Bus, deviceID string) error {
	return b.KVDelete(topics.BucketDevices, deviceID)
}

// List returns all devices currently in the bucket (synchronous read).
// Prefer Registry.All() for steady-state access; use List for one-shot
// reads from API handlers where no registry is available.
func List(b bus.Bus) (map[string]itypes.DeviceConfig, error) {
	keys, err := b.KVKeys(topics.BucketDevices)
	if err != nil {
		return nil, err
	}
	out := make(map[string]itypes.DeviceConfig, len(keys))
	for _, key := range keys {
		data, _, err := b.KVGet(topics.BucketDevices, key)
		if err != nil {
			continue
		}
		var cfg itypes.DeviceConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		out[key] = cfg
	}
	return out, nil
}
