//go:build profinet || profinetcontroller || all

package profinet

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"time"
)

// DCPResponder handles DCP protocol requests for a PROFINET IO Device.
// It responds to Identify, Set IP/Name, and Get requests.
type DCPResponder struct {
	transport *Transport
	log       *slog.Logger

	mu          sync.RWMutex
	stationName string
	vendorName  string
	vendorID    uint16
	deviceID    uint16
	ip          net.IP
	mask        net.IP
	gateway     net.IP
	ipSet       bool

	// Callback for IP/Name changes from the controller
	onIPSet   func(ip, mask, gateway net.IP, permanent bool) error
	onNameSet func(name string)
}

// DCPResponderConfig configures a DCP responder.
type DCPResponderConfig struct {
	StationName string
	VendorName  string
	VendorID    uint16
	DeviceID    uint16
	IP          net.IP
	Mask        net.IP
	Gateway     net.IP

	OnIPSet   func(ip, mask, gateway net.IP, permanent bool) error
	OnNameSet func(name string)
}

// NewDCPResponder creates a new DCP responder.
func NewDCPResponder(transport *Transport, cfg DCPResponderConfig, log *slog.Logger) *DCPResponder {
	ip := cfg.IP
	if ip == nil {
		ip = net.IPv4zero.To4()
	}
	mask := cfg.Mask
	if mask == nil {
		mask = net.IPv4zero.To4()
	}
	gw := cfg.Gateway
	if gw == nil {
		gw = net.IPv4zero.To4()
	}

	return &DCPResponder{
		transport:   transport,
		log:         log,
		stationName: cfg.StationName,
		vendorName:  cfg.VendorName,
		vendorID:    cfg.VendorID,
		deviceID:    cfg.DeviceID,
		ip:          ip,
		mask:        mask,
		gateway:     gw,
		ipSet:       !ip.Equal(net.IPv4zero),
		onIPSet:     cfg.OnIPSet,
		onNameSet:   cfg.OnNameSet,
	}
}

// Run starts the DCP responder loop. It blocks until the context is cancelled.
// Note: When used with the central frame dispatcher in Device, use HandleFrame instead.
func (r *DCPResponder) Run(ctx context.Context) error {
	r.log.Info("dcp: responder started",
		"interface", r.transport.InterfaceName(),
		"stationName", r.stationName,
		"mac", r.transport.LocalMAC(),
	)

	for {
		frame, payload, srcMAC, err := r.transport.RecvFrame(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.log.Debug("dcp: recv error", "error", err)
			continue
		}
		_ = frame // we use srcMAC from addr

		frameID, err := ParseFrameID(payload)
		if err != nil {
			continue
		}

		if !IsDCPFrame(frameID) {
			continue // not a DCP frame, skip (could be RT cyclic data etc.)
		}

		dcpFrame, err := ParseDCPFrame(payload)
		if err != nil {
			r.log.Debug("dcp: parse error", "error", err)
			continue
		}

		r.handleDCPFrame(ctx, dcpFrame, srcMAC)
	}
}

// HandleFrame processes a single incoming DCP frame. Called by the device's
// central frame dispatcher instead of the standalone Run loop.
func (r *DCPResponder) HandleFrame(payload []byte, srcMAC net.HardwareAddr) {
	dcpFrame, err := ParseDCPFrame(payload)
	if err != nil {
		r.log.Debug("dcp: parse error", "error", err)
		return
	}
	r.handleDCPFrame(context.Background(), dcpFrame, srcMAC)
}

func (r *DCPResponder) handleDCPFrame(ctx context.Context, f *DCPFrame, srcMAC net.HardwareAddr) {
	switch {
	case f.IsIdentifyRequest():
		r.handleIdentify(f, srcMAC)
	case f.IsSetRequest():
		r.handleSet(f, srcMAC)
	case f.IsGetRequest():
		r.handleGet(f, srcMAC)
	}
}

