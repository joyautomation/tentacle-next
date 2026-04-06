//go:build modbusserver || all

// Modbus TCP server — raw MBAP/PDU implementation using net.Listener.
// No external library; just TCP sockets and byte-level Modbus framing.
package modbusserver

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// WriteCallback is invoked when a Modbus client writes to a register or coil.
// fc is "coil" or "holding", address is the 0-based register/coil address.
type WriteCallback func(fc string, address int)

// TCPServer is a Modbus TCP server that serves registers from a RegisterStore.
type TCPServer struct {
	port     int
	unitID   int
	store    *RegisterStore
	onWrite  WriteCallback
	listener net.Listener

	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	stopped bool
}

// NewTCPServer creates a Modbus TCP server (not yet listening).
func NewTCPServer(port, unitID int, store *RegisterStore, onWrite WriteCallback) *TCPServer {
	return &TCPServer{
		port:    port,
		unitID:  unitID,
		store:   store,
		onWrite: onWrite,
		conns:   make(map[net.Conn]struct{}),
	}
}

// Start begins listening for Modbus TCP connections.
func (s *TCPServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return nil // already running
	}

	addr := fmt.Sprintf(":%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("modbusserver: listen on %s: %w", addr, err)
	}
	s.listener = ln
	s.stopped = false

	slog.Info("modbusserver: TCP server listening", "port", s.port, "unitId", s.unitID)

	go s.acceptLoop()
	return nil
}

// Stop closes all connections and the listener.
func (s *TCPServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener == nil {
		return
	}

	s.stopped = true
	_ = s.listener.Close()
	s.listener = nil

	for conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = make(map[net.Conn]struct{})

	slog.Info("modbusserver: TCP server stopped", "port", s.port)
}

// Port returns the port the server is configured to listen on.
func (s *TCPServer) Port() int {
	return s.port
}

func (s *TCPServer) acceptLoop() {
	for {
		s.mu.Lock()
		ln := s.listener
		stopped := s.stopped
		s.mu.Unlock()

		if stopped || ln == nil {
			return
		}

		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			stopped = s.stopped
			s.mu.Unlock()
			if !stopped {
				slog.Warn("modbusserver: accept error", "error", err)
			}
			return
		}

		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()

		go func(c net.Conn) {
			s.handleConnection(c)
			s.mu.Lock()
			delete(s.conns, c)
			s.mu.Unlock()
		}(conn)
	}
}

func (s *TCPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	var buf []byte
	chunk := make([]byte, 4096)

	for {
		n, err := conn.Read(chunk)
		if err != nil {
			return // connection closed or error
		}

		buf = append(buf, chunk[:n]...)

		// Process complete MBAP frames
		for len(buf) >= 7 {
			txID := binary.BigEndian.Uint16(buf[0:2])
			// protocolID := binary.BigEndian.Uint16(buf[2:4]) // always 0
			mbapLength := int(binary.BigEndian.Uint16(buf[4:6]))
			totalLen := 6 + mbapLength

			if len(buf) < totalLen {
				break // incomplete frame, wait for more data
			}

			reqUnitID := buf[6]
			pdu := make([]byte, totalLen-7)
			copy(pdu, buf[7:totalLen])
			buf = buf[totalLen:]

			// Only respond to our unit ID or broadcast (0)
			if int(reqUnitID) != s.unitID && reqUnitID != 0 {
				continue
			}

			response := s.handlePDU(pdu)

			// Build MBAP response frame
			frame := make([]byte, 7+len(response))
			binary.BigEndian.PutUint16(frame[0:2], txID)
			binary.BigEndian.PutUint16(frame[2:4], 0) // protocol ID
			binary.BigEndian.PutUint16(frame[4:6], uint16(1+len(response)))
			frame[6] = reqUnitID
			copy(frame[7:], response)

			if _, err := conn.Write(frame); err != nil {
				return
			}
		}
	}
}

func (s *TCPServer) handlePDU(pdu []byte) []byte {
	if len(pdu) == 0 {
		return []byte{0x81, 0x01} // illegal function
	}

	fc := pdu[0]
	switch fc {
	case 0x01:
		return s.handleReadBits(pdu, true)
	case 0x02:
		return s.handleReadBits(pdu, false)
	case 0x03:
		return s.handleReadWords(pdu, true)
	case 0x04:
		return s.handleReadWords(pdu, false)
	case 0x05:
		return s.handleWriteSingleCoil(pdu)
	case 0x06:
		return s.handleWriteSingleRegister(pdu)
	case 0x0F:
		return s.handleWriteMultipleCoils(pdu)
	case 0x10:
		return s.handleWriteMultipleRegisters(pdu)
	default:
		return []byte{fc | 0x80, 0x01} // illegal function
	}
}

// FC 0x01 (Read Coils) / FC 0x02 (Read Discrete Inputs)
func (s *TCPServer) handleReadBits(pdu []byte, isCoil bool) []byte {
	if len(pdu) < 5 {
		return []byte{pdu[0] | 0x80, 0x03} // illegal data value
	}

	fc := pdu[0]
	addr := int(binary.BigEndian.Uint16(pdu[1:3]))
	count := int(binary.BigEndian.Uint16(pdu[3:5]))
	byteCount := (count + 7) / 8

	resp := make([]byte, 2+byteCount)
	resp[0] = fc
	resp[1] = byte(byteCount)

	s.store.mu.RLock()
	for i := 0; i < count; i++ {
		var val bool
		if isCoil {
			val = s.store.coils[addr+i]
		} else {
			val = s.store.discretes[addr+i]
		}
		if val {
			resp[2+i/8] |= 1 << uint(i%8)
		}
	}
	s.store.mu.RUnlock()

	return resp
}

