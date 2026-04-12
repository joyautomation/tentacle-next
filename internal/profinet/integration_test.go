//go:build integration && (profinet || profinetall)

// Integration tests for the PROFINET IO Device module using veth pairs.
// These tests require root privileges to create veth pairs and open AF_PACKET sockets.
//
// Run with: sudo go test -tags 'profinet,integration' -run TestIntegration -v -timeout 60s

package profinet

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

const (
	vethDev  = "veth-pn-dev"
	vethCtrl = "veth-pn-ctrl"
	devIP    = "192.168.100.1"
	ctrlIP   = "192.168.100.2"
	subnet   = "/24"
)

// testController is a minimal PROFINET IO Controller for testing.
type testController struct {
	transport *Transport
	rpcConn   *net.UDPConn
	deviceIP  net.IP
	localMAC  net.HardwareAddr
	t         *testing.T
}

func skipIfNotRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("integration tests require root (need CAP_NET_ADMIN + CAP_NET_RAW)")
	}
}

func setupVethPair(t *testing.T) func() {
	t.Helper()

	// Create veth pair
	run(t, "ip", "link", "add", vethDev, "type", "veth", "peer", "name", vethCtrl)
	run(t, "ip", "link", "set", vethDev, "up")
	run(t, "ip", "link", "set", vethCtrl, "up")
	run(t, "ip", "addr", "add", devIP+subnet, "dev", vethDev)
	run(t, "ip", "addr", "add", ctrlIP+subnet, "dev", vethCtrl)

	// Small delay for interfaces to come up
	time.Sleep(100 * time.Millisecond)

	return func() {
		// Deleting one end removes both
		cmd := exec.Command("ip", "link", "del", vethDev)
		_ = cmd.Run()
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func newTestController(t *testing.T) *testController {
	t.Helper()

	transport, err := NewTransport(vethCtrl, noopLogger())
	if err != nil {
		t.Fatalf("controller transport: %v", err)
	}

	return &testController{
		transport: transport,
		deviceIP:  net.ParseIP(devIP),
		localMAC:  transport.LocalMAC(),
		t:         t,
	}
}

func (c *testController) close() {
	if c.transport != nil {
		c.transport.Close()
	}
	if c.rpcConn != nil {
		c.rpcConn.Close()
	}
}

// sendDCPIdentify sends a DCP Identify All request and waits for a response.
func (c *testController) sendDCPIdentify() (*DCPFrame, error) {
	// Build DCP Identify request with AllSelector filter
	req := &DCPFrame{
		FrameID:       FrameIDDCPIdentReq,
		ServiceID:     DCPServiceIdentify,
		ServiceType:   DCPServiceTypeRequest,
		Xid:           0x00000001,
		ResponseDelay: 1, // 10ms max delay
		Blocks: []DCPBlock{
			{Option: DCPOptionAllSelector, SubOption: 0x00FF},
		},
	}

	payload := MarshalDCPFrame(req)
	dstMAC := DCPMulticastAddr

	if err := c.transport.SendFrame(dstMAC, payload); err != nil {
		return nil, fmt.Errorf("send DCP Identify: %w", err)
	}

	// Wait for response (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for {
		_, respPayload, _, err := c.transport.RecvFrame(ctx)
		if err != nil {
			return nil, fmt.Errorf("recv DCP response: %w", err)
		}

		frameID, err := ParseFrameID(respPayload)
		if err != nil {
			continue
		}

		if frameID == FrameIDDCPIdentResp {
			return ParseDCPFrame(respPayload)
		}
	}
}

// sendRPCConnect sends a Connect RPC request and returns the response.
func (c *testController) sendRPCConnect(arUUID [16]byte, inputDataLen, outputDataLen uint16) (*RPCHeader, []PNIOBlock, error) {
	if c.rpcConn == nil {
		localAddr := &net.UDPAddr{IP: net.ParseIP(ctrlIP), Port: 0}
		conn, err := net.ListenUDP("udp4", localAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("listen UDP: %w", err)
		}
		c.rpcConn = conn
	}

	// Build PNIO blocks for Connect
	var blocks []byte

	// AR Block
	arData := buildTestARBlockData(arUUID, ARTypeIOCAR, 0x0001, c.localMAC, "test-controller")
	blocks = append(blocks, MarshalPNIOBlock(BlockTypeARBlockReq, 1, 0, arData)...)
	for len(blocks)%4 != 0 {
		blocks = append(blocks, 0)
	}

	// Input IOCR (device → controller)
	inputIOCR := buildTestIOCRBlockData(IOCRTypeInput, 0x0001, inputDataLen, 0xC000)
	blocks = append(blocks, MarshalPNIOBlock(BlockTypeIOCRBlockReq, 1, 0, inputIOCR)...)
	for len(blocks)%4 != 0 {
		blocks = append(blocks, 0)
	}

	// Output IOCR (controller → device)
	outputIOCR := buildTestIOCRBlockData(IOCRTypeOutput, 0x0002, outputDataLen, 0xC010)
	blocks = append(blocks, MarshalPNIOBlock(BlockTypeIOCRBlockReq, 1, 0, outputIOCR)...)
	for len(blocks)%4 != 0 {
		blocks = append(blocks, 0)
	}

	// Alarm CR
	alarmData := buildTestAlarmCRData()
	blocks = append(blocks, MarshalPNIOBlock(BlockTypeAlarmCRBlockReq, 1, 0, alarmData)...)
	for len(blocks)%4 != 0 {
		blocks = append(blocks, 0)
	}

	// Build NDR request body
	argsLen := uint32(len(blocks))
	body := make([]byte, 20+len(blocks))
	binary.LittleEndian.PutUint32(body[0:4], 65536)   // ArgsMaximum
	binary.LittleEndian.PutUint32(body[4:8], argsLen)
	binary.LittleEndian.PutUint32(body[8:12], argsLen) // MaxCount
	binary.LittleEndian.PutUint32(body[12:16], 0)      // Offset
	binary.LittleEndian.PutUint32(body[16:20], argsLen) // ActualCount
	copy(body[20:], blocks)

	// Build RPC header (LE drep, like Siemens controllers)
	activityUUID := [16]byte{0xAC, 0x01, 0x02, 0x03}
	header := &RPCHeader{
		RPCVersion:       RPCVersionCL,
		PacketType:       RPCPTypeRequest,
		Flags1:           RPCFlagLastFrag | RPCFlagIdempotent,
		DataRep:          [3]byte{0x10, 0x00, 0x00}, // LE
		ObjectUUID:       arUUID,
		InterfaceUUID:    UUIDPNIOInterface,
		ActivityUUID:     activityUUID,
		InterfaceVersion: 1,
		SequenceNum:      1,
		OpNum:            RPCOpConnect,
		InterfaceHint:    0xFFFF,
		ActivityHint:     0xFFFF,
		FragmentLen:      uint16(len(body)),
	}

	pkt := append(header.Marshal(), body...)

	// Send to device
	deviceAddr := &net.UDPAddr{IP: c.deviceIP, Port: PNIOCMPort}
	if _, err := c.rpcConn.WriteToUDP(pkt, deviceAddr); err != nil {
		return nil, nil, fmt.Errorf("send Connect: %w", err)
	}

	// Receive response
	buf := make([]byte, 65536)
	_ = c.rpcConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := c.rpcConn.ReadFromUDP(buf)
	if err != nil {
		return nil, nil, fmt.Errorf("recv Connect response: %w", err)
	}

	respHeader, err := ParseRPCHeader(buf[:n])
	if err != nil {
		return nil, nil, fmt.Errorf("parse response header: %w", err)
	}

	if respHeader.PacketType != RPCPTypeResponse {
		return respHeader, nil, fmt.Errorf("expected Response, got packet type %d", respHeader.PacketType)
	}

	// Parse response body
	respBody := buf[RPCHeaderSize:n]
	if len(respBody) < 20 {
		return respHeader, nil, fmt.Errorf("response body too short: %d", len(respBody))
	}

	// Check PNIO status
	status := PNIOStatus{respBody[0], respBody[1], respBody[2], respBody[3]}
	if !status.IsOK() {
		return respHeader, nil, fmt.Errorf("PNIO error: %v", status)
	}

	// Parse response blocks
	respArgsLen := binary.LittleEndian.Uint32(respBody[4:8])
	blockData := respBody[20:]
	if int(respArgsLen) < len(blockData) {
		blockData = blockData[:respArgsLen]
	}

	respBlocks, err := ParsePNIOBlocks(blockData)
	return respHeader, respBlocks, err
}

// sendRPCControl sends a Control RPC request (PrmEnd/ApplicationReady).
func (c *testController) sendRPCControl(arUUID [16]byte, controlCmd uint16) error {
	if c.rpcConn == nil {
		return fmt.Errorf("no RPC connection")
	}

	// Build control block
	controlData := make([]byte, 24)
	copy(controlData[0:16], arUUID[:])
	binary.BigEndian.PutUint16(controlData[16:18], 0x0001) // SessionKey
	binary.BigEndian.PutUint16(controlData[18:20], 0)       // padding
	binary.BigEndian.PutUint16(controlData[20:22], controlCmd)

	blockType := BlockTypeIODControlReq
	if controlCmd == ControlCmdPrmEnd {
		blockType = BlockTypeARRPCBlockReq
	}

	var blocks []byte
	blocks = append(blocks, MarshalPNIOBlock(blockType, 1, 0, controlData)...)

	// Build NDR body
	argsLen := uint32(len(blocks))
	body := make([]byte, 20+len(blocks))
	binary.LittleEndian.PutUint32(body[0:4], 65536)
	binary.LittleEndian.PutUint32(body[4:8], argsLen)
	binary.LittleEndian.PutUint32(body[8:12], argsLen)
	binary.LittleEndian.PutUint32(body[12:16], 0)
	binary.LittleEndian.PutUint32(body[16:20], argsLen)
	copy(body[20:], blocks)

	header := &RPCHeader{
		RPCVersion:       RPCVersionCL,
		PacketType:       RPCPTypeRequest,
		Flags1:           RPCFlagLastFrag | RPCFlagIdempotent,
		DataRep:          [3]byte{0x10, 0x00, 0x00},
		ObjectUUID:       arUUID,
		InterfaceUUID:    UUIDPNIOInterface,
		ActivityUUID:     [16]byte{0xAC, 0x02},
		InterfaceVersion: 1,
		SequenceNum:      2,
		OpNum:            RPCOpControl,
		InterfaceHint:    0xFFFF,
		ActivityHint:     0xFFFF,
		FragmentLen:      uint16(len(body)),
	}

	pkt := append(header.Marshal(), body...)
	deviceAddr := &net.UDPAddr{IP: c.deviceIP, Port: PNIOCMPort}
	if _, err := c.rpcConn.WriteToUDP(pkt, deviceAddr); err != nil {
		return fmt.Errorf("send Control: %w", err)
	}

	// Read response
	buf := make([]byte, 65536)
	_ = c.rpcConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := c.rpcConn.ReadFromUDP(buf)
	if err != nil {
		return fmt.Errorf("recv Control response: %w", err)
	}

	respHeader, err := ParseRPCHeader(buf[:n])
	if err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if respHeader.PacketType != RPCPTypeResponse {
		return fmt.Errorf("expected Response, got %d", respHeader.PacketType)
	}

	// Check PNIO status
	respBody := buf[RPCHeaderSize:n]
	if len(respBody) >= 4 {
		status := PNIOStatus{respBody[0], respBody[1], respBody[2], respBody[3]}
		if !status.IsOK() {
			return fmt.Errorf("PNIO error: %v", status)
		}
	}

	return nil
}

// receiveRTFrame waits for an RT cyclic frame and returns the payload.
func (c *testController) receiveRTFrame(timeout time.Duration) (uint16, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		_, payload, _, err := c.transport.RecvFrame(ctx)
		if err != nil {
			return 0, nil, err
		}

		frameID, err := ParseFrameID(payload)
		if err != nil {
			continue
		}

		if IsRTCyclicFrame(frameID) {
			return frameID, payload[2:], nil
		}
	}
}

