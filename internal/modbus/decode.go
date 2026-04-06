//go:build modbus || all

package modbus

import (
	"encoding/binary"
	"fmt"
	"math"
)

// registerCount returns the number of 16-bit registers needed for a datatype.
func registerCount(datatype string) int {
	switch datatype {
	case "boolean", "int16", "uint16":
		return 1
	case "int32", "uint32", "float32":
		return 2
	case "float64":
		return 4
	default:
		return 1
	}
}

// arrangeBytes reorders register bytes according to the byte order.
// The function is self-inverse: applying the same order twice returns the original.
// Input bytes are in ABCD (big-endian) order by default from the wire.
func arrangeBytes(data []byte, order string) []byte {
	out := make([]byte, len(data))
	switch order {
	case "BADC":
		// Swap bytes within each 16-bit word
		for i := 0; i+1 < len(data); i += 2 {
			out[i] = data[i+1]
			out[i+1] = data[i]
		}
	case "CDAB":
		// Swap 16-bit words
		if len(data) == 4 {
			copy(out[0:2], data[2:4])
			copy(out[2:4], data[0:2])
		} else if len(data) == 8 {
			copy(out[0:2], data[6:8])
			copy(out[2:4], data[4:6])
			copy(out[4:6], data[2:4])
			copy(out[6:8], data[0:2])
		} else {
			copy(out, data)
		}
	case "DCBA":
		// Full byte reversal
		for i := range data {
			out[i] = data[len(data)-1-i]
		}
	default: // "ABCD" or empty — big-endian, no reordering
		copy(out, data)
	}
	return out
}

// decodeRegisters converts raw register bytes into a Go value based on the datatype.
// The data should already be in big-endian (ABCD) wire order; byteOrder specifies
// any reordering to apply before interpretation.
func decodeRegisters(data []byte, datatype string, byteOrder string) (interface{}, error) {
	needed := registerCount(datatype) * 2
	if len(data) < needed {
		return nil, fmt.Errorf("need %d bytes for %s, got %d", needed, datatype, len(data))
	}

	raw := arrangeBytes(data[:needed], byteOrder)

	switch datatype {
	case "boolean":
		return raw[0] != 0 || raw[1] != 0, nil
	case "int16":
		return int64(int16(binary.BigEndian.Uint16(raw))), nil
	case "uint16":
		return int64(binary.BigEndian.Uint16(raw)), nil
	case "int32":
		return int64(int32(binary.BigEndian.Uint32(raw))), nil
	case "uint32":
		return int64(binary.BigEndian.Uint32(raw)), nil
	case "float32":
		bits := binary.BigEndian.Uint32(raw)
		return float64(math.Float32frombits(bits)), nil
	case "float64":
		bits := binary.BigEndian.Uint64(raw)
		return math.Float64frombits(bits), nil
	default:
		return int64(binary.BigEndian.Uint16(raw)), nil
	}
}

// decodeBoolFromCoils extracts a boolean value from a coil/discrete response byte.
// bitOffset is the bit position within the byte array.
func decodeBoolFromCoils(data []byte, bitOffset int) bool {
	byteIdx := bitOffset / 8
	bitIdx := uint(bitOffset % 8)
	if byteIdx >= len(data) {
		return false
	}
	return data[byteIdx]&(1<<bitIdx) != 0
}

// encodeValue converts a Go value into register bytes suitable for a write command.
// Returns the bytes in big-endian order (ABCD) and the register count.
func encodeValue(value interface{}, datatype string, byteOrder string) ([]byte, int, error) {
	regCount := registerCount(datatype)
	size := regCount * 2
	raw := make([]byte, size)

	switch datatype {
	case "boolean":
		v, ok := toBool(value)
		if !ok {
			return nil, 0, fmt.Errorf("cannot convert %v to boolean", value)
		}
		if v {
			binary.BigEndian.PutUint16(raw, 1)
		}
	case "int16":
		v, ok := toInt64(value)
		if !ok {
			return nil, 0, fmt.Errorf("cannot convert %v to int16", value)
		}
		binary.BigEndian.PutUint16(raw, uint16(int16(v)))
	case "uint16":
		v, ok := toInt64(value)
		if !ok {
			return nil, 0, fmt.Errorf("cannot convert %v to uint16", value)
		}
		binary.BigEndian.PutUint16(raw, uint16(v))
	case "int32":
		v, ok := toInt64(value)
		if !ok {
			return nil, 0, fmt.Errorf("cannot convert %v to int32", value)
		}
		binary.BigEndian.PutUint32(raw, uint32(int32(v)))
	case "uint32":
		v, ok := toInt64(value)
		if !ok {
			return nil, 0, fmt.Errorf("cannot convert %v to uint32", value)
		}
		binary.BigEndian.PutUint32(raw, uint32(v))
	case "float32":
		v, ok := toFloat64(value)
		if !ok {
			return nil, 0, fmt.Errorf("cannot convert %v to float32", value)
		}
		binary.BigEndian.PutUint32(raw, math.Float32bits(float32(v)))
	case "float64":
		v, ok := toFloat64(value)
		if !ok {
			return nil, 0, fmt.Errorf("cannot convert %v to float64", value)
		}
		binary.BigEndian.PutUint64(raw, math.Float64bits(v))
	default:
		v, ok := toInt64(value)
		if !ok {
			return nil, 0, fmt.Errorf("cannot convert %v to %s", value, datatype)
		}
		binary.BigEndian.PutUint16(raw, uint16(v))
	}

	// Apply byte order
	ordered := arrangeBytes(raw, byteOrder)
	return ordered, regCount, nil
}

// toBool converts an interface value to bool.
func toBool(v interface{}) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case float64:
		return val != 0, true
	case float32:
		return val != 0, true
	case int:
		return val != 0, true
	case int64:
		return val != 0, true
	case json_number:
		f, err := val.Float64()
		return f != 0 && err == nil, err == nil
	}
	return false, false
}

// toInt64 converts a numeric interface value to int64.
func toInt64(v interface{}) (int64, bool) {
	switch val := v.(type) {
	case float64:
		return int64(val), true
	case float32:
		return int64(val), true
	case int:
		return int64(val), true
	case int64:
		return val, true
	case int32:
		return int64(val), true
	case uint32:
		return int64(val), true
	case uint16:
		return int64(val), true
	case json_number:
		i, err := val.Int64()
		if err != nil {
			f, err2 := val.Float64()
			if err2 != nil {
				return 0, false
			}
			return int64(f), true
		}
		return i, true
	}
	return 0, false
}

// toFloat64 converts a numeric interface value to float64.
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint16:
		return float64(val), true
	case json_number:
		f, err := val.Float64()
		return f, err == nil
	}
	return 0, false
}

// json_number is the interface implemented by json.Number.
type json_number interface {
	Float64() (float64, error)
	Int64() (int64, error)
	String() string
}
