//go:build orchestrator || all

package orchestrator

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	otypes "github.com/joyautomation/tentacle/internal/types"
)

// startCommandListener subscribes to orchestrator.command for request/reply.
// Returns the subscription so it can be cleaned up on stop.
func startCommandListener(b bus.Bus, config *OrchestratorConfig, mod *Module) (bus.Subscription, error) {
	sub, err := b.Subscribe(topics.OrchestratorCommand, func(subject string, data []byte, reply bus.ReplyFunc) {
		var req otypes.OrchestratorCommandRequest
		if err := json.Unmarshal(data, &req); err != nil {
			slog.Warn("command: error parsing request", "error", err)
			respondError(reply, "unknown", err.Error())
			return
		}

		var resp otypes.OrchestratorCommandResponse
		switch req.Action {
		case "get-registry":
			resp = handleGetRegistry(req.RequestID)
		case "check-internet":
			resp = handleCheckInternet(req.RequestID)
		case "get-module-versions":
			resp = handleGetModuleVersions(req.RequestID, req.ModuleID, config, mod)
		case "restart-service":
			resp = handleRestartService(req.RequestID, req.ModuleID, mod)
		default:
			resp = otypes.OrchestratorCommandResponse{
				RequestID: req.RequestID,
				Success:   false,
				Error:     "Unknown action: " + req.Action,
				Timestamp: time.Now().UnixMilli(),
			}
		}

		respData, err := json.Marshal(resp)
		if err != nil {
			slog.Warn("command: failed to marshal response", "error", err)
			return
		}
		if reply != nil {
			if err := reply(respData); err != nil {
				slog.Warn("command: failed to reply", "error", err)
			}
		}
	})

	if err != nil {
		slog.Warn("command: failed to subscribe", "subject", topics.OrchestratorCommand, "error", err)
		return nil, err
	}

	slog.Info("command: listening", "subject", topics.OrchestratorCommand)
	return sub, nil
}

func respondError(reply bus.ReplyFunc, requestID, errMsg string) {
	resp := otypes.OrchestratorCommandResponse{
		RequestID: requestID,
		Success:   false,
		Error:     errMsg,
		Timestamp: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(resp)
	if reply != nil {
		reply(data)
	}
}

func handleGetRegistry(requestID string) otypes.OrchestratorCommandResponse {
	modules := make([]otypes.ModuleRegistryInfo, len(moduleRegistry))
	for i, m := range moduleRegistry {
		var configFields []otypes.ModuleConfigField
		for _, cf := range m.RequiredConfig {
			configFields = append(configFields, otypes.ModuleConfigField{
				EnvVar:      cf.EnvVar,
				Description: cf.Description,
				Default:     cf.Default,
				Required:    cf.Required,
			})
		}
		modules[i] = otypes.ModuleRegistryInfo{
			ModuleID:       m.ModuleID,
			Repo:           m.Repo,
			Description:    m.Description,
			Category:       m.Category,
			Runtime:        m.Runtime,
			RequiredConfig: configFields,
		}
	}
	return otypes.OrchestratorCommandResponse{
		RequestID: requestID,
		Success:   true,
		Modules:   modules,
		Timestamp: time.Now().UnixMilli(),
	}
}

func handleCheckInternet(requestID string) otypes.OrchestratorCommandResponse {
	online := checkInternet()
	return otypes.OrchestratorCommandResponse{
		RequestID: requestID,
		Success:   true,
		Online:    &online,
		Timestamp: time.Now().UnixMilli(),
	}
}

func handleRestartService(requestID, modID string, mod *Module) otypes.OrchestratorCommandResponse {
	if modID == "" {
		return otypes.OrchestratorCommandResponse{
			RequestID: requestID,
			Success:   false,
			Error:     "moduleId is required for restart-service",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	if mod != nil && mod.IsMonolith() {
		// Monolith mode: stop and restart the goroutine
		mod.mu.Lock()
		rm, isRunning := mod.running[modID]
		mod.mu.Unlock()

		if !isRunning {
			return otypes.OrchestratorCommandResponse{
				RequestID: requestID,
				Success:   false,
				Error:     "Module not running: " + modID,
				Timestamp: time.Now().UnixMilli(),
			}
		}

		factory, hasFactory := mod.factories[modID]
		if !hasFactory {
			return otypes.OrchestratorCommandResponse{
				RequestID: requestID,
				Success:   false,
				Error:     "No factory for module: " + modID,
				Timestamp: time.Now().UnixMilli(),
			}
		}

		// Stop existing
		slog.Info("command: restarting in-process module", "moduleId", modID)
		rm.mod.Stop()
		rm.cancel()

		// Start new instance
		newMod := factory(modID)
		ctx, cancel := context.WithCancel(context.Background())
		mod.mu.Lock()
		mod.running[modID] = &runningModule{mod: newMod, cancel: cancel}
		mod.mu.Unlock()

		go func() {
			if err := newMod.Start(ctx, mod.b); err != nil {
				slog.Error("command: restarted module failed", "moduleId", modID, "error", err)
			}
			mod.mu.Lock()
			delete(mod.running, modID)
			mod.mu.Unlock()
		}()

		return otypes.OrchestratorCommandResponse{
			RequestID: requestID,
			Success:   true,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// Bare-metal mode: systemd restart
	if !unitExists(modID) {
		return otypes.OrchestratorCommandResponse{
			RequestID: requestID,
			Success:   false,
			Error:     "Service unit not found: " + modID,
			Timestamp: time.Now().UnixMilli(),
		}
	}
	if ok := systemctlRestart(modID); !ok {
		return otypes.OrchestratorCommandResponse{
			RequestID: requestID,
			Success:   false,
			Error:     "Failed to restart " + modID,
			Timestamp: time.Now().UnixMilli(),
		}
	}
	return otypes.OrchestratorCommandResponse{
		RequestID: requestID,
		Success:   true,
		Timestamp: time.Now().UnixMilli(),
	}
}

func handleGetModuleVersions(requestID, modID string, config *OrchestratorConfig, mod *Module) otypes.OrchestratorCommandResponse {
	if modID == "" {
		return otypes.OrchestratorCommandResponse{
			RequestID: requestID,
			Success:   false,
			Error:     "moduleId is required for get-module-versions",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	entry := getRegistryEntry(modID)
	if entry == nil {
		return otypes.OrchestratorCommandResponse{
			RequestID: requestID,
			Success:   false,
			Error:     "Unknown module: " + modID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	if mod != nil && mod.IsMonolith() {
		versions := &otypes.ModuleVersionInfo{
			ModuleID:          modID,
			InstalledVersions: []string{"embedded"},
			ActiveVersion:     "embedded",
		}
		return otypes.OrchestratorCommandResponse{
			RequestID: requestID,
			Success:   true,
			Versions:  versions,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	installedVersions := listInstalledVersions(modID, config)
	activeVersion := getActiveVersion(entry, config)
	latestVersion := resolveLatestVersion(entry, config)

	versions := &otypes.ModuleVersionInfo{
		ModuleID:          modID,
		InstalledVersions: installedVersions,
		LatestVersion:     latestVersion,
		ActiveVersion:     activeVersion,
	}

	return otypes.OrchestratorCommandResponse{
		RequestID: requestID,
		Success:   true,
		Versions:  versions,
		Timestamp: time.Now().UnixMilli(),
	}
}
