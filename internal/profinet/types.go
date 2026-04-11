//go:build profinet || profinetall

// Package profinet implements a PROFINET IO Device using the p-net stack.
// It exposes Tentacle's tag space over PROFINET RT, allowing Siemens PLCs
// to natively read/write variables without gateway hardware.
package profinet

import "fmt"

// ProfinetConfig defines the full PROFINET IO Device configuration.
type ProfinetConfig struct {
	StationName   string       `json:"stationName"`   // PROFINET station name (used for DCP discovery)
	InterfaceName string       `json:"interfaceName"` // Network interface for raw L2 access (e.g. "eth1")
	VendorID      uint16       `json:"vendorId"`      // PROFINET vendor ID
	DeviceID      uint16       `json:"deviceId"`      // PROFINET device ID
	DeviceName    string       `json:"deviceName"`    // Human-readable device name
	CycleTimeUs   uint32       `json:"cycleTimeUs"`   // Cyclic I/O period in microseconds (default 1000)
	Slots         []SlotConfig `json:"slots"`         // Slot definitions (slot 0 = DAP, auto-generated)
}

// SlotConfig defines a single PROFINET module slot.
type SlotConfig struct {
	SlotNumber    uint16          `json:"slotNumber"`    // 0 = DAP (auto), 1+ = user I/O modules
	ModuleIdentNo uint32         `json:"moduleIdentNo"` // PROFINET module identification number
	Subslots      []SubslotConfig `json:"subslots"`
}

// SubslotConfig defines a submodule within a slot.
type SubslotConfig struct {
	SubslotNumber    uint16       `json:"subslotNumber"`
	SubmoduleIdentNo uint32       `json:"submoduleIdentNo"` // PROFINET submodule identification number
	Direction        Direction    `json:"direction"`         // Data direction from the IO Device's perspective
	InputSize        uint16       `json:"inputSize"`         // Bytes of input data (device -> controller)
	OutputSize       uint16       `json:"outputSize"`        // Bytes of output data (controller -> device)
	Tags             []TagMapping `json:"tags"`
}

// Direction indicates the data flow direction for a subslot.
type Direction string

const (
	DirectionInput       Direction = "input"       // Device -> Controller only
	DirectionOutput      Direction = "output"      // Controller -> Device only
	DirectionInputOutput Direction = "inputOutput" // Bidirectional
)

// TagMapping maps a Tentacle variable to a byte position within a subslot's I/O data buffer.
type TagMapping struct {
	TagID      string       `json:"tagId"`      // Tentacle variable ID
	ByteOffset uint16       `json:"byteOffset"` // Offset within the subslot data buffer
	BitOffset  uint8        `json:"bitOffset"`  // Bit offset within the byte (for bool types, 0-7)
	Datatype   ProfinetType `json:"datatype"`   // Wire data type
	Source     string       `json:"source"`     // Bus subject for value updates (input tags)
}

// ProfinetType identifies the wire data type for packing/unpacking I/O buffers.
type ProfinetType string

const (
	TypeBool    ProfinetType = "bool"
	TypeUint8   ProfinetType = "uint8"
	TypeInt8    ProfinetType = "int8"
	TypeUint16  ProfinetType = "uint16"
	TypeInt16   ProfinetType = "int16"
	TypeUint32  ProfinetType = "uint32"
	TypeInt32   ProfinetType = "int32"
	TypeFloat32 ProfinetType = "float32"
	TypeUint64  ProfinetType = "uint64"
	TypeInt64   ProfinetType = "int64"
	TypeFloat64 ProfinetType = "float64"
)

// TypeSize returns the byte size of a ProfinetType.
func TypeSize(t ProfinetType) int {
	switch t {
	case TypeBool, TypeUint8, TypeInt8:
		return 1
	case TypeUint16, TypeInt16:
		return 2
	case TypeUint32, TypeInt32, TypeFloat32:
		return 4
	case TypeUint64, TypeInt64, TypeFloat64:
		return 8
	default:
		return 0
	}
}