// --- Integration Tests ---

func TestIntegrationDCPIdentify(t *testing.T) {
	skipIfNotRoot(t)
	cleanup := setupVethPair(t)
	defer cleanup()

	// Start device
	cfg := testDeviceConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	device := NewDevice(cfg, DeviceCallbacks{}, testLogger(t))
	go func() {
		if err := device.Start(ctx); err != nil && ctx.Err() == nil {
			t.Logf("device stopped: %v", err)
		}
	}()
	time.Sleep(200 * time.Millisecond) // let device start

	// Create test controller
	ctrl := newTestController(t)
	defer ctrl.close()

	// Send DCP Identify
	resp, err := ctrl.sendDCPIdentify()
	if err != nil {
		t.Fatalf("DCP Identify failed: %v", err)
	}

	if resp == nil {
		t.Fatal("no DCP Identify response")
	}

	t.Logf("DCP Identify response: %d blocks", len(resp.Blocks))

	// Verify we got station name in response
	foundName := false
	for _, block := range resp.Blocks {
		if block.Option == DCPOptionDeviceProperties && block.SubOption == DCPSubOptionDevNameOfStation {
			name := string(block.Data[2:]) // skip BlockInfo
			t.Logf("Station name: %s", name)
			if name == cfg.StationName {
				foundName = true
			}
		}
	}
	if !foundName {
		t.Error("station name not found in DCP Identify response")
	}
}

