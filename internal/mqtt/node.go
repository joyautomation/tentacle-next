//go:build mqtt || all

package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/joyautomation/tentacle/internal/sparkplug"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// NodeState represents the current Sparkplug B edge node state.
type NodeState int

const (
	StateDisconnected NodeState = iota
	StateDead                   // Connected but not yet born
	StateBorn                   // Connected and NBIRTH published
)

// SparkplugNode manages a Sparkplug B Edge Node MQTT connection.
type SparkplugNode struct {
	mu sync.RWMutex

	config itypes.MqttBridgeConfig
	log    *slog.Logger
	state  NodeState
	client pahomqtt.Client

	// Sparkplug B sequence numbers
	bdSeq uint64 // Birth/death sequence (incremented after each NDEATH)
	seq   uint64 // Message sequence (0-255, wraps)

	// Metrics registered for this node
	nodeMetrics   []sparkplug.Metric
	deviceMetrics map[string][]sparkplug.Metric // deviceID → metrics

	// DCMD callback
	onNodeCommand   func(metrics []sparkplug.Metric)
	onDeviceCommand func(deviceID string, metrics []sparkplug.Metric)

	// STATE topic callback (for store-forward)
	onHostState func(hostID string, online bool)

	// Fired whenever state changes. Runs asynchronously; callers should
	// re-read State() rather than relying on the argument ordering.
	onStateChange func(state NodeState)

	// Called before Birth() so the bridge can refresh device metrics and RBE.
	onBeforeBirth func()
}

// NewSparkplugNode creates a new edge node but does not connect.
//
// Sparkplug requires bdSeq to start at zero and increment by one on every new
// MQTT CONNECT packet — including across process restarts. When BdSeqFile is
// configured, the saved value is treated as "the bdSeq used by the previous
// MQTT CONNECT"; this constructor advances it by one so the next Connect()
// uses the correct next value. Without BdSeqFile, bdSeq always starts at 0
// (fine for dev; fails TCK Monitor assertions across multi-test runs).
//
// The file is written by Connect() right after a successful connection — so
// the on-disk value always reflects the most recent bdSeq actually used on
// the wire, never a speculative one.
func NewSparkplugNode(cfg itypes.MqttBridgeConfig, log *slog.Logger) *SparkplugNode {
	bdSeq := uint64(0)
	if cfg.BdSeqFile != "" {
		if data, err := os.ReadFile(cfg.BdSeqFile); err == nil {
			s := strings.TrimSpace(string(data))
			if v, err := strconv.ParseUint(s, 10, 64); err == nil {
				bdSeq = (v + 1) % 256
			}
		}
	}
	return &SparkplugNode{
		config:        cfg,
		log:           log,
		state:         StateDisconnected,
		bdSeq:         bdSeq,
		deviceMetrics: make(map[string][]sparkplug.Metric),
	}
}

// writeBdSeq atomically persists bdSeq to a file. Uses a temp+rename so a
// crash mid-write doesn't corrupt the saved value.
func writeBdSeq(path string, bdSeq uint64) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(bdSeq, 10)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// State returns the current node state.
func (n *SparkplugNode) State() NodeState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state
}

// IsBrokerReachable reports whether the underlying MQTT client currently
// has an open session to the broker (independent of Sparkplug NBIRTH).
func (n *SparkplugNode) IsBrokerReachable() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.client != nil && n.client.IsConnected()
}

// setStateLocked updates state and fires the OnStateChange callback
// asynchronously if the state actually changed. Caller must hold n.mu.
func (n *SparkplugNode) setStateLocked(s NodeState) {
	if n.state == s {
		return
	}
	n.state = s
	if cb := n.onStateChange; cb != nil {
		go cb(s)
	}
}

// OnNodeCommand sets a callback for NCMD messages.
func (n *SparkplugNode) OnNodeCommand(fn func(metrics []sparkplug.Metric)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onNodeCommand = fn
}

// OnDeviceCommand sets a callback for DCMD messages.
func (n *SparkplugNode) OnDeviceCommand(fn func(deviceID string, metrics []sparkplug.Metric)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onDeviceCommand = fn
}

