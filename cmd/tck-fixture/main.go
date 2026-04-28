// Command tck-fixture publishes synthetic PlcDataMessage values to NATS so the
// MQTT bridge has a device + variables to publish DBIRTH/DDATA for. Without it,
// the TCK reports DBIRTH-related assertions as NOT EXECUTED.
//
// Mirrors the message format `internal/mqtt/bridge.go` reads from
// `gateway.data.{deviceId}.{variableId}` topics.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

func main() {
	natsURL := flag.String("nats", envOr("NATS_URL", "nats://localhost:4222"), "NATS URL")
	deviceID := flag.String("device", envOr("FIXTURE_DEVICE_ID", "Device1"), "Sparkplug device id")
	moduleID := flag.String("module", envOr("FIXTURE_MODULE_ID", "gateway"), "module id (gateway publishes data on gateway.data.>)")
	interval := flag.Duration("interval", 1*time.Second, "publish interval")
	flag.Parse()

	nc, err := nats.Connect(*natsURL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		slog.Error("nats connect failed", "error", err, "url", *natsURL)
		os.Exit(1)
	}
	defer nc.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.Info("tck-fixture started", "nats", *natsURL, "device", *deviceID, "module", *moduleID)

	tick := time.NewTicker(*interval)
	defer tick.Stop()

	var counter int64
	publish(nc, *moduleID, *deviceID, counter)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			counter++
			publish(nc, *moduleID, *deviceID, counter)
		}
	}
}

// publish sends one tick's worth of data: a boolean, an int, and a float so the
// TCK observes multiple datatypes in DBIRTH.
func publish(nc *nats.Conn, moduleID, deviceID string, counter int64) {
	now := time.Now().UnixMilli()

	send := func(varID, datatype string, value interface{}) {
		msg := types.PlcDataMessage{
			ModuleID:   moduleID,
			DeviceID:   deviceID,
			VariableID: varID,
			Value:      value,
			Timestamp:  now,
			Datatype:   datatype,
		}
		data, _ := json.Marshal(msg)
		if err := nc.Publish(topics.Data(moduleID, deviceID, varID), data); err != nil {
			slog.Warn("publish failed", "var", varID, "error", err)
		}
	}

	send("counter", "number", counter)
	send("toggle", "boolean", counter%2 == 0)
	send("temperature", "number", 20.0+float64(counter%10))
	send("label", "string", "tck-fixture")

	// UDT instance — exercises Sparkplug Template definition + instance assertions.
	udtTmpl := &types.UdtTemplateDefinition{
		Name:    "Pump",
		Version: "1.0",
		Members: []types.UdtMemberDefinition{
			{Name: "rpm", Datatype: "number"},
			{Name: "running", Datatype: "boolean"},
			{Name: "model", Datatype: "string"},
		},
	}
	udtMsg := types.PlcDataMessage{
		ModuleID:    moduleID,
		DeviceID:    deviceID,
		VariableID:  "pump1",
		Datatype:    "udt",
		Timestamp:   now,
		UdtTemplate: udtTmpl,
		Value: map[string]interface{}{
			"rpm":     1200.0 + float64(counter%5),
			"running": counter%4 != 0,
			"model":   "P-100",
		},
	}
	udtData, _ := json.Marshal(udtMsg)
	if err := nc.Publish(topics.Data(moduleID, deviceID, "pump1"), udtData); err != nil {
		slog.Warn("publish udt failed", "error", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
