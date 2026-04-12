//go:build profinetcontroller

package profinetcontroller

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/joyautomation/tentacle/internal/profinet"
)

// RPCClient sends PNIO DCE/RPC requests over UDP.
type RPCClient struct {
	conn      *net.UDPConn
	deviceAddr *net.UDPAddr
	log       *slog.Logger
	seqNum    atomic.Uint32
}

// NewRPCClient creates an RPC client targeting the given device.
func NewRPCClient(deviceIP net.IP, log *slog.Logger) (*RPCClient, error) {
	localAddr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("listen UDP: %w", err)
	}

	return &RPCClient{
		conn:       conn,
		deviceAddr: &net.UDPAddr{IP: deviceIP, Port: profinet.PNIOCMPort},
		log:        log,
	}, nil
}

// Close closes the UDP connection.
func (c *RPCClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// Connect sends a PNIO Connect RPC and returns the negotiated parameters.
func (c *RPCClient) Connect(ctx context.Context, params ConnectParams) (*ConnectResult, error) {
	// Build PNIO blocks
	var blocks []byte

	// AR Block Request
	arData := buildARBlockData(params)
	blocks = append(blocks, profinet.MarshalPNIOBlock(profinet.BlockTypeARBlockReq, 1, 0, arData)...)
	blocks = padTo4(blocks)

	// Input IOCR (device → controller): controller wants to receive input data
	inputIOCR := buildIOCRData(profinet.IOCRTypeInput, 0x0001, params.InputDataLen, 0x0000, params.CycleTimeMs)
	blocks = append(blocks, profinet.MarshalPNIOBlock(profinet.BlockTypeIOCRBlockReq, 1, 0, inputIOCR)...)
	blocks = padTo4(blocks)

	// Output IOCR (controller → device): controller sends output data
	outputIOCR := buildIOCRData(profinet.IOCRTypeOutput, 0x0002, params.OutputDataLen, params.OutputFrameID, params.CycleTimeMs)
	blocks = append(blocks, profinet.MarshalPNIOBlock(profinet.BlockTypeIOCRBlockReq, 1, 0, outputIOCR)...)
	blocks = padTo4(blocks)

	// Alarm CR
	alarmCR := buildAlarmCRData()
	blocks = append(blocks, profinet.MarshalPNIOBlock(profinet.BlockTypeAlarmCRBlockReq, 1, 0, alarmCR)...)
	blocks = padTo4(blocks)

	// Expected Submodule
	expectedSub := buildExpectedSubmoduleData(params.Slots)
	blocks = append(blocks, profinet.MarshalPNIOBlock(profinet.BlockTypeExpectedSubmoduleReq, 1, 0, expectedSub)...)
	blocks = padTo4(blocks)

	// Send RPC Connect
	respBody, err := c.sendRPC(ctx, profinet.RPCOpConnect, params.ARUUID, blocks)
	if err != nil {
		return nil, fmt.Errorf("connect RPC: %w", err)
	}

	// Parse response blocks
	respBlocks, err := profinet.ParsePNIOBlocks(respBody)
	if err != nil {
		return nil, fmt.Errorf("parse response blocks: %w", err)
	}

	result := &ConnectResult{}
	for _, b := range respBlocks {
		switch b.Type {
		case profinet.BlockTypeIOCRBlockRes:
			if len(b.Data) >= 6 {
				iocrType := binary.BigEndian.Uint16(b.Data[0:2])
				frameID := binary.BigEndian.Uint16(b.Data[4:6])
				if iocrType == profinet.IOCRTypeInput {
					result.InputFrameID = frameID
				}
			}
		case profinet.BlockTypeARBlockRes:
			if len(b.Data) >= 20 {
				result.SessionKey = binary.BigEndian.Uint16(b.Data[18:20])
			}
		}
	}

	c.log.Info("rpc-client: Connect OK", "inputFrameID", fmt.Sprintf("0x%04X", result.InputFrameID))
	return result, nil
}

// Control sends a Control RPC (PrmEnd, ApplicationReady, Release).
func (c *RPCClient) Control(ctx context.Context, arUUID [16]byte, sessionKey uint16, controlCmd uint16) error {
	controlData := make([]byte, 24)
	copy(controlData[0:16], arUUID[:])
	binary.BigEndian.PutUint16(controlData[16:18], sessionKey)
	binary.BigEndian.PutUint16(controlData[20:22], controlCmd)

	blockType := uint16(profinet.BlockTypeIODControlReq)
	if controlCmd == profinet.ControlCmdPrmEnd {
		blockType = profinet.BlockTypeARRPCBlockReq
	}

	var blocks []byte
	blocks = append(blocks, profinet.MarshalPNIOBlock(blockType, 1, 0, controlData)...)

	_, err := c.sendRPC(ctx, profinet.RPCOpControl, arUUID, blocks)
	if err != nil {
		return fmt.Errorf("control RPC (cmd=%d): %w", controlCmd, err)
	}

	c.log.Info("rpc-client: Control OK", "cmd", controlCmd)
	return nil
}

// Release sends a Release RPC.
func (c *RPCClient) Release(ctx context.Context, arUUID [16]byte, sessionKey uint16) error {
	controlData := make([]byte, 24)
	copy(controlData[0:16], arUUID[:])
	binary.BigEndian.PutUint16(controlData[16:18], sessionKey)
	binary.BigEndian.PutUint16(controlData[20:22], profinet.ControlCmdRelease)

	var blocks []byte
	blocks = append(blocks, profinet.MarshalPNIOBlock(profinet.BlockTypeIODControlReq, 1, 0, controlData)...)

	_, err := c.sendRPC(ctx, profinet.RPCOpRelease, arUUID, blocks)
	return err
}

// sendRPC sends an RPC request and returns the response body (blocks portion).
func (c *RPCClient) sendRPC(ctx context.Context, opNum uint16, arUUID [16]byte, pnioBlocks []byte) ([]byte, error) {
	seqNum := c.seqNum.Add(1)

	// Build NDR request body
	argsLen := uint32(len(pnioBlocks))
	body := make([]byte, 20+len(pnioBlocks))
	binary.LittleEndian.PutUint32(body[0:4], 65536)    // ArgsMaximum
	binary.LittleEndian.PutUint32(body[4:8], argsLen)
	binary.LittleEndian.PutUint32(body[8:12], argsLen)  // MaxCount
	binary.LittleEndian.PutUint32(body[12:16], 0)       // Offset
	binary.LittleEndian.PutUint32(body[16:20], argsLen) // ActualCount
	copy(body[20:], pnioBlocks)

	// Build RPC header (LE drep, like Siemens controllers)
	header := &profinet.RPCHeader{
		RPCVersion:       profinet.RPCVersionCL,
		PacketType:       profinet.RPCPTypeRequest,
		Flags1:           profinet.RPCFlagLastFrag | profinet.RPCFlagIdempotent,
		DataRep:          [3]byte{0x10, 0x00, 0x00}, // LE
		ObjectUUID:       arUUID,
		InterfaceUUID:    profinet.UUIDPNIOInterface,
		ActivityUUID:     newActivityUUID(seqNum),
		InterfaceVersion: 1,
		SequenceNum:      seqNum,
		OpNum:            opNum,
		InterfaceHint:    0xFFFF,
		ActivityHint:     0xFFFF,
		FragmentLen:      uint16(len(body)),
	}

	pkt := append(header.Marshal(), body...)

	// Send
	if _, err := c.conn.WriteToUDP(pkt, c.deviceAddr); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	// Receive response
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}
	_ = c.conn.SetReadDeadline(deadline)

	buf := make([]byte, 65536)
	n, _, err := c.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("recv: %w", err)
	}

	respHeader, err := profinet.ParseRPCHeader(buf[:n])
	if err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}

	if respHeader.PacketType != profinet.RPCPTypeResponse {
		return nil, fmt.Errorf("unexpected packet type: %d", respHeader.PacketType)
	}

	// Parse response body
	respBody := buf[profinet.RPCHeaderSize:n]
	if len(respBody) < 20 {
		return nil, fmt.Errorf("response body too short: %d", len(respBody))
	}

	// Check PNIO status
	status := profinet.PNIOStatus{
		ErrorCode:  respBody[0],
		ErrorDecode: respBody[1],
		ErrorCode1: respBody[2],
		ErrorCode2: respBody[3],
	}
	if !status.IsOK() {
		return nil, fmt.Errorf("PNIO error: %02X %02X %02X %02X", status.ErrorCode, status.ErrorDecode, status.ErrorCode1, status.ErrorCode2)
	}

	// Extract blocks
	respArgsLen := binary.LittleEndian.Uint32(respBody[4:8])
	blockData := respBody[20:]
	if int(respArgsLen) < len(blockData) {
		blockData = blockData[:respArgsLen]
	}

	return blockData, nil
}

