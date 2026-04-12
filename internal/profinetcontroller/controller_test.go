//go:build profinetcontroller || all

package profinetcontroller

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/joyautomation/tentacle/internal/profinet"
)

// --- unpackTagValue tests ---

func TestUnpackBool(t *testing.T) {
	data := []byte{0b00001010} // bits 1 and 3 set
	tag := ControllerTag{ByteOffset: 0, BitOffset: 0, Datatype: "bool"}
	if v := unpackTagValue(tag, data); v != false {
		t.Fatalf("bit0: got %v, want false", v)
	}
	tag.BitOffset = 1
	if v := unpackTagValue(tag, data); v != true {
		t.Fatalf("bit1: got %v, want true", v)
	}
	tag.BitOffset = 3
	if v := unpackTagValue(tag, data); v != true {
		t.Fatalf("bit3: got %v, want true", v)
	}
}

func TestUnpackUint8(t *testing.T) {
	data := []byte{0, 42, 0}
	tag := ControllerTag{ByteOffset: 1, Datatype: "uint8"}
	if v := unpackTagValue(tag, data); v != byte(42) {
		t.Fatalf("got %v, want 42", v)
	}
}

func TestUnpackInt8(t *testing.T) {
	data := []byte{0, 0xFE} // -2
	tag := ControllerTag{ByteOffset: 1, Datatype: "int8"}
	if v := unpackTagValue(tag, data); v != int8(-2) {
		t.Fatalf("got %v, want -2", v)
	}
}

func TestUnpackUint16(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[2:], 0x1234)
	tag := ControllerTag{ByteOffset: 2, Datatype: "uint16"}
	if v := unpackTagValue(tag, data); v != uint16(0x1234) {
		t.Fatalf("got %v, want 0x1234", v)
	}
}

func TestUnpackInt16(t *testing.T) {
	data := make([]byte, 2)
	v16 := int16(-1000)
	binary.BigEndian.PutUint16(data, uint16(v16))
	tag := ControllerTag{ByteOffset: 0, Datatype: "int16"}
	if v := unpackTagValue(tag, data); v != int16(-1000) {
		t.Fatalf("got %v, want -1000", v)
	}
}

func TestUnpackUint32(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, 0xDEADBEEF)
	tag := ControllerTag{ByteOffset: 0, Datatype: "uint32"}
	if v := unpackTagValue(tag, data); v != uint32(0xDEADBEEF) {
		t.Fatalf("got %v, want 0xDEADBEEF", v)
	}
}

func TestUnpackInt32(t *testing.T) {
	data := make([]byte, 4)
	v32 := int32(-99999)
	binary.BigEndian.PutUint32(data, uint32(v32))
	tag := ControllerTag{ByteOffset: 0, Datatype: "int32"}
	if v := unpackTagValue(tag, data); v != int32(-99999) {
		t.Fatalf("got %v, want -99999", v)
	}
}

func TestUnpackFloat32(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, math.Float32bits(3.14))
	tag := ControllerTag{ByteOffset: 0, Datatype: "float32"}
	v := unpackTagValue(tag, data)
	if f, ok := v.(float32); !ok || math.Abs(float64(f)-3.14) > 0.001 {
		t.Fatalf("got %v, want ~3.14", v)
	}
}

func TestUnpackFloat64(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, math.Float64bits(2.71828))
	tag := ControllerTag{ByteOffset: 0, Datatype: "float64"}
	v := unpackTagValue(tag, data)
	if f, ok := v.(float64); !ok || math.Abs(f-2.71828) > 0.0001 {
		t.Fatalf("got %v, want ~2.71828", v)
	}
}

func TestUnpackUint64(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, 0x0102030405060708)
	tag := ControllerTag{ByteOffset: 0, Datatype: "uint64"}
	if v := unpackTagValue(tag, data); v != uint64(0x0102030405060708) {
		t.Fatalf("got %v, want 0x0102030405060708", v)
	}
}

