//go:build nftables || all

// Package nftables manages NAT rules via nftables, exposing configuration and
// rule application through the tentacle Bus interface.
package nftables

import (
	"context"
	"encoding/json"
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

const serviceType = "nftables"

// Module implements module.Module for the nftables NAT management service.
type Module struct {
	b        bus.Bus
	moduleID string

	mu   sync.RWMutex
	subs []bus.Subscription

	stopHeartbeat func()
	stopPublish   chan struct{}
}

// New creates a new nftables Module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "nftables"
	}
	return &Module{
		moduleID:    moduleID,
		stopPublish: make(chan struct{}),
	}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceType }

// Start initializes the nftables module: heartbeat, periodic publishing,
// command handler, state handler, and shutdown handler.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b

	// Start heartbeat.
	m.stopHeartbeat = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		cfg, _ := loadConfig()
		ruleCount := 0
		if cfg != nil {
			ruleCount = len(cfg.NatRules)
		}
		return map[string]interface{}{
			"ruleCount": ruleCount,
		}
	})

	// Set up handlers.
	m.setupCommandHandler()
	m.setupStateHandler()
	m.setupShutdownHandler()

	// Start periodic publishing of current rules.
	go m.publishLoop()

	// Block until context cancelled or signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down all subscriptions and stops background tasks.
func (m *Module) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop periodic publishing.
	select {
	case <-m.stopPublish:
		// Already closed.
	default:
		close(m.stopPublish)
	}

	// Unsubscribe all.
	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil

	if m.stopHeartbeat != nil {
		m.stopHeartbeat()
	}
	return nil
}

// publishLoop publishes the current nftables config every 10 seconds.
func (m *Module) publishLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Publish immediately on start.
	m.publishRules()

	for {
		select {
		case <-m.stopPublish:
			return
		case <-ticker.C:
			m.publishRules()
		}
	}
}

// publishRules publishes the current NftablesConfig to topics.NftablesRules.
func (m *Module) publishRules() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Warn("nftables: failed to load config for publish", "error", err)
		return
	}

	msg := itypes.NftablesStateMessage{
		ModuleID:  m.moduleID,
		Timestamp: time.Now().UnixMilli(),
	}

	// Include the raw ruleset if available.
	if raw, err := getRuleset(); err == nil {
		msg.RawRuleset = raw
	}

	// Also publish the config as JSON in the raw ruleset field if empty.
	_ = cfg // config is available for future metadata use

	data, err := json.Marshal(msg)
	if err != nil {
		slog.Warn("nftables: failed to marshal state message", "error", err)
		return
	}
	if err := m.b.Publish(topics.NftablesRules, data); err != nil {
		slog.Warn("nftables: failed to publish rules", "error", err)
	}
}

// setupCommandHandler registers a handler for nftables.command requests.
func (m *Module) setupCommandHandler() {
	sub, err := m.b.Subscribe(topics.NftablesCommand, func(subject string, data []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}

		var req itypes.NftablesCommandRequest
		if err := json.Unmarshal(data, &req); err != nil {
			resp := itypes.NftablesCommandResponse{
				Success:   false,
				Error:     "invalid request: " + err.Error(),
				Timestamp: time.Now().UnixMilli(),
			}
			replyJSON(reply, resp)
			return
		}

		switch req.Action {
		case "get-config":
			m.handleGetConfig(req, reply)
		case "apply-config":
			m.handleApplyConfig(req, reply)
		default:
			resp := itypes.NftablesCommandResponse{
				RequestID: req.RequestID,
				Success:   false,
				Error:     "unknown action: " + req.Action,
				Timestamp: time.Now().UnixMilli(),
			}
			replyJSON(reply, resp)
		}
	})
	if err != nil {
		slog.Error("nftables: failed to subscribe to command", "subject", topics.NftablesCommand, "error", err)
		return
	}
	m.mu.Lock()
	m.subs = append(m.subs, sub)
	m.mu.Unlock()
}

