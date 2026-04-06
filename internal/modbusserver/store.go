//go:build modbusserver || all

package modbusserver

import (
	"encoding/binary"
	"math"
	"sync"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// RegisterStore is a virtual Modbus device with four register spaces:
// coils, discrete inputs, holding registers, and input registers.
// Thread-safe for concurrent TCP reads and bus-driven writes.
type RegisterStore struct {
	mu        sync.RWMutex
	coils     map[int]bool   // coil address -> value
	discretes map[int]bool   // discrete input address -> value
	holding   map[int]uint16 // holding register address -> uint16 word
	input     map[int]uint16 // input register address -> uint16 word

	tagsByVariable map[string]itypes.ModbusTagConfig // variableId -> tag config
	tagsByAddress  map[string]itypes.ModbusTagConfig // "fc:address" -> tag config
}

// NewRegisterStore creates an empty register store.
func NewRegisterStore() *RegisterStore {
	return &RegisterStore{
		coils:          make(map[int]bool),
		discretes:      make(map[int]bool),
		holding:        make(map[int]uint16),
		input:          make(map[int]uint16),
		tagsByVariable: make(map[string]itypes.ModbusTagConfig),
		tagsByAddress:  make(map[string]itypes.ModbusTagConfig),
	}
}

// RegisterTag adds a tag config so the store knows how to encode/decode values
// at a given address.
func (s *RegisterStore) RegisterTag(tag itypes.ModbusTagConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tagsByVariable[tag.ID] = tag
	key := fcAddressKey(tag.FunctionCode, tag.Address)
	s.tagsByAddress[key] = tag
}

// UpdateFromVariable encodes a PLC variable value into the appropriate
// register or coil space.
func (s *RegisterStore) UpdateFromVariable(variableID string, value interface{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tag, ok := s.tagsByVariable[variableID]
	if !ok {
		return false
	}

	byteOrder := tag.ByteOrder
	if byteOrder == "" {
		byteOrder = "ABCD"
	}

	fc := tag.FunctionCode
	if fc == "coil" || fc == "discrete" {
		boolVal := toBool(value)
		if fc == "coil" {
			s.coils[tag.Address] = boolVal
		} else {
			s.discretes[tag.Address] = boolVal
		}
	} else {
		numVal := toFloat64(value)
		words := encodeToRegisters(numVal, tag.Datatype, byteOrder)
		store := s.holding
		if fc == "input" {
			store = s.input
		}
		for i, w := range words {
			store[tag.Address+i] = w
		}
	}
	return true
}

// ReadTypedValue decodes the register/coil value at the given function code
// and address back into a Go-typed value. Returns nil if no tag is mapped
// to that address.
func (s *RegisterStore) ReadTypedValue(fc string, address int) *ReadResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fcAddressKey(fc, address)
	tag, ok := s.tagsByAddress[key]
	if !ok {
		return nil
	}

	byteOrder := tag.ByteOrder
	if byteOrder == "" {
		byteOrder = "ABCD"
	}

	if fc == "coil" || fc == "discrete" {
		store := s.coils
		if fc == "discrete" {
			store = s.discretes
		}
		return &ReadResult{
			VariableID: tag.ID,
			Value:      store[address],
			Writable:   tag.Writable,
		}
	}

	store := s.holding
	if fc == "input" {
		store = s.input
	}
	count := registerCount(tag.Datatype)
	words := make([]uint16, count)
	for i := 0; i < count; i++ {
		words[i] = store[address+i]
	}

	return &ReadResult{
		VariableID: tag.ID,
		Value:      decodeFromRegisters(words, tag.Datatype, byteOrder),
		Writable:   tag.Writable,
	}
}

// ReadCoil returns the boolean value of a coil.
func (s *RegisterStore) ReadCoil(address int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.coils[address]
}

// ReadDiscrete returns the boolean value of a discrete input.
func (s *RegisterStore) ReadDiscrete(address int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.discretes[address]
}

// ReadHolding returns the uint16 value of a holding register.
func (s *RegisterStore) ReadHolding(address int) uint16 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.holding[address]
}

// ReadInput returns the uint16 value of an input register.
func (s *RegisterStore) ReadInput(address int) uint16 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.input[address]
}

// WriteCoil sets a coil value directly (from a Modbus client write).
func (s *RegisterStore) WriteCoil(address int, value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.coils[address] = value
}

// WriteHolding sets a holding register value directly (from a Modbus client write).
func (s *RegisterStore) WriteHolding(address int, value uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.holding[address] = value
}

// ReadResult is the decoded value at a register address.
type ReadResult struct {
	VariableID string
	Value      interface{}
	Writable   bool
}

// ---- encoding / decoding helpers ----

// registerCount returns how many 16-bit registers a datatype occupies.
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

