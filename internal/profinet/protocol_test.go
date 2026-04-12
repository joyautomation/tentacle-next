//go:build profinet || profinetall

package profinet

import (
	"encoding/binary"
	"log/slog"
	"net"
	"testing"
	"time"
)

// --- RPC Header tests ---

func TestRPCHeaderRoundTripBE(t *testing.T) {
	orig := &RPCHeader{
		RPCVersion:       RPCVersionCL,
		PacketType:       RPCPTypeRequest,
		Flags1:           RPCFlagLastFrag | RPCFlagIdempotent,
		Flags2:           0,
		DataRep:          [3]byte{0x00, 0x00, 0x00}, // big-endian
		SerialHigh:       0x01,
		ObjectUUID:       [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		InterfaceUUID:    UUIDPNIOInterface,
		ActivityUUID:     [16]byte{0xAA, 0xBB, 0xCC, 0xDD},
		ServerBootTime:   12345,
		InterfaceVersion: 1,
		SequenceNum:      42,
		OpNum:            RPCOpConnect,
		InterfaceHint:    0xFFFF,
		ActivityHint:     0xFFFF,
		FragmentLen:      256,
		FragmentNum:      0,
		AuthProto:        0,
		SerialLow:        0x02,
	}

	data := orig.Marshal()
	if len(data) != RPCHeaderSize {
		t.Fatalf("expected %d bytes, got %d", RPCHeaderSize, len(data))
	}

	parsed, err := ParseRPCHeader(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if parsed.RPCVersion != orig.RPCVersion {
		t.Errorf("RPCVersion: got %d, want %d", parsed.RPCVersion, orig.RPCVersion)
	}
	if parsed.PacketType != orig.PacketType {
		t.Errorf("PacketType: got %d, want %d", parsed.PacketType, orig.PacketType)
	}
	if parsed.ObjectUUID != orig.ObjectUUID {
		t.Errorf("ObjectUUID mismatch")
	}
	if parsed.InterfaceUUID != orig.InterfaceUUID {
		t.Errorf("InterfaceUUID mismatch: got %x, want %x", parsed.InterfaceUUID, orig.InterfaceUUID)
	}
	if parsed.OpNum != orig.OpNum {
		t.Errorf("OpNum: got %d, want %d", parsed.OpNum, orig.OpNum)
	}
	if parsed.SequenceNum != orig.SequenceNum {
		t.Errorf("SequenceNum: got %d, want %d", parsed.SequenceNum, orig.SequenceNum)
	}
	if parsed.FragmentLen != orig.FragmentLen {
		t.Errorf("FragmentLen: got %d, want %d", parsed.FragmentLen, orig.FragmentLen)
	}
	if parsed.SerialHigh != orig.SerialHigh || parsed.SerialLow != orig.SerialLow {
		t.Errorf("Serial: got %d/%d, want %d/%d", parsed.SerialHigh, parsed.SerialLow, orig.SerialHigh, orig.SerialLow)
	}
}

func TestRPCHeaderRoundTripLE(t *testing.T) {
	orig := &RPCHeader{
		RPCVersion:       RPCVersionCL,
		PacketType:       RPCPTypeRequest,
		Flags1:           RPCFlagLastFrag,
		DataRep:          [3]byte{0x10, 0x00, 0x00}, // little-endian
		ObjectUUID:       [16]byte{0xDE, 0xAD, 0xBE, 0xEF},
		InterfaceUUID:    UUIDPNIOInterface,
		ActivityUUID:     [16]byte{0x11, 0x22, 0x33, 0x44},
		ServerBootTime:   99999,
		InterfaceVersion: 1,
		SequenceNum:      7,
		OpNum:            RPCOpControl,
		InterfaceHint:    0xFFFF,
		ActivityHint:     0xFFFF,
		FragmentLen:      100,
		FragmentNum:      0,
	}

	data := orig.Marshal()
	parsed, err := ParseRPCHeader(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if parsed.OpNum != RPCOpControl {
		t.Errorf("OpNum: got %d, want %d", parsed.OpNum, RPCOpControl)
	}
	if parsed.SequenceNum != 7 {
		t.Errorf("SequenceNum: got %d, want 7", parsed.SequenceNum)
	}
	if parsed.InterfaceUUID != UUIDPNIOInterface {
		t.Errorf("InterfaceUUID mismatch after LE round-trip")
	}
	if parsed.FragmentLen != 100 {
		t.Errorf("FragmentLen: got %d, want 100", parsed.FragmentLen)
	}
}

func TestRPCHeaderTooShort(t *testing.T) {
	_, err := ParseRPCHeader(make([]byte, 40))
	if err == nil {
		t.Error("expected error for short header")
	}
}

// --- UUID wire conversion tests ---

func TestUUIDWireConversionBE(t *testing.T) {
	uuid := [16]byte{0xDE, 0xA0, 0x00, 0x01, 0x6C, 0x97, 0x11, 0xD1, 0x82, 0x71, 0x00, 0xA0, 0x24, 0x42, 0xDF, 0x7D}
	wire := uuidToWire(uuid, false) // BE — no swap
	result := uuidFromWire(wire, false)
	if result != uuid {
		t.Errorf("BE round-trip failed: got %x, want %x", result, uuid)
	}
}

func TestUUIDWireConversionLE(t *testing.T) {
	uuid := UUIDPNIOInterface
	wire := uuidToWire(uuid, true) // LE — swap first 3 groups

	// Verify first 4 bytes are reversed
	if wire[0] != uuid[3] || wire[1] != uuid[2] || wire[2] != uuid[1] || wire[3] != uuid[0] {
		t.Errorf("LE wire first group not swapped: got %x", wire[:4])
	}

	// Round-trip
	result := uuidFromWire(wire, true)
	if result != uuid {
		t.Errorf("LE round-trip failed: got %x, want %x", result, uuid)
	}
}

// --- PNIO request/response body tests ---

func TestPNIORequestBodyRoundTrip(t *testing.T) {
	// Create a simple block
	block := MarshalPNIOBlock(BlockTypeARBlockReq, 1, 0, []byte{0x01, 0x02, 0x03, 0x04})

	// Build request body (BE)
	argsLen := uint32(len(block))
	body := make([]byte, 20+len(block))
	binary.BigEndian.PutUint32(body[0:4], 1024) // ArgsMaximum
	binary.BigEndian.PutUint32(body[4:8], argsLen)
	binary.BigEndian.PutUint32(body[8:12], argsLen) // MaxCount
	binary.BigEndian.PutUint32(body[12:16], 0)       // Offset
	binary.BigEndian.PutUint32(body[16:20], argsLen) // ActualCount
	copy(body[20:], block)

	argsMax, blocks, err := ParsePNIORequestBody(body, false)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if argsMax != 1024 {
		t.Errorf("ArgsMaximum: got %d, want 1024", argsMax)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != BlockTypeARBlockReq {
		t.Errorf("block type: got 0x%04X, want 0x%04X", blocks[0].Type, BlockTypeARBlockReq)
	}
}

func TestPNIOResponseBody(t *testing.T) {
	blocks := MarshalPNIOBlock(BlockTypeARBlockRes, 1, 0, []byte{0xAA, 0xBB})
	body := MarshalPNIOResponseBody(PNIOStatusOK, blocks, false)

	// Check status bytes
	if body[0] != 0 || body[1] != 0 || body[2] != 0 || body[3] != 0 {
		t.Errorf("expected OK status, got %x", body[0:4])
	}

	argsLen := binary.BigEndian.Uint32(body[4:8])
	if argsLen != uint32(len(blocks)) {
		t.Errorf("ArgsLength: got %d, want %d", argsLen, len(blocks))
	}
}

// --- PNIO Block tests ---

func TestPNIOBlockRoundTrip(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	data := MarshalPNIOBlock(BlockTypeARBlockReq, 1, 0, payload)

	blocks, err := ParsePNIOBlocks(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	b := blocks[0]
	if b.Type != BlockTypeARBlockReq {
		t.Errorf("type: got 0x%04X, want 0x%04X", b.Type, BlockTypeARBlockReq)
	}
	if b.VersionHigh != 1 || b.VersionLow != 0 {
		t.Errorf("version: got %d.%d, want 1.0", b.VersionHigh, b.VersionLow)
	}
	if len(b.Data) != len(payload) {
		t.Errorf("data length: got %d, want %d", len(b.Data), len(payload))
	}
	for i, v := range payload {
		if b.Data[i] != v {
			t.Errorf("data[%d]: got 0x%02X, want 0x%02X", i, b.Data[i], v)
		}
	}
}

func TestPNIOMultipleBlocks(t *testing.T) {
	var data []byte
	data = append(data, MarshalPNIOBlock(BlockTypeARBlockReq, 1, 0, []byte{0x01, 0x02})...)
	// Pad to 4-byte alignment
	for len(data)%4 != 0 {
		data = append(data, 0)
	}
	data = append(data, MarshalPNIOBlock(BlockTypeIOCRBlockReq, 1, 0, []byte{0x03, 0x04, 0x05, 0x06})...)

	blocks, err := ParsePNIOBlocks(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != BlockTypeARBlockReq {
		t.Errorf("block 0 type: got 0x%04X", blocks[0].Type)
	}
	if blocks[1].Type != BlockTypeIOCRBlockReq {
		t.Errorf("block 1 type: got 0x%04X", blocks[1].Type)
	}
}

// --- AR Manager tests ---

func TestARManagerConnect(t *testing.T) {
	cfg := &ProfinetConfig{
		StationName:   "test-device",
		InterfaceName: "lo",
		VendorID:      0x1234,
		DeviceID:      0x0001,
		DeviceName:    "TestDevice",
		CycleTimeUs:   1000,
	}

	localMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	mgr := NewARManager(cfg, localMAC, nil)
	// Use a no-op logger for testing
	mgr.log = noopLogger()

	// Build a minimal Connect request with AR, IOCR, AlarmCR, ExpectedSubmodule blocks
	arUUID := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}

	// Build AR block data
	arData := buildTestARBlockData(arUUID, 0x0001, 0x0001, localMAC, "test-controller")
	arBlock := PNIOBlock{Type: BlockTypeARBlockReq, Data: arData}

	// Build Input IOCR block data
	inputIOCRData := buildTestIOCRBlockData(IOCRTypeInput, 0x0001, 32, 0xC000)
	inputIOCR := PNIOBlock{Type: BlockTypeIOCRBlockReq, Data: inputIOCRData}

	// Build Output IOCR block data
	outputIOCRData := buildTestIOCRBlockData(IOCRTypeOutput, 0x0002, 32, 0xC001)
	outputIOCR := PNIOBlock{Type: BlockTypeIOCRBlockReq, Data: outputIOCRData}

	// Build Alarm CR block data
	alarmData := buildTestAlarmCRData()
	alarmBlock := PNIOBlock{Type: BlockTypeAlarmCRBlockReq, Data: alarmData}

	blocks := []PNIOBlock{arBlock, inputIOCR, outputIOCR, alarmBlock}

	from := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 100), Port: 49152}
	actUUID := [16]byte{0xAA}

	respBuf, status := mgr.HandleConnect(blocks, from, actUUID)
	if !status.IsOK() {
		t.Fatalf("Connect failed with status: %v", status)
	}
	if len(respBuf) == 0 {
		t.Fatal("empty response")
	}

	// Verify AR was created
	if mgr.ActiveARCount() != 1 {
		t.Errorf("expected 1 AR, got %d", mgr.ActiveARCount())
	}

	ar := mgr.GetAR(arUUID)
	if ar == nil {
		t.Fatal("AR not found")
	}
	if ar.State != ARStateConnected {
		t.Errorf("AR state: got %s, want %s", ar.State, ARStateConnected)
	}
	if ar.InputIOCR == nil {
		t.Error("InputIOCR is nil")
	}
	if ar.OutputIOCR == nil {
		t.Error("OutputIOCR is nil")
	}
	if ar.AlarmCR == nil {
		t.Error("AlarmCR is nil")
	}

	// Parse response blocks
	respBlocks, err := ParsePNIOBlocks(respBuf)
	if err != nil {
		t.Fatalf("failed to parse response blocks: %v", err)
	}

	// Should have: ARBlockRes + 2x IOCRBlockRes + AlarmCRBlockRes + ModuleDiffBlock
	expectedTypes := map[uint16]bool{
		BlockTypeARBlockRes:      false,
		BlockTypeIOCRBlockRes:    false,
		BlockTypeAlarmCRBlockRes: false,
		BlockTypeModuleDiffBlock: false,
	}
	for _, b := range respBlocks {
		expectedTypes[b.Type] = true
	}
	for typ, found := range expectedTypes {
		if !found {
			t.Errorf("missing response block type 0x%04X", typ)
		}
	}
}

func TestARManagerRelease(t *testing.T) {
	cfg := &ProfinetConfig{
		StationName:   "test-device",
		InterfaceName: "lo",
		VendorID:      0x1234,
		DeviceID:      0x0001,
		DeviceName:    "TestDevice",
	}

	localMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	mgr := NewARManager(cfg, localMAC, noopLogger())

	// First, connect
	arUUID := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	arData := buildTestARBlockData(arUUID, 0x0001, 0x0001, localMAC, "ctrl")
	blocks := []PNIOBlock{{Type: BlockTypeARBlockReq, Data: arData}}
	from := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 100), Port: 49152}
	_, status := mgr.HandleConnect(blocks, from, [16]byte{})
	if !status.IsOK() {
		t.Fatalf("Connect failed: %v", status)
	}

	// Now release
	_, status = mgr.HandleRelease(nil, arUUID)
	if !status.IsOK() {
		t.Fatalf("Release failed: %v", status)
	}
	if mgr.ActiveARCount() != 0 {
		t.Errorf("expected 0 ARs after release, got %d", mgr.ActiveARCount())
	}
}

func TestARManagerControlPrmEnd(t *testing.T) {
	cfg := &ProfinetConfig{
		StationName:   "test-device",
		InterfaceName: "lo",
		VendorID:      0x1234,
		DeviceID:      0x0001,
		DeviceName:    "TestDevice",
	}

	localMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	mgr := NewARManager(cfg, localMAC, noopLogger())

	// Track cyclic start
	cyclicStarted := make(chan struct{}, 1)
	mgr.SetCyclicCallbacks(func(ar *AR) {
		cyclicStarted <- struct{}{}
	}, nil)

	// Connect
	arUUID := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	arData := buildTestARBlockData(arUUID, 0x0001, 0x0001, localMAC, "ctrl")
	blocks := []PNIOBlock{{Type: BlockTypeARBlockReq, Data: arData}}
	from := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 100), Port: 49152}
	_, status := mgr.HandleConnect(blocks, from, [16]byte{})
	if !status.IsOK() {
		t.Fatalf("Connect failed: %v", status)
	}

	// Send PrmEnd
	controlData := make([]byte, 24)
	copy(controlData[0:16], arUUID[:])
	binary.BigEndian.PutUint16(controlData[16:18], 0x0001) // SessionKey
	binary.BigEndian.PutUint16(controlData[18:20], 0)       // padding
	binary.BigEndian.PutUint16(controlData[20:22], ControlCmdPrmEnd)
	binary.BigEndian.PutUint16(controlData[22:24], 0) // ControlBlockProperties

	ctrlBlocks := []PNIOBlock{{Type: BlockTypeIODControlReq, Data: controlData}}
	_, status = mgr.HandleControl(ctrlBlocks, arUUID)
	if !status.IsOK() {
		t.Fatalf("Control/PrmEnd failed: %v", status)
	}

	// Wait for cyclic start callback
	select {
	case <-cyclicStarted:
		// OK
	case <-makeTimeout(1):
		t.Error("cyclic start callback not called within timeout")
	}

	// Verify AR entered DATA state
	ar := mgr.GetAR(arUUID)
	if ar == nil {
		t.Fatal("AR not found")
	}
	if ar.State != ARStateData {
		t.Errorf("AR state: got %s, want %s", ar.State, ARStateData)
	}
}

