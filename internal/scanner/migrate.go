package scanner

import (
	"encoding/json"
	"log/slog"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// MigrateLegacyDevices pulls the legacy `devices` field out of a
// consumer config bucket (gateway_config, plc_config) and writes each
// entry to the shared sources bucket. The consumer config is rewritten
// without the `devices` field, so subsequent reads see the modern shape.
//
// Safe to call repeatedly — idempotent. Existing entries in the sources
// bucket are not overwritten (first consumer to start wins; both should
// be semantically equivalent anyway).
func MigrateLegacyDevices(b bus.Bus, log *slog.Logger, configBucket, configKey string) {
	data, rev, err := b.KVGet(configBucket, configKey)
	if err != nil {
		return
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	devRaw, hasDevices := raw["devices"]
	if !hasDevices {
		return
	}

	var devices map[string]itypes.SourceConfig
	if err := json.Unmarshal(devRaw, &devices); err != nil {
		log.Warn("scanner.migrate: failed to parse legacy devices", "bucket", configBucket, "key", configKey, "error", err)
		return
	}

	migrated := 0
	for deviceID, cfg := range devices {
		if _, _, err := b.KVGet(topics.BucketSources, deviceID); err == nil {
			continue
		}
		if err := Put(b, deviceID, cfg); err != nil {
			log.Warn("scanner.migrate: failed to seed source", "deviceId", deviceID, "error", err)
			continue
		}
		migrated++
	}

	delete(raw, "devices")
	cleaned, err := json.Marshal(raw)
	if err == nil {
		if _, err := b.KVPut(configBucket, configKey, cleaned); err != nil {
			log.Warn("scanner.migrate: failed to rewrite config without devices", "bucket", configBucket, "key", configKey, "error", err)
		}
	}

	if migrated > 0 {
		log.Info("scanner.migrate: seeded sources bucket from legacy devices", "bucket", configBucket, "key", configKey, "count", migrated, "revision", rev)
	}
}
