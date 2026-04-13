//go:build profinet || profinetcontroller || all

package profinet

import (
	"encoding/binary"
	"fmt"
	"net"
)

// PNIO block types used in DCE/RPC connect, read, write, and control operations.
// Each block has a 6-byte header: BlockType(2) + BlockLength(2) + BlockVersionHigh(1) + BlockVersionLow(1).

// Block type constants.
const (
	BlockTypeARBlockReq              uint16 = 0x0101
	BlockTypeIOCRBlockReq            uint16 = 0x0102
	BlockTypeAlarmCRBlockReq         uint16 = 0x0103
	BlockTypeExpectedSubmoduleReq    uint16 = 0x0104
	BlockTypeIODControlReq           uint16 = 0x0110
	BlockTypeIODControlRes           uint16 = 0x8110
	BlockTypeARBlockRes              uint16 = 0x8101
	BlockTypeIOCRBlockRes            uint16 = 0x8102
	BlockTypeAlarmCRBlockRes         uint16 = 0x8103
	BlockTypeModuleDiffBlock         uint16 = 0x8104
	BlockTypeIODWriteReqHeader       uint16 = 0x0008
	BlockTypeIODWriteResHeader       uint16 = 0x8008
	BlockTypeIODReadReqHeader        uint16 = 0x0009
	BlockTypeIODReadResHeader        uint16 = 0x8009
	BlockTypeIM0                     uint16 = 0x0020 // I&M 0
	BlockTypeIM1                     uint16 = 0x0021
	BlockTypeIM2                     uint16 = 0x0022
	BlockTypeIM3                     uint16 = 0x0023
	BlockTypeIM4                     uint16 = 0x0024
	BlockTypeARRPCBlockReq           uint16 = 0x0114 // IODControl with PrmEnd
	BlockTypeIOXBlockReq             uint16 = 0x0001 // AlarmNotification high
	BlockTypeIOXBlockReqLow          uint16 = 0x0002 // AlarmNotification low
	BlockTypeRealIdentData           uint16 = 0x0013 // RealIdentificationData (index 0xF000)
	BlockTypeAPIData                 uint16 = 0x001A // APIData (index 0xF821)
	BlockTypeIM0FilterDataSubmodule  uint16 = 0x0030 // I&M0FilterDataSubmodule (index 0xF840)
	BlockTypeIM0FilterDataModule     uint16 = 0x0031 // I&M0FilterDataModule (index 0xF840)
	BlockTypeIM0FilterDataDevice     uint16 = 0x0032 // I&M0FilterDataDevice (index 0xF840)
)

// AR types.
const (
	ARTypeIOCAR          uint16 = 0x0001 // IO Controller AR
	ARTypeIOSAR          uint16 = 0x0006 // IO Supervisor AR
	ARTypeIOCARSingle    uint16 = 0x0010 // Single AR
)

// IOCR types.
const (
	IOCRTypeInput           uint16 = 0x0001
	IOCRTypeOutput          uint16 = 0x0002
	IOCRTypeMulticastProv   uint16 = 0x0003
	IOCRTypeMulticastCons   uint16 = 0x0004
)

// IOCR properties.
const (
	IOCRPropertyRTClass1 uint32 = 0x00000001
	IOCRPropertyRTClass2 uint32 = 0x00000002
	IOCRPropertyRTClass3 uint32 = 0x00000003
)

// Control commands.
const (
	ControlCmdPrmEnd              uint16 = 0x0001
	ControlCmdApplicationReady    uint16 = 0x0002
	ControlCmdRelease             uint16 = 0x0003
	ControlCmdDone                uint16 = 0x0004 // (response)
	ControlCmdReadyForCompanion   uint16 = 0x0008
	ControlCmdReadyForRTClass3    uint16 = 0x0010
)

// PNIOBlock is a raw PNIO block with parsed header.
type PNIOBlock struct {
	Type         uint16
	Length       uint16 // length after the BlockLength field (includes version bytes)
	VersionHigh uint8
	VersionLow  uint8
	Data         []byte // payload after version bytes
}

