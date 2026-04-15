//go:build profinetcontroller

// controller-test is a standalone PROFINET IO Controller for testing
// against our profinet-test IO Device. No NATS or Tentacle infrastructure needed.
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/profinet"
	"github.com/joyautomation/tentacle/internal/profinetcontroller"
)

func main() {
	ifaceName := flag.String("interface", "eno1", "Network interface for PROFINET")
	stationName := flag.String("name", "tentacle-pn", "Device station name to connect to")
	deviceIP := flag.String("ip", "", "Device IP (skip DCP discovery if set)")
	cycleMs := flag.Int("cycle", 32, "Cycle time in ms")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Open raw Ethernet transport
	transport, err := profinet.NewTransport(*ifaceName, log)
	if err != nil {
		log.Error("transport failed", "error", err)
		os.Exit(1)
	}
	defer transport.Close()

	localMAC := transport.LocalMAC()
	log.Info("controller started", "interface", *ifaceName, "mac", localMAC)

	// Discover device
	var devIP net.IP
	var devMAC net.HardwareAddr

	if *deviceIP != "" {
		devIP = net.ParseIP(*deviceIP)
		if devIP == nil {
			log.Error("invalid IP", "ip", *deviceIP)
			os.Exit(1)
		}
		// Still do DCP to get MAC
		log.Info("looking up device MAC via DCP", "stationName", *stationName)
	}

	dcpClient := profinetcontroller.NewDCPClient(transport, log)

	// Start frame receiver in background so DCP responses get routed
	go func() {
		for {
			_, payload, srcMAC, err := transport.RecvFrame(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			if len(payload) < 2 {
				continue
			}
			frameID := binary.BigEndian.Uint16(payload[0:2])

			// Route DCP responses to DCP client
			if frameID == profinet.FrameIDDCPIdentResp {
				dcpClient.HandleResponse(payload, srcMAC)
				continue
			}

			// Route cyclic RT frames (will be handled once cyclic starts)
			_ = frameID
		}
	}()

	log.Info("discovering device via DCP", "stationName", *stationName)
	discovered, err := dcpClient.IdentifyByName(ctx, *stationName, 3*time.Second)
	if err != nil {
		log.Error("DCP discovery failed", "error", err)
		os.Exit(1)
	}

	devIP = discovered.IP
	devMAC = discovered.MAC
	log.Info("device found",
		"stationName", discovered.StationName,
		"ip", devIP,
		"mac", devMAC,
		"vendorID", fmt.Sprintf("0x%04X", discovered.VendorID),
		"deviceID", fmt.Sprintf("0x%04X", discovered.DeviceID),
	)

	// Define the slot/subslot structure matching the device
	// (must match what profinet-test configures)
	slots := []profinetcontroller.SlotSubscription{
		{
			SlotNumber:    1,
			ModuleIdentNo: 0x00000100,
			Subslots: []profinetcontroller.SubslotSubscription{
				{
					SubslotNumber:    1,
					SubmoduleIdentNo: 0x00000001,
					InputSize:        4,
					OutputSize:       4,
					Tags: []profinetcontroller.ControllerTag{
						{TagID: "counter", ByteOffset: 0, Datatype: "float32", Direction: "input"},
						{TagID: "setpoint", ByteOffset: 0, Datatype: "float32", Direction: "output"},
					},
				},
			},
		},
	}

	// Create AR
	ar, err := profinetcontroller.NewControllerAR(
		devIP, devMAC, localMAC,
		*stationName, slots, *cycleMs, log,
	)
	if err != nil {
		log.Error("AR creation failed", "error", err)
		os.Exit(1)
	}

	log.Info("establishing AR...")
	if err := ar.Establish(ctx); err != nil {
		log.Error("AR establishment failed", "error", err)
		os.Exit(1)
	}

	log.Info("*** AR ESTABLISHED - starting cyclic exchange ***",
		"inputFrameID", fmt.Sprintf("0x%04X", ar.InputFrameID),
		"outputFrameID", fmt.Sprintf("0x%04X", ar.OutputFrameID),
	)

	// Start cyclic exchange
	cyclic := profinetcontroller.NewControllerCyclic(transport, ar, func(data []byte) {
		// Parse input: first 4 bytes = float32 counter
		if len(data) >= 4 {
			bits := binary.BigEndian.Uint32(data[0:4])
			counter := math.Float32frombits(bits)
			log.Info("input from device", "counter", counter)
		}
	}, log)

	go cyclic.Start(ctx)

	// Write output values periodically
	go func() {
		setpoint := float32(0.0)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				setpoint += 1.0
				buf := make([]byte, 4)
				binary.BigEndian.PutUint32(buf, math.Float32bits(setpoint))
				cyclic.WriteOutputTag(0, buf)
				log.Info("output to device", "setpoint", setpoint)
			}
		}
	}()

	log.Info("cyclic exchange running. Press Ctrl+C to stop.")
	<-ctx.Done()

	log.Info("shutting down...")
	cyclic.Stop()
	_ = ar.Release(context.Background())
	log.Info("done")
}
