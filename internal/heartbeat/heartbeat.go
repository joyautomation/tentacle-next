// Package heartbeat provides a shared heartbeat publisher for all tentacle modules.
// Every module publishes a heartbeat every 10s to the service_heartbeats KV bucket.
package heartbeat

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const (
	interval = 10 * time.Second
)

// Start begins publishing heartbeats and returns a stop function.
// metadataFn is called on each heartbeat to get module-specific metadata.
// It may be nil if no metadata is needed.
func Start(b bus.Bus, moduleID, serviceType string, metadataFn func() map[string]interface{}) (stop func()) {
	// Ensure the bucket exists
	if err := b.KVCreate(topics.BucketHeartbeats, topics.BucketConfigs()[topics.BucketHeartbeats]); err != nil {
		slog.Warn("heartbeat: failed to create bucket", "error", err)
	}

	startedAt := time.Now().UnixMilli()
	done := make(chan struct{})

	publish := func() {
		hb := types.ServiceHeartbeat{
			ServiceType: serviceType,
			ModuleID:    moduleID,
			LastSeen:    time.Now().UnixMilli(),
			StartedAt:   startedAt,
		}
		if metadataFn != nil {
			hb.Metadata = metadataFn()
		}
		data, err := json.Marshal(hb)
		if err != nil {
			slog.Warn("heartbeat: marshal failed", "error", err)
			return
		}
		if _, err := b.KVPut(topics.BucketHeartbeats, moduleID, data); err != nil {
			slog.Warn("heartbeat: publish failed", "error", err)
		}
	}

	// Publish immediately
	publish()

	// Publish every 10s
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-done:
				ticker.Stop()
				return
			case <-ticker.C:
				publish()
			}
		}
	}()

	return func() {
		close(done)
		// Clean up the heartbeat key
		_ = b.KVDelete(topics.BucketHeartbeats, moduleID)
	}
}
