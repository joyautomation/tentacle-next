//go:build profinet || profinetcontroller || all

package profinet

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// PROFINET IO CM (Connection Management) UDP port.
const PNIOCMPort = 34964

// DCE/RPC connectionless version.
const RPCVersionCL = 4

// DCE/RPC packet types.
const (
	RPCPTypeRequest  uint8 = 0x00
	RPCPTypeResponse uint8 = 0x02
	RPCPTypeReject   uint8 = 0x06
	RPCPTypeNoCall   uint8 = 0x07
	RPCPTypeFack     uint8 = 0x09
)

// DCE/RPC flags1 bits.
const (
	RPCFlagLastFrag   uint8 = 0x01
	RPCFlagFrag       uint8 = 0x02
	RPCFlagNoFack     uint8 = 0x04
	RPCFlagMaybe      uint8 = 0x08
	RPCFlagIdempotent uint8 = 0x10
	RPCFlagBroadcast  uint8 = 0x20
)

// PNIO RPC operation numbers.
const (
	RPCOpConnect      uint16 = 0
	RPCOpRelease      uint16 = 1
	RPCOpRead         uint16 = 2
	RPCOpWrite        uint16 = 3
	RPCOpControl      uint16 = 4
	RPCOpReadImplicit uint16 = 5
)

// Well-known PROFINET interface UUIDs (canonical big-endian byte order).
var (
	// PNIO Interface UUID: DEA00001-6C97-11D1-8271-00A02442DF7D
	UUIDPNIOInterface = [16]byte{
		0xDE, 0xA0, 0x00, 0x01,
		0x6C, 0x97,
		0x11, 0xD1,
		0x82, 0x71,
		0x00, 0xA0, 0x24, 0x42, 0xDF, 0x7D,
	}

	// EPM (Endpoint Mapper) UUID: E1AF8308-5D1F-11C9-91A4-08002B14A0FA
	UUIDEndpointMapper = [16]byte{
		0xE1, 0xAF, 0x83, 0x08,
		0x5D, 0x1F,
		0x11, 0xC9,
		0x91, 0xA4,
		0x08, 0x00, 0x2B, 0x14, 0xA0, 0xFA,
	}
)

// RPCHeader is the 80-byte DCE/RPC connectionless header.
type RPCHeader struct {
	RPCVersion       uint8
	PacketType       uint8
	Flags1           uint8
	Flags2           uint8
	DataRep          [3]byte // [0]=byte order (0x10=LE, 0x00=BE), [1]=char, [2]=float
	SerialHigh       uint8
	ObjectUUID       [16]byte
	InterfaceUUID    [16]byte
	ActivityUUID     [16]byte
	ServerBootTime   uint32
	InterfaceVersion uint32
	SequenceNum      uint32
	OpNum            uint16
	InterfaceHint    uint16
	ActivityHint     uint16
	FragmentLen      uint16
	FragmentNum      uint16
	AuthProto        uint8
	SerialLow        uint8
}

// RPCHeaderSize is the fixed size of a DCE/RPC CL header.
const RPCHeaderSize = 80

// uuidFromWire converts a UUID from wire format (respecting drep byte order) to canonical BE.
func uuidFromWire(data []byte, le bool) [16]byte {
	var uuid [16]byte
	copy(uuid[:], data[:16])
	if le {
		// Byte-swap first 3 groups from LE to BE (clock_seq and node are always BE)
		uuid[0], uuid[1], uuid[2], uuid[3] = data[3], data[2], data[1], data[0]
		uuid[4], uuid[5] = data[5], data[4]
		uuid[6], uuid[7] = data[7], data[6]
	}
	return uuid
}

// uuidToWire converts a UUID from canonical BE to wire format (respecting drep byte order).
func uuidToWire(uuid [16]byte, le bool) []byte {
	buf := make([]byte, 16)
	copy(buf, uuid[:])
	if le {
		buf[0], buf[1], buf[2], buf[3] = uuid[3], uuid[2], uuid[1], uuid[0]
		buf[4], buf[5] = uuid[5], uuid[4]
		buf[6], buf[7] = uuid[7], uuid[6]
	}
	return buf
}

