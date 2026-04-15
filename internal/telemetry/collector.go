//go:build telemetry || all

package telemetry

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/paths"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/internal/version"
	"github.com/joyautomation/tentacle/types"
)

// TelemetryPayload matches the server's POST /v1/telemetry contract.
type TelemetryPayload struct {
	InstanceID       string            `json:"instance_id"`
	UptimeSeconds    int64             `json:"uptime_seconds"`
	Modules          []string          `json:"modules"`
	ModuleCount      int               `json:"module_count"`
	RuntimeVersion   string            `json:"runtime_version"`
	TentacleVersion  string            `json:"tentacle_version"`
	OS               string            `json:"os"`
	Arch             string            `json:"arch"`
	ErrorCounts      map[string]int64  `json:"error_counts"`
}

const instanceIDFile = "telemetry-instance-id"

// instanceID returns or generates a persistent UUID for this tentacle instance.
func loadOrCreateInstanceID() (string, error) {
	idPath := filepath.Join(paths.DataDir(), instanceIDFile)

	data, err := os.ReadFile(idPath)
	if err == nil && len(data) > 0 {
		return string(data), nil
	}

	id, err := generateUUID()
	if err != nil {
		return "", fmt.Errorf("generate instance ID: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(idPath), 0o755); err != nil {
		return id, nil // use the ID even if we can't persist
	}
	_ = os.WriteFile(idPath, []byte(id), 0o644)
	return id, nil
}

func generateUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// Set version 4 and variant bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// collector gathers runtime metrics and module state from the bus.
type collector struct {
	startedAt  time.Time
	instanceID string
	cfg        Config
	b          bus.Bus

	mu             sync.Mutex
	modules        map[string]types.ServiceHeartbeat // moduleID -> latest heartbeat
	errorCounts    map[string]int64                  // category -> count (reset each report)
}

func newCollector(instanceID string, cfg Config, b bus.Bus) *collector {
	return &collector{
		startedAt:   time.Now(),
		instanceID:  instanceID,
		cfg:         cfg,
		b:           b,
		modules:     make(map[string]types.ServiceHeartbeat),
		errorCounts: make(map[string]int64),
	}
}

// updateModules refreshes the module list from the heartbeats KV bucket.
func (c *collector) updateModules() {
	keys, err := c.b.KVKeys(topics.BucketHeartbeats)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear stale entries and rebuild.
	c.modules = make(map[string]types.ServiceHeartbeat, len(keys))
	for _, key := range keys {
		data, _, err := c.b.KVGet(topics.BucketHeartbeats, key)
		if err != nil {
			continue
		}
		var hb types.ServiceHeartbeat
		if json.Unmarshal(data, &hb) == nil {
			c.modules[key] = hb
		}
	}
}

// recordError increments the error count for a given module type.
func (c *collector) recordError(moduleType string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errorCounts[moduleType]++
}

// collect builds the telemetry payload from current state.
func (c *collector) collect() TelemetryPayload {
	c.updateModules()

	c.mu.Lock()
	defer c.mu.Unlock()

	moduleNames := make([]string, 0, len(c.modules))
	for _, hb := range c.modules {
		moduleNames = append(moduleNames, hb.ServiceType)
	}

	// Copy and reset error counts.
	errorCounts := make(map[string]int64, len(c.errorCounts))
	for k, v := range c.errorCounts {
		errorCounts[k] = v
	}
	c.errorCounts = make(map[string]int64)

	return TelemetryPayload{
		InstanceID:      c.instanceID,
		UptimeSeconds:   int64(time.Since(c.startedAt).Seconds()),
		Modules:         moduleNames,
		ModuleCount:     len(moduleNames),
		RuntimeVersion:  runtime.Version(),
		TentacleVersion: version.Version,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		ErrorCounts:     errorCounts,
	}
}

// configHash returns a SHA256 hash of the tentacle config from KV, or empty string.
func (c *collector) configHash() string {
	if !c.cfg.ConfigHash {
		return ""
	}
	data, _, err := c.b.KVGet(topics.BucketTentacleConfig, "config")
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}
