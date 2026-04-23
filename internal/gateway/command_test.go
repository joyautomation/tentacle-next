//go:build gateway || all

package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/scanner"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedSources creates the sources bucket on b and writes the provided
// SourceConfigs, then starts a Registry and waits for it to populate.
// Returns the registry so tests can hold it.
func seedSources(t *testing.T, b bus.Bus, log *slog.Logger, sources map[string]itypes.SourceConfig) *scanner.Registry {
	t.Helper()
	if err := b.KVCreate(topics.BucketSources, bus.KVBucketConfig{History: 1}); err != nil {
		t.Fatalf("create sources bucket: %v", err)
	}
	for id, cfg := range sources {
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal source %s: %v", id, err)
		}
		if _, err := b.KVPut(topics.BucketSources, id, data); err != nil {
			t.Fatalf("put source %s: %v", id, err)
		}
	}
	reg := scanner.NewRegistry(b, log)
	if err := reg.Start(nil); err != nil {
		t.Fatalf("start registry: %v", err)
	}
	// Let the watcher flush initial entries.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if reg.Count() >= len(sources) {
			return reg
		}
		time.Sleep(5 * time.Millisecond)
	}
	return reg
}

func TestCommandRouting_UdtMemberWrite(t *testing.T) {
	b := bus.NewChannelBus()
	defer b.Close()

	g := New("gateway")
	g.b = b
	g.log = testLogger()
	g.sources = seedSources(t, b, g.log, map[string]itypes.SourceConfig{
		"marq_plc1": {Protocol: "ethernetip", Host: "10.0.0.1"},
	})
	defer g.sources.Stop()

	g.config = &itypes.GatewayConfigKV{
		GatewayID:    "gateway",
		Variables:    map[string]itypes.GatewayVariableConfig{},
		UdtTemplates: map[string]itypes.GatewayUdtTemplateConfig{},
		UdtVariables: map[string]itypes.GatewayUdtVariableConfig{
			"AustinTest": {
				ID:       "AustinTest",
				DeviceID: "marq_plc1",
				Tag:      "AustinTest",
				MemberTags: map[string]string{
					"Ack_HiHi": "AustinTest.Ack_HiHi",
					"Ack_Hi":   "AustinTest.Ack_Hi",
				},
				MemberCipTypes: map[string]string{
					"Ack_HiHi": "BOOL",
					"Ack_Hi":   "BOOL",
				},
			},
		},
	}

	g.setupCommandRoutingLocked()

	var received struct {
		mu      sync.Mutex
		subject string
		data    []byte
	}

	_, err := b.Subscribe("ethernetip.command.>", func(subj string, data []byte, reply bus.ReplyFunc) {
		received.mu.Lock()
		received.subject = subj
		received.data = make([]byte, len(data))
		copy(received.data, data)
		received.mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate DCMD for UDT member: gateway.command.AustinTest/Ack_HiHi
	err = b.Publish("gateway.command.AustinTest/Ack_HiHi", []byte("true"))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	received.mu.Lock()
	defer received.mu.Unlock()

	if received.subject != "ethernetip.command.AustinTest.Ack_HiHi" {
		t.Errorf("expected subject ethernetip.command.AustinTest.Ack_HiHi, got %q", received.subject)
	}
	if string(received.data) != "true" {
		t.Errorf("expected data 'true', got %q", string(received.data))
	}
}

func TestCommandRouting_AtomicBidirectionalWrite(t *testing.T) {
	b := bus.NewChannelBus()
	defer b.Close()

	g := New("gateway")
	g.b = b
	g.log = testLogger()
	g.sources = seedSources(t, b, g.log, map[string]itypes.SourceConfig{
		"marq_plc1": {Protocol: "ethernetip", Host: "10.0.0.1"},
	})
	defer g.sources.Stop()

	g.config = &itypes.GatewayConfigKV{
		GatewayID: "gateway",
		Variables: map[string]itypes.GatewayVariableConfig{
			"Motor_Speed": {
				ID: "Motor_Speed", DeviceID: "marq_plc1", Tag: "Motor_Speed",
				Bidirectional: true,
			},
		},
		UdtTemplates: map[string]itypes.GatewayUdtTemplateConfig{},
		UdtVariables: map[string]itypes.GatewayUdtVariableConfig{},
	}

	g.variables["Motor_Speed"] = &TrackedVariable{
		Config: g.config.Variables["Motor_Speed"],
	}

	g.setupCommandRoutingLocked()

	var received struct {
		mu      sync.Mutex
		subject string
		data    []byte
	}

	_, err := b.Subscribe("ethernetip.command.>", func(subj string, data []byte, reply bus.ReplyFunc) {
		received.mu.Lock()
		received.subject = subj
		received.data = make([]byte, len(data))
		copy(received.data, data)
		received.mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}

	err = b.Publish("gateway.command.Motor_Speed", []byte("42"))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	received.mu.Lock()
	defer received.mu.Unlock()

	if received.subject != "ethernetip.command.Motor_Speed" {
		t.Errorf("expected subject ethernetip.command.Motor_Speed, got %q", received.subject)
	}
	if string(received.data) != "42" {
		t.Errorf("expected data '42', got %q", string(received.data))
	}
}

func TestCommandRouting_NonBidirectionalIgnored(t *testing.T) {
	b := bus.NewChannelBus()
	defer b.Close()

	g := New("gateway")
	g.b = b
	g.log = testLogger()
	g.sources = seedSources(t, b, g.log, map[string]itypes.SourceConfig{
		"marq_plc1": {Protocol: "ethernetip", Host: "10.0.0.1"},
	})
	defer g.sources.Stop()

	g.config = &itypes.GatewayConfigKV{
		GatewayID: "gateway",
		Variables: map[string]itypes.GatewayVariableConfig{
			"ReadOnlyTag": {
				ID: "ReadOnlyTag", DeviceID: "marq_plc1", Tag: "ReadOnlyTag",
				Bidirectional: false,
			},
		},
		UdtTemplates: map[string]itypes.GatewayUdtTemplateConfig{},
		UdtVariables: map[string]itypes.GatewayUdtVariableConfig{},
	}

	g.variables["ReadOnlyTag"] = &TrackedVariable{
		Config: g.config.Variables["ReadOnlyTag"],
	}

	g.setupCommandRoutingLocked()

	gotMessage := false
	_, _ = b.Subscribe("ethernetip.command.>", func(subj string, data []byte, reply bus.ReplyFunc) {
		gotMessage = true
	})

	_ = b.Publish("gateway.command.ReadOnlyTag", []byte("99"))
	time.Sleep(50 * time.Millisecond)

	if gotMessage {
		t.Error("expected non-bidirectional tag to be ignored, but command was forwarded")
	}
}

func TestCommandRouting_UnknownUdtMember(t *testing.T) {
	b := bus.NewChannelBus()
	defer b.Close()

	g := New("gateway")
	g.b = b
	g.log = testLogger()
	g.sources = seedSources(t, b, g.log, map[string]itypes.SourceConfig{
		"marq_plc1": {Protocol: "ethernetip", Host: "10.0.0.1"},
	})
	defer g.sources.Stop()

	g.config = &itypes.GatewayConfigKV{
		GatewayID:    "gateway",
		Variables:    map[string]itypes.GatewayVariableConfig{},
		UdtTemplates: map[string]itypes.GatewayUdtTemplateConfig{},
		UdtVariables: map[string]itypes.GatewayUdtVariableConfig{
			"AustinTest": {
				ID:       "AustinTest",
				DeviceID: "marq_plc1",
				Tag:      "AustinTest",
				MemberTags: map[string]string{
					"Ack_HiHi": "AustinTest.Ack_HiHi",
				},
			},
		},
	}

	g.setupCommandRoutingLocked()

	gotMessage := false
	_, _ = b.Subscribe("ethernetip.command.>", func(subj string, data []byte, reply bus.ReplyFunc) {
		gotMessage = true
	})

	_ = b.Publish("gateway.command.AustinTest/NonExistent", []byte("true"))
	time.Sleep(50 * time.Millisecond)

	if gotMessage {
		t.Error("expected unknown UDT member to be ignored, but command was forwarded")
	}
}