// ParseRPCHeader parses a DCE/RPC connectionless header from raw bytes.
// UUIDs are converted to canonical big-endian format.
func ParseRPCHeader(data []byte) (*RPCHeader, error) {
	if len(data) < RPCHeaderSize {
		return nil, fmt.Errorf("RPC header too short: %d bytes", len(data))
	}

	h := &RPCHeader{
		RPCVersion: data[0],
		PacketType: data[1],
		Flags1:     data[2],
		Flags2:     data[3],
		SerialHigh: data[7],
		AuthProto:  data[78],
		SerialLow:  data[79],
	}
	copy(h.DataRep[:], data[4:7])

	le := h.IsLittleEndian()

	// Parse UUIDs, converting from drep to canonical BE
	h.ObjectUUID = uuidFromWire(data[8:24], le)
	h.InterfaceUUID = uuidFromWire(data[24:40], le)
	h.ActivityUUID = uuidFromWire(data[40:56], le)

	if le {
		h.ServerBootTime = binary.LittleEndian.Uint32(data[56:60])
		h.InterfaceVersion = binary.LittleEndian.Uint32(data[60:64])
		h.SequenceNum = binary.LittleEndian.Uint32(data[64:68])
		h.OpNum = binary.LittleEndian.Uint16(data[68:70])
		h.InterfaceHint = binary.LittleEndian.Uint16(data[70:72])
		h.ActivityHint = binary.LittleEndian.Uint16(data[72:74])
		h.FragmentLen = binary.LittleEndian.Uint16(data[74:76])
		h.FragmentNum = binary.LittleEndian.Uint16(data[76:78])
	} else {
		h.ServerBootTime = binary.BigEndian.Uint32(data[56:60])
		h.InterfaceVersion = binary.BigEndian.Uint32(data[60:64])
		h.SequenceNum = binary.BigEndian.Uint32(data[64:68])
		h.OpNum = binary.BigEndian.Uint16(data[68:70])
		h.InterfaceHint = binary.BigEndian.Uint16(data[70:72])
		h.ActivityHint = binary.BigEndian.Uint16(data[72:74])
		h.FragmentLen = binary.BigEndian.Uint16(data[74:76])
		h.FragmentNum = binary.BigEndian.Uint16(data[76:78])
	}

	return h, nil
}

// Marshal serializes a DCE/RPC CL header using the byte order specified in DataRep.
// UUIDs are converted from canonical BE to wire format.
func (h *RPCHeader) Marshal() []byte {
	buf := make([]byte, RPCHeaderSize)
	buf[0] = h.RPCVersion
	buf[1] = h.PacketType
	buf[2] = h.Flags1
	buf[3] = h.Flags2
	copy(buf[4:7], h.DataRep[:])
	buf[7] = h.SerialHigh

	le := h.IsLittleEndian()
	copy(buf[8:24], uuidToWire(h.ObjectUUID, le))
	copy(buf[24:40], uuidToWire(h.InterfaceUUID, le))
	copy(buf[40:56], uuidToWire(h.ActivityUUID, le))

	if le {
		binary.LittleEndian.PutUint32(buf[56:60], h.ServerBootTime)
		binary.LittleEndian.PutUint32(buf[60:64], h.InterfaceVersion)
		binary.LittleEndian.PutUint32(buf[64:68], h.SequenceNum)
		binary.LittleEndian.PutUint16(buf[68:70], h.OpNum)
		binary.LittleEndian.PutUint16(buf[70:72], h.InterfaceHint)
		binary.LittleEndian.PutUint16(buf[72:74], h.ActivityHint)
		binary.LittleEndian.PutUint16(buf[74:76], h.FragmentLen)
		binary.LittleEndian.PutUint16(buf[76:78], h.FragmentNum)
	} else {
		binary.BigEndian.PutUint32(buf[56:60], h.ServerBootTime)
		binary.BigEndian.PutUint32(buf[60:64], h.InterfaceVersion)
		binary.BigEndian.PutUint32(buf[64:68], h.SequenceNum)
		binary.BigEndian.PutUint16(buf[68:70], h.OpNum)
		binary.BigEndian.PutUint16(buf[70:72], h.InterfaceHint)
		binary.BigEndian.PutUint16(buf[72:74], h.ActivityHint)
		binary.BigEndian.PutUint16(buf[74:76], h.FragmentLen)
		binary.BigEndian.PutUint16(buf[76:78], h.FragmentNum)
	}

	buf[78] = h.AuthProto
	buf[79] = h.SerialLow
	return buf
}

// IsLittleEndian returns true if the header uses LE byte order.
func (h *RPCHeader) IsLittleEndian() bool {
	return h.DataRep[0] == 0x10
}

