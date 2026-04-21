//go:build telemetry || all

// Package telemetry collects fleet health data and error reports from the
// running tentacle instance and POSTs them to an external telemetry server.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const serviceType = "telemetry"

// Module implements the module.Module interface for telemetry collection.
type Module struct {
	moduleID string
	log      *slog.Logger
	b        bus.Bus
	cfg      Config

	collector *collector
	logBuf    *logRingBuffer

	stopHeartbeat func()
	stopCollect   chan struct{}
	subs          []bus.Subscription

	httpClient *http.Client
}

// New creates a new telemetry module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "telemetry"
	}
	return &Module{
		moduleID:    moduleID,
		stopCollect: make(chan struct{}),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceType }

// Start initializes the telemetry module and begins collection.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	// Ensure config bucket exists.
	if err := b.KVCreate(topics.BucketTentacleConfig, topics.BucketConfigs()[topics.BucketTentacleConfig]); err != nil {
		m.log.Warn("telemetry: failed to create config bucket", "error", err)
	}

	// Load config (KV → env → defaults) and persist for settings UI.
	m.cfg = loadConfig(b)
	saveConfig(b, &m.cfg)

	// Register config schema so the web UI settings page can render it.
	if schemaSub, err := config.RegisterSchema(b, serviceType, configSchema); err == nil {
		m.subs = append(m.subs, schemaSub)
	}

	// Watch for config changes at runtime.
	cfgWatchSub, err := b.KVWatchAll(topics.BucketTentacleConfig, func(key string, value []byte, op bus.KVOperation) {
		const prefix = "telemetry."
		if !strings.HasPrefix(key, prefix) {
			return
		}
		m.log.Info("telemetry: config changed", "key", key)
		m.cfg = loadConfig(b)
	})
	if err == nil {
		m.subs = append(m.subs, cfgWatchSub)
	}

	// Load or create persistent instance ID.
	instanceID, err := loadOrCreateInstanceID()
	if err != nil {
		return err
	}
	m.log.Info("telemetry: instance ID loaded", "instanceID", instanceID)

	m.collector = newCollector(instanceID, m.cfg, b)
	m.logBuf = newLogRingBuffer()

	// Start heartbeat.
	m.stopHeartbeat = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		return map[string]interface{}{
			"enabled":       m.cfg.Enabled,
			"endpoint":      m.cfg.Endpoint,
			"interval":      m.cfg.Interval,
			"errorsEnabled": m.cfg.ErrorsEnabled,
		}
	})

	// Subscribe to all service logs for the ring buffer and error detection.
	logSub, err := b.Subscribe("service.logs.>", func(_ string, data []byte, _ bus.ReplyFunc) {
		var entry types.ServiceLogEntry
		if json.Unmarshal(data, &entry) != nil {
			return
		}
		m.logBuf.push(entry)

		// Detect errors and report them if enabled.
		if m.cfg.Enabled && m.cfg.ErrorsEnabled && isError(entry.Level) {
			m.collector.recordError(entry.ServiceType)
			m.reportError(entry)
		}
	})
	if err != nil {
		m.log.Error("telemetry: failed to subscribe to service logs", "error", err)
	} else {
		m.subs = append(m.subs, logSub)
	}

	// Start periodic telemetry collection.
	go m.collectLoop()

	m.log.Info("telemetry: module started",
		"enabled", m.cfg.Enabled,
		"endpoint", m.cfg.Endpoint,
		"interval", m.cfg.Interval,
		"errorsEnabled", m.cfg.ErrorsEnabled,
	)

	// Block until context cancelled or signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down the telemetry module.
func (m *Module) Stop() error {
	select {
	case <-m.stopCollect:
	default:
		close(m.stopCollect)
	}

	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil

	if m.stopHeartbeat != nil {
		m.stopHeartbeat()
	}

	m.log.Info("telemetry: module stopped")
	return nil
}

// collectLoop runs the periodic telemetry POST.
func (m *Module) collectLoop() {
	// Send initial report shortly after startup.
	time.Sleep(30 * time.Second)
	if m.cfg.Enabled {
		m.sendTelemetry()
	}

	ticker := time.NewTicker(time.Duration(m.cfg.Interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCollect:
			return
		case <-ticker.C:
			if m.cfg.Enabled {
				m.sendTelemetry()
			}
		}
	}
}

// sendTelemetry collects metrics and POSTs them to the server.
func (m *Module) sendTelemetry() {
	payload := m.collector.collect()

	data, err := json.Marshal(payload)
	if err != nil {
		m.log.Warn("telemetry: failed to marshal payload", "error", err)
		return
	}

	if err := m.post("/v1/telemetry", data); err != nil {
		m.log.Warn("telemetry: failed to send telemetry", "error", err)
	}
}

// reportError sends an error report to the server.
func (m *Module) reportError(entry types.ServiceLogEntry) {
	recentLogs := m.logBuf.recent(entry.ModuleID)
	payload := buildErrorPayload(m.collector.instanceID, entry, recentLogs)

	data, err := json.Marshal(payload)
	if err != nil {
		m.log.Warn("telemetry: failed to marshal error payload", "error", err)
		return
	}

	// Fire and forget in a goroutine to avoid blocking the log subscriber.
	go func() {
		if err := m.post("/v1/errors", data); err != nil {
			m.log.Warn("telemetry: failed to send error report", "error", err)
		}
	}()
}

// post sends a JSON payload to the telemetry server.
func (m *Module) post(path string, body []byte) error {
	url := strings.TrimRight(m.cfg.Endpoint, "/") + path

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", m.cfg.APIKey)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return &httpError{StatusCode: resp.StatusCode}
	}
	return nil
}

type httpError struct {
	StatusCode int
}

func (e *httpError) Error() string {
	return http.StatusText(e.StatusCode)
}

// isError returns true for ERROR and FATAL log levels.
func isError(level string) bool {
	l := strings.ToUpper(level)
	return l == "ERROR" || l == "FATAL"
}