// --- LLDP encoding tests ---

func TestLLDPTLVEncoding(t *testing.T) {
	// Chassis ID with MAC subtype
	mac := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	chassisID := append([]byte{0x04}, mac...)
	tlv := encodeLLDPTLV(LLDPTLVChassisID, chassisID)

	// TLV header: type(7bits) + length(9bits) in 2 bytes
	header := binary.BigEndian.Uint16(tlv[0:2])
	tlvType := uint8(header >> 9)
	tlvLen := header & 0x01FF

	if tlvType != LLDPTLVChassisID {
		t.Errorf("TLV type: got %d, want %d", tlvType, LLDPTLVChassisID)
	}
	if tlvLen != uint16(len(chassisID)) {
		t.Errorf("TLV length: got %d, want %d", tlvLen, len(chassisID))
	}
	if tlv[2] != 0x04 { // MAC subtype
		t.Errorf("subtype: got 0x%02X, want 0x04", tlv[2])
	}
}

func TestLLDPEndTLV(t *testing.T) {
	tlv := encodeLLDPTLV(LLDPTLVEnd, nil)
	if len(tlv) != 2 || tlv[0] != 0 || tlv[1] != 0 {
		t.Errorf("End TLV: got %x, want 0000", tlv)
	}
}

// --- Alarm tests ---

