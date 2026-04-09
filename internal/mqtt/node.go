//go:build mqtt || all

package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
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
}

// NewSparkplugNode creates a new edge node but does not connect.
func NewSparkplugNode(cfg itypes.MqttBridgeConfig, log *slog.Logger) *SparkplugNode {
	return &SparkplugNode{
		config:        cfg,
		log:           log,
		state:         StateDisconnected,
		deviceMetrics: make(map[string][]sparkplug.Metric),
	}
}

// State returns the current node state.
func (n *SparkplugNode) State() NodeState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state
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

// Connect creates the MQTT client and connects to the broker.
func (n *SparkplugNode) Connect() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateDisconnected {
		return fmt.Errorf("node is already connected (state=%d)", n.state)
	}

	// Build NDEATH payload for the will message
	ndeathPayload := &sparkplug.Payload{
		Timestamp: uint64(time.Now().UnixMilli()),
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
		SetWill(willTopic, string(willBytes), 0, false).
		SetOnConnectHandler(n.onConnect).
		SetConnectionLostHandler(func(c pahomqtt.Client, err error) {
			n.log.Warn("mqtt: connection lost", "error", err)
			n.mu.Lock()
			n.state = StateDisconnected
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
	n.state = StateDead

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
	n.state = StateDead
	n.mu.Unlock()

	n.Birth()
}

// Birth publishes the NBIRTH message with all registered metrics.
func (n *SparkplugNode) Birth() {
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

	n.state = StateBorn
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
		// Publish NDEATH
		payload := &sparkplug.Payload{
			Timestamp: uint64(time.Now().UnixMilli()),
			Metrics: []sparkplug.Metric{
				sparkplug.NewUInt64Metric("bdSeq", n.bdSeq),
			},
		}
		data, err := sparkplug.EncodePayload(payload)
		if err == nil {
			topic := n.topic("NDEATH")
			token := n.client.Publish(topic, 0, false, data)
			token.Wait()
		}

		// Increment bdSeq (wraps at 256)
		n.bdSeq = (n.bdSeq + 1) % 256
	}

	n.client.Disconnect(250)
	n.client = nil
	n.state = StateDisconnected
	n.log.Info("mqtt: disconnected")
}

// Rebirth triggers a full rebirth sequence (NDEATH → NBIRTH + all DBIRTH).
func (n *SparkplugNode) Rebirth() {
	n.mu.Lock()
	if n.state == StateBorn {
		n.bdSeq = (n.bdSeq + 1) % 256
	}
	n.state = StateDead
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
