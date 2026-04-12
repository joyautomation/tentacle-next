//go:build api || all

package api

import (
	"net/http"

	"github.com/joyautomation/tentacle/internal/topics"
)

// persistedBuckets lists all KV buckets that hold user configuration/state.
// Clearing these restores the system to a fresh-install state.
var persistedBuckets = []string{
	topics.BucketDesiredServices,
	topics.BucketGatewayConfig,
	topics.BucketTentacleConfig,
	topics.BucketServiceEnabled,
	topics.BucketPlcVariables,
	topics.BucketDeviceRegistry,
	topics.BucketBrowseCache,
	topics.BucketScannerEthernetIP,
	topics.BucketScannerOpcUA,
	topics.BucketScannerModbus,
	topics.BucketScannerSNMP,
	topics.BucketProfinetConfig,
	topics.BucketScannerProfinetController,
	topics.BucketConfigMetadata,
}

// handleFactoryReset clears all persisted KV buckets, restoring the system
// to its initial state (no modules configured, triggers setup wizard).
// POST /api/v1/system/factory-reset
func (m *Module) handleFactoryReset(w http.ResponseWriter, r *http.Request) {
	for _, bucket := range persistedBuckets {
		keys, err := m.bus.KVKeys(bucket)
		if err != nil {
			m.log.Warn("factory reset: failed to list keys", "bucket", bucket, "error", err)
			continue
		}
		for _, key := range keys {
			if err := m.bus.KVDelete(bucket, key); err != nil {
				m.log.Warn("factory reset: failed to delete key", "bucket", bucket, "key", key, "error", err)
			}
		}
	}

	m.log.Info("factory reset completed — all configuration cleared")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
