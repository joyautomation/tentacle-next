// Package logger provides an slog.Handler that publishes log entries to the Bus
// in addition to writing to stdout. This replaces the duplicated natsLogger
// struct found in every existing Go module.
package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

// BusHandler is an slog.Handler that publishes ServiceLogEntry to the Bus
// and delegates to a fallback handler for local output.
type BusHandler struct {
	b           bus.Bus
	subject     string
	serviceType string
	moduleID    string
	fallback    slog.Handler
	mu          sync.Mutex
}

// New creates a new BusHandler that publishes logs to the Bus and to stdout.
func New(b bus.Bus, serviceType, moduleID string) *slog.Logger {
	h := &BusHandler{
		b:           b,
		subject:     topics.ServiceLogs(serviceType, moduleID),
		serviceType: serviceType,
		moduleID:    moduleID,
		fallback:    slog.Default().Handler(),
	}
	return slog.New(h)
}

func (h *BusHandler) Enabled(_ context.Context, level slog.Level) bool {
	return true
}

func (h *BusHandler) Handle(_ context.Context, r slog.Record) error {
	// Always write to fallback (stdout)
	_ = h.fallback.Handle(context.Background(), r)

	// Publish to bus
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := types.ServiceLogEntry{
		Timestamp:   r.Time.UnixMilli(),
		Level:       levelToString(r.Level),
		Message:     r.Message,
		ServiceType: h.serviceType,
		ModuleID:    h.moduleID,
	}

	// Extract logger attr if present
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "logger" {
			entry.Logger = a.Value.String()
		}
		return true
	})

	data, err := json.Marshal(entry)
	if err != nil {
		return nil // don't fail on log publish errors
	}
	_ = h.b.Publish(h.subject, data)
	return nil
}

func (h *BusHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *BusHandler) WithGroup(name string) slog.Handler {
	return h
}

func levelToString(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// LogInfo is a convenience function matching the existing module pattern.
func LogInfo(b bus.Bus, serviceType, moduleID, logger, msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	slog.Info(formatted, "logger", logger)

	entry := types.ServiceLogEntry{
		Timestamp:   time.Now().UnixMilli(),
		Level:       "info",
		Message:     formatted,
		ServiceType: serviceType,
		ModuleID:    moduleID,
		Logger:      logger,
	}
	data, _ := json.Marshal(entry)
	_ = b.Publish(topics.ServiceLogs(serviceType, moduleID), data)
}
