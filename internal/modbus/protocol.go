//go:build modbus || all

package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MBAP header is 7 bytes: TransactionID(2) + ProtocolID(2) + Length(2) + UnitID(1)
const mbapHeaderSize = 7

// Modbus function codes.
const (
	fcReadCoils            = 1
	fcReadDiscreteInputs   = 2
	fcReadHoldingRegisters = 3
	fcReadInputRegisters   = 4
	fcWriteSingleCoil      = 5
	fcWriteSingleRegister  = 6
	fcWriteMultipleCoils   = 15
	fcWriteMultipleRegisters = 16
)

// MBAPFrame holds a decoded Modbus TCP frame.
type MBAPFrame struct {
	TransactionID uint16
	ProtocolID    uint16
	UnitID        byte
	FunctionCode  byte
	Data          []byte
}

// encodeMBAPRequest builds a Modbus TCP request frame.
// pdu is the Protocol Data Unit (function code + payload).
func encodeMBAPRequest(txnID uint16, unitID byte, pdu []byte) []byte {
	length := uint16(len(pdu) + 1) // PDU + unitID
	frame := make([]byte, mbapHeaderSize+len(pdu))
	binary.BigEndian.PutUint16(frame[0:2], txnID)
	binary.BigEndian.PutUint16(frame[2:4], 0) // protocol ID = 0 (Modbus)
	binary.BigEndian.PutUint16(frame[4:6], length)
	frame[6] = unitID
	copy(frame[7:], pdu)
	return frame
}

// decodeMBAPResponse parses a Modbus TCP response from a byte buffer.
// Returns the frame and the number of bytes consumed, or an error.
func decodeMBAPResponse(buf []byte) (*MBAPFrame, int, error) {
	if len(buf) < mbapHeaderSize {
		return nil, 0, fmt.Errorf("incomplete MBAP header: have %d bytes", len(buf))
	}

	txnID := binary.BigEndian.Uint16(buf[0:2])
	protoID := binary.BigEndian.Uint16(buf[2:4])
	length := binary.BigEndian.Uint16(buf[4:6])
	unitID := buf[6]

	totalLen := mbapHeaderSize + int(length) - 1 // -1 because unitID is counted in length
	if len(buf) < totalLen {
		return nil, 0, fmt.Errorf("incomplete frame: need %d bytes, have %d", totalLen, len(buf))
	}

	if int(length) < 2 {
		return nil, totalLen, errors.New("invalid MBAP length")
	}

	fc := buf[7]
	data := make([]byte, int(length)-2) // length - unitID(1) - fc(1)
	copy(data, buf[8:totalLen])

	return &MBAPFrame{
		TransactionID: txnID,
		ProtocolID:    protoID,
		UnitID:        unitID,
		FunctionCode:  fc,
		Data:          data,
	}, totalLen, nil
}

// buildReadPDU creates a read request PDU for FC01-FC04.
func buildReadPDU(fc byte, startAddr uint16, quantity uint16) []byte {
	pdu := make([]byte, 5)
	pdu[0] = fc
	binary.BigEndian.PutUint16(pdu[1:3], startAddr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	return pdu
}

// buildWriteSingleCoilPDU creates a FC05 request PDU.
func buildWriteSingleCoilPDU(addr uint16, value bool) []byte {
	pdu := make([]byte, 5)
	pdu[0] = fcWriteSingleCoil
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	if value {
		pdu[3] = 0xFF
		pdu[4] = 0x00
	}
	return pdu
}

// buildWriteSingleRegisterPDU creates a FC06 request PDU.
func buildWriteSingleRegisterPDU(addr uint16, value uint16) []byte {
	pdu := make([]byte, 5)
	pdu[0] = fcWriteSingleRegister
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], value)
	return pdu
}

// buildWriteMultipleRegistersPDU creates a FC16 request PDU.
func buildWriteMultipleRegistersPDU(addr uint16, values []byte) []byte {
	regCount := len(values) / 2
	pdu := make([]byte, 6+len(values))
	pdu[0] = fcWriteMultipleRegisters
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], uint16(regCount))
	pdu[5] = byte(len(values))
	copy(pdu[6:], values)
	return pdu
}

// isExceptionResponse checks if the function code indicates an exception.
func isExceptionResponse(fc byte) bool {
	return fc&0x80 != 0
}

// exceptionMessage returns a human-readable message for a Modbus exception code.
func exceptionMessage(code byte) string {
	switch code {
	case 1:
		return "illegal function"
	case 2:
		return "illegal data address"
	case 3:
		return "illegal data value"
	case 4:
		return "server device failure"
	case 5:
		return "acknowledge"
	case 6:
		return "server device busy"
	default:
		return fmt.Sprintf("unknown exception code %d", code)
	}
}

// fcNameToCode converts a function code name string to its numeric code.
func fcNameToCode(name string) int {
	switch name {
	case "coil":
		return fcReadCoils
	case "discrete":
		return fcReadDiscreteInputs
	case "holding":
		return fcReadHoldingRegisters
	case "input":
		return fcReadInputRegisters
	default:
		return fcReadHoldingRegisters
	}
}

// fcIsCoilOrDiscrete returns true if the function code reads bit values.
func fcIsCoilOrDiscrete(fc int) bool {
	return fc == fcReadCoils || fc == fcReadDiscreteInputs
}
