//go:build mqttbroker || mantle || all

// Package mqttbroker embeds an MQTT broker (mochi-mqtt) inside a tentacle
// monolith. Intended for single-binary mantle deployments and dev/edge use;
// production fleets are expected to point sparkplug-host at an external
// EMQX/HiveMQ/Mosquitto cluster instead.
package mqttbroker

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
)

const serviceType = "mqtt-broker"

type Module struct {
	moduleID string
	log      *slog.Logger

	mu     sync.Mutex
	server *mqtt.Server
	stopHB func()
	subs   []bus.Subscription
}

func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "mqtt-broker"
	}
	return &Module{moduleID: moduleID}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return serviceType }

func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.log = slog.Default().With("serviceType", serviceType, "moduleID", m.moduleID)
	cfg := loadConfig(m.moduleID)

	server := mqtt.New(&mqtt.Options{
		InlineClient: true,
	})

	if cfg.AllowAll {
		if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
			return err
		}
	} else {
		ledger := &auth.Ledger{
			Auth: auth.AuthRules{
				{Username: auth.RString(cfg.Username), Password: auth.RString(cfg.Password), Allow: true},
			},
		}
		if err := server.AddHook(new(auth.Hook), &auth.Options{Ledger: ledger}); err != nil {
			return err
		}
	}

	tcp := listeners.NewTCP(listeners.Config{ID: cfg.ID, Address: cfg.ListenAddr})
	if err := server.AddListener(tcp); err != nil {
		return err
	}

	m.mu.Lock()
	m.server = server
	m.mu.Unlock()

	go func() {
		if err := server.Serve(); err != nil {
			m.log.Error("mqtt-broker: serve failed", "error", err)
		}
	}()
	m.log.Info("mqtt-broker: listening", "addr", cfg.ListenAddr, "allowAll", cfg.AllowAll)

	m.stopHB = heartbeat.Start(b, m.moduleID, serviceType, func() map[string]interface{} {
		return map[string]interface{}{
			"clientsConnected": atomic.LoadInt64(&server.Info.ClientsConnected),
			"messagesSent":     atomic.LoadInt64(&server.Info.MessagesSent),
			"messagesReceived": atomic.LoadInt64(&server.Info.MessagesReceived),
			"listenAddr":       cfg.ListenAddr,
		}
	})

	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("mqtt-broker: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.mu.Lock()
	m.subs = append(m.subs, shutdownSub)
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
	srv := m.server
	m.server = nil
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
	if srv != nil {
		_ = srv.Close()
	}
	if m.log != nil {
		m.log.Info("mqtt-broker: stopped")
	}
	return nil
}
