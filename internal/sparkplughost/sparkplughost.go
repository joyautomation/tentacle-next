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
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/sparkplug"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const serviceType = "sparkplug-host"

// Node is a single tracked Sparkplug B edge node.
type Node struct {
	GroupID     string             `json:"groupId"`
	NodeID      string             `json:"nodeId"`
	Online      bool               `json:"online"`
	LastSeen    int64              `json:"lastSeen"`
	FirstSeen   int64              `json:"firstSeen"`
	BdSeq       int64              `json:"bdSeq"`
	Devices     map[string]*Device `json:"devices"`
	NbirthTime  int64              `json:"nbirthTime,omitempty"`
	NdeathTime  int64              `json:"ndeathTime,omitempty"`
	MetricCount int                `json:"metricCount"`

	// Verbs lists the Node Control/* command metric names this node
	// advertised in its NBIRTH. Mantle UI uses this for capability discovery.
	Verbs []string `json:"verbs,omitempty"`

	// BrowseCaches holds the most-recent browse result per device, populated
	// by responses to Node Control/Browse RPC commands. Keyed by deviceId.
	// Stored as raw JSON so the wire shape matches the local browse-cache API.
	BrowseCaches map[string]json.RawMessage `json:"-"`

	// rpcInflight maps requestId → reply channel for outstanding RPC calls
	// targeting this node. Populated when mantle issues NCMD; drained by
	// the Node Status/<Verb> NDATA handler. Not serialized.
	rpcInflight map[string]chan rpcResponse `json:"-"`
}