func TestUnpackInt64(t *testing.T) {
	data := make([]byte, 8)
	v64 := int64(-123456789)
	binary.BigEndian.PutUint64(data, uint64(v64))
	tag := ControllerTag{ByteOffset: 0, Datatype: "int64"}
	if v := unpackTagValue(tag, data); v != int64(-123456789) {
		t.Fatalf("got %v, want -123456789", v)
	}
}

func TestUnpackOutOfBounds(t *testing.T) {
	data := []byte{0x01}
	tag := ControllerTag{ByteOffset: 0, Datatype: "uint16"}
	if v := unpackTagValue(tag, data); v != nil {
		t.Fatalf("expected nil for out-of-bounds, got %v", v)
	}
}

// --- packTagValue tests ---

func TestPackBoolTrue(t *testing.T) {
	tag := ControllerTag{BitOffset: 2, Datatype: "bool"}
	result := packTagValue(tag, true)
	if len(result) != 1 || result[0] != (1<<2) {
		t.Fatalf("got %v, want [0x04]", result)
	}
}

func TestPackBoolFalse(t *testing.T) {
	tag := ControllerTag{BitOffset: 5, Datatype: "bool"}
	result := packTagValue(tag, false)
	if len(result) != 1 || result[0] != 0 {
		t.Fatalf("got %v, want [0x00]", result)
	}
}

func TestPackUint16(t *testing.T) {
	tag := ControllerTag{Datatype: "uint16"}
	result := packTagValue(tag, float64(0x1234))
	if len(result) != 2 {
		t.Fatalf("wrong length: %d", len(result))
	}
	if v := binary.BigEndian.Uint16(result); v != 0x1234 {
		t.Fatalf("got 0x%04X, want 0x1234", v)
	}
}

func TestPackFloat32(t *testing.T) {
	tag := ControllerTag{Datatype: "float32"}
	result := packTagValue(tag, float64(3.14))
	if len(result) != 4 {
		t.Fatalf("wrong length: %d", len(result))
	}
	bits := binary.BigEndian.Uint32(result)
	f := math.Float32frombits(bits)
	if math.Abs(float64(f)-3.14) > 0.001 {
		t.Fatalf("got %f, want ~3.14", f)
	}
}

func TestPackFloat64(t *testing.T) {
	tag := ControllerTag{Datatype: "float64"}
	result := packTagValue(tag, float64(2.71828))
	if len(result) != 8 {
		t.Fatalf("wrong length: %d", len(result))
	}
	bits := binary.BigEndian.Uint64(result)
	f := math.Float64frombits(bits)
	if math.Abs(f-2.71828) > 0.0001 {
		t.Fatalf("got %f, want ~2.71828", f)
	}
}

func TestPackInt32(t *testing.T) {
	tag := ControllerTag{Datatype: "int32"}
	result := packTagValue(tag, float64(-99999))
	if len(result) != 4 {
		t.Fatalf("wrong length: %d", len(result))
	}
	v := int32(binary.BigEndian.Uint32(result))
	if v != -99999 {
		t.Fatalf("got %d, want -99999", v)
	}
}

// --- packTagValue round-trip tests ---

func TestRoundTripFloat32(t *testing.T) {
	tag := ControllerTag{ByteOffset: 0, Datatype: "float32"}
	original := float64(42.5)
	packed := packTagValue(tag, original)
	unpacked := unpackTagValue(tag, packed)
	f, ok := unpacked.(float32)
	if !ok || math.Abs(float64(f)-original) > 0.001 {
		t.Fatalf("round-trip failed: %v -> %v", original, unpacked)
	}
}

func TestRoundTripFloat64(t *testing.T) {
	tag := ControllerTag{ByteOffset: 0, Datatype: "float64"}
	original := float64(123456.789)
	packed := packTagValue(tag, original)
	unpacked := unpackTagValue(tag, packed)
	f, ok := unpacked.(float64)
	if !ok || math.Abs(f-original) > 0.0001 {
		t.Fatalf("round-trip failed: %v -> %v", original, unpacked)
	}
}

