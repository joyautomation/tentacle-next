//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

// historyStreamEvent is the SSE payload for a live data update.
// Shape mirrors cortex subscribeRealtime expectations: caller filters
// client-side by (nodeId, deviceId, metricId).
type historyStreamEvent struct {
	NodeID    string      `json:"nodeId"`
	DeviceID  string      `json:"deviceId"`
	MetricID  string      `json:"metricId"`
	Value     interface{} `json:"value"`
	Timestamp int64       `json:"timestamp"`
	Datatype  string      `json:"datatype,omitempty"`
}

// handleStreamHistory opens an SSE connection that pushes every PlcDataMessage
// seen on *.data.> as a live data event. UDT messages are flattened into one
// event per member (metricId = variableId/member) to match the stored format.
// GET /api/v1/history/stream
func (m *Module) handleStreamHistory(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	events := make(chan historyStreamEvent, 256)

	sub, err := m.bus.Subscribe(topics.AllData(), func(subject string, data []byte, _ bus.ReplyFunc) {
		var msg types.PlcDataMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}
		if !msg.HistoryEnabled {
			return
		}
		ts := msg.Timestamp
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}

		if msg.Datatype == "udt" {
			assembled, ok := msg.Value.(map[string]interface{})
			if !ok {
				return
			}
			for memberName, memberValue := range assembled {
				ev := historyStreamEvent{
					NodeID:    msg.ModuleID,
					DeviceID:  msg.DeviceID,
					MetricID:  msg.VariableID + "/" + memberName,
					Value:     memberValue,
					Timestamp: ts,
				}
				select {
				case events <- ev:
				default:
				}
			}
			return
		}

		ev := historyStreamEvent{
			NodeID:    msg.ModuleID,
			DeviceID:  msg.DeviceID,
			MetricID:  msg.VariableID,
			Value:     msg.Value,
			Timestamp: ts,
			Datatype:  msg.Datatype,
		}
		select {
		case events <- ev:
		default:
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "subscribe failed: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	// Heartbeat comments keep proxies from timing out the connection.
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events:
			if err := sse.WriteEvent("data", ev); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := sse.WriteEvent("ping", map[string]int64{"t": time.Now().UnixMilli()}); err != nil {
				return
			}
		}
	}
}
