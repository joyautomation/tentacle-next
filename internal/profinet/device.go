//go:build profinet || profinetall

package profinet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// Device coordinates the PROFINET protocol layers (DCP, future RPC, RT).
type Device struct {
	cfg       *ProfinetConfig
	transport *Transport
	dcp       *DCPResponder
	log       *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc

	// Callbacks
	onIPSet   func(ip, mask, gateway net.IP)
	onNameSet func(name string)
}

// DeviceCallbacks contains optional callbacks for protocol events.
type DeviceCallbacks struct {
	OnIPSet   func(ip, mask, gateway net.IP) // Called when controller assigns IP via DCP
	OnNameSet func(name string)              // Called when controller assigns station name via DCP
}

// NewDevice creates a new PROFINET device with the given configuration.
func NewDevice(cfg *ProfinetConfig, callbacks DeviceCallbacks, log *slog.Logger) *Device {
	return &Device{
		cfg:       cfg,
		log:       log,
		onIPSet:   callbacks.OnIPSet,
		onNameSet: callbacks.OnNameSet,
	}
}

// Start initializes the transport and protocol layers, then runs the DCP responder.
// Blocks until the context is cancelled or an error occurs.
func (d *Device) Start(ctx context.Context) error {
	d.mu.Lock()

	transport, err := NewTransport(d.cfg.InterfaceName, d.log)
	if err != nil {
		d.mu.Unlock()
		return fmt.Errorf("transport: %w", err)
	}
	d.transport = transport

	// Get current IP from the interface (if any)
	ip, mask, gw := d.getInterfaceIP()

	d.dcp = NewDCPResponder(transport, DCPResponderConfig{
		StationName: d.cfg.StationName,
		VendorName:  d.cfg.DeviceName,
		VendorID:    d.cfg.VendorID,
		DeviceID:    d.cfg.DeviceID,
		IP:          ip,
		Mask:        mask,
		Gateway:     gw,
		OnIPSet:     d.onIPSet,
		OnNameSet:   d.onNameSet,
	}, d.log)

	ctx, d.cancel = context.WithCancel(ctx)
	d.mu.Unlock()

	d.log.Info("profinet: device started",
		"interface", d.cfg.InterfaceName,
		"mac", transport.LocalMAC(),
		"stationName", d.cfg.StationName,
		"vendorID", fmt.Sprintf("0x%04X", d.cfg.VendorID),
		"deviceID", fmt.Sprintf("0x%04X", d.cfg.DeviceID),
	)

	// Run DCP responder (blocks until context cancelled)
	// Future: also start RPC server and RT cyclic handler here as goroutines
	err = d.dcp.Run(ctx)

	d.transport.Close()
	return err
}

// Stop terminates the device and all protocol layers.
func (d *Device) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

// getInterfaceIP reads the current IP address from the network interface.
func (d *Device) getInterfaceIP() (ip, mask, gateway net.IP) {
	iface, err := net.InterfaceByName(d.cfg.InterfaceName)
	if err != nil {
		return net.IPv4zero.To4(), net.IPv4zero.To4(), net.IPv4zero.To4()
	}

	addrs, err := iface.Addrs()
	if err != nil || len(addrs) == 0 {
		return net.IPv4zero.To4(), net.IPv4zero.To4(), net.IPv4zero.To4()
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ipNet.IP.To4() == nil {
			continue // skip IPv6
		}
		return ipNet.IP.To4(), net.IP(ipNet.Mask).To4(), net.IPv4zero.To4()
	}

	return net.IPv4zero.To4(), net.IPv4zero.To4(), net.IPv4zero.To4()
}
