//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/scanner"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// ─── Device Helpers ────────────────────────────────────────────────────────

// getDevice reads a single DeviceConfig from the shared devices bucket.
func (m *Module) getDevice(deviceID string) (itypes.DeviceConfig, bool) {
	data, _, err := m.bus.KVGet(topics.BucketDevices, deviceID)
	if err != nil {
		return itypes.DeviceConfig{}, false
	}
	var cfg itypes.DeviceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return itypes.DeviceConfig{}, false
	}
	return cfg, true
}

// putDevice writes a DeviceConfig to the shared devices bucket.
func (m *Module) putDevice(deviceID string, cfg itypes.DeviceConfig) error {
	return scanner.Put(m.bus, deviceID, cfg)
}

// deleteDevice removes a DeviceConfig from the shared devices bucket.
func (m *Module) deleteDevice(deviceID string) error {
	return scanner.Delete(m.bus, deviceID)
}

// listDevices returns all DeviceConfig entries from the shared devices bucket.
func (m *Module) listDevices() (map[string]itypes.DeviceConfig, error) {
	return scanner.List(m.bus)
}

// ─── HTTP Handlers ─────────────────────────────────────────────────────────

// deviceEntry pairs a deviceId with its DeviceConfig for list/get responses.
type deviceEntry struct {
	DeviceID string `json:"deviceId"`
	itypes.DeviceConfig
}

// handleListDevices returns all devices.
// GET /api/v1/devices
func (m *Module) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := m.listDevices()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list devices: %v", err))
		return
	}
	out := make([]deviceEntry, 0, len(devices))
	for id, cfg := range devices {
		out = append(out, deviceEntry{DeviceID: id, DeviceConfig: cfg})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	writeJSON(w, http.StatusOK, out)
}

// handleGetDevice returns a single device.
// GET /api/v1/devices/{deviceId}
func (m *Module) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "deviceId")
	dev, ok := m.getDevice(deviceID)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("device %q not found", deviceID))
		return
	}
	writeJSON(w, http.StatusOK, deviceEntry{DeviceID: deviceID, DeviceConfig: dev})
}

// handleSetDevice creates or replaces a device.
// PUT /api/v1/devices/{deviceId}
func (m *Module) handleSetDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "deviceId")

	var body itypes.DeviceConfig
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if body.Protocol == "" {
		writeError(w, http.StatusBadRequest, "protocol is required")
		return
	}

	if err := m.putDevice(deviceID, body); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put device: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, deviceEntry{DeviceID: deviceID, DeviceConfig: body})
}

// handleDeleteDevice removes a device.
// DELETE /api/v1/devices/{deviceId}
func (m *Module) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "deviceId")
	if err := m.deleteDevice(deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete device: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
