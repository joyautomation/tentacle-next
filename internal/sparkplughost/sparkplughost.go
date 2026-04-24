//go:build sparkplughost || mantle || all

// Package sparkplughost is the Sparkplug B Host Application (a.k.a. Primary
// Host) module: it connects to an MQTT broker (embedded mqtt-broker, EMQX,
// HiveMQ, Mosquitto, etc.), subscribes to all Sparkplug B node/device
// messages, decodes the protobuf payload, and republishes each metric onto
// the bus as a PlcDataMessage so history and downstream consumers see it.
//
// This is the inbound side of mantle. Edge tentacles run sparkplug-node and
// publish data; mantle runs sparkplug-host and consumes it.
package sparkplughost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/sparkplug"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const serviceType = "sparkplug-host"

type Module struct {
	moduleID string
	log      *slog.Logger

	mu     sync.Mutex
	client paho.Client
	stopHB func()
	subs   []bus.Subscription

	stats struct {
		messagesReceived atomic.Int64
		metricsPublished atomic.Int64
		decodeErrors     atomic.Int64
	}
}

func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "sparkplug-host"
	}
	return &Module{moduleID: moduleID}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceType }

func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.log = slog.Default().With("serviceType", serviceType, "moduleID", m.moduleID)
	cfg := loadConfig(m.moduleID)

	opts := paho.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetCleanSession(cfg.CleanSession).
		SetKeepAlive(time.Duration(cfg.KeepAlive) * time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	subTopic := buildSubscriptionFilter(cfg)
	opts.OnConnect = func(c paho.Client) {
		m.log.Info("sparkplug-host: connected to broker", "broker", cfg.BrokerURL, "filter", subTopic)
		token := c.Subscribe(subTopic, 0, func(_ paho.Client, msg paho.Message) {
			m.handleMessage(b, msg.Topic(), msg.Payload())
		})
		if token.Wait() && token.Error() != nil {
			m.log.Error("sparkplug-host: subscribe failed", "error", token.Error())
		}
	}
	opts.OnConnectionLost = func(_ paho.Client, err error) {
		m.log.Warn("sparkplug-host: connection lost", "error", err)
	}

	client := paho.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("connect to %s: %w", cfg.BrokerURL, token.Error())
	}
	m.mu.Lock()
	m.client = client
	m.mu.Unlock()

	m.stopHB = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		return map[string]interface{}{
			"broker":           cfg.BrokerURL,
			"filter":           subTopic,
			"messagesReceived": m.stats.messagesReceived.Load(),
			"metricsPublished": m.stats.metricsPublished.Load(),
			"decodeErrors":     m.stats.decodeErrors.Load(),
		}
	})

	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("sparkplug-host: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.mu.Lock()
	m.subs = append(m.subs, shutdownSub)
	m.mu.Unlock()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

func (m *Module) Stop() error {
	m.mu.Lock()
	client := m.client
	m.client = nil
	subs := m.subs
	m.subs = nil
	stopHB := m.stopHB
	m.stopHB = nil
	m.mu.Unlock()

	for _, s := range subs {
		_ = s.Unsubscribe()
	}
	if stopHB != nil {
		stopHB()
	}
	if client != nil && client.IsConnected() {
		client.Disconnect(250)
	}
	if m.log != nil {
		m.log.Info("sparkplug-host: stopped")
	}
	return nil
}

// buildSubscriptionFilter returns the MQTT topic filter to subscribe to.
// Sparkplug B topic shape: spBv1.0/<group>/<messageType>/<edgeNode>/<device>
// We subscribe to all message types and devices (including node-level which
// has 4 segments — handled via a second filter).
//
// If SharedGroup is set, wraps each filter as $share/<group>/... for MQTT 5
// shared subscriptions (HA fan-out).
func buildSubscriptionFilter(cfg Config) string {
	base := "spBv1.0/" + cfg.GroupFilter + "/+/+/#"
	if cfg.SharedGroup != "" {
		return "$share/" + cfg.SharedGroup + "/" + base
	}
	return base
}

// handleMessage decodes a single Sparkplug message and republishes its
// metrics onto the bus as PlcDataMessage values.
func (m *Module) handleMessage(b bus.Bus, topic string, payload []byte) {
	m.stats.messagesReceived.Add(1)

	parts := strings.Split(topic, "/")
	if len(parts) < 4 || parts[0] != "spBv1.0" {
		return
	}
	group := parts[1]
	msgType := parts[2]
	edgeNode := parts[3]
	device := ""
	if len(parts) >= 5 {
		device = parts[4]
	}

	// Skip STATE messages (host application status, not data).
	if msgType == "STATE" {
		return
	}

	pl, err := sparkplug.DecodePayload(payload)
	if err != nil {
		m.stats.decodeErrors.Add(1)
		m.log.Debug("sparkplug-host: decode failed", "topic", topic, "error", err)
		return
	}

	deviceKey := encodeDeviceKey(group, edgeNode, device)
	for i := range pl.Metrics {
		metric := &pl.Metrics[i]
		if metric.IsNull {
			continue
		}
		m.publishMetric(b, deviceKey, metric, msgType)
	}
}

// encodeDeviceKey packs Sparkplug group/edgeNode/device into a single
// dotted-segment-safe deviceID using underscores. NATS subjects can't
// contain dots within a segment, and history groups by deviceID so we keep
// the shape lossless and reversible.
func encodeDeviceKey(group, edgeNode, device string) string {
	if device == "" {
		return group + "_" + edgeNode
	}
	return group + "_" + edgeNode + "_" + device
}

// sanitizeMetricID makes a metric name safe for use as a NATS subject
// segment. Sparkplug allows "/" and other chars in metric names; we replace
// problematic ones with "_" while keeping the original on the JSON message
// for downstream consumers.
func sanitizeMetricID(name string) string {
	r := strings.NewReplacer(".", "_", " ", "_", "/", "_")
	return r.Replace(name)
}

func (m *Module) publishMetric(b bus.Bus, deviceKey string, metric *sparkplug.Metric, msgType string) {
	datatype := datatypeName(metric.Datatype)
	msg := types.PlcDataMessage{
		ModuleID:       m.moduleID,
		DeviceID:       deviceKey,
		VariableID:     metric.Name,
		Value:          metric.Value,
		Timestamp:      int64(metric.Timestamp),
		Datatype:       datatype,
		HistoryEnabled: true,
	}
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		m.log.Debug("sparkplug-host: marshal failed", "metric", metric.Name, "error", err)
		return
	}

	subject := topics.Data(m.moduleID, deviceKey, sanitizeMetricID(metric.Name))
	if err := b.Publish(subject, data); err != nil {
		m.log.Debug("sparkplug-host: bus publish failed", "subject", subject, "error", err)
		return
	}
	m.stats.metricsPublished.Add(1)
	_ = msgType // reserved for future per-message-type routing
}

// datatypeName maps a Sparkplug datatype id to history's datatype string.
// The full enum is in internal/sparkplug/pb; we collapse to the four
// categories the historian recognizes: number, boolean, string, udt.
func datatypeName(dt uint32) string {
	switch dt {
	case 11: // Boolean
		return "boolean"
	case 12: // String
		return "string"
	case 13: // DateTime
		return "number"
	case 14: // Text
		return "string"
	case 15: // UUID
		return "string"
	case 16: // DataSet
		return "string"
	case 17: // Bytes
		return "string"
	case 18: // File
		return "string"
	case 19: // Template
		return "udt"
	case 20, 21, 22, 23: // Property arrays
		return "string"
	default:
		// Numeric types (1-10, 24+) all map to "number".
		return "number"
	}
}