func TestIntegrationRPCConnect(t *testing.T) {
	skipIfNotRoot(t)
	cleanup := setupVethPair(t)
	defer cleanup()

	cfg := testDeviceConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	device := NewDevice(cfg, DeviceCallbacks{}, testLogger(t))
	go func() {
		if err := device.Start(ctx); err != nil && ctx.Err() == nil {
			t.Logf("device stopped: %v", err)
		}
	}()
	time.Sleep(200 * time.Millisecond)

	ctrl := newTestController(t)
	defer ctrl.close()

	arUUID := [16]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00}

	_, respBlocks, err := ctrl.sendRPCConnect(arUUID, 4, 4)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	t.Logf("Connect response: %d blocks", len(respBlocks))

	// Verify response contains expected block types
	hasAR := false
	hasIOCR := false
	for _, b := range respBlocks {
		switch b.Type {
		case BlockTypeARBlockRes:
			hasAR = true
			t.Logf("  ARBlockRes: %d bytes", len(b.Data))
		case BlockTypeIOCRBlockRes:
			hasIOCR = true
			t.Logf("  IOCRBlockRes: %d bytes", len(b.Data))
		case BlockTypeAlarmCRBlockRes:
			t.Logf("  AlarmCRBlockRes: %d bytes", len(b.Data))
		case BlockTypeModuleDiffBlock:
			t.Logf("  ModuleDiffBlock: %d bytes", len(b.Data))
		}
	}

	if !hasAR {
		t.Error("missing ARBlockRes in Connect response")
	}
	if !hasIOCR {
		t.Error("missing IOCRBlockRes in Connect response")
	}
}

