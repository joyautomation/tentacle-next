//go:build profinet

// profinet-test is a standalone PROFINET IO Device for testing against
// Siemens PRONETA or other IO Controllers. It runs without NATS or
// any other Tentacle infrastructure.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/profinet"
)

func main() {
	ifaceName := flag.String("interface", "eth1", "Network interface for PROFINET")
	stationName := flag.String("name", "tentacle-pn", "PROFINET station name")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := &profinet.ProfinetConfig{
		StationName:   *stationName,
		InterfaceName: *ifaceName,
		VendorID:      0x0042,
		DeviceID:      0x0001,
		DeviceName:    "TentaclePN",
		CycleTimeUs:   1000,
		Slots: []profinet.SlotConfig{
			{
				SlotNumber:    1,
				ModuleIdentNo: 0x00000100,
				Subslots: []profinet.SubslotConfig{
					{
						SubslotNumber:    1,
						SubmoduleIdentNo: 0x00000001,
						Direction:        profinet.DirectionInputOutput,
						InputSize:        4,
						OutputSize:       4,
						Tags: []profinet.TagMapping{
							{TagID: "counter", ByteOffset: 0, Datatype: profinet.TypeFloat32, Source: "local"},
							{TagID: "setpoint", ByteOffset: 0, Datatype: profinet.TypeFloat32},
						},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		log.Error("invalid config", "error", err)
		os.Exit(1)
	}

	// Generate GSDML
	gsdml, err := profinet.GenerateGSDML(cfg)
	if err != nil {
		log.Error("GSDML generation failed", "error", err)
		os.Exit(1)
	}
	gsdmlFile := "/tmp/" + profinet.GSDMLFilename(cfg)
	if err := os.WriteFile(gsdmlFile, gsdml, 0644); err != nil {
		log.Error("failed to write GSDML", "error", err)
		os.Exit(1)
	}
	log.Info("GSDML written", "path", gsdmlFile)

	// Counter for input data
	var counter atomic.Int64

	// Increment counter in background
	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			counter.Add(1)
		}
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	device := profinet.NewDevice(cfg, profinet.DeviceCallbacks{
		OnIPSet: func(ip, mask, gateway net.IP, permanent bool) error {
			log.Info("IP assignment from controller", "ip", ip, "mask", mask, "gateway", gateway, "permanent", permanent)
			return applyIP(*ifaceName, ip, mask, gateway, log)
		},
		OnNameSet: func(name string) {
			log.Info("station name set by controller", "name", name)
		},
		OnConnected: func() {
			log.Info("*** CONNECTED - cyclic exchange active ***")
		},
		OnDisconnected: func() {
			log.Info("*** DISCONNECTED ***")
		},
		GetInputData: func(sub *profinet.SubslotConfig) []byte {
			c := counter.Load()
			val := float32(math.Mod(float64(c), 1000))
			return profinet.PackInputBuffer(sub, map[string]interface{}{
				"counter": val,
			})
		},
		OnOutputData: func(sub *profinet.SubslotConfig, data []byte) {
			values := profinet.UnpackOutputBuffer(sub, data)
			if sp, ok := values["setpoint"]; ok {
				log.Info("output from controller", "setpoint", sp)
			}
		},
	}, log)

	log.Info("starting PROFINET IO Device",
		"interface", *ifaceName,
		"stationName", *stationName,
		"vendorID", fmt.Sprintf("0x%04X", cfg.VendorID),
		"deviceID", fmt.Sprintf("0x%04X", cfg.DeviceID),
		"gsdml", gsdmlFile,
	)

	if err := device.Start(ctx); err != nil && ctx.Err() == nil {
		log.Error("device stopped with error", "error", err)
		os.Exit(1)
	}

	log.Info("device stopped")
}

// applyIP configures an IPv4 address on the interface using ip commands.
func applyIP(ifaceName string, ip, mask, gateway net.IP, log *slog.Logger) error {
	if ip.Equal(net.IPv4zero) {
		// Clear IP (DHCP requested)
		cmd := exec.Command("ip", "addr", "flush", "dev", ifaceName)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ip addr flush: %s: %w", out, err)
		}
		log.Info("IP flushed", "interface", ifaceName)
		return nil
	}

	// Calculate prefix length from mask
	ones, _ := net.IPMask(mask.To4()).Size()
	cidr := fmt.Sprintf("%s/%d", ip.String(), ones)

	// Flush existing addresses first
	cmd := exec.Command("ip", "addr", "flush", "dev", ifaceName)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Warn("ip addr flush failed (continuing)", "output", string(out), "error", err)
	}

	// Add new address
	cmd = exec.Command("ip", "addr", "add", cidr, "dev", ifaceName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip addr add %s: %s: %w", cidr, out, err)
	}

	log.Info("IP applied", "interface", ifaceName, "cidr", cidr)

	// Add default gateway if provided
	if gateway != nil && !gateway.Equal(net.IPv4zero) {
		cmd = exec.Command("ip", "route", "add", "default", "via", gateway.String(), "dev", ifaceName)
		out, err = cmd.CombinedOutput()
		if err != nil {
			log.Warn("gateway route failed (may already exist)", "output", string(out), "error", err)
		}
	}

	return nil
}
