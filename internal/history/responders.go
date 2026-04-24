//go:build history || all || mantle

package history

import (
	"context"
	"encoding/json"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// registerQueryResponders wires request/reply subscriptions for
// history.query, history.usage, history.enabled, and history.metrics.
// Subscriptions are appended to h.subs for shutdown cleanup.
func (h *History) registerQueryResponders(ctx context.Context, b bus.Bus) {
	querySub, err := b.Subscribe(topics.HistoryQuery, func(subject string, data []byte, reply bus.ReplyFunc) {
		h.handleQuery(ctx, data, reply)
	})
	if err != nil {
		h.log.Warn("history: failed to subscribe", "subject", topics.HistoryQuery, "error", err)
	}

	usageSub, err := b.Subscribe(topics.HistoryUsage, func(subject string, data []byte, reply bus.ReplyFunc) {
		h.handleUsage(ctx, data, reply)
	})
	if err != nil {
		h.log.Warn("history: failed to subscribe", "subject", topics.HistoryUsage, "error", err)
	}

	enabledSub, err := b.Subscribe(topics.HistoryEnabled, func(subject string, data []byte, reply bus.ReplyFunc) {
		h.handleEnabled(data, reply)
	})
	if err != nil {
		h.log.Warn("history: failed to subscribe", "subject", topics.HistoryEnabled, "error", err)
	}

	metricsSub, err := b.Subscribe(topics.HistoryMetrics, func(subject string, data []byte, reply bus.ReplyFunc) {
		h.handleMetrics(ctx, data, reply)
	})
	if err != nil {
		h.log.Warn("history: failed to subscribe", "subject", topics.HistoryMetrics, "error", err)
	}

	h.mu.Lock()
	if querySub != nil {
		h.subs = append(h.subs, querySub)
	}
	if usageSub != nil {
		h.subs = append(h.subs, usageSub)
	}
	if enabledSub != nil {
		h.subs = append(h.subs, enabledSub)
	}
	if metricsSub != nil {
		h.subs = append(h.subs, metricsSub)
	}
	h.mu.Unlock()
}

func (h *History) handleQuery(ctx context.Context, data []byte, reply bus.ReplyFunc) {
	var req itypes.HistoryQueryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		sendReply(reply, itypes.HistoryQueryResponse{
			RequestID: req.RequestID,
			Success:   false,
			Error:     "invalid request: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		})
		return
	}

	h.mu.Lock()
	db := h.db
	h.mu.Unlock()
	if db == nil {
		sendReply(reply, itypes.HistoryQueryResponse{
			RequestID: req.RequestID,
			Success:   false,
			Error:     "database not available",
			Timestamp: time.Now().UnixMilli(),
		})
		return
	}

	results, err := queryHistory(ctx, db, req)
	resp := itypes.HistoryQueryResponse{
		RequestID: req.RequestID,
		Timestamp: time.Now().UnixMilli(),
	}
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
	} else {
		resp.Success = true
		resp.Results = results
	}
	sendReply(reply, resp)
}

func (h *History) handleUsage(ctx context.Context, data []byte, reply bus.ReplyFunc) {
	var req struct {
		RequestID string `json:"requestId"`
	}
	_ = json.Unmarshal(data, &req)

	h.mu.Lock()
	db := h.db
	h.mu.Unlock()
	resp := itypes.HistoryUsageResponse{
		RequestID: req.RequestID,
		Timestamp: time.Now().UnixMilli(),
	}
	if db == nil {
		resp.Success = false
		resp.Error = "database not available"
		sendReply(reply, resp)
		return
	}

	stats, err := queryUsage(ctx, db)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
	} else {
		resp.Success = true
		resp.Usage = stats
	}
	sendReply(reply, resp)
}

func (h *History) handleEnabled(data []byte, reply bus.ReplyFunc) {
	var req struct {
		RequestID string `json:"requestId"`
	}
	_ = json.Unmarshal(data, &req)

	sendReply(reply, itypes.HistoryEnabledResponse{
		RequestID: req.RequestID,
		Enabled:   true,
		Timestamp: time.Now().UnixMilli(),
	})
}

func (h *History) handleMetrics(ctx context.Context, data []byte, reply bus.ReplyFunc) {
	var req struct {
		RequestID string `json:"requestId"`
	}
	_ = json.Unmarshal(data, &req)

	h.mu.Lock()
	db := h.db
	h.mu.Unlock()
	resp := itypes.HistoryMetricsResponse{
		RequestID: req.RequestID,
		Timestamp: time.Now().UnixMilli(),
	}
	if db == nil {
		resp.Success = false
		resp.Error = "database not available"
		sendReply(reply, resp)
		return
	}

	metrics, err := queryMetricsList(ctx, db)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
	} else {
		resp.Success = true
		resp.Metrics = metrics
	}
	sendReply(reply, resp)
}

func sendReply(reply bus.ReplyFunc, v interface{}) {
	if reply == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = reply(data)
}