// ParsePNIOBlocks parses a sequence of PNIO blocks from a byte slice.
func ParsePNIOBlocks(data []byte) ([]PNIOBlock, error) {
	var blocks []PNIOBlock
	offset := 0
	for offset+6 <= len(data) {
		block := PNIOBlock{
			Type:         binary.BigEndian.Uint16(data[offset : offset+2]),
			Length:       binary.BigEndian.Uint16(data[offset+2 : offset+4]),
			VersionHigh: data[offset+4],
			VersionLow:  data[offset+5],
		}

		// BlockLength includes version (2 bytes) but not Type and Length fields
		payloadLen := int(block.Length) - 2
		if payloadLen < 0 {
			payloadLen = 0
		}
		dataStart := offset + 6
		dataEnd := dataStart + payloadLen
		if dataEnd > len(data) {
			return blocks, fmt.Errorf("block type 0x%04X at offset %d: payload extends beyond data (need %d, have %d)",
				block.Type, offset, dataEnd, len(data))
		}

		block.Data = make([]byte, payloadLen)
		copy(block.Data, data[dataStart:dataEnd])
		blocks = append(blocks, block)

		// Advance past this block: 4 (type+length) + BlockLength
		offset += 4 + int(block.Length)
		// Pad to 4-byte alignment
		if offset%4 != 0 {
			offset += 4 - (offset % 4)
		}
	}
	return blocks, nil
}

// MarshalPNIOBlock serializes a single PNIO block.
func MarshalPNIOBlock(blockType uint16, versionHigh, versionLow uint8, payload []byte) []byte {
	blockLength := uint16(2 + len(payload)) // version (2) + payload
	buf := make([]byte, 6+len(payload))
	binary.BigEndian.PutUint16(buf[0:2], blockType)
	binary.BigEndian.PutUint16(buf[2:4], blockLength)
	buf[4] = versionHigh
	buf[5] = versionLow
	copy(buf[6:], payload)
	return buf
}

// Parsed block structures for the most important block types.

// ARBlockReq is the Application Relationship block from a Connect request.
type ARBlockReq struct {
	ARType           uint16
	ARUUID           [16]byte
	SessionKey       uint16
	CMInitiatorMAC   net.HardwareAddr
	CMInitiatorObjUUID [16]byte
	ARProperties     uint32
	CMInitiatorActivityTimeout uint16
	CMInitiatorUDPRTPort       uint16
	CMInitiatorStationName     string
}

// ParseARBlockReq parses an AR block request from block data (after version bytes).
func ParseARBlockReq(data []byte) (*ARBlockReq, error) {
	if len(data) < 52 {
		return nil, fmt.Errorf("ARBlockReq too short: %d bytes", len(data))
	}
	ar := &ARBlockReq{
		ARType:     binary.BigEndian.Uint16(data[0:2]),
		SessionKey: binary.BigEndian.Uint16(data[18:20]),
	}
	copy(ar.ARUUID[:], data[2:18])
	ar.CMInitiatorMAC = net.HardwareAddr(make([]byte, 6))
	copy(ar.CMInitiatorMAC, data[20:26])
	copy(ar.CMInitiatorObjUUID[:], data[26:42])
	ar.ARProperties = binary.BigEndian.Uint32(data[42:46])
	ar.CMInitiatorActivityTimeout = binary.BigEndian.Uint16(data[46:48])
	ar.CMInitiatorUDPRTPort = binary.BigEndian.Uint16(data[48:50])
	nameLen := binary.BigEndian.Uint16(data[50:52])
	if int(52+nameLen) > len(data) {
		return nil, fmt.Errorf("ARBlockReq station name length %d exceeds data", nameLen)
	}
	ar.CMInitiatorStationName = string(data[52 : 52+nameLen])
	return ar, nil
}

