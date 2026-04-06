//go:build network

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/network"
)

func main() {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	moduleID := os.Getenv("MODULE_ID")
	if moduleID == "" {
		moduleID = "network"
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	nb, err := bus.ConnectNATSBus(natsURL)
	if err != nil {
		slog.Error("nats connect failed", "error", err)
		os.Exit(1)
	}
	defer nb.Close()

	m := network.New(moduleID)
	slog.Info("starting", "module", moduleID)
	if err := m.Start(ctx, nb); err != nil {
		slog.Error("module failed", "error", err)
		os.Exit(1)
	}
}