// OnHostState sets a callback for STATE/{primaryHostId} messages.
func (n *SparkplugNode) OnHostState(fn func(hostID string, online bool)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onHostState = fn
}

// OnBeforeBirth sets a callback invoked before Birth() publishes NBIRTH/DBIRTH.
// The bridge uses this to refresh device metrics with current values and reset RBE.
func (n *SparkplugNode) OnBeforeBirth(fn func()) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onBeforeBirth = fn
}

// OnStateChange sets a callback fired whenever the node's state transitions.
// The callback runs in a new goroutine, so the bridge should re-read State()
// rather than trust the argument order across concurrent transitions.
func (n *SparkplugNode) OnStateChange(fn func(state NodeState)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onStateChange = fn
}

// Connect creates the MQTT client and connects to the broker.
func (n *SparkplugNode) Connect() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateDisconnected {
		return fmt.Errorf("node is already connected (state=%d)", n.state)
	}

	// Build NDEATH payload for the will message. Per Sparkplug B, NDEATH MUST
	// NOT include a sequence number and the only metric MUST be the bdSeq.
	ndeathPayload := &sparkplug.Payload{
		Timestamp: uint64(time.Now().UnixMilli()),
		OmitSeq:   true,
		Metrics: []sparkplug.Metric{
			sparkplug.NewUInt64Metric("bdSeq", n.bdSeq),
		},
	}
	willBytes, err := sparkplug.EncodePayload(ndeathPayload)
	if err != nil {
		return fmt.Errorf("encode NDEATH: %w", err)
	}

	willTopic := n.topic("NDEATH")

	opts := pahomqtt.NewClientOptions().
		AddBroker(n.config.BrokerURL).
		SetClientID(n.config.ClientID).
		SetKeepAlive(time.Duration(n.config.Keepalive) * time.Second).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetConnectTimeout(30 * time.Second).
		// Sparkplug B requires Will Message at QoS 1, not retained.
		SetWill(willTopic, string(willBytes), 1, false).
		SetOnConnectHandler(n.onConnect).
		SetConnectionLostHandler(func(c pahomqtt.Client, err error) {
			n.log.Warn("mqtt: connection lost", "error", err)
			n.mu.Lock()
			// Don't set StateDisconnected — paho's auto-reconnect is still
			// holding the client. StateDead signals "TCP down or reconnecting,
			// NBIRTH pending"; IsBrokerReachable() returns false off the
			// paho client's IsConnected(). When paho reconnects, onConnect
			// runs and stays StateDead until Birth() succeeds.
			n.setStateLocked(StateDead)
			n.mu.Unlock()
		}).
		SetReconnectingHandler(func(c pahomqtt.Client, opts *pahomqtt.ClientOptions) {
			n.log.Info("mqtt: reconnecting...")
		})

	if n.config.Username != "" {
		opts.SetUsername(n.config.Username)
		opts.SetPassword(n.config.Password)
	}

	if n.config.TLSEnabled {
		tlsCfg, err := buildTLSConfig(n.config)
		if err != nil {
			return fmt.Errorf("TLS config: %w", err)
		}
		opts.SetTLSConfig(tlsCfg)
	}

	client := pahomqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}

	n.client = client
	n.setStateLocked(StateDead)

	// Persist the bdSeq we just used on the wire. The next process to read
	// this file will use (saved + 1), giving the TCK Monitor an unbroken
	// 0,1,2,3,... sequence across restarts.
	if n.config.BdSeqFile != "" {
		if err := writeBdSeq(n.config.BdSeqFile, n.bdSeq); err != nil {
			n.log.Warn("mqtt: failed to persist bdSeq", "path", n.config.BdSeqFile, "error", err)
		}
	}

	n.log.Info("mqtt: connected to broker", "broker", n.config.BrokerURL)
	return nil
}

