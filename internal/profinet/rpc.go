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

// DCE/RPC flags1 bits (bit 0 is reserved for implementation).
const (
	RPCFlagLastFrag   uint8 = 0x02
	RPCFlagFrag       uint8 = 0x04
	RPCFlagNoFack     uint8 = 0x08
	RPCFlagMaybe      uint8 = 0x10
	RPCFlagIdempotent uint8 = 0x20
	RPCFlagBroadcast  uint8 = 0x40
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

	// NDR Transfer Syntax UUID: 8A885D04-1CEB-11C9-9FE8-08002B104860
	UUIDNDRTransferSyntax = [16]byte{
		0x8A, 0x88, 0x5D, 0x04,
		0x1C, 0xEB,
		0x11, 0xC9,
		0x9F, 0xE8,
		0x08, 0x00, 0x2B, 0x10, 0x48, 0x60,
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
	localIP  net.IP
	log      *slog.Logger
	bootTime uint32

	mu sync.Mutex
}

// NewRPCServer creates a new PNIO RPC server.
func NewRPCServer(cfg *ProfinetConfig, arMgr *ARManager, localMAC net.HardwareAddr, localIP net.IP, log *slog.Logger) *RPCServer {
	return &RPCServer{
		addr:     fmt.Sprintf(":%d", PNIOCMPort),
		arMgr:    arMgr,
		cfg:      cfg,
		localMAC: localMAC,
		localIP:  localIP,
		log:      log,
		bootTime: uint32(time.Now().Unix()),
	}
}

// SetIP updates the local IP address (thread-safe).
func (s *RPCServer) SetIP(ip net.IP) {
	s.mu.Lock()
	s.localIP = ip.To4()
	s.mu.Unlock()
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
		s.handleEPM(header, body, from)
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
	case RPCOpReadImplicit:
		s.handleRead(header, body, from, le)
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

// PROFINET IO Object Instance UUID base: DEA00000-6C97-11D1-8271-00xx-xxxx-xxxx
// The node bytes encode: interface_index(2) + DeviceID(2) + VendorID(2)
var UUIDPNIOObjectBase = [16]byte{
	0xDE, 0xA0, 0x00, 0x00,
	0x6C, 0x97,
	0x11, 0xD1,
	0x82, 0x71,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
}

// handleEPM responds to DCE/RPC Endpoint Mapper lookup requests.
// PROFINET EPM is stateful with 2 entries returned across multiple requests:
//  1. EPMv4 interface tower (sequence 0)
//  2. PNIO interface tower (sequence 1)
//  3. NOT_REGISTERED (sequence >= 2)
// Reference: p-net pf_cmrpc_epm.c, PN-AL-protocol Section 4.10.3
func (s *RPCServer) handleEPM(header *RPCHeader, body []byte, from *net.UDPAddr) {
	le := header.IsLittleEndian()
	s.log.Info("rpc: EPM lookup request", "from", from, "opnum", header.OpNum, "seq", header.SequenceNum)

	// We only handle ept_lookup (opnum 2)
	if header.OpNum != 2 {
		s.log.Debug("rpc: EPM opnum not supported", "opnum", header.OpNum)
		return
	}

	s.mu.Lock()
	localIP := s.localIP
	s.mu.Unlock()
	if localIP == nil {
		localIP = net.IPv4zero.To4()
	}

	// Determine state from the entry_handle in the request body.
	// NDR layout (flat, per p-net pf_get_epm_lookup_request):
	//   [0:4]   inquiry_type
	//   [4:8]   object referent ID
	//   [8:24]  object UUID (16)
	//   [24:28] interface referent ID
	//   [28:44] interface UUID (16)
	//   [44:46] interface ver major
	//   [46:48] interface ver minor
	//   [48:52] version_option
	//   [52:72] entry_handle (4 byte seq + 16 byte UUID)
	//   [72:76] max_entries
	handleZero := true
	if len(body) >= 72 {
		for _, b := range body[52:72] {
			if b != 0 {
				handleZero = false
				break
			}
		}
		s.log.Debug("rpc: EPM handle", "bytes", fmt.Sprintf("%x", body[52:72]), "zero", handleZero)
	}

	var respBody []byte
	if handleZero {
		// First request: return PNIO tower entry with a non-zero handle.
		// return_code=0 tells the client "more entries may exist".
		// The non-zero handle ensures the follow-up request won't restart from scratch.
		s.log.Debug("rpc: EPM returning PNIO entry with non-zero handle")
		tower := s.buildTower(UUIDPNIOInterface, 1, localIP, PNIOCMPort)
		objectUUID := s.pnioObjectUUID()
		handle := s.generateEPMHandle()
		respBody = s.buildEPMLookupResponse(objectUUID, tower, handle, le)
	} else {
		// Follow-up request (non-zero handle): no more entries
		s.log.Debug("rpc: EPM no more entries")
		respBody = s.buildEPMNoEntryResponse(le)
	}

	s.sendEPMResponse(header, respBody, from)
}

func (s *RPCServer) sendEPMResponse(header *RPCHeader, respBody []byte, to *net.UDPAddr) {
	resp := &RPCHeader{
		RPCVersion:       RPCVersionCL,
		PacketType:       RPCPTypeResponse,
		Flags1:           RPCFlagLastFrag | RPCFlagNoFack | RPCFlagIdempotent,
		Flags2:           0,
		DataRep:          header.DataRep,
		SerialHigh:       header.SerialHigh,
		ObjectUUID:       header.ObjectUUID,
		InterfaceUUID:    header.InterfaceUUID,
		ActivityUUID:     header.ActivityUUID,
		ServerBootTime:   s.bootTime,
		InterfaceVersion: header.InterfaceVersion,
		SequenceNum:      header.SequenceNum,
		OpNum:            header.OpNum,
		InterfaceHint:    0xFFFF,
		ActivityHint:     0xFFFF,
		FragmentLen:      uint16(len(respBody)),
		FragmentNum:      0,
		AuthProto:        0,
		SerialLow:        header.SerialLow,
	}

	pkt := append(resp.Marshal(), respBody...)

	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()

	if conn != nil {
		if _, err := conn.WriteToUDP(pkt, to); err != nil {
			s.log.Warn("rpc: EPM send failed", "error", err)
		}
	}
}

// generateEPMHandle creates a non-zero EPM context handle from timestamp + MAC.
func (s *RPCServer) generateEPMHandle() [20]byte {
	var h [20]byte
	// entry_handle (4 bytes) = 0
	// handle UUID (16 bytes) based on timestamp and MAC
	ts := uint32(time.Now().UnixMicro())
	binary.LittleEndian.PutUint32(h[4:8], ts&0x0000FFFF)             // time_low
	binary.LittleEndian.PutUint16(h[8:10], uint16(ts>>16)&0x00FF)    // time_mid
	binary.LittleEndian.PutUint16(h[10:12], uint16(ts>>24)|0x1000)   // time_hi_and_version
	h[12] = 0x80                                                      // clock_hi_and_reserved
	h[13] = 0x0C                                                      // clock_low
	if len(s.localMAC) >= 6 {
		copy(h[14:20], s.localMAC[:6]) // node = MAC address
	}
	return h
}

// pnioObjectUUID builds the PNIO IO Object Instance UUID with encoded device identity.
// Format: DEA00000-6C97-11D1-8271-00:01:DeviceID_hi:DeviceID_lo:VendorID_hi:VendorID_lo
func (s *RPCServer) pnioObjectUUID() [16]byte {
	uuid := UUIDPNIOObjectBase
	uuid[10] = 0x00                          // interface index high
	uuid[11] = 0x01                          // interface index low
	uuid[12] = byte(s.cfg.DeviceID >> 8)     // DeviceID high
	uuid[13] = byte(s.cfg.DeviceID & 0xFF)   // DeviceID low
	uuid[14] = byte(s.cfg.VendorID >> 8)     // VendorID high
	uuid[15] = byte(s.cfg.VendorID & 0xFF)   // VendorID low
	return uuid
}

// buildTower creates a DCE/RPC tower for a given interface UUID.
func (s *RPCServer) buildTower(ifUUID [16]byte, versionMajor uint16, ip net.IP, port uint16) []byte {
	var t []byte

	// Number of floors
	t = append(t, 0x05, 0x00) // 5 floors, LE

	// Floor 1: Interface UUID
	t = append(t, 0x13, 0x00) // LHS length = 19
	t = append(t, 0x0D)       // protocol: UUID
	t = append(t, uuidToWire(ifUUID, true)...)
	t = append(t, byte(versionMajor), byte(versionMajor>>8)) // version major (LE)
	t = append(t, 0x02, 0x00) // RHS length = 2
	t = append(t, 0x00, 0x00) // version minor = 0

	// Floor 2: Transfer syntax (NDR)
	t = append(t, 0x13, 0x00) // LHS length = 19
	t = append(t, 0x0D)       // protocol: UUID
	t = append(t, uuidToWire(UUIDNDRTransferSyntax, true)...)
	t = append(t, 0x02, 0x00) // NDR version major = 2
	t = append(t, 0x02, 0x00) // RHS length = 2
	t = append(t, 0x00, 0x00) // NDR version minor = 0

	// Floor 3: RPC connectionless (ncadg)
	t = append(t, 0x01, 0x00) // LHS length = 1
	t = append(t, 0x0A)       // protocol: connectionless RPC
	t = append(t, 0x02, 0x00) // RHS length = 2
	t = append(t, 0x00, 0x00) // minor version = 0

	// Floor 4: UDP transport
	t = append(t, 0x01, 0x00) // LHS length = 1
	t = append(t, 0x08)       // protocol: UDP
	t = append(t, 0x02, 0x00) // RHS length = 2
	t = append(t, byte(port>>8), byte(port&0xFF)) // port in network byte order

	// Floor 5: IP address
	t = append(t, 0x01, 0x00) // LHS length = 1
	t = append(t, 0x09)       // protocol: IP
	t = append(t, 0x04, 0x00) // RHS length = 4
	ip4 := ip.To4()
	if ip4 == nil {
		ip4 = net.IPv4zero.To4()
	}
	t = append(t, ip4...)

	return t
}

// buildEPMLookupResponse creates the NDR body for an ept_lookup response with one entry.
// Wire layout matches p-net pf_put_lookup_response_data() exactly.
func (s *RPCServer) buildEPMLookupResponse(objectUUID [16]byte, tower []byte, handle [20]byte, le bool) []byte {
	buf32 := epmBuf32(le)

	var resp []byte

	// Context handle (20 bytes): rpc_entry_handle(4) + handle_uuid(16)
	resp = append(resp, handle[:]...)

	// num_entry = 1
	resp = append(resp, buf32(1)...)

	// NDR conformant/varying array header for entries
	resp = append(resp, buf32(1)...) // max_count = 1
	resp = append(resp, buf32(0)...) // offset = 0
	resp = append(resp, buf32(1)...) // actual_count = 1

	// Entry[0]: object UUID (16 bytes, wire format)
	resp = append(resp, uuidToWire(objectUUID, le)...)

	// Entry[0]: tower pointer (referent ID = 3, per p-net PF_RPC_TOWER_REFERENTID)
	resp = append(resp, buf32(3)...)

	// Entry[0]: annotation — NDR varying string [string] char[64]
	// Varying encoding: offset(4) + actual_count(4) + data (no max_count since size is fixed)
	ann := s.buildAnnotation()
	// actual_count = length up to and including null terminator
	annLen := uint32(len(ann))
	for i, b := range ann {
		if b == 0 {
			annLen = uint32(i + 1)
			break
		}
	}
	resp = append(resp, buf32(0)...)      // offset = 0
	resp = append(resp, buf32(annLen)...) // actual_count
	resp = append(resp, ann[:annLen]...)
	// Pad annotation to 4-byte boundary
	if pad := int(annLen) % 4; pad != 0 {
		resp = append(resp, make([]byte, 4-pad)...)
	}

	// Tower pointee (deferred pointer data for twr_t conformant struct):
	// max_count(4) + tower_length(4) + tower_data
	towerLen := uint32(len(tower))
	resp = append(resp, buf32(towerLen)...) // conformant max_count
	resp = append(resp, buf32(towerLen)...) // tower_length field
	resp = append(resp, tower...)

	// Pad tower to 4-byte boundary
	if pad := len(tower) % 4; pad != 0 {
		resp = append(resp, make([]byte, 4-pad)...)
	}

	// return_code = 0 (success)
	resp = append(resp, buf32(0)...)

	return resp
}

// buildEPMNoEntryResponse creates the NDR body for a "no more entries" EPM response.
func (s *RPCServer) buildEPMNoEntryResponse(le bool) []byte {
	buf32 := epmBuf32(le)

	var resp []byte

	// Zero handle (20 bytes) — done
	resp = append(resp, make([]byte, 20)...)

	// num_entry = 0
	resp = append(resp, buf32(0)...)

	// NDR conformant/varying array header (empty)
	resp = append(resp, buf32(0)...) // max_count = 0
	resp = append(resp, buf32(0)...) // offset = 0
	resp = append(resp, buf32(0)...) // actual_count = 0

	// return_code = EPM_NOT_REGISTERED (0x16c9a0d6)
	resp = append(resp, buf32(0x16c9a0d6)...)

	return resp
}

// buildAnnotation returns a 64-byte annotation string for EPM entries.
// Format matches p-net: "%-25s %-20s %5u %c%3u%3u%3u"
func (s *RPCServer) buildAnnotation() []byte {
	name := s.cfg.DeviceName
	if len(name) > 25 {
		name = name[:25]
	}
	ann := fmt.Sprintf("%-25s %-20s %5d V  0  0  1", name, "", 1)
	buf := make([]byte, 64)
	copy(buf, []byte(ann))
	return buf
}

// epmBuf32 returns a helper function that encodes uint32 in the given byte order.
func epmBuf32(le bool) func(uint32) []byte {
	if le {
		return func(v uint32) []byte {
			b := make([]byte, 4)
			binary.LittleEndian.PutUint32(b, v)
			return b
		}
	}
	return func(v uint32) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, v)
		return b
	}
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

	// Mirror the Idempotent flag from the request — DCE/RPC requires it.
	flags := RPCFlagLastFrag | RPCFlagNoFack
	if reqHeader.Flags1&RPCFlagIdempotent != 0 {
		flags |= RPCFlagIdempotent
	}

	resp := &RPCHeader{
		RPCVersion:       RPCVersionCL,
		PacketType:       RPCPTypeResponse,
		Flags1:           flags,
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
