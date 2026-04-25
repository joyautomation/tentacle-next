//go:build fleet || mantle || all

// Package fleet is the Mantle fleet-management module: it subscribes to
// Sparkplug B NBIRTH/NDEATH/NDATA (and DBIRTH/DDEATH/DDATA) events on the
// broker to maintain an inventory of edge nodes — who's online, when they
// last published, how many devices and metrics each one is announcing.
//
// Pairs with sparkplug-host (which handles data) and history (which stores
// it). Fleet is the "who's alive" view.
package fleet

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
)

const serviceType = "fleet"

// Node is a single tracked edge node in the fleet.
type Node struct {
	GroupID     string            `json:"groupId"`
	NodeID      string            `json:"nodeId"`
	Online      bool              `json:"online"`
	LastSeen    int64             `json:"lastSeen"`    // unix ms
	FirstSeen   int64             `json:"firstSeen"`   // unix ms
	BdSeq       int64             `json:"bdSeq"`
	Devices     map[string]*Device `json:"devices"`
	NbirthTime  int64             `json:"nbirthTime,omitempty"`
	NdeathTime  int64             `json:"ndeathTime,omitempty"`
	MetricCount int               `json:"metricCount"`
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

	mu     sync.RWMutex
	client paho.Client
	nodes  map[string]*Node // keyed by "group/node"
	stopHB func()
	subs   []bus.Subscription

	stats struct {
		nbirth atomic.Int64
		ndeath atomic.Int64
		dbirth atomic.Int64
		ddeath atomic.Int64
		data   atomic.Int64
	}
}

func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "fleet"
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

	for _, bucket := range []string{topics.BucketTentacleConfig, topics.BucketServiceEnabled, topics.BucketHeartbeats} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("fleet: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	cfg := loadConfig(b, m.moduleID)
	saveConfig(b, cfg)

	if schemaSub, err := config.RegisterSchema(b, serviceType, configSchema); err == nil {
		m.mu.Lock()
		m.subs = append(m.subs, schemaSub)
		m.mu.Unlock()
	}

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

	subTopic := "spBv1.0/" + cfg.GroupFilter + "/+/+/#"
	opts.OnConnect = func(c paho.Client) {
		m.log.Info("fleet: connected to broker", "broker", cfg.BrokerURL, "filter", subTopic)
		token := c.Subscribe(subTopic, 0, func(_ paho.Client, msg paho.Message) {
			m.handleMessage(msg.Topic(), msg.Payload())
		})
		if token.Wait() && token.Error() != nil {
			m.log.Error("fleet: subscribe failed", "error", token.Error())
		}
	}
	opts.OnConnectionLost = func(_ paho.Client, err error) {
		m.log.Warn("fleet: connection lost", "error", err)
	}

	client := paho.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("fleet: connect to %s: %w", cfg.BrokerURL, token.Error())
	}
	m.mu.Lock()
	m.client = client
	m.mu.Unlock()

	m.stopHB = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		m.mu.RLock()
		total := len(m.nodes)
		online := 0
		staleMs := int64(cfg.StaleSeconds) * 1000
		now := time.Now().UnixMilli()
		for _, n := range m.nodes {
			if n.Online && now-n.LastSeen < staleMs {
				online++
			}
		}
		m.mu.RUnlock()
		return map[string]interface{}{
			"broker":     cfg.BrokerURL,
			"filter":     subTopic,
			"nodes":      total,
			"online":     online,
			"nbirth":     m.stats.nbirth.Load(),
			"ndeath":     m.stats.ndeath.Load(),
			"dbirth":     m.stats.dbirth.Load(),
			"ddeath":     m.stats.ddeath.Load(),
			"dataFrames": m.stats.data.Load(),
		}
	})

	// Bus API: request fleet.nodes to get current inventory snapshot.
	nodesSub, _ := b.Subscribe(topics.FleetNodes, func(_ string, _ []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}
		data, err := json.Marshal(m.snapshot())
		if err != nil {
			return
		}
		_ = reply(data)
	})

	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(_ string, _ []byte, _ bus.ReplyFunc) {
		m.log.Info("fleet: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})

	m.mu.Lock()
	m.subs = append(m.subs, nodesSub, shutdownSub)
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
		m.log.Info("fleet: stopped")
	}
	return nil
}

// handleMessage updates the inventory based on a Sparkplug B topic/payload.
func (m *Module) handleMessage(topic string, payload []byte) {
	parts := strings.Split(topic, "/")
	if len(parts) < 4 || parts[0] != "spBv1.0" {
		return
	}
	group := parts[1]
	msgType := parts[2]
	node := parts[3]
	device := ""
	if len(parts) >= 5 {
		device = parts[4]
	}

	// Ignore host-application STATE frames.
	if msgType == "STATE" {
		return
	}

	now := time.Now().UnixMilli()
	n := m.getOrCreateNode(group, node, now)

	m.mu.Lock()
	defer m.mu.Unlock()

	n.LastSeen = now
	switch msgType {
	case "NBIRTH":
		m.stats.nbirth.Add(1)
		n.Online = true
		n.NbirthTime = now
		if pl, err := sparkplug.DecodePayload(payload); err == nil {
			n.MetricCount = len(pl.Metrics)
			for i := range pl.Metrics {
				if pl.Metrics[i].Name == "bdSeq" {
					if v, ok := pl.Metrics[i].Value.(uint64); ok {
						n.BdSeq = int64(v)
					}
				}
			}
		}
	case "NDEATH":
		m.stats.ndeath.Add(1)
		n.Online = false
		n.NdeathTime = now
		// Any devices under this node are implicitly offline.
		for _, d := range n.Devices {
			d.Online = false
		}
	case "DBIRTH":
		m.stats.dbirth.Add(1)
		if device != "" {
			d := m.getOrCreateDevice(n, device)
			d.Online = true
			d.LastSeen = now
			if pl, err := sparkplug.DecodePayload(payload); err == nil {
				d.MetricCount = len(pl.Metrics)
			}
		}
	case "DDEATH":
		m.stats.ddeath.Add(1)
		if device != "" {
			if d, ok := n.Devices[device]; ok {
				d.Online = false
				d.LastSeen = now
			}
		}
	case "NDATA", "DDATA":
		m.stats.data.Add(1)
		if device != "" {
			d := m.getOrCreateDevice(n, device)
			d.LastSeen = now
			d.Online = true
		}
	}
}

// getOrCreateNode is internal; caller must not hold m.mu yet.
func (m *Module) getOrCreateNode(group, node string, now int64) *Node {
	key := group + "/" + node
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.nodes[key]; ok {
		return n
	}
	n := &Node{
		GroupID:   group,
		NodeID:    node,
		FirstSeen: now,
		LastSeen:  now,
		Devices:   make(map[string]*Device),
	}
	m.nodes[key] = n
	return n
}

// getOrCreateDevice assumes caller holds m.mu.
func (m *Module) getOrCreateDevice(n *Node, device string) *Device {
	if d, ok := n.Devices[device]; ok {
		return d
	}
	d := &Device{DeviceID: device}
	n.Devices[device] = d
	return d
}

// snapshot returns a deep-ish copy of the current inventory safe to marshal.
func (m *Module) snapshot() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		cp := *n
		cp.Devices = make(map[string]*Device, len(n.Devices))
		for k, d := range n.Devices {
			dCopy := *d
			cp.Devices[k] = &dCopy
		}
		out = append(out, &cp)
	}
	return out
}