// IOCRBlockReq is the IO Communication Relationship block from a Connect request.
type IOCRBlockReq struct {
	IOCRType         uint16
	IOCRReference    uint16
	LT               uint16 // EtherType, should be 0x8892
	IOCRProperties   uint32
	DataLength       uint16 // C_SDU length
	FrameID          uint16
	SendClockFactor  uint16
	ReductionRatio   uint16
	Phase            uint16
	Sequence         uint16
	FrameSendOffset  uint32
	WatchdogFactor   uint16
	DataHoldFactor   uint16
	IOCRTagHeader    uint16
	IOCRMulticastMAC net.HardwareAddr
	NumberOfAPIs     uint16
	APIs             []IOCRAPIEntry
}

// IOCRAPIEntry describes one API within an IOCR.
type IOCRAPIEntry struct {
	API          uint32
	IODataObjects []IOCRDataObject
	IOCSs         []IOCRIOxS
}

// IOCRDataObject describes a single IO data object within an IOCR API.
type IOCRDataObject struct {
	SlotNumber    uint16
	SubslotNumber uint16
	FrameOffset   uint16
}

// IOCRIOxS describes a single IO status within an IOCR API.
type IOCRIOxS struct {
	SlotNumber    uint16
	SubslotNumber uint16
	FrameOffset   uint16
}

// ParseIOCRBlockReq parses an IOCR block request.
func ParseIOCRBlockReq(data []byte) (*IOCRBlockReq, error) {
	if len(data) < 38 {
		return nil, fmt.Errorf("IOCRBlockReq too short: %d bytes", len(data))
	}
	iocr := &IOCRBlockReq{
		IOCRType:        binary.BigEndian.Uint16(data[0:2]),
		IOCRReference:   binary.BigEndian.Uint16(data[2:4]),
		LT:              binary.BigEndian.Uint16(data[4:6]),
		IOCRProperties:  binary.BigEndian.Uint32(data[6:10]),
		DataLength:      binary.BigEndian.Uint16(data[10:12]),
		FrameID:         binary.BigEndian.Uint16(data[12:14]),
		SendClockFactor: binary.BigEndian.Uint16(data[14:16]),
		ReductionRatio:  binary.BigEndian.Uint16(data[16:18]),
		Phase:           binary.BigEndian.Uint16(data[18:20]),
		Sequence:        binary.BigEndian.Uint16(data[20:22]),
		FrameSendOffset: binary.BigEndian.Uint32(data[22:26]),
		WatchdogFactor:  binary.BigEndian.Uint16(data[26:28]),
		DataHoldFactor:  binary.BigEndian.Uint16(data[28:30]),
		IOCRTagHeader:   binary.BigEndian.Uint16(data[30:32]),
	}
	iocr.IOCRMulticastMAC = net.HardwareAddr(make([]byte, 6))
	copy(iocr.IOCRMulticastMAC, data[32:38])
	if len(data) < 40 {
		return iocr, nil
	}
	iocr.NumberOfAPIs = binary.BigEndian.Uint16(data[38:40])

	offset := 40
	for i := 0; i < int(iocr.NumberOfAPIs); i++ {
		if offset+6 > len(data) {
			break
		}
		entry := IOCRAPIEntry{
			API: binary.BigEndian.Uint32(data[offset : offset+4]),
		}
		numDataObjects := binary.BigEndian.Uint16(data[offset+4 : offset+6])
		offset += 6

		for j := 0; j < int(numDataObjects); j++ {
			if offset+6 > len(data) {
				break
			}
			obj := IOCRDataObject{
				SlotNumber:    binary.BigEndian.Uint16(data[offset : offset+2]),
				SubslotNumber: binary.BigEndian.Uint16(data[offset+2 : offset+4]),
				FrameOffset:   binary.BigEndian.Uint16(data[offset+4 : offset+6]),
			}
			entry.IODataObjects = append(entry.IODataObjects, obj)
			offset += 6
		}

		if offset+2 > len(data) {
			break
		}
		numIOCS := binary.BigEndian.Uint16(data[offset : offset+2])
		offset += 2

		for j := 0; j < int(numIOCS); j++ {
			if offset+6 > len(data) {
				break
			}
			cs := IOCRIOxS{
				SlotNumber:    binary.BigEndian.Uint16(data[offset : offset+2]),
				SubslotNumber: binary.BigEndian.Uint16(data[offset+2 : offset+4]),
				FrameOffset:   binary.BigEndian.Uint16(data[offset+4 : offset+6]),
			}
			entry.IOCSs = append(entry.IOCSs, cs)
			offset += 6
		}

		iocr.APIs = append(iocr.APIs, entry)
	}

	return iocr, nil
}