func (r *DCPResponder) handleIdentify(req *DCPFrame, srcMAC net.HardwareAddr) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !req.MatchesFilter(r.stationName, r.vendorID, r.deviceID) {
		return // filter doesn't match us
	}

	// Add random delay for multicast responses (0-400ms) to avoid collisions
	if req.ResponseDelay > 0 {
		maxDelay := int(req.ResponseDelay) // in units of 10ms
		if maxDelay > 0 {
			delay := time.Duration(rand.Intn(maxDelay*10)) * time.Millisecond
			time.Sleep(delay)
		}
	}

	r.log.Debug("dcp: responding to Identify", "from", srcMAC, "xid", req.Xid)

	var ipBlockInfo uint16
	if r.ipSet {
		ipBlockInfo = DCPBlockInfoIPSet
	} else {
		ipBlockInfo = DCPBlockInfoIPNotSet
	}

	// Build response blocks
	nameBlock := dcpBlockNameOfStation(r.stationName)
	vendorBlock := dcpBlockVendor(r.vendorName)
	devIDBlock := dcpBlockDeviceID(r.vendorID, r.deviceID)
	roleBlock := dcpBlockDeviceRole(DeviceRoleIODevice)
	instanceBlock := dcpBlockDeviceInstance(0x00, 0x01)
	ipBlock := dcpBlockIPSuite(r.ip, r.mask, r.gateway, ipBlockInfo)
	dhcpBlock := dcpBlockDHCPClientID(r.transport.LocalMAC())

	blocks := []DCPBlock{
		nameBlock,
		vendorBlock,
		devIDBlock,
		roleBlock,
		instanceBlock,
		ipBlock,
		dhcpBlock,
		dcpBlockDeviceOptions([]DCPBlock{nameBlock, vendorBlock, devIDBlock, roleBlock, instanceBlock, ipBlock, dhcpBlock}),
	}

	resp := &DCPFrame{
		FrameID:     FrameIDDCPIdentResp,
		ServiceID:   DCPServiceIdentify,
		ServiceType: DCPServiceTypeResponse,
		Xid:         req.Xid,
		Blocks:      blocks,
	}

	if err := r.transport.SendDCPResponse(srcMAC, resp); err != nil {
		r.log.Warn("dcp: failed to send Identify response", "error", err)
	}
}

func (r *DCPResponder) handleSet(req *DCPFrame, srcMAC net.HardwareAddr) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.log.Debug("dcp: received Set request", "from", srcMAC, "xid", req.Xid, "blocks", len(req.Blocks))

	var respBlocks []DCPBlock
	// Collect async callbacks to fire AFTER sending the DCP response.
	// DCP has strict timing — the response must be sent immediately.
	type deferredIPSet struct {
		ip, mask, gw net.IP
		permanent    bool
	}
	var deferred *deferredIPSet

	for _, block := range req.Blocks {
		r.log.Debug("dcp: processing Set block", "option", fmt.Sprintf("0x%02X", block.Option),
			"subOption", fmt.Sprintf("0x%02X", block.SubOption), "dataLen", len(block.Data))

		switch {
		case block.Option == DCPOptionIP && block.SubOption == DCPSubOptionIPSuite:
			// SET block data: BlockQualifier(2) + IP(4) + Mask(4) + Gateway(4)
			if len(block.Data) < 14 {
				r.log.Warn("dcp: IP suite SET block too short", "len", len(block.Data))
				respBlocks = append(respBlocks, dcpBlockControlResponse(block.Option, block.SubOption, 0x03))
				continue
			}
			// BlockQualifier bit 0: 1=permanent, 0=temporary
			permanent := block.Data[1]&0x01 != 0
			ip, mask, gw, err := ParseIPSuiteBlock(block.Data[2:])
			if err != nil {
				r.log.Warn("dcp: invalid IP suite in Set", "error", err)
				respBlocks = append(respBlocks, dcpBlockControlResponse(block.Option, block.SubOption, 0x03))
				continue
			}

			// Update state immediately so subsequent DCP Identify reports the new IP
			r.ip = ip
			r.mask = mask
			r.gateway = gw
			r.ipSet = !ip.Equal(net.IPv4zero)

			r.log.Info("dcp: IP set by controller", "ip", ip, "mask", mask, "gateway", gw, "permanent", permanent)
			respBlocks = append(respBlocks, dcpBlockControlResponse(block.Option, block.SubOption, 0x00))

			// Defer the actual network apply until after the response is sent
			deferred = &deferredIPSet{ip: ip, mask: mask, gw: gw, permanent: permanent}

		case block.Option == DCPOptionDeviceProperties && block.SubOption == DCPSubOptionDevNameOfStation:
			// SET block data: BlockQualifier(2) + NameOfStation
			nameData := block.Data
			if len(nameData) >= 2 {
				nameData = nameData[2:] // skip BlockQualifier
			}
			name := string(nameData)
			r.stationName = name

			r.log.Info("dcp: station name set by controller", "name", name)
			if r.onNameSet != nil {
				r.onNameSet(name)
			}

			respBlocks = append(respBlocks, dcpBlockControlResponse(block.Option, block.SubOption, 0x00))

		case block.Option == DCPOptionDHCP:
			// DHCP option from controller (e.g., PRONETA "Obtain IP from DHCP server")
			r.log.Info("dcp: DHCP requested by controller", "subOption", fmt.Sprintf("0x%02X", block.SubOption))

			r.ip = net.IPv4zero.To4()
			r.mask = net.IPv4zero.To4()
			r.gateway = net.IPv4zero.To4()
			r.ipSet = false
			respBlocks = append(respBlocks, dcpBlockControlResponse(block.Option, block.SubOption, 0x00))

			// Defer DHCP activation until after the response is sent
			deferred = &deferredIPSet{
				ip: net.IPv4zero.To4(), mask: net.IPv4zero.To4(), gw: net.IPv4zero.To4(), permanent: true,
			}

		default:
			// Unsupported option — respond with error
			r.log.Warn("dcp: unsupported Set option", "option", fmt.Sprintf("0x%02X", block.Option),
				"subOption", fmt.Sprintf("0x%02X", block.SubOption))
			respBlocks = append(respBlocks, dcpBlockControlResponse(block.Option, block.SubOption, 0x02))
		}
	}

	// Send response immediately — DCP timing is critical
	resp := &DCPFrame{
		FrameID:     FrameIDDCPGetSet,
		ServiceID:   DCPServiceSet,
		ServiceType: DCPServiceTypeResponse,
		Xid:         req.Xid,
		Blocks:      respBlocks,
	}

	if err := r.transport.SendDCPResponse(srcMAC, resp); err != nil {
		r.log.Warn("dcp: failed to send Set response", "error", err)
	}

	// Fire the network apply callback asynchronously
	if deferred != nil && r.onIPSet != nil {
		d := deferred
		go func() {
			if err := r.onIPSet(d.ip, d.mask, d.gw, d.permanent); err != nil {
				r.log.Warn("dcp: async IP apply failed", "error", err)
			}
		}()
	}
}