func TestRoundTripUint32(t *testing.T) {
	tag := ControllerTag{ByteOffset: 0, Datatype: "uint32"}
	packed := packTagValue(tag, float64(0xCAFEBABE))
	unpacked := unpackTagValue(tag, packed)
	if v, ok := unpacked.(uint32); !ok || v != 0xCAFEBABE {
		t.Fatalf("round-trip failed: got %v", unpacked)
	}
}

// --- toFloat64 tests ---

func TestToFloat64FromJSONNumber(t *testing.T) {
	// JSON numbers arrive as float64 from json.Unmarshal by default
	v, ok := toFloat64(float64(42))
	if !ok || v != 42 {
		t.Fatalf("got %v %v, want 42 true", v, ok)
	}
}

func TestToFloat64FromInt(t *testing.T) {
	v, ok := toFloat64(int(100))
	if !ok || v != 100 {
		t.Fatalf("got %v %v, want 100 true", v, ok)
	}
}

func TestToFloat64FromString(t *testing.T) {
	_, ok := toFloat64("not a number")
	if ok {
		t.Fatal("expected false for string input")
	}
}

// --- backoffDuration tests ---

func TestBackoffDuration(t *testing.T) {
	if d := backoffDuration(0); d != 0 {
		t.Fatalf("failures=0: got %v, want 0", d)
	}
	if d := backoffDuration(1); d != 1e9 {
		t.Fatalf("failures=1: got %v, want 1s", d)
	}
	if d := backoffDuration(100); d != 30e9 {
		t.Fatalf("failures=100: got %v, want 30s (cap)", d)
	}
}

// --- rebuildDeviceTags tests ---

func TestRebuildDeviceTags(t *testing.T) {
	dev := &DeviceState{
		Subscribers: map[string]*Subscriber{
			"sub1": {
				Tags: map[string]ControllerTag{
					"tag1": {TagID: "tag1", Datatype: "uint16"},
					"tag2": {TagID: "tag2", Datatype: "float32"},
				},
			},
			"sub2": {
				Tags: map[string]ControllerTag{
					"tag2": {TagID: "tag2", Datatype: "float32"}, // duplicate
					"tag3": {TagID: "tag3", Datatype: "bool"},
				},
			},
		},
		allTags: make(map[string]ControllerTag),
	}

	rebuildDeviceTags(dev)

	if len(dev.allTags) != 3 {
		t.Fatalf("expected 3 unique tags, got %d", len(dev.allTags))
	}
	for _, id := range []string{"tag1", "tag2", "tag3"} {
		if _, ok := dev.allTags[id]; !ok {
			t.Fatalf("missing tag %q", id)
		}
	}
}

// --- DCP response parsing tests ---

func TestParseDCPResponseStationName(t *testing.T) {
	resp := &DCPResponse{
		SrcMAC: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		Frame: &profinet.DCPFrame{
			Blocks: []profinet.DCPBlock{
				{
					Option:    profinet.DCPOptionDeviceProperties,
					SubOption: profinet.DCPSubOptionDevNameOfStation,
					Data:      append([]byte{0x00, 0x00}, []byte("test-device")...), // 2-byte BlockInfo + name
				},
			},
		},
	}

	dev := parseDCPResponse(resp)
	if dev == nil {
		t.Fatal("expected non-nil device")
	}
	if dev.StationName != "test-device" {
		t.Fatalf("station name: got %q, want %q", dev.StationName, "test-device")
	}
	if dev.MAC.String() != "00:11:22:33:44:55" {
		t.Fatalf("MAC: got %s", dev.MAC.String())
	}
}

// --- RPC client builder tests ---

