//go:build profinet || profinetcontroller || all

package profinet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// Device coordinates all PROFINET protocol layers: DCP, DCE/RPC, cyclic RT,
// alarms, and LLDP. It runs a central frame dispatcher that routes incoming
// raw Ethernet frames to the appropriate handler.
type Device struct {
	cfg       *ProfinetConfig
	transport *Transport
	dcp       *DCPResponder
	rpc       *RPCServer
	arMgr     *ARManager
	lldp      *LLDPSender
	cyclic    *CyclicHandler
	alarm     *AlarmHandler
	log       *slog.Logger

	callbacks DeviceCallbacks

	mu          sync.Mutex
	cancel      context.CancelFunc
	appliedIP   net.IP // IP applied via DCP SET (nil if none)
	appliedMask net.IP
}

// DeviceCallbacks contains callbacks for protocol events and data integration.
type DeviceCallbacks struct {
	OnIPSet        func(ip, mask, gateway net.IP) error // Called when controller assigns IP via DCP
	OnNameSet      func(name string)              // Called when controller assigns station name via DCP
	OnConnected    func()                         // Called when AR enters DATA state
	OnDisconnected func()                         // Called when AR is released
	GetInputData   func(sub *SubslotConfig) []byte
	OnOutputData   func(sub *SubslotConfig, data []byte)
}

// NewDevice creates a new PROFINET device with the given configuration.
func NewDevice(cfg *ProfinetConfig, callbacks DeviceCallbacks, log *slog.Logger) *Device {
	return &Device{
		cfg:       cfg,
		log:       log,
		callbacks: callbacks,
	}
}

// Start initializes all protocol layers and runs the central frame dispatcher.
// Blocks until the context is cancelled or an error occurs.
func (d *Device) Start(ctx context.Context) error {
	d.mu.Lock()

	// Create transport (raw Ethernet on EtherType 0x8892)
	transport, err := NewTransport(d.cfg.InterfaceName, d.log)
	if err != nil {
		d.mu.Unlock()
		return fmt.Errorf("transport: %w", err)
	}
	d.transport = transport

	// Get current IP from the interface
	ip, mask, gw := d.getInterfaceIP()

	// Create AR manager
	d.arMgr = NewARManager(d.cfg, transport.LocalMAC(), d.log)

	// Wire up cyclic start/stop callbacks on the AR manager
	d.arMgr.SetCyclicCallbacks(
		func(ar *AR) { d.onCyclicStart(ctx, ar) },
		func(ar *AR) { d.onCyclicStop(ar) },
	)

	// Create DCP responder — wrap OnIPSet to track applied IP and update LLDP
	wrappedOnIPSet := func(newIP, newMask, gateway net.IP) error {
		if d.callbacks.OnIPSet != nil {
			if err := d.callbacks.OnIPSet(newIP, newMask, gateway); err != nil {
				return err
			}
		}
		d.mu.Lock()
		d.appliedIP = newIP
		d.appliedMask = newMask
		d.mu.Unlock()
		if d.lldp != nil {
			d.lldp.SetIP(newIP)
		}
		return nil
	}
	d.dcp = NewDCPResponder(transport, DCPResponderConfig{
		StationName: d.cfg.StationName,
		VendorName:  d.cfg.DeviceName,
		VendorID:    d.cfg.VendorID,
		DeviceID:    d.cfg.DeviceID,
		IP:          ip,
		Mask:        mask,
		Gateway:     gw,
		OnIPSet:     wrappedOnIPSet,
		OnNameSet:   d.callbacks.OnNameSet,
	}, d.log)

	// Create RPC server (UDP port 34964)
	d.rpc = NewRPCServer(d.cfg, d.arMgr, transport.LocalMAC(), d.log)

	// Create LLDP sender
	d.lldp = NewLLDPSender(transport, d.cfg, transport.LocalMAC(), ip, d.log)

	ctx, d.cancel = context.WithCancel(ctx)
	d.mu.Unlock()

	d.log.Info("profinet: device started",
		"interface", d.cfg.InterfaceName,
		"mac", transport.LocalMAC(),
		"stationName", d.cfg.StationName,
		"vendorID", fmt.Sprintf("0x%04X", d.cfg.VendorID),
		"deviceID", fmt.Sprintf("0x%04X", d.cfg.DeviceID),
	)

	// Start RPC server in background
	go func() {
		if err := d.rpc.Run(ctx); err != nil && ctx.Err() == nil {
			d.log.Error("profinet: RPC server stopped with error", "error", err)
		}
	}()

	// Start LLDP sender in background
	go func() {
		if err := d.lldp.Run(ctx); err != nil && ctx.Err() == nil {
			d.log.Error("profinet: LLDP sender stopped with error", "error", err)
		}
	}()

	// Run central frame dispatcher (blocks until context cancelled)
	err = d.frameLoop(ctx)

	d.transport.Close()
	return err
}