// handleGetConfig returns the current nftables config.
func (m *Module) handleGetConfig(req itypes.NftablesCommandRequest, reply bus.ReplyFunc) {
	cfg, err := loadConfig()
	if err != nil {
		resp := itypes.NftablesCommandResponse{
			RequestID: req.RequestID,
			Success:   false,
			Error:     "failed to load config: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		}
		replyJSON(reply, resp)
		return
	}
	resp := itypes.NftablesCommandResponse{
		RequestID: req.RequestID,
		Success:   true,
		Config:    cfg,
		Timestamp: time.Now().UnixMilli(),
	}
	replyJSON(reply, resp)
}

// handleApplyConfig saves the config, generates nft rules, applies them,
// syncs IP aliases, and enables forwarding.
func (m *Module) handleApplyConfig(req itypes.NftablesCommandRequest, reply bus.ReplyFunc) {
	cfg := &itypes.NftablesConfig{NatRules: req.NatRules}

	// Save config to disk.
	if err := saveConfig(cfg); err != nil {
		resp := itypes.NftablesCommandResponse{
			RequestID: req.RequestID,
			Success:   false,
			Error:     "failed to save config: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		}
		replyJSON(reply, resp)
		return
	}

	// Generate nft rules file.
	rulesContent := generateRules(cfg.NatRules)
	if err := writeRulesFile(rulesContent); err != nil {
		resp := itypes.NftablesCommandResponse{
			RequestID: req.RequestID,
			Success:   false,
			Error:     "failed to write rules file: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		}
		replyJSON(reply, resp)
		return
	}

	// Apply rules via nft.
	if err := applyRules(); err != nil {
		resp := itypes.NftablesCommandResponse{
			RequestID: req.RequestID,
			Success:   false,
			Error:     "failed to apply rules: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		}
		replyJSON(reply, resp)
		return
	}

	// Sync IP aliases (delegate to network module).
	if err := syncAliases(m.b, cfg.NatRules); err != nil {
		slog.Warn("nftables: alias sync had errors", "error", err)
		// Non-fatal: rules are already applied.
	}

	// Enable IPv4 forwarding.
	if err := enableForwarding(); err != nil {
		slog.Warn("nftables: failed to enable forwarding", "error", err)
		// Non-fatal: rules are already applied.
	}

	resp := itypes.NftablesCommandResponse{
		RequestID: req.RequestID,
		Success:   true,
		Config:    cfg,
		Timestamp: time.Now().UnixMilli(),
	}
	replyJSON(reply, resp)

	// Immediately publish updated rules.
	m.publishRules()
}

// setupStateHandler registers a handler that returns the current nftables
// state on request to nftables.state.
func (m *Module) setupStateHandler() {
	sub, err := m.b.Subscribe(topics.NftablesState, func(subject string, data []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}
		msg := itypes.NftablesStateMessage{
			ModuleID:  m.moduleID,
			Timestamp: time.Now().UnixMilli(),
		}
		if raw, err := getRuleset(); err == nil {
			msg.RawRuleset = raw
		}
		replyJSON(reply, msg)
	})
	if err != nil {
		slog.Error("nftables: failed to subscribe to state", "subject", topics.NftablesState, "error", err)
		return
	}
	m.mu.Lock()
	m.subs = append(m.subs, sub)
	m.mu.Unlock()
}

// setupShutdownHandler listens for a directed shutdown command.
func (m *Module) setupShutdownHandler() {
	sub, err := m.b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		slog.Info("nftables: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	if err != nil {
		slog.Error("nftables: failed to subscribe to shutdown", "error", err)
		return
	}
	m.mu.Lock()
	m.subs = append(m.subs, sub)
	m.mu.Unlock()
}

// replyJSON marshals v and sends it via the reply function.
func replyJSON(reply bus.ReplyFunc, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("nftables: failed to marshal reply", "error", err)
		return
	}
	if err := reply(data); err != nil {
		slog.Warn("nftables: failed to send reply", "error", err)
	}
}
