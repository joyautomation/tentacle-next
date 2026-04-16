package types

// DeviceCommStatus reports the live communication status of a single device
// tracked by a scanner module. It is surfaced through the module heartbeat
// under the "devices" metadata key and consumed by the gateway UI.
type DeviceCommStatus struct {
	DeviceID            string `json:"deviceId"`
	State               string `json:"state"` // "connected" | "connecting" | "disconnected" | "error"
	LastReadAt          int64  `json:"lastReadAt,omitempty"`  // unix ms of last successful read
	LastErrorAt         int64  `json:"lastErrorAt,omitempty"` // unix ms of last error
	LastError           string `json:"lastError,omitempty"`
	ConsecutiveFailures int    `json:"consecutiveFailures,omitempty"`
}
