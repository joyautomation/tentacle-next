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
// entry to the shared devices bucket. The consumer config is rewritten
// without the `devices` field, so subsequent reads see the modern shape.
//
// Safe to call repeatedly — idempotent. Existing entries in the devices
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

	var devices map[string]itypes.DeviceConfig
	if err := json.Unmarshal(devRaw, &devices); err != nil {
		log.Warn("scanner.migrate: failed to parse legacy devices", "bucket", configBucket, "key", configKey, "error", err)
		return
	}

	migrated := 0
	for deviceID, cfg := range devices {
		if _, _, err := b.KVGet(topics.BucketDevices, deviceID); err == nil {
			continue
		}
		if err := Put(b, deviceID, cfg); err != nil {
			log.Warn("scanner.migrate: failed to seed device", "deviceId", deviceID, "error", err)
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
		log.Info("scanner.migrate: seeded devices bucket from legacy devices", "bucket", configBucket, "key", configKey, "count", migrated, "revision", rev)
	}
}

// MigrateLegacySourcesBucket drains the old `sources` bucket (pre-rename)
// into the new `devices` bucket. Idempotent: existing entries in the new
// bucket are not overwritten. Safe to call on every startup; a no-op
// once the old bucket is drained (or never existed).
func MigrateLegacySourcesBucket(b bus.Bus, log *slog.Logger) {
	keys, err := b.KVKeys(topics.BucketDevicesLegacy)
	if err != nil || len(keys) == 0 {
		return
	}

	migrated := 0
	for _, key := range keys {
		data, _, err := b.KVGet(topics.BucketDevicesLegacy, key)
		if err != nil {
			continue
		}
		if _, _, err := b.KVGet(topics.BucketDevices, key); err == nil {
			_ = b.KVDelete(topics.BucketDevicesLegacy, key)
			continue
		}
		if _, err := b.KVPut(topics.BucketDevices, key, data); err != nil {
			log.Warn("scanner.migrate: failed to copy legacy source to devices", "deviceId", key, "error", err)
			continue
		}
		_ = b.KVDelete(topics.BucketDevicesLegacy, key)
		migrated++
	}

	if migrated > 0 {
		log.Info("scanner.migrate: drained legacy sources bucket into devices", "count", migrated)
	}
}
