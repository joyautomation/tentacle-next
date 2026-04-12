//go:build profinet || profinetall

package profinet

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"

	"github.com/mdlayher/ethernet"
	"github.com/mdlayher/packet"
)

// Transport handles raw Ethernet frame send/receive for PROFINET protocols.
// It listens on EtherType 0x8892 (PROFINET) using AF_PACKET sockets.
type Transport struct {
	conn      *packet.Conn
	iface     *net.Interface
	localMAC  net.HardwareAddr
	log       *slog.Logger
}

// NewTransport creates a new raw Ethernet transport bound to the given interface.
// Requires CAP_NET_RAW or root privileges.
func NewTransport(ifaceName string, log *slog.Logger) (*Transport, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %q: %w", ifaceName, err)
	}

	// Listen for PROFINET frames (EtherType 0x8892)
	conn, err := packet.Listen(iface, packet.Raw, int(EtherTypePNIO), nil)
	if err != nil {
		return nil, fmt.Errorf("packet listen on %q: %w", ifaceName, err)
	}

	return &Transport{
		conn:     conn,
		iface:    iface,
		localMAC: iface.HardwareAddr,
		log:      log,
	}, nil
}

// LocalMAC returns the MAC address of the bound interface.
func (t *Transport) LocalMAC() net.HardwareAddr {
	return t.localMAC
}

// InterfaceName returns the name of the bound interface.
func (t *Transport) InterfaceName() string {
	return t.iface.Name
}

// RecvFrame reads a raw Ethernet frame and returns the parsed Ethernet header
// and the payload (everything after the Ethernet header). Blocks until a frame
// arrives or the context is cancelled.
func (t *Transport) RecvFrame(ctx context.Context) (*ethernet.Frame, []byte, net.HardwareAddr, error) {
	buf := make([]byte, 1522) // Max Ethernet frame + VLAN tag

	for {
		// Check context before blocking read
		select {
		case <-ctx.Done():
			return nil, nil, nil, ctx.Err()
		default:
		}

		n, addr, err := t.conn.ReadFrom(buf)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read: %w", err)
		}
		if n < 14 {
			continue // too short for Ethernet header
		}

		// For packet.Raw, we get the full Ethernet frame including header.
		// Parse the Ethernet header manually since mdlayher/packet gives us raw bytes.
		frame := &ethernet.Frame{}
		if err := frame.UnmarshalBinary(buf[:n]); err != nil {
			// If unmarshal fails, try to extract source MAC from raw header
			t.log.Debug("transport: frame unmarshal failed", "error", err)
			continue
		}

		var srcMAC net.HardwareAddr
		if addr != nil {
			srcMAC = addr.(*packet.Addr).HardwareAddr
		} else {
			srcMAC = frame.Source
		}

		return frame, frame.Payload, srcMAC, nil
	}
}

// SendFrame sends a raw Ethernet frame to the specified destination MAC.
func (t *Transport) SendFrame(dstMAC net.HardwareAddr, payload []byte) error {
	frame := &ethernet.Frame{
		Destination: dstMAC,
		Source:      t.localMAC,
		EtherType:   ethernet.EtherType(EtherTypePNIO),
		Payload:     payload,
	}

	data, err := frame.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}

	// Pad to minimum Ethernet frame size (64 bytes including FCS, but we don't add FCS)
	// The kernel/NIC handles FCS. Minimum payload is 46 bytes.
	if len(data) < 60 {
		padded := make([]byte, 60)
		copy(padded, data)
		data = padded
	}

	addr := &packet.Addr{HardwareAddr: dstMAC}
	_, err = t.conn.WriteTo(data, addr)
	return err
}

// SendDCPResponse sends a DCP response frame to the given destination MAC.
func (t *Transport) SendDCPResponse(dstMAC net.HardwareAddr, dcpFrame *DCPFrame) error {
	payload := MarshalDCPFrame(dcpFrame)
	return t.SendFrame(dstMAC, payload)
}

// SendFrameWithEtherType sends a raw Ethernet frame with an arbitrary EtherType.
// Used for LLDP (0x88CC) and other non-PROFINET frames via the same AF_PACKET socket.
func (t *Transport) SendFrameWithEtherType(dstMAC net.HardwareAddr, etherType uint16, payload []byte) error {
	frame := &ethernet.Frame{
		Destination: dstMAC,
		Source:      t.localMAC,
		EtherType:   ethernet.EtherType(etherType),
		Payload:     payload,
	}

	data, err := frame.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}

	if len(data) < 60 {
		padded := make([]byte, 60)
		copy(padded, data)
		data = padded
	}

	addr := &packet.Addr{HardwareAddr: dstMAC}
	_, err = t.conn.WriteTo(data, addr)
	return err
}

// Close closes the underlying packet connection.
func (t *Transport) Close() error {
	return t.conn.Close()
}

// ParseFrameID extracts the PROFINET FrameID from the beginning of a payload.
func ParseFrameID(payload []byte) (uint16, error) {
	if len(payload) < 2 {
		return 0, fmt.Errorf("payload too short for FrameID: %d bytes", len(payload))
	}
	return binary.BigEndian.Uint16(payload[0:2]), nil
}

// IsDCPFrame checks if the FrameID indicates a DCP frame.
func IsDCPFrame(frameID uint16) bool {
	return frameID >= FrameIDDCPHello && frameID <= FrameIDDCPIdentResp
}
