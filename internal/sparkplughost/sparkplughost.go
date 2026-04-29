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
	"bytes"
	"context"
	cryptorand "crypto/rand"
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

	// BrowseCaches holds the most-recent browse result per device, captured
	// from Sparkplug "_meta/browse" String metrics emitted by edge tentacles
	// after a successful protocol browse. Keyed by deviceId. Stored as raw
	// JSON so the wire shape matches the local browse-cache API.
	BrowseCaches map[string]json.RawMessage `json:"browseCaches,omitempty"`

	// rpcInflight maps requestId → reply channel for outstanding RPC calls
	// targeting this node. Populated when mantle issues NCMD; drained by
	// the Node Status/<Verb> NDATA handler. Not serialized.
	rpcInflight map[string]chan sparkplug.RPCResponse `json:"-"`
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

	mu               sync.Mutex
	client           paho.Client
	stateTopic       string
	connectTimestamp int64 // timestamp baked into Will/BIRTH/DEATH; reused so they match
	stopHB           func()
	subs             []bus.Subscription

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
	// Sparkplug B 3.0 §6.4.2: STATE Will payload timestamp must be a current
	// UTC millisecond value at CONNECT time. The Sparkplug TCK Monitor parses
	// the Will payload and rejects anything outside its UTC window (60s).
	// The BIRTH timestamp MUST match the Will timestamp (TCK assertions
	// host-topic-phid-birth-payload-timestamp and
	// message-flow-phid-sparkplug-state-publish-payload-timestamp).
	connectTimestamp := time.Now().UnixMilli()
	offlinePayload := []byte(fmt.Sprintf(`{"online":false,"timestamp":%d}`, connectTimestamp))

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

	subFilters := buildSubscriptionFilters(cfg)
	subTopic := strings.Join(subFilters, ",")
	opts.OnConnect = func(c paho.Client) {
		m.log.Info("sparkplug-host: connected to broker", "broker", cfg.BrokerURL, "filter", subTopic, "primaryHostId", cfg.PrimaryHostID)

		// Sparkplug B 3.0 §6.4.2 / host-topic-phid-birth-sub-required:
		// Subscribe to the Sparkplug topic namespace BEFORE publishing the
		// retained STATE birth. Otherwise the host can race past edge
		// NBIRTH/DBIRTH messages that arrived between its CONNECT and SUB.
		filterMap := make(map[string]byte, len(subFilters))
		for _, f := range subFilters {
			filterMap[f] = 0
		}
		online := []byte(fmt.Sprintf(`{"online":true,"timestamp":%d}`, connectTimestamp))
		if token := c.SubscribeMultiple(filterMap, func(client paho.Client, msg paho.Message) {
			// Sparkplug B 3.0 §6.4.2 / message-flow-hid-sparkplug-state-message-delivered:
			// If we receive a STATE message on our own host_id with online:false,
			// we MUST immediately republish our BIRTH with the SAME timestamp as
			// the original CONNECT Will payload.
			if msg.Topic() == stateTopic && bytes.Contains(msg.Payload(), []byte(`"online":false`)) {
				m.log.Info("sparkplug-host: received STATE offline injection - republishing BIRTH", "topic", stateTopic)
				if t := client.Publish(stateTopic, 1, true, online); t.Wait() && t.Error() != nil {
					m.log.Warn("sparkplug-host: STATE resend failed", "error", t.Error())
				}
				return
			}
			m.handleMessage(b, msg.Topic(), msg.Payload())
		}); token.Wait() && token.Error() != nil {
			m.log.Error("sparkplug-host: subscribe failed", "error", token.Error())
		}

		if t := c.Publish(stateTopic, 1, true, online); t.Wait() && t.Error() != nil {
			m.log.Warn("sparkplug-host: STATE publish failed", "topic", stateTopic, "error", t.Error())
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
	m.connectTimestamp = connectTimestamp
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

	nodesDeleteSub, _ := b.Subscribe(sparkplug.SubjectHostNodesDelete, func(_ string, data []byte, reply bus.ReplyFunc) {
		var req struct {
			GroupID string `json:"groupId"`
			NodeID  string `json:"nodeId"`
		}
		if err := json.Unmarshal(data, &req); err != nil || req.GroupID == "" || req.NodeID == "" {
			if reply != nil {
				_ = reply([]byte(`{"removed":false,"error":"invalid request"}`))
			}
			return
		}
		key := req.GroupID + "/" + req.NodeID
		m.invMu.Lock()
		_, existed := m.nodes[key]
		delete(m.nodes, key)
		m.invMu.Unlock()
		if reply != nil {
			if existed {
				_ = reply([]byte(`{"removed":true}`))
			} else {
				_ = reply([]byte(`{"removed":false}`))
			}
		}
	})

	verbSub, _ := b.Subscribe(sparkplug.SubjectHostVerb, func(_ string, data []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}
		var req sparkplug.HostVerbRequest
		if err := json.Unmarshal(data, &req); err != nil {
			out, _ := json.Marshal(sparkplug.RPCResponse{Verb: req.Verb, OK: false, Error: "invalid request: " + err.Error()})
			_ = reply(out)
			return
		}
		timeout := time.Duration(req.TimeoutMs) * time.Millisecond
		resp, err := m.sendVerb(req.GroupID, req.NodeID, req.Verb, req.Params, timeout)
		if err != nil {
			out, _ := json.Marshal(sparkplug.RPCResponse{Verb: req.Verb, OK: false, Error: err.Error()})
			_ = reply(out)
			return
		}
		out, _ := json.Marshal(resp)
		_ = reply(out)
	})

	browseCacheSub, _ := b.Subscribe(sparkplug.SubjectHostBrowseCache, func(_ string, data []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}
		var req sparkplug.HostBrowseCacheRequest
		if err := json.Unmarshal(data, &req); err != nil {
			out, _ := json.Marshal(sparkplug.HostBrowseCacheReply{Error: "invalid request: " + err.Error()})
			_ = reply(out)
			return
		}
		key := req.GroupID + "/" + req.NodeID
		m.invMu.RLock()
		var cache json.RawMessage
		if n, ok := m.nodes[key]; ok {
			if c, ok := n.BrowseCaches[req.DeviceID]; ok {
				cache = append(json.RawMessage(nil), c...)
			}
		}
		m.invMu.RUnlock()
		out, _ := json.Marshal(sparkplug.HostBrowseCacheReply{Cache: cache})
		_ = reply(out)
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
	if nodesDeleteSub != nil {
		m.subs = append(m.subs, nodesDeleteSub)
	}
	if verbSub != nil {
		m.subs = append(m.subs, verbSub)
	}
	if browseCacheSub != nil {
		m.subs = append(m.subs, browseCacheSub)
	}
	m.subs = append(m.subs, shutdownSub)
	m.mu.Unlock()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	// Sparkplug B 3.0 §6.4.2: an intentionally-disconnecting Host Application
	// MUST publish a DEATH (online:false) before sending the MQTT DISCONNECT
	// (TCK SessionTerminationTest assertions
	// host-topic-phid-death-payload-timestamp-disconnect-clean,
	// operational-behavior-host-application-disconnect-intentional,
	// operational-behavior-host-application-termination).
	_ = m.Stop()
	return nil
}

func (m *Module) Stop() error {
	m.mu.Lock()
	client := m.client
	m.client = nil
	stateTopic := m.stateTopic
	m.stateTopic = ""
	connectTs := m.connectTimestamp
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
			// Use the original Will timestamp so the DEATH payload matches
			// what the TCK Monitor expects (BIRTH and DEATH should share the
			// same connect-time UTC value during a session).
			offline := []byte(fmt.Sprintf(`{"online":false,"timestamp":%d}`, connectTs))
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

// buildSubscriptionFilters returns the MQTT topic filters to subscribe to.
// Sparkplug B topic shape: spBv1.0/<group>/<messageType>/<edgeNode>/<device>
// Node-level messages (NBIRTH/NDEATH/NDATA/NCMD) have 4 segments; device-level
// messages have 5. We subscribe to both shapes explicitly because some brokers
// (including the embedded mqtt-broker) do not match `#` against zero levels,
// so a single `+/+/+/#` filter misses 4-segment topics.
//
// Sparkplug B 3.0 §6.4.2 also requires a Host Application to subscribe on
// its own spBv1.0/STATE/<host_id> topic so it can detect rogue clients
// publishing on its STATE namespace (host-topic-phid-birth-sub-required,
// payloads-state-subscribe, message-flow-phid-sparkplug-subscription).
//
// If SharedGroup is set, wraps the data filters as $share/<group>/... for
// MQTT 5 shared subscriptions (HA fan-out). The STATE filter is always
// non-shared so every host instance observes its own STATE topic.
func buildSubscriptionFilters(cfg Config) []string {
	// When GroupFilter is the wildcard "+", subscribe to the entire Sparkplug
	// namespace with `spBv1.0/#`. The Sparkplug TCK SessionEstablishmentTest
	// checks for this exact literal filter (checkSubscribes in the TCK source)
	// and FAILs assertions host-topic-phid-birth-sub-required,
	// message-flow-phid-sparkplug-subscription, payloads-state-subscribe if
	// the host subscribes only to narrower patterns like spBv1.0/+/+/+.
	var bases []string
	if cfg.GroupFilter == "" || cfg.GroupFilter == "+" {
		bases = []string{"spBv1.0/#"}
	} else {
		bases = []string{
			"spBv1.0/" + cfg.GroupFilter + "/+/+",
			"spBv1.0/" + cfg.GroupFilter + "/+/+/#",
		}
	}
	if cfg.SharedGroup != "" {
		shared := make([]string, len(bases))
		for i, b := range bases {
			shared[i] = "$share/" + cfg.SharedGroup + "/" + b
		}
		bases = shared
	}
	if cfg.PrimaryHostID != "" {
		bases = append(bases, "spBv1.0/STATE/"+cfg.PrimaryHostID)
	}
	return bases
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

	m.captureMeta(b, group, edgeNode, device, pl)
	if msgType == "NDATA" && device == "" {
		m.captureRPCReplies(group, edgeNode, pl)
	}
}

// captureRPCReplies scans a node-level NDATA for `Node Status/<Verb>` String
// metrics. Each one carries an RPCResponse JSON envelope referencing the
// requestId that mantle issued via NCMD; we drain rpcInflight[requestId] so
// the originating caller (API handler awaiting verb completion) wakes up.
func (m *Module) captureRPCReplies(group, edgeNode string, pl *sparkplug.Payload) {
	key := group + "/" + edgeNode
	for i := range pl.Metrics {
		metric := &pl.Metrics[i]
		if metric.IsNull || !strings.HasPrefix(metric.Name, sparkplug.NodeStatusPrefix) {
			continue
		}
		raw, ok := metric.Value.(string)
		if !ok {
			continue
		}
		var resp sparkplug.RPCResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			m.log.Debug("sparkplug-host: bad RPC reply", "metric", metric.Name, "error", err)
			continue
		}
		if resp.RequestID == "" {
			continue
		}
		m.invMu.Lock()
		n := m.nodes[key]
		var ch chan sparkplug.RPCResponse
		if n != nil {
			ch = n.rpcInflight[resp.RequestID]
			delete(n.rpcInflight, resp.RequestID)
		}
		m.invMu.Unlock()
		if ch != nil {
			select {
			case ch <- resp:
			default:
			}
		}
	}
}

// sendVerb publishes a `Node Control/<Verb>` NCMD targeting the named node
// and waits for the matching `Node Status/<Verb>` NDATA reply. Returns the
// decoded RPCResponse, or an error if publish or wait fails. timeout=0 uses
// a 15s default; the in-flight registration is cleaned up on every exit path.
func (m *Module) sendVerb(group, nodeID, verb string, params json.RawMessage, timeout time.Duration) (sparkplug.RPCResponse, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil || !client.IsConnected() {
		return sparkplug.RPCResponse{}, fmt.Errorf("sparkplug-host not connected to broker")
	}

	requestID := newRequestID()
	req := sparkplug.RPCRequest{RequestID: requestID, Verb: verb, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return sparkplug.RPCResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	metric := sparkplug.NewStringMetric(sparkplug.NodeControlPrefix+verb, string(body))
	payload := &sparkplug.Payload{
		Timestamp: uint64(time.Now().UnixMilli()),
		OmitSeq:   true, // Sparkplug B: NCMD has no seq
		Metrics:   []sparkplug.Metric{metric},
	}
	encoded, err := sparkplug.EncodePayload(payload)
	if err != nil {
		return sparkplug.RPCResponse{}, fmt.Errorf("encode NCMD: %w", err)
	}

	key := group + "/" + nodeID
	ch := make(chan sparkplug.RPCResponse, 1)

	m.invMu.Lock()
	n, ok := m.nodes[key]
	if !ok {
		m.invMu.Unlock()
		return sparkplug.RPCResponse{}, fmt.Errorf("unknown node %s", key)
	}
	if n.rpcInflight == nil {
		n.rpcInflight = make(map[string]chan sparkplug.RPCResponse)
	}
	n.rpcInflight[requestID] = ch
	m.invMu.Unlock()

	cleanup := func() {
		m.invMu.Lock()
		if n2 := m.nodes[key]; n2 != nil {
			delete(n2.rpcInflight, requestID)
		}
		m.invMu.Unlock()
	}

	topic := fmt.Sprintf("spBv1.0/%s/NCMD/%s", group, nodeID)
	if t := client.Publish(topic, 0, false, encoded); t.WaitTimeout(5*time.Second) && t.Error() != nil {
		cleanup()
		return sparkplug.RPCResponse{}, fmt.Errorf("publish NCMD: %w", t.Error())
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		cleanup()
		return sparkplug.RPCResponse{}, fmt.Errorf("verb %s timed out after %s", verb, timeout)
	}
}

// newRequestID returns a short hex string suitable for an RPC requestId.
// Deliberately not relying on uuid to keep this package's dep surface minimal.
func newRequestID() string {
	b := make([]byte, 8)
	if _, err := cryptorand.Read(b); err != nil {
		// Fall back to a timestamp + counter; collision risk is minimal at
		// our request rate and the consequence is just a stale rpcInflight
		// slot eventually GC'd by the next cleanup.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// captureMeta scans a payload for "_meta/*" metrics and routes their values
// into per-node observed-state on the host. Today only "_meta/browse" is
// recognized — it carries the JSON browse cache published by the edge after
// a successful protocol scan and lands in Node.BrowseCaches[device].
func (m *Module) captureMeta(b bus.Bus, group, edgeNode, device string, pl *sparkplug.Payload) {
	if device == "" {
		return
	}
	var browse string
	var browseSet bool
	var browseTs int64
	for i := range pl.Metrics {
		metric := &pl.Metrics[i]
		if metric.IsNull || !isMetaMetric(metric.Name) {
			continue
		}
		switch metric.Name {
		case "_meta/browse":
			if s, ok := metric.Value.(string); ok {
				browse = s
				browseSet = true
				browseTs = int64(metric.Timestamp)
			}
		}
	}
	if !browseSet {
		return
	}

	key := group + "/" + edgeNode
	m.invMu.Lock()
	n, ok := m.nodes[key]
	if !ok {
		m.invMu.Unlock()
		return
	}
	if n.BrowseCaches == nil {
		n.BrowseCaches = make(map[string]json.RawMessage)
	}
	if json.Valid([]byte(browse)) {
		n.BrowseCaches[device] = json.RawMessage(browse)
	} else {
		// Cache is opaque to the host but downstream consumers expect JSON.
		// Wrap as a JSON string so the API response stays well-formed.
		if mb, err := json.Marshal(browse); err == nil {
			n.BrowseCaches[device] = mb
		}
	}
	m.invMu.Unlock()

	if b != nil {
		evt := sparkplug.HostBrowseCacheUpdated{
			GroupID:    group,
			NodeID:     edgeNode,
			DeviceID:   device,
			CachedAtMs: browseTs,
		}
		if data, err := json.Marshal(evt); err == nil {
			if err := b.Publish(sparkplug.SubjectHostBrowseCacheUpdated, data); err != nil {
				m.log.Debug("sparkplug-host: browse cache updated publish failed", "error", err)
			}
		}
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

func isMetaMetric(name string) bool { return strings.HasPrefix(name, sparkplug.MetaPrefix) }

func (m *Module) publishMetric(b bus.Bus, deviceKey string, metric *sparkplug.Metric, msgType string) {
	datatype := datatypeName(metric.Datatype)
	// _meta/* metrics are interop signals (browse cache, driver health,
	// topology, …), not telemetry. Skip the historian — keeping snapshots
	// of these would balloon storage and isn't useful (the latest one is
	// always the answer).
	historyEnabled := !isMetaMetric(metric.Name)
	msg := types.PlcDataMessage{
		ModuleID:       m.moduleID,
		DeviceID:       deviceKey,
		VariableID:     metric.Name,
		Value:          metric.Value,
		Timestamp:      int64(metric.Timestamp),
		Datatype:       datatype,
		HistoryEnabled: historyEnabled,
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
			rpcInflight:  make(map[string]chan sparkplug.RPCResponse),
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
		if len(n.BrowseCaches) > 0 {
			cp.BrowseCaches = make(map[string]json.RawMessage, len(n.BrowseCaches))
			for k, v := range n.BrowseCaches {
				cp.BrowseCaches[k] = v
			}
		} else {
			cp.BrowseCaches = nil
		}
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
