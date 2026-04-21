//go:build api || all

package api

import (
	"encoding/json"
	"net/http"

	"github.com/joyautomation/tentacle/internal/topics"
	ttypes "github.com/joyautomation/tentacle/types"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// readJSON decodes the request body into v.
func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// isModuleRunning checks if any module with the given serviceType has a
// recent heartbeat in the KV store. Use this to skip NATS requests that
// would just timeout waiting for a module that isn't running.
func (m *Module) isModuleRunning(serviceType string) bool {
	keys, err := m.bus.KVKeys(topics.BucketHeartbeats)
	if err != nil {
		return false
	}
	for _, key := range keys {
		data, _, err := m.bus.KVGet(topics.BucketHeartbeats, key)
		if err != nil {
			continue
		}
		var hb ttypes.ServiceHeartbeat
		if json.Unmarshal(data, &hb) != nil {
			continue
		}
		if hb.ServiceType == serviceType {
			return true
		}
	}
	return false
}