func TestAlarmACKMarshal(t *testing.T) {
	ack := MarshalAlarmAck(0x0001, 0x0002, 5)
	if len(ack) != 12 {
		t.Fatalf("expected 12 bytes, got %d", len(ack))
	}

	frameID := binary.BigEndian.Uint16(ack[0:2])
	if frameID != FrameIDAlarmHigh {
		t.Errorf("FrameID: got 0x%04X, want 0x%04X", frameID, FrameIDAlarmHigh)
	}

	pduType := binary.BigEndian.Uint16(ack[2:4])
	if pduType != RTAPDUTypeACK {
		t.Errorf("PDU type: got 0x%04X, want 0x%04X", pduType, RTAPDUTypeACK)
	}

	ackSeq := binary.BigEndian.Uint16(ack[10:12])
	if ackSeq != 5 {
		t.Errorf("AckSeqNum: got %d, want 5", ackSeq)
	}
}

// --- Cyclic frame tests ---

func TestBuildRTFrame(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	iocs := []byte{IOxSGood}
	frame := BuildRTFrame(0xC001, data, iocs, 42)

	frameID := binary.BigEndian.Uint16(frame[0:2])
	if frameID != 0xC001 {
		t.Errorf("FrameID: got 0x%04X, want 0xC001", frameID)
	}

	// Data starts at offset 2
	for i, v := range data {
		if frame[2+i] != v {
			t.Errorf("data[%d]: got 0x%02X, want 0x%02X", i, frame[2+i], v)
		}
	}

	// IOCS after data
	if frame[2+len(data)] != IOxSGood {
		t.Errorf("IOCS: got 0x%02X, want 0x%02X", frame[2+len(data)], IOxSGood)
	}

	// Trailer: CycleCounter(2) + DataStatus(1) + TransferStatus(1)
	trailerOffset := 2 + len(data) + len(iocs)
	cycleCounter := binary.BigEndian.Uint16(frame[trailerOffset:])
	if cycleCounter != 42 {
		t.Errorf("CycleCounter: got %d, want 42", cycleCounter)
	}
	if frame[trailerOffset+2] != DataStatusNormal {
		t.Errorf("DataStatus: got 0x%02X, want 0x%02X", frame[trailerOffset+2], DataStatusNormal)
	}
}

