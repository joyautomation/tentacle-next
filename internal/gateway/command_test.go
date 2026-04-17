//go:build gateway || all

package gateway

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCommandRouting_UdtMemberWrite(t *testing.T) {
	b := bus.NewChannelBus()
	defer b.Close()

	g := New("gateway")
	g.b = b
	g.log = testLogger()

	g.config = &itypes.GatewayConfigKV{
		GatewayID: "gateway",
		Devices: map[string]itypes.GatewayDeviceConfig{
			"marq_plc1": {Protocol: "ethernetip", Host: "10.0.0.1"},
		},
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

	g.config = &itypes.GatewayConfigKV{
		GatewayID: "gateway",
		Devices: map[string]itypes.GatewayDeviceConfig{
			"marq_plc1": {Protocol: "ethernetip", Host: "10.0.0.1"},
		},
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

	g.config = &itypes.GatewayConfigKV{
		GatewayID: "gateway",
		Devices: map[string]itypes.GatewayDeviceConfig{
			"marq_plc1": {Protocol: "ethernetip", Host: "10.0.0.1"},
		},
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

	g.config = &itypes.GatewayConfigKV{
		GatewayID: "gateway",
		Devices: map[string]itypes.GatewayDeviceConfig{
			"marq_plc1": {Protocol: "ethernetip", Host: "10.0.0.1"},
		},
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
