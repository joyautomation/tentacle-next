//go:build all

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joyautomation/tentacle/internal/api"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/ethernetip"
	"github.com/joyautomation/tentacle/internal/ethernetipserver"
	"github.com/joyautomation/tentacle/internal/gateway"
	"github.com/joyautomation/tentacle/internal/history"
	"github.com/joyautomation/tentacle/internal/modbus"
	"github.com/joyautomation/tentacle/internal/modbusserver"
	"github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/mqtt"
	"github.com/joyautomation/tentacle/internal/network"
	"github.com/joyautomation/tentacle/internal/nftables"
	"github.com/joyautomation/tentacle/internal/opcua"
	"github.com/joyautomation/tentacle/internal/orchestrator"
	"github.com/joyautomation/tentacle/internal/snmp"
	"github.com/joyautomation/tentacle/internal/topics"
)

func main() {
	mode := flag.String("mode", "", "Run a single module with NATS bus (distributed mode)")
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS server URL (for --mode)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if *mode != "" {
		runSingle(ctx, *mode, *natsURL)
	} else {
		runMonolith(ctx)
	}
}

// runMonolith starts core modules (api + orchestrator) with a channel-based Bus.
// Optional modules are managed by the orchestrator as in-process goroutines,
// started/stopped via the desired_services KV bucket.
func runMonolith(ctx context.Context) {
	slog.Info("starting tentacle monolith")

	b := bus.NewChannelBus()
	defer b.Close()

	// Create KV buckets.
	for name, cfg := range topics.BucketConfigs() {
		if err := b.KVCreate(name, cfg); err != nil {
			slog.Error("failed to create KV bucket", "bucket", name, "error", err)
			os.Exit(1)
		}
	}

	// Module factories for optional modules managed by the orchestrator.
	factories := map[string]orchestrator.ModuleFactory{
		"gateway":           func(id string) module.Module { return gateway.New(id) },
		"ethernetip":        func(id string) module.Module { return ethernetip.New(id) },
		"opcua":             func(id string) module.Module { return opcua.New(id) },
		"snmp":              func(id string) module.Module { return snmp.New(id) },
		"modbus":            func(id string) module.Module { return modbus.New(id) },
		"mqtt":              func(id string) module.Module { return mqtt.New(id) },
		"ethernetip-server": func(id string) module.Module { return ethernetipserver.New(id) },
		"modbus-server":     func(id string) module.Module { return modbusserver.New(id) },
		"history":           func(id string) module.Module { return history.New(id) },
		"network":           func(id string) module.Module { return network.New(id) },
		"nftables":          func(id string) module.Module { return nftables.New(id) },
	}

	// Core modules: always running.
	coreModules := []module.Module{
		api.New("api"),
		orchestrator.New("orchestrator", orchestrator.WithModuleFactories(factories)),
	}

	// Start core modules as goroutines.
	errCh := make(chan error, len(coreModules))
	for _, m := range coreModules {
		m := m
		go func() {
			slog.Info("starting module", "module", m.ModuleID(), "service", m.ServiceType())
			if err := m.Start(ctx, b); err != nil {
				slog.Error("module failed", "module", m.ModuleID(), "error", err)
				errCh <- err
			}
		}()
	}

	// Wait for shutdown signal or first core module failure.
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-errCh:
		slog.Error("module error triggered shutdown", "error", err)
	}

	// Stop core modules (orchestrator.Stop will also stop any running optional modules).
	for _, m := range coreModules {
		if err := m.Stop(); err != nil {
			slog.Warn("module stop error", "module", m.ModuleID(), "error", err)
		}
	}
	slog.Info("tentacle stopped")
}

// runSingle starts a single module with a NATS-backed Bus.
func runSingle(ctx context.Context, mode, natsURL string) {
	slog.Info("starting tentacle", "mode", mode)

	nb, err := bus.ConnectNATSBus(natsURL)
	if err != nil {
		slog.Error("failed to connect to NATS", "url", natsURL, "error", err)
		os.Exit(1)
	}
	defer nb.Close()

	m := moduleByName(mode)
	if m == nil {
		fmt.Fprintf(os.Stderr, "unknown module: %s\n", mode)
		os.Exit(1)
	}

	if err := m.Start(ctx, nb); err != nil {
		slog.Error("module failed", "module", mode, "error", err)
		os.Exit(1)
	}
}

func moduleByName(name string) module.Module {
	switch name {
	case "gateway":
		return gateway.New("gateway")
	case "ethernetip":
		return ethernetip.New("ethernetip")
	case "opcua":
		return opcua.New("opcua")
	case "snmp":
		return snmp.New("snmp")
	case "modbus":
		return modbus.New("modbus")
	case "mqtt":
		return mqtt.New("mqtt")
	case "ethernetipserver":
		return ethernetipserver.New("ethernetip-server")
	case "modbusserver":
		return modbusserver.New("modbus-server")
	case "orchestrator":
		return orchestrator.New("orchestrator")
	case "history":
		return history.New("history")
	case "network":
		return network.New("network")
	case "nftables":
		return nftables.New("nftables")
	case "api":
		return api.New("api")
	default:
		return nil
	}
}