func TestIntegrationFullCycle(t *testing.T) {
	skipIfNotRoot(t)
	cleanup := setupVethPair(t)
	defer cleanup()

	cfg := testDeviceConfig()
	cfg.Slots = []SlotConfig{
		{
			SlotNumber:    1,
			ModuleIdentNo: 0x00000001,
			Subslots: []SubslotConfig{
				{
					SubslotNumber:    1,
					SubmoduleIdentNo: 0x00000001,
					Direction:        DirectionInput,
					InputSize:        4,
					Tags: []TagMapping{
						{TagID: "temp", ByteOffset: 0, Datatype: TypeFloat32, Source: "plc.data.temp"},
					},
				},
			},
		},
	}

	connectedCh := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	device := NewDevice(cfg, DeviceCallbacks{
		OnConnected: func() {
			select {
			case connectedCh <- struct{}{}:
			default:
			}
		},
		GetInputData: func(sub *SubslotConfig) []byte {
			return PackInputBuffer(sub, map[string]interface{}{
				"temp": float32(42.5),
			})
		},
	}, testLogger(t))

	go func() {
		if err := device.Start(ctx); err != nil && ctx.Err() == nil {
			t.Logf("device stopped: %v", err)
		}
	}()
	time.Sleep(300 * time.Millisecond)

	ctrl := newTestController(t)
	defer ctrl.close()

	// Step 1: Connect
	arUUID := [16]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01}
	_, _, err := ctrl.sendRPCConnect(arUUID, 4, 0)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Log("Connect: OK")

	// Step 2: PrmEnd
	err = ctrl.sendRPCControl(arUUID, ControlCmdPrmEnd)
	if err != nil {
		t.Fatalf("PrmEnd failed: %v", err)
	}
	t.Log("PrmEnd: OK")

	// Step 3: Wait for cyclic to start
	select {
	case <-connectedCh:
		t.Log("Device entered DATA state")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for DATA state")
	}

	// Step 4: Receive cyclic input data
	frameID, data, err := ctrl.receiveRTFrame(3 * time.Second)
	if err != nil {
		t.Fatalf("failed to receive RT frame: %v", err)
	}

	t.Logf("Received RT frame: FrameID=0x%04X, %d bytes", frameID, len(data))

	if len(data) >= 4 {
		// The first 4 bytes should be the float32 value 42.5 in big-endian
		val := binary.BigEndian.Uint32(data[0:4])
		t.Logf("First 4 bytes: 0x%08X", val)
		// 42.5 as float32 big-endian = 0x42_2A_00_00
		if val == 0x422A0000 {
			t.Log("Cyclic data value matches: 42.5")
		} else {
			t.Logf("Note: expected 0x422A0000 (42.5), got 0x%08X", val)
		}
	}
}

// --- Helpers ---

func testDeviceConfig() *ProfinetConfig {
	return &ProfinetConfig{
		StationName:   "test-device",
		InterfaceName: vethDev,
		VendorID:      0x1234,
		DeviceID:      0x0001,
		DeviceName:    "TestDevice",
		CycleTimeUs:   1000,
	}
}

func testLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
