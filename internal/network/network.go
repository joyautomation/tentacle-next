//go:build network || all

// Package network monitors host network interfaces via sysfs and provides
// netplan configuration management over the Bus.
package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

const serviceType = "network"

// Network implements the module.Module interface for host network monitoring.
type Network struct {
	b        bus.Bus
	moduleID string
	log      *slog.Logger

	mu   sync.Mutex
	subs []bus.Subscription

	stopHeartbeat func()
	stopPublish   chan struct{}
}

// New creates a new Network module with the given module ID.
func New(moduleID string) *Network {
	if moduleID == "" {
		moduleID = "network"
	}
	return &Network{
		moduleID:    moduleID,
		stopPublish: make(chan struct{}),
	}
}

func (n *Network) ModuleID() string    { return n.moduleID }
func (n *Network) ServiceType() string { return serviceType }

// Start initializes the network module, begins periodic publishing, and
// registers Bus handlers. It blocks until ctx is cancelled or a signal is received.
func (n *Network) Start(ctx context.Context, b bus.Bus) error {
	n.b = b
	n.log = slog.Default().With("serviceType", n.ServiceType(), "moduleID", n.ModuleID())

	// Start heartbeat.
	n.stopHeartbeat = heartbeat.Start(b, n.moduleID, serviceType, func() map[string]interface{} {
		ifaces, err := readInterfaces(n.log)
		if err != nil {
			return map[string]interface{}{"interfaceCount": 0, "error": err.Error()}
		}
		return map[string]interface{}{"interfaceCount": len(ifaces)}
	})

	// Subscribe to network.state (request/reply).
	stateSub, err := b.Subscribe(topics.NetworkState, n.handleState)
	if err != nil {
		n.log.Error("network: failed to subscribe to state", "error", err)
	} else {
		n.mu.Lock()
		n.subs = append(n.subs, stateSub)
		n.mu.Unlock()
	}

	// Subscribe to network.command (request/reply).
	cmdSub, err := b.Subscribe(topics.NetworkCommand, n.handleCommand)
	if err != nil {
		n.log.Error("network: failed to subscribe to command", "error", err)
	} else {
		n.mu.Lock()
		n.subs = append(n.subs, cmdSub)
		n.mu.Unlock()
	}

	// Subscribe to shutdown.
	shutdownSub, _ := b.Subscribe(topics.Shutdown(n.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		n.log.Info("network: received shutdown command via Bus")
		n.Stop()
		os.Exit(0)
	})
	n.mu.Lock()
	n.subs = append(n.subs, shutdownSub)
	n.mu.Unlock()

	// Start periodic interface publishing.
	go n.publishLoop()

	n.log.Info("network: module started", "moduleID", n.moduleID)

	// Block until context cancelled or signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down subscriptions and stops the periodic publisher.
func (n *Network) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Signal the publish loop to stop.
	select {
	case <-n.stopPublish:
		// Already closed.
	default:
		close(n.stopPublish)
	}

	for _, sub := range n.subs {
		_ = sub.Unsubscribe()
	}
	n.subs = nil

	if n.stopHeartbeat != nil {
		n.stopHeartbeat()
	}

	n.log.Info("network: module stopped", "moduleID", n.moduleID)
	return nil
}

// publishLoop publishes interface state every 10 seconds.
func (n *Network) publishLoop() {
	// Publish immediately on start.
	n.publishInterfaces()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.stopPublish:
			return
		case <-ticker.C:
			n.publishInterfaces()
		}
	}
}

// publishInterfaces reads all network interfaces and publishes a
// NetworkStateMessage to topics.NetworkInterfaces.
func (n *Network) publishInterfaces() {
	ifaces, err := readInterfaces(n.log)
	if err != nil {
		n.log.Warn("network: failed to read interfaces", "error", err)
		return
	}

	msg := itypes.NetworkStateMessage{
		ModuleID:   n.moduleID,
		Timestamp:  time.Now().UnixMilli(),
		Interfaces: ifaces,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		n.log.Warn("network: failed to marshal state", "error", err)
		return
	}

	if err := n.b.Publish(topics.NetworkInterfaces, data); err != nil {
		n.log.Warn("network: failed to publish interfaces", "error", err)
	}
}

// handleState is the handler for network.state request/reply messages.
func (n *Network) handleState(subject string, data []byte, reply bus.ReplyFunc) {
	if reply == nil {
		return
	}

	ifaces, err := readInterfaces(n.log)
	if err != nil {
		n.log.Warn("network: failed to read interfaces for state request", "error", err)
		resp, _ := json.Marshal(itypes.NetworkStateMessage{
			ModuleID:  n.moduleID,
			Timestamp: time.Now().UnixMilli(),
		})
		_ = reply(resp)
		return
	}

	msg := itypes.NetworkStateMessage{
		ModuleID:   n.moduleID,
		Timestamp:  time.Now().UnixMilli(),
		Interfaces: ifaces,
	}

	resp, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_ = reply(resp)
}

// handleCommand is the handler for network.command request/reply messages.
func (n *Network) handleCommand(subject string, data []byte, reply bus.ReplyFunc) {
	if reply == nil {
		return
	}

	var req itypes.NetworkCommandRequest
	if err := json.Unmarshal(data, &req); err != nil {
		resp, _ := json.Marshal(itypes.NetworkCommandResponse{
			RequestID: req.RequestID,
			Success:   false,
			Error:     "invalid request: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		})
		_ = reply(resp)
		return
	}

	var cmdResp itypes.NetworkCommandResponse
	cmdResp.RequestID = req.RequestID
	cmdResp.Timestamp = time.Now().UnixMilli()

	switch req.Action {
	case "get-config":
		configs, err := readConfig(n.log)
		if err != nil {
			cmdResp.Success = false
			cmdResp.Error = err.Error()
		} else {
			cmdResp.Success = true
			cmdResp.Config = configs
		}

	case "apply-config":
		if err := applyConfig(n.log, req.Interfaces); err != nil {
			cmdResp.Success = false
			cmdResp.Error = err.Error()
		} else {
			cmdResp.Success = true
		}

	case "add-address":
		if req.InterfaceName == "" || req.Address == "" {
			cmdResp.Success = false
			cmdResp.Error = "interfaceName and address are required"
		} else if err := addAddress(n.log, req.InterfaceName, req.Address); err != nil {
			cmdResp.Success = false
			cmdResp.Error = err.Error()
		} else {
			cmdResp.Success = true
		}

	case "remove-address":
		if req.InterfaceName == "" || req.Address == "" {
			cmdResp.Success = false
			cmdResp.Error = "interfaceName and address are required"
		} else if err := removeAddress(n.log, req.InterfaceName, req.Address); err != nil {
			cmdResp.Success = false
			cmdResp.Error = err.Error()
		} else {
			cmdResp.Success = true
		}

	default:
		cmdResp.Success = false
		cmdResp.Error = fmt.Sprintf("unknown action: %s", req.Action)
	}

	resp, err := json.Marshal(cmdResp)
	if err != nil {
		n.log.Warn("network: failed to marshal command response", "error", err)
		return
	}
	_ = reply(resp)
}