// ProfinetStatus reports the current state of the PROFINET IO Device.
type ProfinetStatus struct {
	Connected     bool   `json:"connected"`
	StationName   string `json:"stationName"`
	InterfaceName string `json:"interfaceName"`
	ControllerIP  string `json:"controllerIP,omitempty"` // IP of the connected controller
	AREP          uint32 `json:"arep,omitempty"`         // Application Relationship Endpoint
	InputSlots    int    `json:"inputSlots"`
	OutputSlots   int    `json:"outputSlots"`
}

// Validate checks the ProfinetConfig for consistency errors.
func (c *ProfinetConfig) Validate() error {
	if c.StationName == "" {
		return fmt.Errorf("stationName is required")
	}
	if c.InterfaceName == "" {
		return fmt.Errorf("interfaceName is required")
	}
	if c.VendorID == 0 {
		return fmt.Errorf("vendorId must be non-zero")
	}
	if c.DeviceID == 0 {
		return fmt.Errorf("deviceId must be non-zero")
	}
	if c.CycleTimeUs == 0 {
		c.CycleTimeUs = 1000 // default 1ms
	}

	slotNums := make(map[uint16]bool)
	for i, slot := range c.Slots {
		if slotNums[slot.SlotNumber] {
			return fmt.Errorf("duplicate slotNumber %d", slot.SlotNumber)
		}
		slotNums[slot.SlotNumber] = true

		subslotNums := make(map[uint16]bool)
		for j, sub := range slot.Subslots {
			if subslotNums[sub.SubslotNumber] {
				return fmt.Errorf("slot %d: duplicate subslotNumber %d", slot.SlotNumber, sub.SubslotNumber)
			}
			subslotNums[sub.SubslotNumber] = true

			if err := validateSubslot(slot.SlotNumber, &c.Slots[i].Subslots[j], sub); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSubslot(slotNum uint16, _ *SubslotConfig, sub SubslotConfig) error {
	switch sub.Direction {
	case DirectionInput:
		if sub.InputSize == 0 {
			return fmt.Errorf("slot %d subslot %d: input direction requires inputSize > 0", slotNum, sub.SubslotNumber)
		}
	case DirectionOutput:
		if sub.OutputSize == 0 {
			return fmt.Errorf("slot %d subslot %d: output direction requires outputSize > 0", slotNum, sub.SubslotNumber)
		}
	case DirectionInputOutput:
		if sub.InputSize == 0 && sub.OutputSize == 0 {
			return fmt.Errorf("slot %d subslot %d: inputOutput direction requires at least one non-zero size", slotNum, sub.SubslotNumber)
		}
	default:
		return fmt.Errorf("slot %d subslot %d: invalid direction %q", slotNum, sub.SubslotNumber, sub.Direction)
	}

	// Validate tag mappings don't overflow the buffer
	for _, tag := range sub.Tags {
		size := TypeSize(tag.Datatype)
		if size == 0 {
			return fmt.Errorf("slot %d subslot %d tag %q: unknown datatype %q", slotNum, sub.SubslotNumber, tag.TagID, tag.Datatype)
		}

		// Determine which buffer this tag maps into based on whether it has a source
		// Tags with a source are input (device->controller), tags without are output (controller->device)
		var bufSize uint16
		if tag.Source != "" {
			bufSize = sub.InputSize
		} else {
			bufSize = sub.OutputSize
		}

		if tag.Datatype == TypeBool {
			if tag.BitOffset > 7 {
				return fmt.Errorf("slot %d subslot %d tag %q: bitOffset must be 0-7", slotNum, sub.SubslotNumber, tag.TagID)
			}
			if uint16(tag.ByteOffset)+1 > bufSize {
				return fmt.Errorf("slot %d subslot %d tag %q: bool at offset %d overflows buffer of %d bytes",
					slotNum, sub.SubslotNumber, tag.TagID, tag.ByteOffset, bufSize)
			}
		} else {
			if uint16(tag.ByteOffset)+uint16(size) > bufSize {
				return fmt.Errorf("slot %d subslot %d tag %q: %s at offset %d overflows buffer of %d bytes",
					slotNum, sub.SubslotNumber, tag.TagID, tag.Datatype, tag.ByteOffset, bufSize)
			}
		}
	}

	return nil
}