func TestIsRTCyclicFrame(t *testing.T) {
	tests := []struct {
		frameID uint16
		want    bool
	}{
		{0x0100, true},
		{0xC001, true},
		{0xF7FF, true},
		{0xF800, false},  // acyclic
		{0xFC01, false},  // alarm high
		{0xFEFC, false},  // DCP
		{0x00FF, false},  // below range
	}
	for _, tt := range tests {
		if got := IsRTCyclicFrame(tt.frameID); got != tt.want {
			t.Errorf("IsRTCyclicFrame(0x%04X) = %v, want %v", tt.frameID, got, tt.want)
		}
	}
}

func TestIsAlarmFrame(t *testing.T) {
	if !IsAlarmFrame(FrameIDAlarmHigh) {
		t.Error("expected FrameIDAlarmHigh to be alarm frame")
	}
	if !IsAlarmFrame(FrameIDAlarmLow) {
		t.Error("expected FrameIDAlarmLow to be alarm frame")
	}
	if IsAlarmFrame(0xC001) {
		t.Error("expected 0xC001 to not be alarm frame")
	}
}

// --- PNIOStatus tests ---

func TestPNIOStatusOK(t *testing.T) {
	if !PNIOStatusOK.IsOK() {
		t.Error("PNIOStatusOK should be OK")
	}

	data := PNIOStatusOK.Marshal()
	if len(data) != 4 {
		t.Fatalf("expected 4 bytes, got %d", len(data))
	}
	for i, v := range data {
		if v != 0 {
			t.Errorf("byte %d: got 0x%02X, want 0x00", i, v)
		}
	}
}

