//go:build api || all

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joyautomation/tentacle/internal/paths"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/internal/version"
)

const (
	telemetryDefaultEndpoint = "https://telemetry.joyautomation.com"
	telemetryDefaultAPIKey   = "c71a01c68f9d3c0fca4e5c44bf8d16872706cabafda00aa47571f9cbe54d7e61"
	telemetryInstanceIDFile  = "telemetry-instance-id"
)

// handleGetTelemetryStatus returns the current telemetry consent state.
func (m *Module) handleGetTelemetryStatus(w http.ResponseWriter, r *http.Request) {
	enabled := m.getTelemetryConfig("TELEMETRY_ENABLED", "false")
	errorsEnabled := m.getTelemetryConfig("TELEMETRY_ERRORS_ENABLED", "true")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":       enabled == "true",
		"errorsEnabled": errorsEnabled == "true",
	})
}

// handleReportError receives a log entry from the frontend and POSTs it
// to the telemetry server as an error report. Works independently of
// whether the telemetry module is running.
func (m *Module) handleReportError(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message     string `json:"message"`
		Level       string `json:"level"`
		ServiceType string `json:"serviceType"`
		ModuleID    string `json:"moduleId"`
		Timestamp   int64  `json:"timestamp"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if body.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	// Load instance ID.
	instanceID := m.loadTelemetryInstanceID()

	// Build stack hash for dedup.
	stackHash := fmt.Sprintf("%x", sha256.Sum256([]byte(body.Message+body.ServiceType)))

	// Gather recent logs for this module from our log buffer.
	logContext := m.getRecentLogsForModule(body.ModuleID)

	// Build error payload matching server contract.
	payload := map[string]interface{}{
		"instance_id":    instanceID,
		"error_message":  body.Message,
		"stack_hash":     stackHash,
		"module_type":    body.ServiceType,
		"module_version": version.Version,
		"os":             runtime.GOOS,
		"arch":           runtime.GOARCH,
		"runtime_version": runtime.Version(),
		"log_context":    logContext,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal payload")
		return
	}

	// POST to telemetry server.
	endpoint := m.getTelemetryConfig("TELEMETRY_ENDPOINT", telemetryDefaultEndpoint)
	apiKey := m.getTelemetryConfig("TELEMETRY_API_KEY", telemetryDefaultAPIKey)

	url := strings.TrimRight(endpoint, "/") + "/v1/errors"
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to reach telemetry server: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("telemetry server returned %d", resp.StatusCode))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reported"})
}

// getTelemetryConfig reads a telemetry config value from KV, falling back to env then default.
func (m *Module) getTelemetryConfig(envVar, defaultVal string) string {
	if data, _, err := m.bus.KVGet(topics.BucketTentacleConfig, "telemetry."+envVar); err == nil && len(data) > 0 {
		return string(data)
	}
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return defaultVal
}

// loadTelemetryInstanceID reads the persisted instance ID, or returns "unknown".
func (m *Module) loadTelemetryInstanceID() string {
	idPath := filepath.Join(paths.DataDir(), telemetryInstanceIDFile)
	data, err := os.ReadFile(idPath)
	if err == nil && len(data) > 0 {
		return string(data)
	}
	return "unknown"
}

// getRecentLogsForModule returns recent log entries for a given module from the API log buffer.
func (m *Module) getRecentLogsForModule(moduleID string) []map[string]interface{} {
	m.logsMu.RLock()
	defer m.logsMu.RUnlock()

	var result []map[string]interface{}
	for _, entry := range m.logBuf {
		if entry.ModuleID == moduleID || moduleID == "" {
			result = append(result, map[string]interface{}{
				"timestamp": entry.Timestamp,
				"level":     entry.Level,
				"message":   entry.Message,
				"module":    entry.ModuleID,
			})
		}
	}

	// Return last 20 entries at most.
	if len(result) > 20 {
		result = result[len(result)-20:]
	}
	return result
}