// FC 0x03 (Read Holding Registers) / FC 0x04 (Read Input Registers)
func (s *TCPServer) handleReadWords(pdu []byte, isHolding bool) []byte {
	if len(pdu) < 5 {
		return []byte{pdu[0] | 0x80, 0x03}
	}

	fc := pdu[0]
	addr := int(binary.BigEndian.Uint16(pdu[1:3]))
	count := int(binary.BigEndian.Uint16(pdu[3:5]))

	resp := make([]byte, 2+count*2)
	resp[0] = fc
	resp[1] = byte(count * 2)

	s.store.mu.RLock()
	for i := 0; i < count; i++ {
		var word uint16
		if isHolding {
			word = s.store.holding[addr+i]
		} else {
			word = s.store.input[addr+i]
		}
		binary.BigEndian.PutUint16(resp[2+i*2:4+i*2], word)
	}
	s.store.mu.RUnlock()

	return resp
}

// FC 0x05 (Write Single Coil)
func (s *TCPServer) handleWriteSingleCoil(pdu []byte) []byte {
	if len(pdu) < 5 {
		return []byte{0x85, 0x03}
	}

	addr := int(binary.BigEndian.Uint16(pdu[1:3]))
	raw := binary.BigEndian.Uint16(pdu[3:5])
	value := raw == 0xFF00

	s.store.WriteCoil(addr, value)

	if s.onWrite != nil {
		s.onWrite("coil", addr)
	}

	// Echo request as response
	resp := make([]byte, 5)
	resp[0] = 0x05
	binary.BigEndian.PutUint16(resp[1:3], uint16(addr))
	binary.BigEndian.PutUint16(resp[3:5], raw)
	return resp
}

// FC 0x06 (Write Single Register)
func (s *TCPServer) handleWriteSingleRegister(pdu []byte) []byte {
	if len(pdu) < 5 {
		return []byte{0x86, 0x03}
	}

	addr := int(binary.BigEndian.Uint16(pdu[1:3]))
	value := binary.BigEndian.Uint16(pdu[3:5])

	s.store.WriteHolding(addr, value)

	if s.onWrite != nil {
		s.onWrite("holding", addr)
	}

	// Echo request as response
	resp := make([]byte, 5)
	resp[0] = 0x06
	binary.BigEndian.PutUint16(resp[1:3], uint16(addr))
	binary.BigEndian.PutUint16(resp[3:5], value)
	return resp
}

// FC 0x0F (Write Multiple Coils)
func (s *TCPServer) handleWriteMultipleCoils(pdu []byte) []byte {
	if len(pdu) < 6 {
		return []byte{0x8F, 0x03}
	}

	addr := int(binary.BigEndian.Uint16(pdu[1:3]))
	count := int(binary.BigEndian.Uint16(pdu[3:5]))
	// pdu[5] = byte count
	// pdu[6..] = coil data bytes

	if len(pdu) < 6+int(pdu[5]) {
		return []byte{0x8F, 0x03}
	}

	s.store.mu.Lock()
	for i := 0; i < count; i++ {
		byteIdx := 6 + i/8
		if byteIdx >= len(pdu) {
			break
		}
		val := (pdu[byteIdx] >> uint(i%8)) & 1
		s.store.coils[addr+i] = val == 1
	}
	s.store.mu.Unlock()

	if s.onWrite != nil {
		for i := 0; i < count; i++ {
			s.onWrite("coil", addr+i)
		}
	}

	resp := make([]byte, 5)
	resp[0] = 0x0F
	binary.BigEndian.PutUint16(resp[1:3], uint16(addr))
	binary.BigEndian.PutUint16(resp[3:5], uint16(count))
	return resp
}

// FC 0x10 (Write Multiple Registers)
func (s *TCPServer) handleWriteMultipleRegisters(pdu []byte) []byte {
	if len(pdu) < 6 {
		return []byte{0x90, 0x03}
	}

	addr := int(binary.BigEndian.Uint16(pdu[1:3]))
	count := int(binary.BigEndian.Uint16(pdu[3:5]))
	// pdu[5] = byte count
	// pdu[6..] = register data

	needed := 6 + count*2
	if len(pdu) < needed {
		return []byte{0x90, 0x03}
	}

	s.store.mu.Lock()
	for i := 0; i < count; i++ {
		word := binary.BigEndian.Uint16(pdu[6+i*2 : 8+i*2])
		s.store.holding[addr+i] = word
	}
	s.store.mu.Unlock()

	if s.onWrite != nil {
		// Signal write at the start address (manager will decode the full typed value)
		s.onWrite("holding", addr)
	}

	resp := make([]byte, 5)
	resp[0] = 0x10
	binary.BigEndian.PutUint16(resp[1:3], uint16(addr))
	binary.BigEndian.PutUint16(resp[3:5], uint16(count))
	return resp
}