// rpcResponse carries the decoded JSON envelope of an NDATA Node Status reply.
// Phase 3 will move this into internal/sparkplug alongside the request shape.
type rpcResponse struct {
	RequestID string          `json:"requestId"`
	OK        bool            `json:"ok"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// Device is a single device under an edge node.
type Device struct {
	DeviceID    string `json:"deviceId"`
	Online      bool   `json:"online"`
	LastSeen    int64  `json:"lastSeen"`
	MetricCount int    `json:"metricCount"`
}

type Module struct {
	moduleID string
	log      *slog.Logger

	mu         sync.Mutex
	client     paho.Client
	stateTopic string
	stopHB     func()
	subs       []bus.Subscription

	invMu sync.RWMutex
	nodes map[string]*Node // keyed by "group/node"

	stats struct {
		messagesReceived atomic.Int64
		metricsPublished atomic.Int64
		decodeErrors     atomic.Int64
		nbirth           atomic.Int64
		ndeath           atomic.Int64
		dbirth           atomic.Int64
		ddeath           atomic.Int64
	}
}

func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "sparkplug-host"
	}
	return &Module{
		moduleID: moduleID,
		nodes:    make(map[string]*Node),
	}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceType }

func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.log = slog.Default().With("serviceType", serviceType, "moduleID", m.moduleID)

	// Ensure required KV buckets exist
	for _, bucket := range []string{topics.BucketTentacleConfig, topics.BucketServiceEnabled, topics.BucketHeartbeats} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("sparkplug-host: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	cfg := loadConfig(b, m.moduleID)
	saveConfig(b, cfg)

	if schemaSub, err := config.RegisterSchema(b, serviceType, configSchema); err == nil {
		m.mu.Lock()
		m.subs = append(m.subs, schemaSub)
		m.mu.Unlock()
	}

	stateTopic := "spBv1.0/STATE/" + cfg.PrimaryHostID
	offlinePayload := []byte(`{"online":false,"timestamp":0}`)

	opts := paho.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetCleanSession(cfg.CleanSession).
		SetKeepAlive(time.Duration(cfg.KeepAlive) * time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetBinaryWill(stateTopic, offlinePayload, 1, true)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	subTopic := buildSubscriptionFilter(cfg)
	opts.OnConnect = func(c paho.Client) {
		m.log.Info("sparkplug-host: connected to broker", "broker", cfg.BrokerURL, "filter", subTopic, "primaryHostId", cfg.PrimaryHostID)

		// Sparkplug B 3.0 Host Application: publish retained STATE ONLINE on connect.
		online := []byte(fmt.Sprintf(`{"online":true,"timestamp":%d}`, time.Now().UnixMilli()))
		if t := c.Publish(stateTopic, 1, true, online); t.Wait() && t.Error() != nil {
			m.log.Warn("sparkplug-host: STATE publish failed", "topic", stateTopic, "error", t.Error())
		}

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
	m.stateTopic = stateTopic
	m.mu.Unlock()

	m.stopHB = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		m.invMu.RLock()
		total := len(m.nodes)
		online := 0
		staleMs := int64(cfg.StaleSeconds) * 1000
		now := time.Now().UnixMilli()
		for _, n := range m.nodes {
			if n.Online && now-n.LastSeen < staleMs {
				online++
			}
		}
		m.invMu.RUnlock()
		return map[string]interface{}{
			"broker":           cfg.BrokerURL,
			"filter":           subTopic,
			"messagesReceived": m.stats.messagesReceived.Load(),
			"metricsPublished": m.stats.metricsPublished.Load(),
			"decodeErrors":     m.stats.decodeErrors.Load(),
			"nodes":            total,
			"online":           online,
			"nbirth":           m.stats.nbirth.Load(),
			"ndeath":           m.stats.ndeath.Load(),
			"dbirth":           m.stats.dbirth.Load(),
			"ddeath":           m.stats.ddeath.Load(),
		}
	})

	nodesSub, _ := b.Subscribe(sparkplug.SubjectHostNodes, func(_ string, _ []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}
		data, err := json.Marshal(m.snapshot())
		if err != nil {
			return
		}
		_ = reply(data)
	})

	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("sparkplug-host: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.mu.Lock()
	if nodesSub != nil {
		m.subs = append(m.subs, nodesSub)
	}
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
	stateTopic := m.stateTopic
	m.stateTopic = ""
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
		if stateTopic != "" {
			offline := []byte(fmt.Sprintf(`{"online":false,"timestamp":%d}`, time.Now().UnixMilli()))
			if t := client.Publish(stateTopic, 1, true, offline); t.WaitTimeout(500*time.Millisecond) && t.Error() != nil {
				if m.log != nil {
					m.log.Warn("sparkplug-host: STATE OFFLINE publish failed", "error", t.Error())
				}
			}
		}
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

	m.publishFrameEvent(b, msgType, group, edgeNode, device, pl)
	m.updateInventory(msgType, group, edgeNode, device, len(pl.Metrics), pl)

	deviceKey := encodeDeviceKey(group, edgeNode, device)
	for i := range pl.Metrics {
		metric := &pl.Metrics[i]
		if metric.IsNull {
			continue
		}
		m.publishMetric(b, deviceKey, metric, msgType)
	}
}

// publishFrameEvent emits a sparkplug.FrameEvent on the bus so downstream
// modules (fleet, etc.) don't need their own MQTT connection.
func (m *Module) publishFrameEvent(b bus.Bus, msgType, group, edgeNode, device string, pl *sparkplug.Payload) {
	evt := sparkplug.FrameEvent{
		Type:        msgType,
		GroupID:     group,
		EdgeNode:    edgeNode,
		Device:      device,
		Timestamp:   time.Now().UnixMilli(),
		MetricCount: len(pl.Metrics),
	}
	if msgType == "NBIRTH" || msgType == "NDEATH" {
		for i := range pl.Metrics {
			if pl.Metrics[i].Name == "bdSeq" {
				if v, ok := pl.Metrics[i].Value.(uint64); ok {
					evt.BdSeq = int64(v)
				}
				break
			}
		}
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if err := b.Publish(sparkplug.SubjectHostFrame, data); err != nil {
		m.log.Debug("sparkplug-host: frame event publish failed", "error", err)
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

// updateInventory tracks edge nodes/devices observed via Sparkplug B frames.
// It runs after publishFrameEvent so the bus event still fires for downstream
// consumers, while the inventory map serves the local /api/v1/sparkplug-host/nodes view.
func (m *Module) updateInventory(msgType, group, edgeNode, device string, metricCount int, pl *sparkplug.Payload) {
	now := time.Now().UnixMilli()

	m.invMu.Lock()
	defer m.invMu.Unlock()

	key := group + "/" + edgeNode
	n, ok := m.nodes[key]
	if !ok {
		n = &Node{
			GroupID:      group,
			NodeID:       edgeNode,
			FirstSeen:    now,
			Devices:      make(map[string]*Device),
			BrowseCaches: make(map[string]json.RawMessage),
			rpcInflight:  make(map[string]chan rpcResponse),
		}
		m.nodes[key] = n
	}
	n.LastSeen = now

	switch msgType {
	case "NBIRTH":
		m.stats.nbirth.Add(1)
		n.Online = true
		n.NbirthTime = now
		n.MetricCount = metricCount
		for i := range pl.Metrics {
			if pl.Metrics[i].Name == "bdSeq" {
				if v, ok := pl.Metrics[i].Value.(uint64); ok {
					n.BdSeq = int64(v)
				}
				break
			}
		}
	case "NDEATH":
		m.stats.ndeath.Add(1)
		n.Online = false
		n.NdeathTime = now
		for _, d := range n.Devices {
			d.Online = false
		}
	case "DBIRTH":
		m.stats.dbirth.Add(1)
		if device != "" {
			d := getOrCreateDevice(n, device)
			d.Online = true
			d.LastSeen = now
			d.MetricCount = metricCount
		}
	case "DDEATH":
		m.stats.ddeath.Add(1)
		if device != "" {
			if d, ok := n.Devices[device]; ok {
				d.Online = false
				d.LastSeen = now
			}
		}
	case "NDATA":
		n.Online = true
	case "DDATA":
		n.Online = true
		if device != "" {
			d := getOrCreateDevice(n, device)
			d.Online = true
			d.LastSeen = now
		}
	}
}

func getOrCreateDevice(n *Node, device string) *Device {
	if d, ok := n.Devices[device]; ok {
		return d
	}
	d := &Device{DeviceID: device}
	n.Devices[device] = d
	return d
}

// snapshot returns a deep-ish copy of the current inventory safe to marshal.
func (m *Module) snapshot() []*Node {
	m.invMu.RLock()
	defer m.invMu.RUnlock()
	out := make([]*Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		cp := *n
		cp.Devices = make(map[string]*Device, len(n.Devices))
		for k, d := range n.Devices {
			dCopy := *d
			cp.Devices[k] = &dCopy
		}
		if len(n.Verbs) > 0 {
			cp.Verbs = append([]string(nil), n.Verbs...)
		}
		cp.BrowseCaches = nil
		cp.rpcInflight = nil
		out = append(out, &cp)
	}
	return out
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