func (r *DCPResponder) handleGet(req *DCPFrame, srcMAC net.HardwareAddr) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	r.log.Debug("dcp: received Get request", "from", srcMAC, "xid", req.Xid)

	var respBlocks []DCPBlock

	for _, block := range req.Blocks {
		switch {
		case block.Option == DCPOptionDeviceProperties && block.SubOption == DCPSubOptionDevNameOfStation:
			respBlocks = append(respBlocks, dcpBlockNameOfStation(r.stationName))

		case block.Option == DCPOptionDeviceProperties && block.SubOption == DCPSubOptionDevVendor:
			respBlocks = append(respBlocks, dcpBlockVendor(r.vendorName))

		case block.Option == DCPOptionDeviceProperties && block.SubOption == DCPSubOptionDevID:
			respBlocks = append(respBlocks, dcpBlockDeviceID(r.vendorID, r.deviceID))

		case block.Option == DCPOptionDeviceProperties && block.SubOption == DCPSubOptionDevRole:
			respBlocks = append(respBlocks, dcpBlockDeviceRole(DeviceRoleIODevice))

		case block.Option == DCPOptionDeviceProperties && block.SubOption == DCPSubOptionDevInstance:
			respBlocks = append(respBlocks, dcpBlockDeviceInstance(0x00, 0x01))

		case block.Option == DCPOptionIP && block.SubOption == DCPSubOptionIPSuite:
			var info uint16
			if r.ipSet {
				info = DCPBlockInfoIPSet
			}
			respBlocks = append(respBlocks, dcpBlockIPSuite(r.ip, r.mask, r.gateway, info))

		case block.Option == DCPOptionAllSelector:
			// Return all supported properties
			var info uint16
			if r.ipSet {
				info = DCPBlockInfoIPSet
			}
			respBlocks = append(respBlocks,
				dcpBlockNameOfStation(r.stationName),
				dcpBlockVendor(r.vendorName),
				dcpBlockDeviceID(r.vendorID, r.deviceID),
				dcpBlockDeviceRole(DeviceRoleIODevice),
				dcpBlockDeviceInstance(0x00, 0x01),
				dcpBlockIPSuite(r.ip, r.mask, r.gateway, info),
			)
		}
	}

	resp := &DCPFrame{
		FrameID:     FrameIDDCPGetSet,
		ServiceID:   DCPServiceGet,
		ServiceType: DCPServiceTypeResponse,
		Xid:         req.Xid,
		Blocks:      respBlocks,
	}

	if err := r.transport.SendDCPResponse(srcMAC, resp); err != nil {
		r.log.Warn("dcp: failed to send Get response", "error", err)
	}
}

// SetIP updates the device's IP configuration.
func (r *DCPResponder) SetIP(ip, mask, gateway net.IP) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ip = ip.To4()
	r.mask = mask.To4()
	r.gateway = gateway.To4()
	r.ipSet = !ip.Equal(net.IPv4zero)
}

// SetStationName updates the device's station name.
func (r *DCPResponder) SetStationName(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stationName = name
}

// StationName returns the current station name.
func (r *DCPResponder) StationName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stationName
}