// onConnect is called on initial connect and on reconnect.
func (n *SparkplugNode) onConnect(c pahomqtt.Client) {
	n.log.Info("mqtt: (re)connected, subscribing to commands")

	// Subscribe to NCMD
	ncmdTopic := n.topic("NCMD")
	c.Subscribe(ncmdTopic, 0, func(c pahomqtt.Client, msg pahomqtt.Message) {
		n.handleCommand(msg, "")
	})

	// Subscribe to DCMD wildcard
	dcmdTopic := fmt.Sprintf("spBv1.0/%s/DCMD/%s/+", n.config.GroupID, n.config.EdgeNode)
	c.Subscribe(dcmdTopic, 0, func(c pahomqtt.Client, msg pahomqtt.Message) {
		deviceID := parseDeviceID(msg.Topic())
		n.handleCommand(msg, deviceID)
	})

	// Subscribe to STATE topics for store-forward
	if n.config.PrimaryHostID != "" {
		// Sparkplug B 2.0 format
		stateTopic := "STATE/" + n.config.PrimaryHostID
		c.Subscribe(stateTopic, 0, n.handleStateLegacy)

		// Sparkplug B 3.0 format
		stateTopicV3 := "spBv1.0/STATE/" + n.config.PrimaryHostID
		c.Subscribe(stateTopicV3, 0, n.handleStateV3)
	}

	// Auto-publish NBIRTH on (re)connect
	n.mu.Lock()
	n.setStateLocked(StateDead)
	n.mu.Unlock()

	n.Birth()
}

// Birth publishes the NBIRTH message with all registered metrics.
func (n *SparkplugNode) Birth() {
	// Let the bridge refresh device metrics and reset RBE before we publish.
	// This runs outside the lock since the bridge acquires its own locks.
	n.mu.RLock()
	cb := n.onBeforeBirth
	n.mu.RUnlock()
	if cb != nil {
		cb()
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state == StateDisconnected || n.client == nil {
		return
	}

	// Reset sequence
	n.seq = 0

	// Build NBIRTH payload
	metrics := []sparkplug.Metric{
		sparkplug.NewUInt64Metric("bdSeq", n.bdSeq),
		sparkplug.NewBoolMetric("Node Control/Rebirth", false),
	}
	metrics = append(metrics, n.nodeMetrics...)

	payload := &sparkplug.Payload{
		Timestamp: uint64(time.Now().UnixMilli()),
		Seq:       n.seq,
		Metrics:   metrics,
	}

	data, err := sparkplug.EncodePayload(payload)
	if err != nil {
		n.log.Error("mqtt: failed to encode NBIRTH", "error", err)
		return
	}

	topic := n.topic("NBIRTH")
	token := n.client.Publish(topic, 0, false, data)
	token.Wait()
	if err := token.Error(); err != nil {
		n.log.Error("mqtt: failed to publish NBIRTH", "error", err)
		return
	}

	n.setStateLocked(StateBorn)
	n.log.Info("mqtt: NBIRTH published", "metrics", len(metrics), "bdSeq", n.bdSeq)

	// Publish DBIRTH for each device
	for deviceID, deviceMetrics := range n.deviceMetrics {
		n.publishDeviceBirthLocked(deviceID, deviceMetrics)
	}
}

func (n *SparkplugNode) publishDeviceBirthLocked(deviceID string, metrics []sparkplug.Metric) {
	n.seq = (n.seq + 1) % 256

	payload := &sparkplug.Payload{
		Timestamp: uint64(time.Now().UnixMilli()),
		Seq:       n.seq,
		Metrics:   metrics,
	}

	data, err := sparkplug.EncodePayload(payload)
	if err != nil {
		n.log.Error("mqtt: failed to encode DBIRTH", "device", deviceID, "error", err)
		return
	}

	topic := n.deviceTopic("DBIRTH", deviceID)
	token := n.client.Publish(topic, 0, false, data)
	token.Wait()
	if err := token.Error(); err != nil {
		n.log.Error("mqtt: failed to publish DBIRTH", "device", deviceID, "error", err)
		return
	}

	n.log.Info("mqtt: DBIRTH published", "device", deviceID, "metrics", len(metrics))
}

// PublishDeviceData publishes a DDATA message for a specific device.
func (n *SparkplugNode) PublishDeviceData(deviceID string, metrics []sparkplug.Metric) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateBorn || n.client == nil {
		return fmt.Errorf("node not born (state=%d)", n.state)
	}

	n.seq = (n.seq + 1) % 256

	payload := &sparkplug.Payload{
		Timestamp: uint64(time.Now().UnixMilli()),
		Seq:       n.seq,
		Metrics:   metrics,
	}

	data, err := sparkplug.EncodePayload(payload)
	if err != nil {
		return fmt.Errorf("encode DDATA: %w", err)
	}

	topic := n.deviceTopic("DDATA", deviceID)
	token := n.client.Publish(topic, 0, false, data)
	token.Wait()
	return token.Error()
}