// AlarmCRBlockReq is the Alarm CR block from a Connect request.
type AlarmCRBlockReq struct {
	AlarmCRType       uint16
	LT                uint16
	AlarmCRProperties uint32
	RTATimeoutFactor  uint16
	RTARetries        uint16
	LocalAlarmRef     uint16
	MaxAlarmDataLen   uint16
	AlarmCRTagHeaderH uint16
	AlarmCRTagHeaderL uint16
}

// ParseAlarmCRBlockReq parses an Alarm CR block request.
func ParseAlarmCRBlockReq(data []byte) (*AlarmCRBlockReq, error) {
	if len(data) < 18 {
		return nil, fmt.Errorf("AlarmCRBlockReq too short: %d bytes", len(data))
	}
	return &AlarmCRBlockReq{
		AlarmCRType:       binary.BigEndian.Uint16(data[0:2]),
		LT:                binary.BigEndian.Uint16(data[2:4]),
		AlarmCRProperties: binary.BigEndian.Uint32(data[4:8]),
		RTATimeoutFactor:  binary.BigEndian.Uint16(data[8:10]),
		RTARetries:        binary.BigEndian.Uint16(data[10:12]),
		LocalAlarmRef:     binary.BigEndian.Uint16(data[12:14]),
		MaxAlarmDataLen:   binary.BigEndian.Uint16(data[14:16]),
		AlarmCRTagHeaderH: binary.BigEndian.Uint16(data[16:18]),
		// AlarmCRTagHeaderL is optional
	}, nil
}

// ExpectedSubmoduleReq represents the expected submodule block from a Connect request.
type ExpectedSubmoduleReq struct {
	APIs []ExpectedAPI
}

// ExpectedAPI represents one API within an ExpectedSubmodule block.
type ExpectedAPI struct {
	API     uint32
	Modules []ExpectedModule
}

// ExpectedModule represents one expected module.
type ExpectedModule struct {
	SlotNumber       uint16
	ModuleIdentNumber uint32
	ModuleProperties uint16
	Submodules       []ExpectedSubmodule
}

// ExpectedSubmodule represents one expected submodule.
type ExpectedSubmodule struct {
	SubslotNumber       uint16
	SubmoduleIdentNumber uint32
	SubmoduleProperties uint16
	DataDescription     []SubmoduleDataDesc
}

// SubmoduleDataDesc describes the I/O data for a submodule.
type SubmoduleDataDesc struct {
	DataDirection uint16 // 0x0001=Input, 0x0002=Output
	DataLength    uint16
	LengthIOPS    uint8
	LengthIOCS    uint8
}

