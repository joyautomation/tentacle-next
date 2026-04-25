//go:build fleet || mantle || all

// Package fleet is the Mantle fleet-management module: it subscribes to
// FrameEvent values published by sparkplug-host (via the bus) and maintains
// an inventory of edge nodes — who's online, when they last published,
// how many devices and metrics each one is announcing.
//
// Fleet does NOT open its own MQTT connection. sparkplug-host already
// parses every Sparkplug B frame; fleet consumes the resulting events.
package fleet

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/config"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/sparkplug"
	"github.com/joyautomation/tentacle/internal/topics"
)

const serviceType = "fleet"

// Node is a single tracked edge node in the fleet.
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

	mu          sync.RWMutex
	nodes       map[string]*Node // keyed by "group/node"
	groupFilter string
	stopHB      func()
	subs        []bus.Subscription

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
	m.mu.Lock()
	m.groupFilter = cfg.GroupFilter
	m.mu.Unlock()

	if schemaSub, err := config.RegisterSchema(b, serviceType, configSchema); err == nil {
		m.mu.Lock()
		m.subs = append(m.subs, schemaSub)
		m.mu.Unlock()
	}

	frameSub, err := b.Subscribe(sparkplug.SubjectHostFrame, func(_ string, data []byte, _ bus.ReplyFunc) {
		var evt sparkplug.FrameEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		m.handleFrame(evt)
	})
	if err != nil {
		m.log.Error("fleet: subscribe to host frame events failed", "error", err)
	}

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
			"source":     sparkplug.SubjectHostFrame,
			"groupFilter": cfg.GroupFilter,
			"nodes":      total,
			"online":     online,
			"nbirth":     m.stats.nbirth.Load(),
			"ndeath":     m.stats.ndeath.Load(),
			"dbirth":     m.stats.dbirth.Load(),
			"ddeath":     m.stats.ddeath.Load(),
			"dataFrames": m.stats.data.Load(),
		}
	})

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
	if frameSub != nil {
		m.subs = append(m.subs, frameSub)
	}
	m.subs = append(m.subs, nodesSub, shutdownSub)
	m.mu.Unlock()

	m.log.Info("fleet: subscribed to host frame events", "subject", sparkplug.SubjectHostFrame, "groupFilter", cfg.GroupFilter)

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
	if m.log != nil {
		m.log.Info("fleet: stopped")
	}
	return nil
}

// handleFrame updates inventory based on a FrameEvent from sparkplug-host.
func (m *Module) handleFrame(evt sparkplug.FrameEvent) {
	m.mu.RLock()
	gf := m.groupFilter
	m.mu.RUnlock()
	if gf != "" && gf != "+" && gf != evt.GroupID {
		return
	}

	now := evt.Timestamp
	if now == 0 {
		now = time.Now().UnixMilli()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := evt.GroupID + "/" + evt.EdgeNode
	n, ok := m.nodes[key]
	if !ok {
		n = &Node{
			GroupID:   evt.GroupID,
			NodeID:    evt.EdgeNode,
			FirstSeen: now,
			Devices:   make(map[string]*Device),
		}
		m.nodes[key] = n
	}
	n.LastSeen = now

	switch evt.Type {
	case "NBIRTH":
		m.stats.nbirth.Add(1)
		n.Online = true
		n.NbirthTime = now
		n.BdSeq = evt.BdSeq
		n.MetricCount = evt.MetricCount
	case "NDEATH":
		m.stats.ndeath.Add(1)
		n.Online = false
		n.NdeathTime = now
		for _, d := range n.Devices {
			d.Online = false
		}
	case "DBIRTH":
		m.stats.dbirth.Add(1)
		if evt.Device != "" {
			d := m.getOrCreateDeviceLocked(n, evt.Device)
			d.Online = true
			d.LastSeen = now
			d.MetricCount = evt.MetricCount
		}
	case "DDEATH":
		m.stats.ddeath.Add(1)
		if evt.Device != "" {
			if d, ok := n.Devices[evt.Device]; ok {
				d.Online = false
				d.LastSeen = now
			}
		}
	case "NDATA":
		m.stats.data.Add(1)
		n.Online = true
	case "DDATA":
		m.stats.data.Add(1)
		n.Online = true
		if evt.Device != "" {
			d := m.getOrCreateDeviceLocked(n, evt.Device)
			d.Online = true
			d.LastSeen = now
		}
	}
}

func (m *Module) getOrCreateDeviceLocked(n *Node, device string) *Device {
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
