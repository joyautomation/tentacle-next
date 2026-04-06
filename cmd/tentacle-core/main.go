package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joyautomation/tentacle/internal/api"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/gateway"
	"github.com/joyautomation/tentacle/internal/module"
)

func main() {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	nb, err := bus.ConnectNATSBus(natsURL)
	if err != nil {
		slog.Error("nats connect failed", "error", err)
		os.Exit(1)
	}
	defer nb.Close()

	modules := []module.Module{
		gateway.New("gateway"),
		api.New("api"),
	}

	errCh := make(chan error, len(modules))
	for _, m := range modules {
		m := m
		go func() {
			if err := m.Start(ctx, nb); err != nil {
				errCh <- err
			}
		}()
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		slog.Error("module failed", "error", err)
	}

	for _, m := range modules {
		m.Stop()
	}
}
