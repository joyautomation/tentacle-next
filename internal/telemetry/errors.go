//go:build telemetry || all

package telemetry

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"sync"

	"github.com/joyautomation/tentacle/internal/version"
	"github.com/joyautomation/tentacle/types"
)

const ringSize = 50

// ErrorPayload matches the server's POST /v1/errors contract.
type ErrorPayload struct {
	InstanceID      string            `json:"instance_id"`
	ErrorMessage    string            `json:"error_message"`
	StackTrace      string            `json:"stack_trace,omitempty"`
	StackHash       string            `json:"stack_hash"`
	ModuleType      string            `json:"module_type"`
	ModuleActivity  string            `json:"module_activity,omitempty"`
	ModuleVersion   string            `json:"module_version"`
	OS              string            `json:"os"`
	Arch            string            `json:"arch"`
	RuntimeVersion  string            `json:"runtime_version"`
	SanitizedConfig map[string]string `json:"sanitized_config,omitempty"`
	LogContext      []LogContextEntry `json:"log_context"`
}

// LogContextEntry is a recent log line sent with an error report.
type LogContextEntry struct {
	Timestamp int64  `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Module    string `json:"module,omitempty"`
}

// logRingBuffer keeps the last N log entries per module.
type logRingBuffer struct {
	mu      sync.Mutex
	entries map[string]*ring // moduleID -> ring
}

type ring struct {
	buf  []types.ServiceLogEntry
	pos  int
	full bool
}

func newLogRingBuffer() *logRingBuffer {
	return &logRingBuffer{
		entries: make(map[string]*ring),
	}
}

// push adds a log entry to the ring buffer for the given module.
func (rb *logRingBuffer) push(entry types.ServiceLogEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	r, ok := rb.entries[entry.ModuleID]
	if !ok {
		r = &ring{buf: make([]types.ServiceLogEntry, ringSize)}
		rb.entries[entry.ModuleID] = r
	}
	r.buf[r.pos] = entry
	r.pos = (r.pos + 1) % ringSize
	if r.pos == 0 {
		r.full = true
	}
}

// recent returns the last N log entries for a module in chronological order.
func (rb *logRingBuffer) recent(moduleID string) []LogContextEntry {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	r, ok := rb.entries[moduleID]
	if !ok {
		return nil
	}

	var entries []types.ServiceLogEntry
	if r.full {
		// Ring has wrapped: read from pos to end, then start to pos.
		entries = make([]types.ServiceLogEntry, ringSize)
		copy(entries, r.buf[r.pos:])
		copy(entries[ringSize-r.pos:], r.buf[:r.pos])
	} else {
		entries = make([]types.ServiceLogEntry, r.pos)
		copy(entries, r.buf[:r.pos])
	}

	result := make([]LogContextEntry, len(entries))
	for i, e := range entries {
		result[i] = LogContextEntry{
			Timestamp: e.Timestamp,
			Level:     e.Level,
			Message:   e.Message,
			Module:    e.ModuleID,
		}
	}
	return result
}

// buildErrorPayload creates an error report from a log entry.
func buildErrorPayload(instanceID string, entry types.ServiceLogEntry, recentLogs []LogContextEntry) ErrorPayload {
	stackHash := fmt.Sprintf("%x", sha256.Sum256([]byte(entry.Message+entry.ServiceType)))

	return ErrorPayload{
		InstanceID:     instanceID,
		ErrorMessage:   entry.Message,
		StackHash:      stackHash,
		ModuleType:     entry.ServiceType,
		ModuleVersion:  version.Version,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		RuntimeVersion: runtime.Version(),
		LogContext:     recentLogs,
	}
}