// PublishDeviceDataPayload publishes a pre-built payload as DDATA.
// Used by the store-forward drain to replay historical data.
func (n *SparkplugNode) PublishDeviceDataPayload(deviceID string, payload *sparkplug.Payload) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateBorn || n.client == nil {
		return fmt.Errorf("node not born (state=%d)", n.state)
	}

	// Use its own seq
	n.seq = (n.seq + 1) % 256
	payload.Seq = n.seq

	data, err := sparkplug.EncodePayload(payload)
	if err != nil {
		return err
	}

	topic := n.deviceTopic("DDATA", deviceID)
	token := n.client.Publish(topic, 0, false, data)
	token.Wait()
	return token.Error()
}

// SetNodeMetrics replaces all node-level metrics.
func (n *SparkplugNode) SetNodeMetrics(metrics []sparkplug.Metric) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nodeMetrics = metrics
}

// SetDeviceMetrics replaces all metrics for a device.
// Returns true if this is a new device that needs DBIRTH.
func (n *SparkplugNode) SetDeviceMetrics(deviceID string, metrics []sparkplug.Metric) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	_, existed := n.deviceMetrics[deviceID]
	n.deviceMetrics[deviceID] = metrics
	return !existed
}

// PublishDeviceBirth publishes DBIRTH for a device.
func (n *SparkplugNode) PublishDeviceBirth(deviceID string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateBorn || n.client == nil {
		return
	}

	metrics, ok := n.deviceMetrics[deviceID]
	if !ok {
		return
	}
	n.publishDeviceBirthLocked(deviceID, metrics)
}

// Disconnect gracefully disconnects, publishing NDEATH.
func (n *SparkplugNode) Disconnect() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.client == nil {
		return
	}

	if n.state == StateBorn {
		// Publish NDEATH at QoS 1, not retained. Per Sparkplug B, NDEATH
		// MUST NOT include seq and the only metric MUST be bdSeq.
		payload := &sparkplug.Payload{
			Timestamp: uint64(time.Now().UnixMilli()),
			OmitSeq:   true,
			Metrics: []sparkplug.Metric{
				sparkplug.NewUInt64Metric("bdSeq", n.bdSeq),
			},
		}
		data, err := sparkplug.EncodePayload(payload)
		if err == nil {
			topic := n.topic("NDEATH")
			token := n.client.Publish(topic, 1, false, data)
			token.Wait()
		}

		// Increment in-memory bdSeq in case this node instance reconnects.
		// Don't write the file here — it always reflects the bdSeq used by
		// the most recent successful Connect(). The next process starts
		// reads that value and advances by one in NewSparkplugNode.
		n.bdSeq = (n.bdSeq + 1) % 256
	}

	n.client.Disconnect(250)
	n.client = nil
	n.setStateLocked(StateDisconnected)
	n.log.Info("mqtt: disconnected")
}

// Rebirth triggers a full rebirth sequence (NDEATH → NBIRTH + all DBIRTH).
// Per Sparkplug B, Rebirth is a logical session restart that does NOT involve
// a new MQTT CONNECT — bdSeq tracks MQTT CONNECT packets, not Sparkplug
// births, so it must stay the same across a Rebirth.
func (n *SparkplugNode) Rebirth() {
	n.mu.Lock()
	n.setStateLocked(StateDead)
	n.mu.Unlock()

	n.Birth()
}

// ═══════════════════════════════════════════════════════════════════════════
// Internal: topic helpers
// ═══════════════════════════════════════════════════════════════════════════

func (n *SparkplugNode) topic(messageType string) string {
	return fmt.Sprintf("spBv1.0/%s/%s/%s", n.config.GroupID, messageType, n.config.EdgeNode)
}

