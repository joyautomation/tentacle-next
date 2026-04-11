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
	"github.com/joyautomation/tentacle/types"
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

	// Subscribe to network.browse (request/reply) for gateway discovery.
	browseSub, err := b.Subscribe(topics.Browse("network"), n.handleBrowse)
	if err != nil {
		n.log.Error("network: failed to subscribe to browse", "error", err)
	} else {
		n.mu.Lock()
		n.subs = append(n.subs, browseSub)
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

// publishInterfaces reads all network interfaces and publishes:
//  1. A bulk NetworkStateMessage to topics.NetworkInterfaces (for the web UI).
//  2. Individual PlcDataMessage values per property per interface (for the gateway).
func (n *Network) publishInterfaces() {
	ifaces, err := readInterfaces(n.log)
	if err != nil {
		n.log.Warn("network: failed to read interfaces", "error", err)
		return
	}

	nowMs := time.Now().UnixMilli()

	// 1. Bulk publish for the web UI (unchanged).
	msg := itypes.NetworkStateMessage{
		ModuleID:   n.moduleID,
		Timestamp:  nowMs,
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

	// 2. Per-property publish for gateway consumption.
	for _, iface := range ifaces {
		n.publishInterfaceProperties(iface, nowMs)
	}
}

// publishInterfaceProperties publishes individual PlcDataMessage values for
// each property of a network interface to network.data.network.{iface}_{prop}.
// All interfaces share the single "network" device ID so the gateway treats
// them as UDT variables under one device.
func (n *Network) publishInterfaceProperties(iface itypes.NetworkInterface, nowMs int64) {
	props := map[string]struct {
		value    interface{}
		datatype string
	}{
		"operstate": {iface.Operstate, "string"},
		"carrier":   {iface.Carrier, "boolean"},
		"speed":     {iface.Speed, "number"},
		"mtu":       {iface.Mtu, "number"},
		"mac":       {iface.Mac, "string"},
	}
	if iface.Statistics != nil {
		props["rxBytes"] = struct {
			value    interface{}
			datatype string
		}{iface.Statistics.RxBytes, "number"}
		props["txBytes"] = struct {
			value    interface{}
			datatype string
		}{iface.Statistics.TxBytes, "number"}
		props["rxPackets"] = struct {
			value    interface{}
			datatype string
		}{iface.Statistics.RxPackets, "number"}
		props["txPackets"] = struct {
			value    interface{}
			datatype string
		}{iface.Statistics.TxPackets, "number"}
		props["rxErrors"] = struct {
			value    interface{}
			datatype string
		}{iface.Statistics.RxErrors, "number"}
		props["txErrors"] = struct {
			value    interface{}
			datatype string
		}{iface.Statistics.TxErrors, "number"}
		props["rxDropped"] = struct {
			value    interface{}
			datatype string
		}{iface.Statistics.RxDropped, "number"}
		props["txDropped"] = struct {
			value    interface{}
			datatype string
		}{iface.Statistics.TxDropped, "number"}
	}

	sanitizedIface := types.SanitizeForSubject(iface.Name)
	for prop, pv := range props {
		// Compound tag: {iface}_{prop} e.g. "eth0_operstate"
		tag := sanitizedIface + "_" + prop
		dataMsg := types.PlcDataMessage{
			ModuleID:   "network",
			DeviceID:   "network",
			VariableID: tag,
			Value:      pv.value,
			Timestamp:  nowMs,
			Datatype:   pv.datatype,
		}
		data, err := json.Marshal(dataMsg)
		if err != nil {
			continue
		}
		// Subject: network.data.network.{iface}_{prop}
		subject := fmt.Sprintf("network.data.network.%s", tag)
		_ = n.b.Publish(subject, data)
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

// handleBrowse responds to network.browse with discovered interfaces and their
// properties, formatted as a browse result the gateway API can transform.
func (n *Network) handleBrowse(subject string, data []byte, reply bus.ReplyFunc) {
	if reply == nil {
		return
	}

	// Parse optional browseId for async progress.
	var req struct {
		BrowseID string `json:"browseId"`
		Async    bool   `json:"async"`
	}
	_ = json.Unmarshal(data, &req)

	ifaces, err := readInterfaces(n.log)
	if err != nil {
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = reply(resp)
		return
	}

	// Build a UDT template for NetworkInterface.
	templateMembers := []map[string]interface{}{
		{"name": "operstate", "datatype": "string"},
		{"name": "carrier", "datatype": "boolean"},
		{"name": "speed", "datatype": "number"},
		{"name": "mtu", "datatype": "number"},
		{"name": "mac", "datatype": "string"},
		{"name": "rxBytes", "datatype": "number"},
		{"name": "txBytes", "datatype": "number"},
		{"name": "rxPackets", "datatype": "number"},
		{"name": "txPackets", "datatype": "number"},
		{"name": "rxErrors", "datatype": "number"},
		{"name": "txErrors", "datatype": "number"},
		{"name": "rxDropped", "datatype": "number"},
		{"name": "txDropped", "datatype": "number"},
	}

	udtTemplate := map[string]interface{}{
		"name":    "NetworkInterface",
		"version": "1.0",
		"members": templateMembers,
	}

	// Build one UDT instance per discovered interface.
	type udtInstance struct {
		Name         string            `json:"name"`
		TemplateName string            `json:"templateName"`
		MemberTags   map[string]string `json:"memberTags"`
	}
	instances := make([]udtInstance, 0, len(ifaces))
	for _, iface := range ifaces {
		memberTags := make(map[string]string)
		for _, m := range templateMembers {
			name := m["name"].(string)
			memberTags[name] = name
		}
		instances = append(instances, udtInstance{
			Name:         iface.Name,
			TemplateName: "NetworkInterface",
			MemberTags:   memberTags,
		})
	}

	// Send progress if async.
	if req.Async && req.BrowseID != "" {
		progress := map[string]interface{}{
			"phase":           "completed",
			"discoveredCount": len(ifaces),
			"totalCount":      len(ifaces),
			"message":         fmt.Sprintf("discovered %d interfaces", len(ifaces)),
		}
		progressData, _ := json.Marshal(progress)
		_ = n.b.Publish(topics.BrowseProgress("network", req.BrowseID), progressData)

		// Also publish result on the result subject.
		result := map[string]interface{}{
			"udts":      map[string]interface{}{"NetworkInterface": udtTemplate},
			"instances": instances,
		}
		resultData, _ := json.Marshal(result)
		_ = n.b.Publish(fmt.Sprintf("network.browse.result.%s", req.BrowseID), resultData)
	}

	resp := map[string]interface{}{
		"udts":      map[string]interface{}{"NetworkInterface": udtTemplate},
		"instances": instances,
		"browseId":  req.BrowseID,
	}
	respData, _ := json.Marshal(resp)
	_ = reply(respData)
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
