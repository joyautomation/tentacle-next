//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/joyautomation/tentacle/internal/topics"
	ttypes "github.com/joyautomation/tentacle/types"
)

// Target identifies a remote tentacle for fleet-mode configurator endpoints.
// Local-mode requests omit ?target=...; remote-mode requests pass
// ?target=<groupId>/<nodeId> and the API dispatches to git+sparkplug-host.
type Target struct {
	Group string
	Node  string
}

// IsRemote reports whether the request is targeting a remote tentacle.
func (t Target) IsRemote() bool { return t.Group != "" && t.Node != "" }

// parseTarget extracts ?target=<group>/<node> from the request. Returns a
// zero Target (IsRemote() == false) when absent or malformed; the caller
// then falls through to the existing local-KV path.
func parseTarget(r *http.Request) Target {
	raw := strings.TrimSpace(r.URL.Query().Get("target"))
	if raw == "" {
		return Target{}
	}
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Target{}
	}
	return Target{Group: parts[0], Node: parts[1]}
}

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