// encodeToRegisters converts a numeric value into uint16 register words
// using the specified byte order.
func encodeToRegisters(value float64, datatype string, byteOrder string) []uint16 {
	count := registerCount(datatype)
	raw := make([]byte, count*2)

	switch datatype {
	case "boolean":
		if value != 0 {
			binary.BigEndian.PutUint16(raw, 1)
		} else {
			binary.BigEndian.PutUint16(raw, 0)
		}
	case "int16":
		binary.BigEndian.PutUint16(raw, uint16(int16(value)))
	case "uint16":
		binary.BigEndian.PutUint16(raw, uint16(value))
	case "int32":
		binary.BigEndian.PutUint32(raw, uint32(int32(value)))
	case "uint32":
		binary.BigEndian.PutUint32(raw, uint32(value))
	case "float32":
		binary.BigEndian.PutUint32(raw, math.Float32bits(float32(value)))
	case "float64":
		binary.BigEndian.PutUint64(raw, math.Float64bits(value))
	default:
		binary.BigEndian.PutUint16(raw, uint16(value))
	}

	// Convert bytes to uint16 words (big-endian)
	words := make([]uint16, count)
	for i := 0; i < count; i++ {
		words[i] = binary.BigEndian.Uint16(raw[i*2 : i*2+2])
	}

	return arrangeWords(words, byteOrder)
}

// decodeFromRegisters converts uint16 register words back into a typed value.
func decodeFromRegisters(words []uint16, datatype string, byteOrder string) interface{} {
	// Undo byte-order arrangement (all transforms are self-inverse)
	arranged := arrangeWords(words, byteOrder)

	raw := make([]byte, len(arranged)*2)
	for i, w := range arranged {
		binary.BigEndian.PutUint16(raw[i*2:i*2+2], w)
	}

	switch datatype {
	case "boolean":
		return words[0] != 0
	case "int16":
		return float64(int16(binary.BigEndian.Uint16(raw)))
	case "uint16":
		return float64(binary.BigEndian.Uint16(raw))
	case "int32":
		return float64(int32(binary.BigEndian.Uint32(raw)))
	case "uint32":
		return float64(binary.BigEndian.Uint32(raw))
	case "float32":
		return float64(math.Float32frombits(binary.BigEndian.Uint32(raw)))
	case "float64":
		return math.Float64frombits(binary.BigEndian.Uint64(raw))
	default:
		return float64(binary.BigEndian.Uint16(raw))
	}
}

// arrangeWords reorders uint16 words according to the byte order.
// All transformations are self-inverse (applying twice = identity).
func arrangeWords(words []uint16, byteOrder string) []uint16 {
	if byteOrder == "ABCD" || len(words) == 0 {
		return words
	}

	// Flatten words to bytes
	raw := make([]byte, len(words)*2)
	for i, w := range words {
		raw[i*2] = byte(w >> 8)
		raw[i*2+1] = byte(w & 0xff)
	}

	var arranged []byte
	switch byteOrder {
	case "DCBA":
		arranged = make([]byte, len(raw))
		for i, j := 0, len(raw)-1; i < len(raw); i, j = i+1, j-1 {
			arranged[i] = raw[j]
		}
	case "BADC":
		arranged = make([]byte, len(raw))
		for i := 0; i < len(raw); i += 2 {
			arranged[i] = raw[i+1]
			arranged[i+1] = raw[i]
		}
	case "CDAB":
		arranged = make([]byte, len(raw))
		for i := 0; i+3 < len(raw); i += 4 {
			arranged[i] = raw[i+2]
			arranged[i+1] = raw[i+3]
			arranged[i+2] = raw[i]
			arranged[i+3] = raw[i+1]
		}
		// Handle remaining bytes that don't fill a 4-byte group (e.g. 2-byte types)
		remainder := len(raw) % 4
		if remainder > 0 {
			start := len(raw) - remainder
			copy(arranged[start:], raw[start:])
		}
	default:
		return words
	}

	result := make([]uint16, len(words))
	for i := range result {
		result[i] = uint16(arranged[i*2])<<8 | uint16(arranged[i*2+1])
	}
	return result
}

// fcAddressKey builds a map key from function code name and address.
func fcAddressKey(fc string, address int) string {
	// Use a simple format for fast lookup
	buf := make([]byte, 0, len(fc)+12)
	buf = append(buf, fc...)
	buf = append(buf, ':')
	buf = append(buf, intToString(address)...)
	return string(buf)
}

// intToString is a minimal int-to-string without importing strconv.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// toBool converts an interface{} to a boolean.
func toBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case float32:
		return val != 0
	case int:
		return val != 0
	case int64:
		return val != 0
	case int32:
		return val != 0
	case uint64:
		return val != 0
	default:
		return false
	}
}

// toFloat64 converts a numeric interface{} to float64.
func toFloat64(v interface{}) float64 {
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
