//go:build modbusserver || all

package modbusserver

import (
	"github.com/joyautomation/tentacle/internal/bus"
)

// VirtualDevice holds the state for a single virtual Modbus device:
// its register store, TCP server, and the bus subscription feeding it data.
type VirtualDevice struct {
	DeviceID       string
	Store          *RegisterStore
	Server         *TCPServer
	SourceModuleID string
	DataSub        bus.Subscription
}

// WriteEvent is emitted when a Modbus client writes to a register or coil.
// The manager uses it to publish the decoded value back to the bus.
type WriteEvent struct {
	FunctionCode string // "coil", "discrete", "holding", "input"
	Address      int
	Value        interface{} // decoded typed value (number or bool)
	VariableID   string
}