// Stop terminates the device and all protocol layers.
func (d *Device) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cyclic != nil {
		d.cyclic.Stop()
		d.cyclic = nil
	}
	if d.rpc != nil {
		d.rpc.Stop()
	}
	// Remove controller-assigned IP from the interface
	if d.appliedIP != nil && d.cfg != nil {
		if err := RemoveInterfaceIP(d.cfg.InterfaceName, d.appliedIP, d.appliedMask); err != nil {
			d.log.Warn("profinet: failed to remove applied IP on stop", "error", err)
		} else {
			d.log.Info("profinet: removed controller-assigned IP", "ip", d.appliedIP)
		}
		d.appliedIP = nil
		d.appliedMask = nil
	}
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

// frameLoop reads all incoming Ethernet frames and dispatches to the appropriate handler.
func (d *Device) frameLoop(ctx context.Context) error {
	for {
		_, payload, srcMAC, err := d.transport.RecvFrame(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			d.log.Debug("profinet: recv error", "error", err)
			continue
		}

		frameID, err := ParseFrameID(payload)
		if err != nil {
			continue
		}

		switch {
		case IsDCPFrame(frameID):
			d.dcp.HandleFrame(payload, srcMAC)

		case IsRTCyclicFrame(frameID):
			d.mu.Lock()
			cyclic := d.cyclic
			d.mu.Unlock()
			if cyclic != nil {
				cyclic.HandleOutputFrame(payload)
			}

		case IsAlarmFrame(frameID):
			d.mu.Lock()
			alarm := d.alarm
			d.mu.Unlock()
			if alarm != nil {
				alarm.HandleFrame(payload, srcMAC)
			}
		}
	}
}

// onCyclicStart is called by the AR manager when an AR enters DATA state.
func (d *Device) onCyclicStart(ctx context.Context, ar *AR) {
	d.mu.Lock()

	// Stop any existing cyclic handler
	if d.cyclic != nil {
		d.cyclic.Stop()
	}

	cyclic := NewCyclicHandler(d.transport, ar, d.cfg, CyclicCallbacks{
		GetInputData: d.callbacks.GetInputData,
		OnOutputData: d.callbacks.OnOutputData,
	}, d.log)
	d.cyclic = cyclic

	// Create alarm handler for this AR
	d.alarm = NewAlarmHandler(d.transport, ar, d.log)

	d.mu.Unlock()

	// Start cyclic sender in background
	go cyclic.Start(ctx)

	d.log.Info("profinet: cyclic exchange started")

	if d.callbacks.OnConnected != nil {
		d.callbacks.OnConnected()
	}
}

// onCyclicStop is called by the AR manager when an AR is released.
func (d *Device) onCyclicStop(ar *AR) {
	d.mu.Lock()
	if d.cyclic != nil {
		d.cyclic.Stop()
		d.cyclic = nil
	}
	d.alarm = nil
	d.mu.Unlock()

	d.log.Info("profinet: cyclic exchange stopped")

	if d.callbacks.OnDisconnected != nil {
		d.callbacks.OnDisconnected()
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

// ARManager returns the device's AR manager (for testing/integration).
func (d *Device) ARManager() *ARManager {
	return d.arMgr
}

// Transport returns the device's transport (for testing/integration).
func (d *Device) Transport() *Transport {
	return d.transport
}