// ParseExpectedSubmoduleReq parses an ExpectedSubmodule block.
func ParseExpectedSubmoduleReq(data []byte) (*ExpectedSubmoduleReq, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("ExpectedSubmoduleReq too short: %d bytes", len(data))
	}

	result := &ExpectedSubmoduleReq{}
	numAPIs := binary.BigEndian.Uint16(data[0:2])
	offset := 2

	for i := 0; i < int(numAPIs); i++ {
		if offset+6 > len(data) {
			break
		}
		api := ExpectedAPI{
			API: binary.BigEndian.Uint32(data[offset : offset+4]),
		}
		numModules := binary.BigEndian.Uint16(data[offset+4 : offset+6])
		offset += 6

		for j := 0; j < int(numModules); j++ {
			if offset+10 > len(data) {
				break
			}
			mod := ExpectedModule{
				SlotNumber:        binary.BigEndian.Uint16(data[offset : offset+2]),
				ModuleIdentNumber: binary.BigEndian.Uint32(data[offset+2 : offset+6]),
				ModuleProperties:  binary.BigEndian.Uint16(data[offset+6 : offset+8]),
			}
			numSubmodules := binary.BigEndian.Uint16(data[offset+8 : offset+10])
			offset += 10

			for k := 0; k < int(numSubmodules); k++ {
				if offset+12 > len(data) {
					break
				}
				sub := ExpectedSubmodule{
					SubslotNumber:        binary.BigEndian.Uint16(data[offset : offset+2]),
					SubmoduleIdentNumber: binary.BigEndian.Uint32(data[offset+2 : offset+6]),
					SubmoduleProperties:  binary.BigEndian.Uint16(data[offset+6 : offset+8]),
				}
				// SubmoduleProperties bit 0-1 = type: 0=NO_IO, 1=INPUT, 2=OUTPUT, 3=INPUT_OUTPUT
				subType := sub.SubmoduleProperties & 0x0003

				numDescs := 0
				switch subType {
				case 0: // NO_IO
					numDescs = 0
				case 1, 2: // INPUT or OUTPUT
					numDescs = 1
				case 3: // INPUT_OUTPUT
					numDescs = 2
				}

				descOffset := offset + 8
				for d := 0; d < numDescs; d++ {
					if descOffset+8 > len(data) {
						break
					}
					desc := SubmoduleDataDesc{
						DataDirection: binary.BigEndian.Uint16(data[descOffset : descOffset+2]),
						DataLength:    binary.BigEndian.Uint16(data[descOffset+2 : descOffset+4]),
						LengthIOPS:    data[descOffset+4],
						LengthIOCS:    data[descOffset+5],
					}
					sub.DataDescription = append(sub.DataDescription, desc)
					descOffset += 8 // 2+2+1+1+2(padding)
				}
				offset = descOffset

				mod.Submodules = append(mod.Submodules, sub)
			}

			api.Modules = append(api.Modules, mod)
		}

		result.APIs = append(result.APIs, api)
	}

	return result, nil
}

// Response block builders.

// MarshalARBlockRes creates an AR Block Response.
func MarshalARBlockRes(arType uint16, arUUID [16]byte, sessionKey uint16, deviceMAC net.HardwareAddr, deviceUDPPort uint16) []byte {
	payload := make([]byte, 26)
	binary.BigEndian.PutUint16(payload[0:2], arType)
	copy(payload[2:18], arUUID[:])
	binary.BigEndian.PutUint16(payload[18:20], sessionKey)
	copy(payload[20:26], deviceMAC)

	return MarshalPNIOBlock(BlockTypeARBlockRes, 1, 0, payload)
}

// MarshalIOCRBlockRes creates an IOCR Block Response.
func MarshalIOCRBlockRes(iocrType uint16, iocrRef uint16, frameID uint16) []byte {
	payload := make([]byte, 6)
	binary.BigEndian.PutUint16(payload[0:2], iocrType)
	binary.BigEndian.PutUint16(payload[2:4], iocrRef)
	binary.BigEndian.PutUint16(payload[4:6], frameID)
	return MarshalPNIOBlock(BlockTypeIOCRBlockRes, 1, 0, payload)
}

// MarshalAlarmCRBlockRes creates an Alarm CR Block Response.
func MarshalAlarmCRBlockRes(alarmCRType uint16, localAlarmRef uint16, maxAlarmDataLen uint16) []byte {
	payload := make([]byte, 6)
	binary.BigEndian.PutUint16(payload[0:2], alarmCRType)
	binary.BigEndian.PutUint16(payload[2:4], localAlarmRef)
	binary.BigEndian.PutUint16(payload[4:6], maxAlarmDataLen)
	return MarshalPNIOBlock(BlockTypeAlarmCRBlockRes, 1, 0, payload)
}