func newActivityUUID(seq uint32) [16]byte {
	var uuid [16]byte
	uuid[0] = 0xAC
	uuid[1] = 0x01
	binary.BigEndian.PutUint32(uuid[12:16], seq)
	return uuid
}

func buildARBlockData(params ConnectParams) []byte {
	nameLen := len(params.StationName)
	data := make([]byte, 52+nameLen)
	binary.BigEndian.PutUint16(data[0:2], profinet.ARTypeIOCAR)
	copy(data[2:18], params.ARUUID[:])
	binary.BigEndian.PutUint16(data[18:20], 0x0001) // SessionKey
	copy(data[20:26], params.LocalMAC)
	// CMInitiatorObjUUID at 26:42 = zeros
	binary.BigEndian.PutUint32(data[42:46], 0x00000011) // ARProperties: supervisor-takeover allowed
	binary.BigEndian.PutUint16(data[46:48], 100)         // ActivityTimeout (100 * 100ms = 10s)
	binary.BigEndian.PutUint16(data[48:50], profinet.PNIOCMPort)
	binary.BigEndian.PutUint16(data[50:52], uint16(nameLen))
	copy(data[52:], []byte(params.StationName))
	return data
}

func buildIOCRData(iocrType, iocrRef, dataLen, frameID uint16, cycleTimeMs int) []byte {
	// Calculate SendClockFactor and ReductionRatio
	// Base clock = 31.25µs, SendClockFactor * ReductionRatio * 31.25µs = cycle time
	// Common: SendClockFactor=32 (1ms base), ReductionRatio = cycleTimeMs
	sendClockFactor := uint16(32) // 32 * 31.25µs = 1ms
	reductionRatio := uint16(1)
	if cycleTimeMs > 1 {
		reductionRatio = uint16(cycleTimeMs)
	}

	data := make([]byte, 40)
	binary.BigEndian.PutUint16(data[0:2], iocrType)
	binary.BigEndian.PutUint16(data[2:4], iocrRef)
	binary.BigEndian.PutUint16(data[4:6], 0x8892)       // LT = PROFINET
	binary.BigEndian.PutUint32(data[6:10], profinet.IOCRPropertyRTClass1)
	binary.BigEndian.PutUint16(data[10:12], dataLen)
	binary.BigEndian.PutUint16(data[12:14], frameID)
	binary.BigEndian.PutUint16(data[14:16], sendClockFactor)
	binary.BigEndian.PutUint16(data[16:18], reductionRatio)
	binary.BigEndian.PutUint16(data[18:20], 1) // Phase
	binary.BigEndian.PutUint16(data[20:22], 0) // Sequence
	binary.BigEndian.PutUint32(data[22:26], 0xFFFFFFFF) // FrameSendOffset
	binary.BigEndian.PutUint16(data[26:28], 10) // WatchdogFactor
	binary.BigEndian.PutUint16(data[28:30], 3)  // DataHoldFactor
	// IOCRTagHeader at 30:32 = 0
	// MulticastMAC at 32:38 = zeros
	binary.BigEndian.PutUint16(data[38:40], 0) // NumberOfAPIs
	return data
}