func TestPNIOStatusError(t *testing.T) {
	status := PNIOStatus{0xDE, 0x81, 0x01, 0x00}
	if status.IsOK() {
		t.Error("error status should not be OK")
	}
}

// --- I&M0 tests ---

func TestIM0BlockMarshal(t *testing.T) {
	im0 := &IM0Data{
		VendorID:         0x1234,
		HWRevision:       1,
		SWRevisionPrefix: 'V',
		SWRevision:       [3]byte{1, 2, 3},
		IMVersion:        0x0101,
		IMSupported:      0x001E,
	}
	copy(im0.OrderID[:], []byte("TestDevice"))
	copy(im0.IMSerialNumber[:], []byte("SN0001"))

	block := MarshalIM0Block(im0)
	if len(block) < 6 { // At minimum, header
		t.Fatalf("block too short: %d bytes", len(block))
	}

	// Parse as PNIO block
	blocks, err := ParsePNIOBlocks(block)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != BlockTypeIM0 {
		t.Errorf("block type: got 0x%04X, want 0x%04X", blocks[0].Type, BlockTypeIM0)
	}
}

// --- IODControlRes test ---

func TestMarshalIODControlRes(t *testing.T) {
	arUUID := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	block := MarshalIODControlRes(arUUID, 0x0001, ControlCmdDone)

	blocks, err := ParsePNIOBlocks(block)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != BlockTypeIODControlRes {
		t.Errorf("type: got 0x%04X, want 0x%04X", blocks[0].Type, BlockTypeIODControlRes)
	}
}