func TestBuildARBlockData(t *testing.T) {
	params := ConnectParams{
		ARUUID:        [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		LocalMAC:      []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
		StationName:   "my-dev",
		InputDataLen:  10,
		OutputDataLen: 4,
		OutputFrameID: 0xC010,
		CycleTimeMs:   1,
	}

	data := buildARBlockData(params)
	// AR type
	if arType := binary.BigEndian.Uint16(data[0:2]); arType != profinet.ARTypeIOCAR {
		t.Fatalf("AR type: got 0x%04X, want 0x%04X", arType, profinet.ARTypeIOCAR)
	}
	// AR UUID
	for i := 0; i < 16; i++ {
		if data[2+i] != params.ARUUID[i] {
			t.Fatalf("ARUUID mismatch at byte %d", i)
		}
	}
	// MAC
	if data[20] != 0xAA || data[25] != 0xFF {
		t.Fatal("MAC not copied correctly")
	}
	// Station name length
	nameLen := binary.BigEndian.Uint16(data[50:52])
	if nameLen != 6 {
		t.Fatalf("name length: got %d, want 6", nameLen)
	}
	// Station name
	if string(data[52:58]) != "my-dev" {
		t.Fatalf("station name: got %q", string(data[52:58]))
	}
}

func TestBuildIOCRData(t *testing.T) {
	data := buildIOCRData(profinet.IOCRTypeInput, 0x0001, 10, 0x0000, 4)
	iocrType := binary.BigEndian.Uint16(data[0:2])
	if iocrType != profinet.IOCRTypeInput {
		t.Fatalf("IOCR type: got 0x%04X, want 0x%04X", iocrType, profinet.IOCRTypeInput)
	}
	dataLen := binary.BigEndian.Uint16(data[10:12])
	if dataLen != 10 {
		t.Fatalf("data length: got %d, want 10", dataLen)
	}
	frameID := binary.BigEndian.Uint16(data[12:14])
	if frameID != 0x0000 {
		t.Fatalf("frameID: got 0x%04X, want 0x0000", frameID)
	}
	// Check reduction ratio for 4ms
	reduction := binary.BigEndian.Uint16(data[16:18])
	if reduction != 4 {
		t.Fatalf("reduction ratio: got %d, want 4", reduction)
	}
}

func TestBuildExpectedSubmoduleData(t *testing.T) {
	slots := []SlotSubscription{
		{
			SlotNumber:    0,
			ModuleIdentNo: 0x00000001,
			Subslots: []SubslotSubscription{
				{SubslotNumber: 1, SubmoduleIdentNo: 0x00000001, InputSize: 4},
			},
		},
		{
			SlotNumber:    1,
			ModuleIdentNo: 0x00000010,
			Subslots: []SubslotSubscription{
				{SubslotNumber: 1, SubmoduleIdentNo: 0x00000011, InputSize: 8, OutputSize: 4},
			},
		},
	}

	data := buildExpectedSubmoduleData(slots)
	// Number of APIs
	numAPIs := binary.BigEndian.Uint16(data[0:2])
	if numAPIs != 1 {
		t.Fatalf("numAPIs: got %d, want 1", numAPIs)
	}
	// Number of modules
	numModules := binary.BigEndian.Uint16(data[6:8])
	if numModules != 2 {
		t.Fatalf("numModules: got %d, want 2", numModules)
	}
}

func TestBuildAlarmCRData(t *testing.T) {
	data := buildAlarmCRData()
	alarmType := binary.BigEndian.Uint16(data[0:2])
	if alarmType != 0x0001 {
		t.Fatalf("alarm CR type: got 0x%04X, want 0x0001", alarmType)
	}
	lt := binary.BigEndian.Uint16(data[2:4])
	if lt != 0x8892 {
		t.Fatalf("LT: got 0x%04X, want 0x8892", lt)
	}
}

func TestPadTo4(t *testing.T) {
	tests := []struct {
		in  int
		out int
	}{
		{0, 0},
		{1, 4},
		{2, 4},
		{3, 4},
		{4, 4},
		{5, 8},
	}
	for _, tt := range tests {
		result := padTo4(make([]byte, tt.in))
		if len(result) != tt.out {
			t.Errorf("padTo4(len=%d): got %d, want %d", tt.in, len(result), tt.out)
		}
	}
}
