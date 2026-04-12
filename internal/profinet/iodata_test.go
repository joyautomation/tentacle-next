//go:build profinet || profinetcontroller || all

package profinet

import (
	"math"
	"testing"
)

func TestPackUnpackScalarTypes(t *testing.T) {
	tests := []struct {
		name     string
		datatype ProfinetType
		value    interface{}
		want     interface{} // expected after round-trip
		bufSize  uint16
	}{
		{"uint8", TypeUint8, float64(42), uint8(42), 1},
		{"int8 positive", TypeInt8, float64(100), int8(100), 1},
		{"int8 negative", TypeInt8, float64(-50), int8(-50), 1},
		{"uint16", TypeUint16, float64(1234), uint16(1234), 2},
		{"int16 positive", TypeInt16, float64(32000), int16(32000), 2},
		{"int16 negative", TypeInt16, float64(-32000), int16(-32000), 2},
		{"uint32", TypeUint32, float64(70000), uint32(70000), 4},
		{"int32 positive", TypeInt32, float64(100000), int32(100000), 4},
		{"int32 negative", TypeInt32, float64(-100000), int32(-100000), 4},
		{"float32", TypeFloat32, float64(3.14), float32(3.14), 4},
		{"float32 negative", TypeFloat32, float64(-273.15), float32(-273.15), 4},
		{"uint64", TypeUint64, float64(1e15), uint64(1e15), 8},
		{"int64 positive", TypeInt64, float64(1e15), int64(1e15), 8},
		{"int64 negative", TypeInt64, float64(-1e15), int64(-1e15), 8},
		{"float64", TypeFloat64, float64(math.Pi), float64(math.Pi), 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := &SubslotConfig{
				Direction: DirectionInput,
				InputSize: tt.bufSize,
				Tags: []TagMapping{
					{TagID: "tag1", ByteOffset: 0, Datatype: tt.datatype, Source: "test.data.dev.tag1"},
				},
			}
			values := map[string]interface{}{"tag1": tt.value}
			buf := PackInputBuffer(sub, values)

			if uint16(len(buf)) != tt.bufSize {
				t.Fatalf("buffer size = %d, want %d", len(buf), tt.bufSize)
			}

			// Unpack using output direction (no Source)
			outSub := &SubslotConfig{
				Direction:  DirectionOutput,
				OutputSize: tt.bufSize,
				Tags: []TagMapping{
					{TagID: "tag1", ByteOffset: 0, Datatype: tt.datatype},
				},
			}
			result := UnpackOutputBuffer(outSub, buf)
			got, ok := result["tag1"]
			if !ok {
				t.Fatal("tag1 not found in unpacked result")
			}

			if got != tt.want {
				t.Errorf("round-trip got %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestPackUnpackBool(t *testing.T) {
	sub := &SubslotConfig{
		Direction: DirectionInput,
		InputSize: 2,
		Tags: []TagMapping{
			{TagID: "bit0", ByteOffset: 0, BitOffset: 0, Datatype: TypeBool, Source: "s"},
			{TagID: "bit3", ByteOffset: 0, BitOffset: 3, Datatype: TypeBool, Source: "s"},
			{TagID: "bit7", ByteOffset: 0, BitOffset: 7, Datatype: TypeBool, Source: "s"},
			{TagID: "byte1_bit2", ByteOffset: 1, BitOffset: 2, Datatype: TypeBool, Source: "s"},
		},
	}

	values := map[string]interface{}{
		"bit0":       true,
		"bit3":       true,
		"bit7":       false,
		"byte1_bit2": true,
	}

	buf := PackInputBuffer(sub, values)

	// byte 0: bit0=1, bit3=1 => 0b00001001 = 0x09
	if buf[0] != 0x09 {
		t.Errorf("byte[0] = 0x%02x, want 0x09", buf[0])
	}
	// byte 1: bit2=1 => 0b00000100 = 0x04
	if buf[1] != 0x04 {
		t.Errorf("byte[1] = 0x%02x, want 0x04", buf[1])
	}

	// Unpack
	outSub := &SubslotConfig{
		Direction:  DirectionOutput,
		OutputSize: 2,
		Tags: []TagMapping{
			{TagID: "bit0", ByteOffset: 0, BitOffset: 0, Datatype: TypeBool},
			{TagID: "bit3", ByteOffset: 0, BitOffset: 3, Datatype: TypeBool},
			{TagID: "bit7", ByteOffset: 0, BitOffset: 7, Datatype: TypeBool},
			{TagID: "byte1_bit2", ByteOffset: 1, BitOffset: 2, Datatype: TypeBool},
		},
	}
	result := UnpackOutputBuffer(outSub, buf)

	if result["bit0"] != true {
		t.Error("bit0 should be true")
	}
	if result["bit3"] != true {
		t.Error("bit3 should be true")
	}
	if result["bit7"] != false {
		t.Error("bit7 should be false")
	}
	if result["byte1_bit2"] != true {
		t.Error("byte1_bit2 should be true")
	}
}

func TestPackMultipleTagsAtOffsets(t *testing.T) {
	sub := &SubslotConfig{
		Direction: DirectionInput,
		InputSize: 10,
		Tags: []TagMapping{
			{TagID: "temp", ByteOffset: 0, Datatype: TypeFloat32, Source: "s"},
			{TagID: "count", ByteOffset: 4, Datatype: TypeUint32, Source: "s"},
			{TagID: "status", ByteOffset: 8, Datatype: TypeUint16, Source: "s"},
		},
	}

	values := map[string]interface{}{
		"temp":   float64(25.5),
		"count":  float64(1000),
		"status": float64(3),
	}

	buf := PackInputBuffer(sub, values)
	if len(buf) != 10 {
		t.Fatalf("buffer length = %d, want 10", len(buf))
	}

	outSub := &SubslotConfig{
		Direction:  DirectionOutput,
		OutputSize: 10,
		Tags: []TagMapping{
			{TagID: "temp", ByteOffset: 0, Datatype: TypeFloat32},
			{TagID: "count", ByteOffset: 4, Datatype: TypeUint32},
			{TagID: "status", ByteOffset: 8, Datatype: TypeUint16},
		},
	}
	result := UnpackOutputBuffer(outSub, buf)

	if got := result["temp"].(float32); got != 25.5 {
		t.Errorf("temp = %v, want 25.5", got)
	}
	if got := result["count"].(uint32); got != 1000 {
		t.Errorf("count = %v, want 1000", got)
	}
	if got := result["status"].(uint16); got != 3 {
		t.Errorf("status = %v, want 3", got)
	}
}

func TestPackMissingValues(t *testing.T) {
	sub := &SubslotConfig{
		Direction: DirectionInput,
		InputSize: 4,
		Tags: []TagMapping{
			{TagID: "a", ByteOffset: 0, Datatype: TypeUint16, Source: "s"},
			{TagID: "b", ByteOffset: 2, Datatype: TypeUint16, Source: "s"},
		},
	}

	// Only provide value for 'a', 'b' should remain zero
	values := map[string]interface{}{"a": float64(0xBEEF)}
	buf := PackInputBuffer(sub, values)

	outSub := &SubslotConfig{
		Direction:  DirectionOutput,
		OutputSize: 4,
		Tags: []TagMapping{
			{TagID: "a", ByteOffset: 0, Datatype: TypeUint16},
			{TagID: "b", ByteOffset: 2, Datatype: TypeUint16},
		},
	}
	result := UnpackOutputBuffer(outSub, buf)
	if result["a"].(uint16) != 0xBEEF {
		t.Errorf("a = %v, want 0xBEEF", result["a"])
	}
	if result["b"].(uint16) != 0 {
		t.Errorf("b = %v, want 0", result["b"])
	}
}

func TestBigEndianByteOrder(t *testing.T) {
	sub := &SubslotConfig{
		Direction: DirectionInput,
		InputSize: 4,
		Tags: []TagMapping{
			{TagID: "val", ByteOffset: 0, Datatype: TypeUint32, Source: "s"},
		},
	}
	values := map[string]interface{}{"val": float64(0x01020304)}
	buf := PackInputBuffer(sub, values)

	// Big-endian: MSB first
	if buf[0] != 0x01 || buf[1] != 0x02 || buf[2] != 0x03 || buf[3] != 0x04 {
		t.Errorf("bytes = %02x %02x %02x %02x, want 01 02 03 04", buf[0], buf[1], buf[2], buf[3])
	}
}

func TestValidateTagOverlaps(t *testing.T) {
	tests := []struct {
		name    string
		sub     SubslotConfig
		wantErr bool
	}{
		{
			name: "no overlap",
			sub: SubslotConfig{
				InputSize: 8,
				Tags: []TagMapping{
					{TagID: "a", ByteOffset: 0, Datatype: TypeUint32, Source: "s"},
					{TagID: "b", ByteOffset: 4, Datatype: TypeUint32, Source: "s"},
				},
			},
			wantErr: false,
		},
		{
			name: "overlapping",
			sub: SubslotConfig{
				InputSize: 8,
				Tags: []TagMapping{
					{TagID: "a", ByteOffset: 0, Datatype: TypeUint32, Source: "s"},
					{TagID: "b", ByteOffset: 2, Datatype: TypeUint32, Source: "s"},
				},
			},
			wantErr: true,
		},
		{
			name: "input and output don't conflict",
			sub: SubslotConfig{
				InputSize:  4,
				OutputSize: 4,
				Tags: []TagMapping{
					{TagID: "in", ByteOffset: 0, Datatype: TypeUint32, Source: "s"},
					{TagID: "out", ByteOffset: 0, Datatype: TypeUint32},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTagOverlaps(&tt.sub)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTagOverlaps() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
