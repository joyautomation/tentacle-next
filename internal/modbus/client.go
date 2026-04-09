//go:build modbus || all

package modbus

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"
)

const (
	connectTimeout = 5 * time.Second
	readTimeout    = 5 * time.Second
	writeTimeout   = 5 * time.Second
	maxReadBuf     = 4096
)

// connect establishes a TCP connection to the Modbus device.
func connect(d *DeviceState) error {
	addr := net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	conn, err := net.DialTimeout("tcp", addr, connectTimeout)
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	d.conn = conn
	d.readBuf = nil
	d.txnID = 0
	return nil
}

// disconnect closes the TCP connection.
func disconnect(d *DeviceState) {
	if d.conn != nil {
		_ = d.conn.Close()
		d.conn = nil
		d.readBuf = nil
	}
}

// nextTxnID returns the next transaction ID and increments it.
func nextTxnID(d *DeviceState) uint16 {
	id := d.txnID
	d.txnID++
	return id
}

// sendAndReceive sends a PDU and reads the MBAP-framed response.
// It handles fragmented TCP reads using an accumulation buffer.
func sendAndReceive(d *DeviceState, pdu []byte) (*MBAPFrame, error) {
	txnID := nextTxnID(d)
	frame := encodeMBAPRequest(txnID, d.UnitID, pdu)

	if err := d.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return nil, fmt.Errorf("set write deadline: %w", err)
	}
	if _, err := d.conn.Write(frame); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	// Read response with accumulation buffer for fragmented reads
	if err := d.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	for {
		// Try to parse a complete frame from existing buffer
		if len(d.readBuf) >= mbapHeaderSize {
			resp, consumed, err := decodeMBAPResponse(d.readBuf)
			if err == nil {
				d.readBuf = d.readBuf[consumed:]
				if isExceptionResponse(resp.FunctionCode) {
					excCode := byte(0)
					if len(resp.Data) > 0 {
						excCode = resp.Data[0]
					}
					return nil, fmt.Errorf("modbus exception FC%d: %s", resp.FunctionCode&0x7F, exceptionMessage(excCode))
				}
				return resp, nil
			}
			// If error is due to incomplete data, fall through to read more
		}

		// Read more data from the connection
		tmp := make([]byte, maxReadBuf)
		n, err := d.conn.Read(tmp)
		if err != nil {
			return nil, fmt.Errorf("read: %w", err)
		}
		d.readBuf = append(d.readBuf, tmp[:n]...)
	}
}

// readRegisters performs a Modbus read for FC03 or FC04 (holding/input registers).
// Returns the raw register bytes (2 bytes per register).
func readRegisters(d *DeviceState, fc byte, startAddr uint16, quantity uint16) ([]byte, error) {
	pdu := buildReadPDU(fc, startAddr, quantity)
	resp, err := sendAndReceive(d, pdu)
	if err != nil {
		return nil, err
	}
	if len(resp.Data) < 1 {
		return nil, fmt.Errorf("empty response data")
	}
	byteCount := int(resp.Data[0])
	if len(resp.Data) < 1+byteCount {
		return nil, fmt.Errorf("truncated response: expected %d bytes, got %d", byteCount, len(resp.Data)-1)
	}
	return resp.Data[1 : 1+byteCount], nil
}

// readCoils performs a Modbus read for FC01 or FC02 (coils/discrete inputs).
// Returns the raw coil bytes (packed bits).
func readCoils(d *DeviceState, fc byte, startAddr uint16, quantity uint16) ([]byte, error) {
	pdu := buildReadPDU(fc, startAddr, quantity)
	resp, err := sendAndReceive(d, pdu)
	if err != nil {
		return nil, err
	}
	if len(resp.Data) < 1 {
		return nil, fmt.Errorf("empty response data")
	}
	byteCount := int(resp.Data[0])
	if len(resp.Data) < 1+byteCount {
		return nil, fmt.Errorf("truncated response: expected %d bytes, got %d", byteCount, len(resp.Data)-1)
	}
	return resp.Data[1 : 1+byteCount], nil
}

// writeSingleCoil writes a single coil (FC05).
func writeSingleCoil(d *DeviceState, addr uint16, value bool) error {
	pdu := buildWriteSingleCoilPDU(addr, value)
	_, err := sendAndReceive(d, pdu)
	if err != nil {
		return fmt.Errorf("write single coil addr %d: %w", addr, err)
	}
	return nil
}

// writeSingleRegister writes a single register (FC06).
func writeSingleRegister(d *DeviceState, addr uint16, value uint16) error {
	pdu := buildWriteSingleRegisterPDU(addr, value)
	_, err := sendAndReceive(d, pdu)
	if err != nil {
		return fmt.Errorf("write single register addr %d: %w", addr, err)
	}
	return nil
}

// writeMultipleRegisters writes multiple registers (FC16).
func writeMultipleRegisters(d *DeviceState, addr uint16, data []byte) error {
	pdu := buildWriteMultipleRegistersPDU(addr, data)
	_, err := sendAndReceive(d, pdu)
	if err != nil {
		return fmt.Errorf("write multiple registers addr %d: %w", addr, err)
	}
	return nil
}

// readBlock reads a single ReadBlock from the device and decodes all contained tags.
// Returns a map of tagID -> decoded value.
func readBlock(d *DeviceState, block ReadBlock, log *slog.Logger) (map[string]interface{}, error) {
	results := make(map[string]interface{})

	if fcIsCoilOrDiscrete(block.FunctionCode) {
		data, err := readCoils(d, byte(block.FunctionCode), uint16(block.StartAddr), uint16(block.Count))
		if err != nil {
			return nil, err
		}
		for _, t := range block.Tags {
			bitOffset := t.Offset
			val := decodeBoolFromCoils(data, bitOffset)
			results[t.Tag.ID] = val
		}
	} else {
		data, err := readRegisters(d, byte(block.FunctionCode), uint16(block.StartAddr), uint16(block.Count))
		if err != nil {
			return nil, err
		}
		for _, t := range block.Tags {
			byteOrder := t.Tag.ByteOrder
			if byteOrder == "" {
				byteOrder = d.ByteOrder
			}
			if byteOrder == "" {
				byteOrder = "ABCD"
			}
			offsetBytes := t.Offset * 2
			val, err := decodeRegisters(data[offsetBytes:], t.Tag.Datatype, byteOrder)
			if err != nil {
				log.Warn("modbus: decode error", "tag", t.Tag.ID, "error", err)
				continue
			}
			results[t.Tag.ID] = val
		}
	}
	return results, nil
}
