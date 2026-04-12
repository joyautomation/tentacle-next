//go:build profinet || profinetcontroller || all

package profinet

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"sync"
	"time"
)

// LLDP constants.
const (
	LLDPEtherType    uint16 = 0x88CC
	LLDPMulticastMAC        = "01:80:c2:00:00:0e"

	// LLDP TLV types
	LLDPTLVEnd             uint8 = 0
	LLDPTLVChassisID       uint8 = 1
	LLDPTLVPortID          uint8 = 2
	LLDPTLVTTL             uint8 = 3
	LLDPTLVPortDesc        uint8 = 4
	LLDPTLVSysName         uint8 = 5
	LLDPTLVSysDesc         uint8 = 6
	LLDPTLVSysCapabilities uint8 = 7
	LLDPTLVMgmtAddr        uint8 = 8
	LLDPTLVOrgSpecific     uint8 = 127

	// PROFINET LLDP OUI: 00:0E:CF
	PNIOOrgOUI1 uint8 = 0x00
	PNIOOrgOUI2 uint8 = 0x0E
	PNIOOrgOUI3 uint8 = 0xCF

	// PROFINET LLDP subtypes
	PNIOSubtypePortStatus uint8 = 0x02
	PNIOSubtypeChassisMac uint8 = 0x05
)

// LLDPSender periodically sends LLDP frames with PROFINET extensions.
type LLDPSender struct {
	transport *Transport
	cfg       *ProfinetConfig
	localMAC  net.HardwareAddr
	localIP   net.IP
	log       *slog.Logger
	interval  time.Duration
	mu        sync.RWMutex
}

// NewLLDPSender creates a new LLDP sender.
func NewLLDPSender(transport *Transport, cfg *ProfinetConfig, localMAC net.HardwareAddr, localIP net.IP, log *slog.Logger) *LLDPSender {
	return &LLDPSender{
		transport: transport,
		cfg:       cfg,
		localMAC:  localMAC,
		localIP:   localIP,
		log:       log,
		interval:  5 * time.Second,
	}
}

// Run sends LLDP frames periodically. Blocks until context is cancelled.
func (s *LLDPSender) Run(ctx context.Context) error {
	s.log.Info("lldp: sender started", "interval", s.interval)

	s.sendFrame()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.sendFrame()
		}
	}
}

func (s *LLDPSender) sendFrame() {
	payload := s.buildLLDPPayload()
	dstMAC, _ := net.ParseMAC(LLDPMulticastMAC)

	// LLDP uses EtherType 0x88CC — send via the transport's raw frame method
	if err := s.transport.SendFrameWithEtherType(dstMAC, LLDPEtherType, payload); err != nil {
		s.log.Debug("lldp: send failed", "error", err)
	}
}

// SetIP updates the management IP address advertised in LLDP frames.
func (s *LLDPSender) SetIP(ip net.IP) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localIP = ip.To4()
}

func (s *LLDPSender) buildLLDPPayload() []byte {
	s.mu.RLock()
	localIP := s.localIP
	s.mu.RUnlock()

	var frame []byte

	// Chassis ID TLV (subtype 4 = MAC address)
	chassisID := append([]byte{0x04}, s.localMAC...)
	frame = append(frame, encodeLLDPTLV(LLDPTLVChassisID, chassisID)...)

	// Port ID TLV (subtype 7 = locally assigned)
	portID := append([]byte{0x07}, []byte(s.cfg.InterfaceName+".0")...)
	frame = append(frame, encodeLLDPTLV(LLDPTLVPortID, portID)...)

	// TTL TLV
	ttl := make([]byte, 2)
	binary.BigEndian.PutUint16(ttl, 20)
	frame = append(frame, encodeLLDPTLV(LLDPTLVTTL, ttl)...)

	// Port Description
	frame = append(frame, encodeLLDPTLV(LLDPTLVPortDesc, []byte(s.cfg.InterfaceName))...)

	// System Name
	frame = append(frame, encodeLLDPTLV(LLDPTLVSysName, []byte(s.cfg.StationName))...)

	// System Description
	sysDesc := "JoyAutomation Tentacle PROFINET IO Device"
	frame = append(frame, encodeLLDPTLV(LLDPTLVSysDesc, []byte(sysDesc))...)

	// System Capabilities (station only)
	sysCap := make([]byte, 4)
	binary.BigEndian.PutUint16(sysCap[0:2], 0x0080) // station capability
	binary.BigEndian.PutUint16(sysCap[2:4], 0x0080) // enabled
	frame = append(frame, encodeLLDPTLV(LLDPTLVSysCapabilities, sysCap)...)

	// Management Address (IPv4)
	if localIP != nil && !localIP.Equal(net.IPv4zero) {
		ip4 := localIP.To4()
		if ip4 != nil {
			var mgmt []byte
			mgmt = append(mgmt, 5)    // addr string length (1 subtype + 4 ip)
			mgmt = append(mgmt, 1)    // subtype: IPv4
			mgmt = append(mgmt, ip4...)
			mgmt = append(mgmt, 2)           // interface numbering: ifIndex
			mgmt = append(mgmt, 0, 0, 0, 1) // interface number
			mgmt = append(mgmt, 0)           // OID string length
			frame = append(frame, encodeLLDPTLV(LLDPTLVMgmtAddr, mgmt)...)
		}
	}

	// PROFINET LLDP: Port Status
	pnPortStatus := make([]byte, 7)
	pnPortStatus[0] = PNIOOrgOUI1
	pnPortStatus[1] = PNIOOrgOUI2
	pnPortStatus[2] = PNIOOrgOUI3
	pnPortStatus[3] = PNIOSubtypePortStatus
	binary.BigEndian.PutUint16(pnPortStatus[4:6], 0x0000) // RT Class 2/3 not supported
	pnPortStatus[6] = 0x00
	frame = append(frame, encodeLLDPTLV(LLDPTLVOrgSpecific, pnPortStatus)...)

	// PROFINET LLDP: Chassis MAC
	pnChassis := make([]byte, 10)
	pnChassis[0] = PNIOOrgOUI1
	pnChassis[1] = PNIOOrgOUI2
	pnChassis[2] = PNIOOrgOUI3
	pnChassis[3] = PNIOSubtypeChassisMac
	copy(pnChassis[4:10], s.localMAC)
	frame = append(frame, encodeLLDPTLV(LLDPTLVOrgSpecific, pnChassis)...)

	// End TLV
	frame = append(frame, 0x00, 0x00)

	return frame
}

func encodeLLDPTLV(tlvType uint8, value []byte) []byte {
	length := len(value)
	header := uint16(tlvType)<<9 | uint16(length&0x01FF)
	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[0:2], header)
	copy(buf[2:], value)
	return buf
}