func buildAlarmCRData() []byte {
	data := make([]byte, 18)
	binary.BigEndian.PutUint16(data[0:2], 0x0001) // AlarmCRType
	binary.BigEndian.PutUint16(data[2:4], 0x8892) // LT
	binary.BigEndian.PutUint16(data[8:10], 10)    // RTATimeoutFactor
	binary.BigEndian.PutUint16(data[10:12], 3)    // RTARetries
	binary.BigEndian.PutUint16(data[12:14], 0x0001) // LocalAlarmRef
	binary.BigEndian.PutUint16(data[14:16], 200)  // MaxAlarmDataLen
	return data
}

func buildExpectedSubmoduleData(slots []SlotSubscription) []byte {
	// Count how many modules we need
	if len(slots) == 0 {
		data := make([]byte, 2)
		binary.BigEndian.PutUint16(data[0:2], 0) // 0 APIs
		return data
	}

	var buf []byte
	// NumberOfAPIs
	numAPIs := make([]byte, 2)
	binary.BigEndian.PutUint16(numAPIs, 1) // 1 API
	buf = append(buf, numAPIs...)

	// API entry
	apiHeader := make([]byte, 6)
	binary.BigEndian.PutUint32(apiHeader[0:4], 0) // API = 0 (default)
	binary.BigEndian.PutUint16(apiHeader[4:6], uint16(len(slots)))
	buf = append(buf, apiHeader...)

	for _, slot := range slots {
		modHeader := make([]byte, 10)
		binary.BigEndian.PutUint16(modHeader[0:2], slot.SlotNumber)
		binary.BigEndian.PutUint32(modHeader[2:6], slot.ModuleIdentNo)
		binary.BigEndian.PutUint16(modHeader[6:8], 0) // ModuleProperties
		binary.BigEndian.PutUint16(modHeader[8:10], uint16(len(slot.Subslots)))
		buf = append(buf, modHeader...)

		for _, sub := range slot.Subslots {
			// Determine submodule type based on I/O sizes
			var subType uint16
			numDescs := 0
			if sub.InputSize > 0 && sub.OutputSize > 0 {
				subType = 3 // INPUT_OUTPUT
				numDescs = 2
			} else if sub.InputSize > 0 {
				subType = 1 // INPUT
				numDescs = 1
			} else if sub.OutputSize > 0 {
				subType = 2 // OUTPUT
				numDescs = 1
			}

			subHeader := make([]byte, 8)
			binary.BigEndian.PutUint16(subHeader[0:2], sub.SubslotNumber)
			binary.BigEndian.PutUint32(subHeader[2:6], sub.SubmoduleIdentNo)
			binary.BigEndian.PutUint16(subHeader[6:8], subType) // SubmoduleProperties
			buf = append(buf, subHeader...)

			// Data descriptions
			for d := 0; d < numDescs; d++ {
				desc := make([]byte, 8)
				if d == 0 && sub.InputSize > 0 {
					binary.BigEndian.PutUint16(desc[0:2], 0x0001) // Input
					binary.BigEndian.PutUint16(desc[2:4], sub.InputSize)
					desc[4] = 1 // LengthIOPS
					desc[5] = 1 // LengthIOCS
				} else {
					binary.BigEndian.PutUint16(desc[0:2], 0x0002) // Output
					binary.BigEndian.PutUint16(desc[2:4], sub.OutputSize)
					desc[4] = 1
					desc[5] = 1
				}
				buf = append(buf, desc...)
			}
		}
	}

	return buf
}

func padTo4(data []byte) []byte {
	for len(data)%4 != 0 {
		data = append(data, 0)
	}
	return data
}