// ParsePNIORequestBody extracts PNIO blocks from a Connect/Write/Control request body.
// NDR layout: ArgsMaximum(4) + ArgsLength(4) + MaxCount(4) + Offset(4) + ActualCount(4) + blocks
func ParsePNIORequestBody(data []byte, le bool) (argsMax uint32, blocks []PNIOBlock, err error) {
	if len(data) < 20 {
		return 0, nil, fmt.Errorf("PNIO request body too short: %d", len(data))
	}

	var get32 func([]byte) uint32
	if le {
		get32 = func(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
	} else {
		get32 = func(b []byte) uint32 { return binary.BigEndian.Uint32(b) }
	}

	argsMax = get32(data[0:4])
	// argsLen := get32(data[4:8])
	// maxCount := get32(data[8:12])
	// offset := get32(data[12:16])
	actualCount := get32(data[16:20])

	blockData := data[20:]
	if int(actualCount) < len(blockData) {
		blockData = blockData[:actualCount]
	}

	blocks, err = ParsePNIOBlocks(blockData)
	return argsMax, blocks, err
}

// MarshalPNIOResponseBody creates an NDR-encoded PNIO response body.
// Layout: PNIOStatus(4) + ArgsLength(4) + MaxCount(4) + Offset(4) + ActualCount(4) + blocks
func MarshalPNIOResponseBody(status PNIOStatus, responseBlocks []byte, le bool) []byte {
	argsLen := uint32(len(responseBlocks))
	buf := make([]byte, 20+len(responseBlocks))

	copy(buf[0:4], status.Marshal())

	var put32 func([]byte, uint32)
	if le {
		put32 = func(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
	} else {
		put32 = func(b []byte, v uint32) { binary.BigEndian.PutUint32(b, v) }
	}

	put32(buf[4:8], argsLen)
	put32(buf[8:12], argsLen)  // MaxCount
	put32(buf[12:16], 0)      // Offset
	put32(buf[16:20], argsLen) // ActualCount
	copy(buf[20:], responseBlocks)

	return buf
}

// RPCServer handles PNIO DCE/RPC requests over UDP.
type RPCServer struct {
	addr     string
	conn     *net.UDPConn
	arMgr    *ARManager
	cfg      *ProfinetConfig
	localMAC net.HardwareAddr
	log      *slog.Logger
	bootTime uint32

	mu sync.Mutex
}

// NewRPCServer creates a new PNIO RPC server.
func NewRPCServer(cfg *ProfinetConfig, arMgr *ARManager, localMAC net.HardwareAddr, log *slog.Logger) *RPCServer {
	return &RPCServer{
		addr:     fmt.Sprintf(":%d", PNIOCMPort),
		arMgr:    arMgr,
		cfg:      cfg,
		localMAC: localMAC,
		log:      log,
		bootTime: uint32(time.Now().Unix()),
	}
}

// Run starts the UDP server. Blocks until context is cancelled.
func (s *RPCServer) Run(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp4", s.addr)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	defer conn.Close()

	s.log.Info("rpc: server listening", "port", PNIOCMPort)

	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.log.Debug("rpc: read error", "error", err)
			continue
		}

		go s.handlePacket(buf[:n], remoteAddr)
	}
}

func (s *RPCServer) handlePacket(data []byte, from *net.UDPAddr) {
	header, err := ParseRPCHeader(data)
	if err != nil {
		s.log.Debug("rpc: invalid header", "error", err)
		return
	}

	if header.RPCVersion != RPCVersionCL {
		return
	}
	if header.PacketType != RPCPTypeRequest {
		return
	}

	body := data[RPCHeaderSize:]

	if header.InterfaceUUID == UUIDPNIOInterface {
		s.handlePNIORequest(header, body, from)
	} else if header.InterfaceUUID == UUIDEndpointMapper {
		s.log.Debug("rpc: EPM request (ignored)", "from", from, "opnum", header.OpNum)
	} else {
		s.log.Debug("rpc: unknown interface", "uuid", fmt.Sprintf("%x", header.InterfaceUUID))
	}
}

func (s *RPCServer) handlePNIORequest(header *RPCHeader, body []byte, from *net.UDPAddr) {
	le := header.IsLittleEndian()

	switch header.OpNum {
	case RPCOpConnect:
		s.handleConnect(header, body, from, le)
	case RPCOpRelease:
		s.handleRelease(header, body, from, le)
	case RPCOpRead:
		s.handleRead(header, body, from, le)
	case RPCOpWrite:
		s.handleWrite(header, body, from, le)
	case RPCOpControl:
		s.handleControl(header, body, from, le)
	default:
		s.log.Warn("rpc: unknown opnum", "opnum", header.OpNum)
	}
}

