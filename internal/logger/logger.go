// Package logger provides an slog.Handler that publishes log entries to the Bus
// in addition to writing to stdout. This replaces the duplicated natsLogger
// struct found in every existing Go module.
package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"slices"
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
	serviceType string
	moduleID    string
	fallback    slog.Handler
	attrs       []slog.Attr
	mu          sync.Mutex
}

// New creates a per-module logger that publishes logs to the Bus and to stdout.
func New(b bus.Bus, serviceType, moduleID string) *slog.Logger {
	h := &BusHandler{
		b:           b,
		serviceType: serviceType,
		moduleID:    moduleID,
		fallback:    slog.NewTextHandler(os.Stdout, nil),
	}
	return slog.New(h)
}

// SetGlobal installs a BusHandler as the global slog default.
// Modules should create child loggers via slog.Default().With("serviceType", ..., "moduleID", ...)
// to get per-module routing. Un-attributed logs go to "tentacle.tentacle".
func SetGlobal(b bus.Bus) {
	h := &BusHandler{
		b:           b,
		serviceType: "tentacle",
		moduleID:    "tentacle",
		fallback:    slog.NewTextHandler(os.Stdout, nil),
	}
	slog.SetDefault(slog.New(h))
}

func (h *BusHandler) Enabled(_ context.Context, level slog.Level) bool {
	return true
}

func (h *BusHandler) Handle(_ context.Context, r slog.Record) error {
	// Always write to fallback (stdout)
	_ = h.fallback.Handle(context.Background(), r)

	// Determine service type and module ID from stored attrs or handler defaults
	serviceType := h.serviceType
	moduleID := h.moduleID

	// Check record attrs for overrides
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "serviceType":
			serviceType = a.Value.String()
		case "moduleID":
			moduleID = a.Value.String()
		}
		return true
	})

	// Build message with extra attributes appended
	msg := r.Message
	loggerName := ""
	skip := map[string]bool{"serviceType": true, "moduleID": true, "logger": true}
	var extras []string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "logger" {
			loggerName = a.Value.String()
		} else if !skip[a.Key] {
			extras = append(extras, fmt.Sprintf("%s=%s", a.Key, a.Value.String()))
		}
		return true
	})
	// Also check stored attrs for logger
	for _, a := range h.attrs {
		if a.Key == "logger" {
			loggerName = a.Value.String()
		}
	}
	if len(extras) > 0 {
		msg = msg + " " + strings.Join(extras, " ")
	}

	// Publish to bus
	entry := types.ServiceLogEntry{
		Timestamp:   r.Time.UnixMilli(),
		Level:       levelToString(r.Level),
		Message:     msg,
		ServiceType: serviceType,
		ModuleID:    moduleID,
		Logger:      loggerName,
	}

	if h.b != nil {
		subject := topics.ServiceLogs(serviceType, moduleID)
		data, err := json.Marshal(entry)
		if err != nil {
			return nil
		}
		_ = h.b.Publish(subject, data)
	}
	return nil
}

func (h *BusHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newH := &BusHandler{
		b:           h.b,
		serviceType: h.serviceType,
		moduleID:    h.moduleID,
		fallback:    h.fallback.WithAttrs(attrs),
		attrs:       append(slices.Clone(h.attrs), attrs...),
	}
	for _, a := range attrs {
		switch a.Key {
		case "serviceType":
			newH.serviceType = a.Value.String()
		case "moduleID":
			newH.moduleID = a.Value.String()
		}
	}
	return newH
}

func (h *BusHandler) WithGroup(name string) slog.Handler {
	return &BusHandler{
		b:           h.b,
		serviceType: h.serviceType,
		moduleID:    h.moduleID,
		fallback:    h.fallback.WithGroup(name),
		attrs:       slices.Clone(h.attrs),
	}
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
func LogInfo(b bus.Bus, serviceType, moduleID, loggerName, msg string, args ...interface{}) {
	entry := types.ServiceLogEntry{
		Timestamp:   time.Now().UnixMilli(),
		Level:       "info",
		Message:     msg,
		ServiceType: serviceType,
		ModuleID:    moduleID,
		Logger:      loggerName,
	}
	data, _ := json.Marshal(entry)
	_ = b.Publish(topics.ServiceLogs(serviceType, moduleID), data)
}
