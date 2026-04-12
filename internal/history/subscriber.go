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
// buffers HistoryRecords if RBE passes. UDT values are flattened into
// individual member records using "/" as the delimiter.
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

	// Skip if history is not enabled for this variable.
	if !msg.HistoryEnabled {
		return
	}

	nowMs := time.Now().UnixMilli()
	nodeID := msg.ModuleID
	deviceID := msg.DeviceID

	if msg.Datatype == "udt" {
		h.handleUdtMessage(&msg, nodeID, deviceID, nowMs)
		return
	}

	// Atomic variable — single record.
	key := fmt.Sprintf("%s:%s:%s:%s", h.groupID, nodeID, deviceID, msg.VariableID)
	state := h.rbeStates.get(key)

	if !rbe.ShouldPublish(state, msg.Value, nowMs, deadbandForMessage(&msg), msg.DisableRBE) {
		return
	}

	rec := buildRecord(h.groupID, nodeID, deviceID, msg.VariableID, msg.Value, msg.Datatype, nowMs)
	rbe.RecordPublish(state, msg.Value, nowMs)

	if h.batch.add(rec) {
		h.flushBatch()
	}
}

// handleUdtMessage flattens a UDT value into individual member records using
// "/" as the path delimiter (e.g., "pump1/pressure"), matching mantle's format.
func (h *History) handleUdtMessage(msg *types.PlcDataMessage, nodeID, deviceID string, nowMs int64) {
	assembled, ok := msg.Value.(map[string]interface{})
	if !ok {
		return
	}

	for memberName, memberValue := range assembled {
		metricID := msg.VariableID + "/" + memberName

		// Per-member RBE check.
		key := fmt.Sprintf("%s:%s:%s:%s", h.groupID, nodeID, deviceID, metricID)
		state := h.rbeStates.get(key)

		// Resolve per-member deadband if available.
		var db *types.DeadBandConfig
		if msg.MemberDeadbands != nil {
			if mdb, ok := msg.MemberDeadbands[memberName]; ok {
				db = &mdb
			}
		}
		if db == nil {
			db = msg.Deadband
		}

		if !rbe.ShouldPublish(state, memberValue, nowMs, db, msg.DisableRBE) {
			continue
		}

		// Determine member datatype from template definition.
		datatype := ""
		if msg.UdtTemplate != nil {
			for _, m := range msg.UdtTemplate.Members {
				if m.Name == memberName {
					datatype = m.Datatype
					break
				}
			}
		}
		if datatype == "" {
			datatype = inferDatatype(memberValue)
		}

		rec := buildRecord(h.groupID, nodeID, deviceID, metricID, memberValue, datatype, nowMs)
		rbe.RecordPublish(state, memberValue, nowMs)

		if h.batch.add(rec) {
			h.flushBatch()
		}
	}
}

// inferDatatype guesses the datatype from a value when template metadata is unavailable.
func inferDatatype(v interface{}) string {
	switch v.(type) {
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64, float32, int, int64, int32, json.Number:
		return "number"
	default:
		return "string"
	}
}

// buildRecord creates a HistoryRecord from a value, populating the
// appropriate typed column.
func buildRecord(groupID, nodeID, deviceID, metricID string, value interface{}, datatype string, timestamp int64) itypes.HistoryRecord {
	rec := itypes.HistoryRecord{
		GroupID:   groupID,
		NodeID:    nodeID,
		DeviceID:  deviceID,
		MetricID:  metricID,
		Timestamp: timestamp,
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
			if isExactInt(f) {
				i := int64(f)
				rec.IntValue = &i
			}
			f32 := float64(f)
			rec.FloatValue = &f32
		} else {
			s := fmt.Sprintf("%v", value)
			rec.StringValue = &s
		}
	default:
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