// MarshalModuleDiffBlock creates a Module Diff Block indicating no differences.
func MarshalModuleDiffBlock(numAPIs uint16) []byte {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload[0:2], numAPIs)
	// For "no difference", we just report 0 APIs or empty diff
	return MarshalPNIOBlock(BlockTypeModuleDiffBlock, 1, 0, payload)
}

// MarshalIODControlRes creates an IODControl response block.
func MarshalIODControlRes(arUUID [16]byte, sessionKey uint16, controlCmd uint16) []byte {
	payload := make([]byte, 28)
	// Padding(2) + ARBlockRes-like header
	copy(payload[0:16], arUUID[:])
	binary.BigEndian.PutUint16(payload[16:18], sessionKey)
	binary.BigEndian.PutUint16(payload[18:20], 0) // AlarmSequenceNumber
	binary.BigEndian.PutUint16(payload[20:22], controlCmd)
	binary.BigEndian.PutUint16(payload[22:24], 0) // ControlBlockProperties
	return MarshalPNIOBlock(BlockTypeIODControlRes, 1, 0, payload)
}

// IOxS values for IO Provider/Consumer Status.
const (
	IOxSGood uint8 = 0x80 // DataState=good, Instance=subslot, no extension
	IOxSBad  uint8 = 0x00 // DataState=bad
)

// DataStatus values for cyclic RT frames.
const (
	DataStatusPrimary    uint8 = 0x01
	DataStatusValid      uint8 = 0x04
	DataStatusRun        uint8 = 0x10
	DataStatusNoProblem  uint8 = 0x20
	DataStatusNormal     uint8 = 0x35 // primary | valid | run | no problem
)

// I&M0 (Identification & Maintenance) data.
type IM0Data struct {
	VendorID         uint16
	OrderID          [20]byte
	IMSerialNumber   [16]byte
	HWRevision       uint16
	SWRevisionPrefix byte
	SWRevision       [3]byte
	RevisionCounter  uint16
	ProfileID        uint16
	ProfileSpecType  uint16
	IMVersion        uint16
	IMSupported      uint16
}

// MarshalIM0Block creates an I&M0 response block.
func MarshalIM0Block(im0 *IM0Data) []byte {
	payload := make([]byte, 64)
	binary.BigEndian.PutUint16(payload[0:2], im0.VendorID)
	copy(payload[2:22], im0.OrderID[:])
	copy(payload[22:38], im0.IMSerialNumber[:])
	binary.BigEndian.PutUint16(payload[38:40], im0.HWRevision)
	payload[40] = im0.SWRevisionPrefix
	payload[41] = im0.SWRevision[0]
	payload[42] = im0.SWRevision[1]
	payload[43] = im0.SWRevision[2]
	binary.BigEndian.PutUint16(payload[44:46], im0.RevisionCounter)
	binary.BigEndian.PutUint16(payload[46:48], im0.ProfileID)
	binary.BigEndian.PutUint16(payload[48:50], im0.ProfileSpecType)
	binary.BigEndian.PutUint16(payload[50:52], im0.IMVersion)
	binary.BigEndian.PutUint16(payload[52:54], im0.IMSupported)
	return MarshalPNIOBlock(BlockTypeIM0, 1, 0, payload)
}

// PNIOStatus represents a PNIO error status.
type PNIOStatus struct {
	ErrorCode  uint8
	ErrorDecode uint8
	ErrorCode1 uint8
	ErrorCode2 uint8
}

// PNIOStatusOK is the success status.
var PNIOStatusOK = PNIOStatus{0, 0, 0, 0}

// Marshal serializes a PNIOStatus to 4 bytes.
func (s PNIOStatus) Marshal() []byte {
	return []byte{s.ErrorCode, s.ErrorDecode, s.ErrorCode1, s.ErrorCode2}
}

// IsOK returns true if the status indicates success.
func (s PNIOStatus) IsOK() bool {
	return s.ErrorCode == 0 && s.ErrorDecode == 0 && s.ErrorCode1 == 0 && s.ErrorCode2 == 0
}