// --- Helpers ---

func buildTestARBlockData(arUUID [16]byte, arType, sessionKey uint16, mac net.HardwareAddr, stationName string) []byte {
	nameLen := len(stationName)
	data := make([]byte, 52+nameLen)
	binary.BigEndian.PutUint16(data[0:2], arType)
	copy(data[2:18], arUUID[:])
	binary.BigEndian.PutUint16(data[18:20], sessionKey)
	copy(data[20:26], mac)
	// CMInitiatorObjUUID at 26:42 (zeros ok)
	binary.BigEndian.PutUint32(data[42:46], 0x00000001) // ARProperties
	binary.BigEndian.PutUint16(data[46:48], 100)        // ActivityTimeout
	binary.BigEndian.PutUint16(data[48:50], 34964)      // UDPRTPort
	binary.BigEndian.PutUint16(data[50:52], uint16(nameLen))
	copy(data[52:], []byte(stationName))
	return data
}

func buildTestIOCRBlockData(iocrType, iocrRef uint16, dataLen uint16, frameID uint16) []byte {
	data := make([]byte, 40) // minimum IOCR block
	binary.BigEndian.PutUint16(data[0:2], iocrType)
	binary.BigEndian.PutUint16(data[2:4], iocrRef)
	binary.BigEndian.PutUint16(data[4:6], 0x8892)  // LT = PROFINET
	binary.BigEndian.PutUint32(data[6:10], 0x00000001) // Properties = RT Class 1
	binary.BigEndian.PutUint16(data[10:12], dataLen)
	binary.BigEndian.PutUint16(data[12:14], frameID)
	binary.BigEndian.PutUint16(data[14:16], 32)    // SendClockFactor
	binary.BigEndian.PutUint16(data[16:18], 1)     // ReductionRatio
	binary.BigEndian.PutUint16(data[18:20], 1)     // Phase
	binary.BigEndian.PutUint16(data[20:22], 0)     // Sequence
	binary.BigEndian.PutUint32(data[22:26], 0xFFFFFFFF) // FrameSendOffset
	binary.BigEndian.PutUint16(data[26:28], 10)    // WatchdogFactor
	binary.BigEndian.PutUint16(data[28:30], 3)     // DataHoldFactor
	binary.BigEndian.PutUint16(data[30:32], 0)     // IOCRTagHeader
	// MulticastMAC at 32:38 (zeros)
	binary.BigEndian.PutUint16(data[38:40], 0) // NumberOfAPIs
	return data
}

func buildTestAlarmCRData() []byte {
	data := make([]byte, 18)
	binary.BigEndian.PutUint16(data[0:2], 0x0001) // AlarmCRType
	binary.BigEndian.PutUint16(data[2:4], 0x8892) // LT
	binary.BigEndian.PutUint32(data[4:8], 0)       // Properties
	binary.BigEndian.PutUint16(data[8:10], 10)    // RTATimeoutFactor
	binary.BigEndian.PutUint16(data[10:12], 3)    // RTARetries
	binary.BigEndian.PutUint16(data[12:14], 0x0001) // LocalAlarmRef
	binary.BigEndian.PutUint16(data[14:16], 200)  // MaxAlarmDataLen
	binary.BigEndian.PutUint16(data[16:18], 0)    // TagHeaderH
	return data
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(devNull{}, nil))
}

type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }

func makeTimeout(seconds int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		<-makeTimer(seconds)
		close(ch)
	}()
	return ch
}

func makeTimer(seconds int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		t := time.NewTimer(time.Duration(seconds) * time.Second)
		<-t.C
		close(ch)
	}()
	return ch
}
