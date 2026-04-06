// Package sparkplug provides user-friendly types and helpers for encoding/decoding
// Sparkplug B payloads on top of the generated protobuf code.
package sparkplug

import (
	pb "github.com/joyautomation/tentacle/internal/sparkplug/pb"
)

// Re-export DataType constants for convenience.
const (
	TypeUnknown  = uint32(pb.DataType_Unknown)
	TypeInt8     = uint32(pb.DataType_Int8)
	TypeInt16    = uint32(pb.DataType_Int16)
	TypeInt32    = uint32(pb.DataType_Int32)
	TypeInt64    = uint32(pb.DataType_Int64)
	TypeUInt8    = uint32(pb.DataType_UInt8)
	TypeUInt16   = uint32(pb.DataType_UInt16)
	TypeUInt32   = uint32(pb.DataType_UInt32)
	TypeUInt64   = uint32(pb.DataType_UInt64)
	TypeFloat    = uint32(pb.DataType_Float)
	TypeDouble   = uint32(pb.DataType_Double)
	TypeBoolean  = uint32(pb.DataType_Boolean)
	TypeString   = uint32(pb.DataType_String)
	TypeDateTime = uint32(pb.DataType_DateTime)
	TypeText     = uint32(pb.DataType_Text)
	TypeTemplate = uint32(pb.DataType_Template)
	TypeBytes    = uint32(pb.DataType_Bytes)
)

// typeStringMap maps string type names to DataType codes.
var typeStringMap = map[string]uint32{
	"Int8": TypeInt8, "Int16": TypeInt16, "Int32": TypeInt32, "Int64": TypeInt64,
	"UInt8": TypeUInt8, "UInt16": TypeUInt16, "UInt32": TypeUInt32, "UInt64": TypeUInt64,
	"Float": TypeFloat, "Double": TypeDouble, "Boolean": TypeBoolean, "String": TypeString,
	"DateTime": TypeDateTime, "Text": TypeText, "Template": TypeTemplate, "Bytes": TypeBytes,
}

// typeCodeMap maps DataType codes to string names.
var typeCodeMap map[uint32]string

func init() {
	typeCodeMap = make(map[uint32]string, len(typeStringMap))
	for name, code := range typeStringMap {
		typeCodeMap[code] = name
	}
}

// TypeFromString returns the DataType code for a type name (e.g., "Double" → 10).
func TypeFromString(s string) uint32 {
	if code, ok := typeStringMap[s]; ok {
		return code
	}
	return TypeUnknown
}

// TypeToString returns the string name for a DataType code (e.g., 10 → "Double").
func TypeToString(code uint32) string {
	if name, ok := typeCodeMap[code]; ok {
		return name
	}
	return "Unknown"
}

// NatsToSparkplugType converts a NATS data type string to a Sparkplug B type code.
func NatsToSparkplugType(natsType string) uint32 {
	switch natsType {
	case "number":
		return TypeDouble
	case "boolean":
		return TypeBoolean
	case "string":
		return TypeString
	case "udt":
		return TypeTemplate
	default:
		return TypeString
	}
}

// SparkplugToNatsType converts a Sparkplug B type code to a NATS data type string.
func SparkplugToNatsType(code uint32) string {
	switch code {
	case TypeInt8, TypeInt16, TypeInt32, TypeInt64,
		TypeUInt8, TypeUInt16, TypeUInt32, TypeUInt64,
		TypeFloat, TypeDouble, TypeDateTime:
		return "number"
	case TypeBoolean:
		return "boolean"
	case TypeTemplate:
		return "udt"
	default:
		return "string"
	}
}