func (n *SparkplugNode) deviceTopic(messageType, deviceID string) string {
	return fmt.Sprintf("spBv1.0/%s/%s/%s/%s", n.config.GroupID, messageType, n.config.EdgeNode, deviceID)
}

// ═══════════════════════════════════════════════════════════════════════════
// Internal: command handling
// ═══════════════════════════════════════════════════════════════════════════

func (n *SparkplugNode) handleCommand(msg pahomqtt.Message, deviceID string) {
	payload, err := sparkplug.DecodePayload(msg.Payload())
	if err != nil {
		n.log.Warn("mqtt: failed to decode command", "topic", msg.Topic(), "error", err)
		return
	}

	// Check for Node Control/Rebirth
	for _, m := range payload.Metrics {
		if m.Name == "Node Control/Rebirth" {
			if v, ok := m.Value.(bool); ok && v {
				n.log.Info("mqtt: rebirth requested via NCMD")
				go n.Rebirth()
				return
			}
		}
	}

	n.mu.RLock()
	ncmdCb := n.onNodeCommand
	dcmdCb := n.onDeviceCommand
	n.mu.RUnlock()

	if deviceID == "" {
		if ncmdCb != nil {
			ncmdCb(payload.Metrics)
		}
	} else {
		if dcmdCb != nil {
			dcmdCb(deviceID, payload.Metrics)
		}
	}
}

// handleStateLegacy handles Sparkplug B 2.0 STATE/{hostId} messages (string payload).
func (n *SparkplugNode) handleStateLegacy(c pahomqtt.Client, msg pahomqtt.Message) {
	online := string(msg.Payload()) == "ONLINE"
	n.mu.RLock()
	cb := n.onHostState
	n.mu.RUnlock()

	if cb != nil {
		hostID := parseHostID(msg.Topic())
		cb(hostID, online)
	}
}

// handleStateV3 handles Sparkplug B 3.0 spBv1.0/STATE/{hostId} messages (JSON payload).
func (n *SparkplugNode) handleStateV3(c pahomqtt.Client, msg pahomqtt.Message) {
	// Simple check — payload contains "online":true or "online":false
	payload := string(msg.Payload())
	online := containsOnlineTrue(payload)

	n.mu.RLock()
	cb := n.onHostState
	n.mu.RUnlock()

	if cb != nil {
		hostID := parseHostIDV3(msg.Topic())
		cb(hostID, online)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Internal: TLS
// ═══════════════════════════════════════════════════════════════════════════

func buildTLSConfig(cfg itypes.MqttBridgeConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{}

	if cfg.TLSCaPath != "" {
		caCert, err := os.ReadFile(cfg.TLSCaPath)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caCert)
		tlsCfg.RootCAs = pool
	}

	if cfg.TLSCertPath != "" && cfg.TLSKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertPath, cfg.TLSKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Internal: topic parsing helpers
// ═══════════════════════════════════════════════════════════════════════════

// parseDeviceID extracts deviceID from spBv1.0/{group}/DCMD/{edgeNode}/{deviceId}
func parseDeviceID(topic string) string {
	parts := splitTopic(topic)
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

// parseHostID extracts hostID from STATE/{hostId}
func parseHostID(topic string) string {
	parts := splitTopic(topic)
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// parseHostIDV3 extracts hostID from spBv1.0/STATE/{hostId}
func parseHostIDV3(topic string) string {
	parts := splitTopic(topic)
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

func splitTopic(topic string) []string {
	result := make([]string, 0, 6)
	start := 0
	for i := 0; i <= len(topic); i++ {
		if i == len(topic) || topic[i] == '/' {
			if i > start {
				result = append(result, topic[start:i])
			}
			start = i + 1
		}
	}
	return result
}

// containsOnlineTrue is a simple check for {"online":true} in a JSON string.
func containsOnlineTrue(s string) bool {
	n := len(s)
	for i := 0; i+13 <= n; i++ {
		if s[i:i+13] == "\"online\":true" {
			return true
		}
		if i+14 <= n && s[i:i+14] == "\"online\": true" {
			return true
		}
	}
	return false
}
