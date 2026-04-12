//go:build profinet || profinetall || profinetcontroller

package profinet

import (
	"encoding/binary"
	"fmt"
	"math"
)

// PROFINET uses big-endian byte order for I/O data on the wire.
var byteOrder = binary.BigEndian

// PackInputBuffer serializes tag values into a byte buffer for a subslot's input data
// (device -> controller). Only tags with a Source (input tags) are packed.
func PackInputBuffer(sub *SubslotConfig, tagValues map[string]interface{}) []byte {
	buf := make([]byte, sub.InputSize)
	for _, tag := range sub.Tags {
		if tag.Source == "" {
			continue // output tag, skip
		}
		val, ok := tagValues[tag.TagID]
		if !ok {
			continue // no value available, leave as zero
		}
		packValue(buf, tag.ByteOffset, tag.BitOffset, tag.Datatype, val)
	}
	return buf
}

// UnpackOutputBuffer deserializes a byte buffer from a subslot's output data
// (controller -> device) into tag values. Only tags without a Source (output tags) are unpacked.
func UnpackOutputBuffer(sub *SubslotConfig, data []byte) map[string]interface{} {
	result := make(map[string]interface{})
	for _, tag := range sub.Tags {
		if tag.Source != "" {
			continue // input tag, skip
		}
		if int(tag.ByteOffset)+TypeSize(tag.Datatype) > len(data) {
			continue // buffer too short
		}
		val := unpackValue(data, tag.ByteOffset, tag.BitOffset, tag.Datatype)
		if val != nil {
			result[tag.TagID] = val
		}
	}
	return result
}

func packValue(buf []byte, offset uint16, bitOffset uint8, dt ProfinetType, val interface{}) {
	f := toFloat(val)
	switch dt {
	case TypeBool:
		b := toBool(val)
		if b {
			buf[offset] |= 1 << bitOffset
		} else {
			buf[offset] &^= 1 << bitOffset
		}
	case TypeUint8:
		buf[offset] = uint8(f)
	case TypeInt8:
		buf[offset] = byte(int8(f))
	case TypeUint16:
		byteOrder.PutUint16(buf[offset:], uint16(f))
	case TypeInt16:
		byteOrder.PutUint16(buf[offset:], uint16(int16(f)))
	case TypeUint32:
		byteOrder.PutUint32(buf[offset:], uint32(f))
	case TypeInt32:
		byteOrder.PutUint32(buf[offset:], uint32(int32(f)))
	case TypeFloat32:
		byteOrder.PutUint32(buf[offset:], math.Float32bits(float32(f)))
	case TypeUint64:
		byteOrder.PutUint64(buf[offset:], uint64(f))
	case TypeInt64:
		byteOrder.PutUint64(buf[offset:], uint64(int64(f)))
	case TypeFloat64:
		byteOrder.PutUint64(buf[offset:], math.Float64bits(f))
	}
}

func unpackValue(data []byte, offset uint16, bitOffset uint8, dt ProfinetType) interface{} {
	switch dt {
	case TypeBool:
		return (data[offset] >> bitOffset & 1) == 1
	case TypeUint8:
		return data[offset]
	case TypeInt8:
		return int8(data[offset])
	case TypeUint16:
		return byteOrder.Uint16(data[offset:])
	case TypeInt16:
		return int16(byteOrder.Uint16(data[offset:]))
	case TypeUint32:
		return byteOrder.Uint32(data[offset:])
	case TypeInt32:
		return int32(byteOrder.Uint32(data[offset:]))
	case TypeFloat32:
		return math.Float32frombits(byteOrder.Uint32(data[offset:]))
	case TypeUint64:
		return byteOrder.Uint64(data[offset:])
	case TypeInt64:
		return int64(byteOrder.Uint64(data[offset:]))
	case TypeFloat64:
		return math.Float64frombits(byteOrder.Uint64(data[offset:]))
	default:
		return nil
	}
}

// toFloat converts any numeric or bool value to float64.
func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint8:
		return float64(n)
	case uint16:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	case bool:
		if n {
			return 1
		}
		return 0
	default:
		return 0
	}
}

// toBool converts a value to bool.
func toBool(v interface{}) bool {
	switch n := v.(type) {
	case bool:
		return n
	case float64:
		return n != 0
	case float32:
		return n != 0
	case int:
		return n != 0
	case int8:
		return n != 0
	case int16:
		return n != 0
	case int32:
		return n != 0
	case int64:
		return n != 0
	case uint8:
		return n != 0
	case uint16:
		return n != 0
	case uint32:
		return n != 0
	case uint64:
		return n != 0
	default:
		return false
	}
}

type byteRegion struct {
	tagID string
	start uint16
	end   uint16 // exclusive
}

// ValidateTagOverlaps checks for overlapping tag byte ranges within a subslot.
func ValidateTagOverlaps(sub *SubslotConfig) error {
	var inputRegions, outputRegions []byteRegion
	for _, tag := range sub.Tags {
		size := TypeSize(tag.Datatype)
		if size == 0 {
			continue
		}
		r := byteRegion{
			tagID: tag.TagID,
			start: tag.ByteOffset,
			end:   tag.ByteOffset + uint16(size),
		}
		if tag.Source != "" {
			inputRegions = append(inputRegions, r)
		} else {
			outputRegions = append(outputRegions, r)
		}
	}

	if err := checkOverlaps(inputRegions); err != nil {
		return fmt.Errorf("input tags: %w", err)
	}
	if err := checkOverlaps(outputRegions); err != nil {
		return fmt.Errorf("output tags: %w", err)
	}
	return nil
}

func checkOverlaps(regions []byteRegion) error {
	for i := 0; i < len(regions); i++ {
		for j := i + 1; j < len(regions); j++ {
			a, b := regions[i], regions[j]
			if a.start < b.end && b.start < a.end {
				return fmt.Errorf("tags %q and %q overlap at bytes [%d,%d) and [%d,%d)",
					a.tagID, b.tagID, a.start, a.end, b.start, b.end)
			}
		}
	}
	return nil
}