func (s *RPCServer) handleConnect(header *RPCHeader, body []byte, from *net.UDPAddr, le bool) {
	_, blocks, err := ParsePNIORequestBody(body, le)
	if err != nil {
		s.log.Warn("rpc: connect parse error", "error", err)
		s.sendErrorResponse(header, from, PNIOStatus{0xDE, 0x81, 0x01, 0x00})
		return
	}

	s.log.Info("rpc: Connect request", "from", from, "blocks", len(blocks))

	respBlocks, status := s.arMgr.HandleConnect(blocks, from, header.ActivityUUID)
	s.sendResponse(header, from, status, respBlocks, le)
}

func (s *RPCServer) handleRelease(header *RPCHeader, body []byte, from *net.UDPAddr, le bool) {
	_, blocks, err := ParsePNIORequestBody(body, le)
	if err != nil {
		s.sendErrorResponse(header, from, PNIOStatus{0xDE, 0x81, 0x01, 0x00})
		return
	}

	s.log.Info("rpc: Release request", "from", from)

	respBlocks, status := s.arMgr.HandleRelease(blocks, header.ObjectUUID)
	s.sendResponse(header, from, status, respBlocks, le)
}

func (s *RPCServer) handleRead(header *RPCHeader, body []byte, from *net.UDPAddr, le bool) {
	_, blocks, err := ParsePNIORequestBody(body, le)
	if err != nil {
		s.sendErrorResponse(header, from, PNIOStatus{0xDE, 0x81, 0x01, 0x00})
		return
	}

	respBlocks, status := s.arMgr.HandleRead(blocks, header.ObjectUUID)
	s.sendResponse(header, from, status, respBlocks, le)
}

func (s *RPCServer) handleWrite(header *RPCHeader, body []byte, from *net.UDPAddr, le bool) {
	_, blocks, err := ParsePNIORequestBody(body, le)
	if err != nil {
		s.sendErrorResponse(header, from, PNIOStatus{0xDE, 0x81, 0x01, 0x00})
		return
	}

	s.log.Debug("rpc: Write request", "from", from, "blocks", len(blocks))

	respBlocks, status := s.arMgr.HandleWrite(blocks, header.ObjectUUID)
	s.sendResponse(header, from, status, respBlocks, le)
}

func (s *RPCServer) handleControl(header *RPCHeader, body []byte, from *net.UDPAddr, le bool) {
	_, blocks, err := ParsePNIORequestBody(body, le)
	if err != nil {
		s.sendErrorResponse(header, from, PNIOStatus{0xDE, 0x81, 0x01, 0x00})
		return
	}

	s.log.Info("rpc: Control request", "from", from, "blocks", len(blocks))

	respBlocks, status := s.arMgr.HandleControl(blocks, header.ObjectUUID)
	s.sendResponse(header, from, status, respBlocks, le)
}

func (s *RPCServer) sendResponse(reqHeader *RPCHeader, to *net.UDPAddr, status PNIOStatus, responseBlocks []byte, le bool) {
	body := MarshalPNIOResponseBody(status, responseBlocks, le)

	resp := &RPCHeader{
		RPCVersion:       RPCVersionCL,
		PacketType:       RPCPTypeResponse,
		Flags1:           RPCFlagLastFrag | RPCFlagNoFack,
		Flags2:           0,
		DataRep:          reqHeader.DataRep,
		SerialHigh:       reqHeader.SerialHigh,
		ObjectUUID:       reqHeader.ObjectUUID,
		InterfaceUUID:    reqHeader.InterfaceUUID,
		ActivityUUID:     reqHeader.ActivityUUID,
		ServerBootTime:   s.bootTime,
		InterfaceVersion: reqHeader.InterfaceVersion,
		SequenceNum:      reqHeader.SequenceNum,
		OpNum:            reqHeader.OpNum,
		InterfaceHint:    0xFFFF,
		ActivityHint:     0xFFFF,
		FragmentLen:      uint16(len(body)),
		FragmentNum:      0,
		AuthProto:        0,
		SerialLow:        reqHeader.SerialLow,
	}

	pkt := append(resp.Marshal(), body...)

	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()

	if conn != nil {
		if _, err := conn.WriteToUDP(pkt, to); err != nil {
			s.log.Warn("rpc: send response failed", "error", err)
		}
	}
}

func (s *RPCServer) sendErrorResponse(reqHeader *RPCHeader, to *net.UDPAddr, status PNIOStatus) {
	s.sendResponse(reqHeader, to, status, nil, reqHeader.IsLittleEndian())
}

// Stop closes the UDP listener.
func (s *RPCServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}
