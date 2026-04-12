//go:build profinetcontroller || all

// Package profinetcontroller implements a PROFINET IO Controller that discovers
// and polls PROFINET IO Devices, mapping their cyclic I/O data to Tentacle tags.
package profinetcontroller

import (
	"net"
	"sync"
)

// SubscribeRequest asks the controller to connect to a PROFINET device.
type SubscribeRequest struct {
	SubscriberID  string             `json:"subscriberId"`
	DeviceID      string             `json:"deviceId"`      // Tentacle device identifier
	StationName   string             `json:"stationName"`   // PROFINET station name (for DCP discovery)
	IP            string             `json:"ip,omitempty"`  // Direct IP (skip DCP if set)
	InterfaceName string             `json:"interfaceName"` // Network interface for raw L2
	VendorID      uint16             `json:"vendorId,omitempty"`
	DeviceIDPN    uint16             `json:"deviceIdPn,omitempty"`
	CycleTimeMs   int                `json:"cycleTimeMs,omitempty"` // Desired cycle time (default 1ms)
	Slots         []SlotSubscription `json:"slots"`
}

// SlotSubscription describes a module slot to read from a device.
type SlotSubscription struct {
	SlotNumber    uint16                `json:"slotNumber"`
	ModuleIdentNo uint32               `json:"moduleIdentNo"`
	Subslots      []SubslotSubscription `json:"subslots"`
}

// SubslotSubscription describes a submodule and its I/O data.
type SubslotSubscription struct {
	SubslotNumber    uint16          `json:"subslotNumber"`
	SubmoduleIdentNo uint32          `json:"submoduleIdentNo"`
	InputSize        uint16          `json:"inputSize"`
	OutputSize       uint16          `json:"outputSize"`
	Tags             []ControllerTag `json:"tags"`
}

// ControllerTag maps a PROFINET I/O byte position to a Tentacle tag.
type ControllerTag struct {
	TagID      string `json:"tagId"`
	ByteOffset uint16 `json:"byteOffset"`
	BitOffset  uint8  `json:"bitOffset,omitempty"`
	Datatype   string `json:"datatype"`
	Direction  string `json:"direction"` // "input" = from device, "output" = to device
}

// UnsubscribeRequest removes a subscription.
type UnsubscribeRequest struct {
	SubscriberID string   `json:"subscriberId"`
	DeviceID     string   `json:"deviceId"`
	TagIDs       []string `json:"tagIds,omitempty"` // nil = all tags
}

// DiscoveredDevice holds information from a DCP Identify response.
type DiscoveredDevice struct {
	MAC         net.HardwareAddr
	StationName string
	VendorID    uint16
	DeviceID    uint16
	IP          net.IP
	Mask        net.IP
	Gateway     net.IP
}

// DeviceState tracks a connected PROFINET device.
type DeviceState struct {
	DeviceID      string
	StationName   string
	IP            net.IP
	MAC           net.HardwareAddr
	InterfaceName string
	VendorID      uint16
	DeviceIDPN    uint16
	CycleTimeMs   int

	Subscribers map[string]*Subscriber
	Slots       []SlotSubscription

	// Connection state
	ar       *ControllerAR
	cyclic   *ControllerCyclic
	rpc      *RPCClient
	stopChan chan struct{}
	stopped  bool
	failures int

	// Tag tracking
	lastValues map[string]interface{}
	allTags    map[string]ControllerTag // tagID -> tag config

	mu sync.Mutex
}

// Subscriber tracks who wants data from this device.
type Subscriber struct {
	SubscriberID string
	Tags         map[string]ControllerTag
}

// ConnectParams holds parameters for the RPC Connect request.
type ConnectParams struct {
	ARUUID        [16]byte
	LocalMAC      net.HardwareAddr
	StationName   string
	InputDataLen  uint16
	OutputDataLen uint16
	OutputFrameID uint16 // Assigned by controller
	CycleTimeMs   int
	Slots         []SlotSubscription
}

// ConnectResult holds the response from a successful Connect.
type ConnectResult struct {
	InputFrameID uint16 // Assigned by device
	SessionKey   uint16
}
