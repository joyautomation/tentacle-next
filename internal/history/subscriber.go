//go:build history || all

package history

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joyautomation/tentacle/internal/rbe"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

// Batch tuning constants.
const (
	batchMaxSize     = 100
	batchFlushPeriod = 1 * time.Second
)

// rawScannerPrefixes are subject prefixes for raw scanner modules whose data
// should NOT be stored directly — only gateway/PLC-processed values are stored.
var rawScannerPrefixes = []string{
	"ethernetip.",
	"snmp.",
	"opcua.",
	"modbus.",
}

// isRawScannerSubject returns true if the NATS subject belongs to a raw
// scanner module that the history module should skip.
func isRawScannerSubject(subject string) bool {
	for _, prefix := range rawScannerPrefixes {
		if strings.HasPrefix(subject, prefix) {
			return true
		}
	}
	return false
}

// handleDataMessage processes a single PlcDataMessage from the bus and
// buffers a HistoryRecord if RBE passes.
func (h *History) handleDataMessage(subject string, data []byte) {
	// Skip raw scanner data — only store gateway/PLC values.
	if isRawScannerSubject(subject) {
		return
	}

	var msg types.PlcDataMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		h.log.Debug("history: failed to parse PlcDataMessage", "subject", subject, "error", err)
		return
	}

	// Skip UDT values — we store atomic values only.
	if msg.Datatype == "udt" {
		return
	}

	// RBE deadband check.
	key := fmt.Sprintf("%s:%s", msg.ModuleID, msg.VariableID)
	state := h.rbeStates.get(key)
	nowMs := time.Now().UnixMilli()

	if !rbe.ShouldPublish(state, msg.Value, nowMs, deadbandForMessage(&msg), msg.DisableRBE) {
		return
	}

	// Build the record.
	rec := buildRecord(msg.ModuleID, msg.VariableID, msg.Value, msg.Datatype, nowMs)

	// Record the publish in RBE state.
	rbe.RecordPublish(state, msg.Value, nowMs)

	// Add to batch; flush immediately if threshold reached.
	if h.batch.add(rec) {
		h.flushBatch()
	}
}

// buildRecord creates a HistoryRecord from a value, populating the
// appropriate typed column.
func buildRecord(moduleID, variableID string, value interface{}, datatype string, timestamp int64) itypes.HistoryRecord {
	rec := itypes.HistoryRecord{
		ModuleID:   moduleID,
		VariableID: variableID,
		Timestamp:  timestamp,
	}

	switch datatype {
	case "boolean":
		if b, ok := toBool(value); ok {
			rec.BoolValue = &b
		}
	case "string":
		s := fmt.Sprintf("%v", value)
		rec.StringValue = &s
	case "number":
		if f, ok := rbe.ToFloat64(value); ok {
			// If the value is an exact integer, store in int_value as well.
			if isExactInt(f) {
				i := int64(f)
				rec.IntValue = &i
			}
			f32 := float64(f)
			rec.FloatValue = &f32
		} else {
			// Fallback: store as string.
			s := fmt.Sprintf("%v", value)
			rec.StringValue = &s
		}
	default:
		// Try numeric first, then boolean, then fall back to string.
		if f, ok := rbe.ToFloat64(value); ok {
			rec.FloatValue = &f
		} else if b, ok := toBool(value); ok {
			rec.BoolValue = &b
		} else {
			s := fmt.Sprintf("%v", value)
			rec.StringValue = &s
		}
	}

	return rec
}

// toBool attempts to convert an interface{} to bool.
func toBool(v interface{}) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case json.Number:
		if i, err := b.Int64(); err == nil {
			return i != 0, true
		}
		return false, false
	default:
		return false, false
	}
}

// isExactInt returns true if f has no fractional part.
func isExactInt(f float64) bool {
	return f == float64(int64(f))
}
