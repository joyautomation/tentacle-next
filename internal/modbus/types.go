//go:build modbus || all

package modbus

import (
	"net"
	"sync"
	"time"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// ReadBlock represents a contiguous range of registers to read in a single request.
type ReadBlock struct {
	FunctionCode int
	StartAddr    int
	Count        int // number of registers (or coils/discretes)
	Tags         []TagInBlock
}

// TagInBlock maps a tag to its position within a ReadBlock.
type TagInBlock struct {
	Tag    itypes.ModbusTagConfig
	Offset int // register offset from block start
}

// Subscriber tracks a single subscriber's interest in a device.
type Subscriber struct {
	SubscriberID string
	ScanRate     int // milliseconds
	Tags         map[string]itypes.ModbusTagConfig // tagID -> config
}

// DeviceState holds the runtime state for a single Modbus device.
type DeviceState struct {
	mu          sync.Mutex
	DeviceID    string
	Host        string
	Port        int
	UnitID      byte
	ByteOrder   string // device-level default
	Subscribers map[string]*Subscriber // subscriberID -> subscriber

	conn       net.Conn
	txnID      uint16
	failures   int
	lastValues map[string]interface{} // tagID -> last published value

	stopChan chan struct{}
	stopped  bool

	// Merged read plan from all subscribers.
	allTags map[string]itypes.ModbusTagConfig // tagID -> config
	blocks  []ReadBlock

	// accumulation buffer for fragmented TCP reads
	readBuf []byte
}

// effectiveScanRate returns the fastest (lowest) scan rate across all subscribers.
func (d *DeviceState) effectiveScanRate() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	min := 0
	for _, sub := range d.Subscribers {
		if min == 0 || sub.ScanRate < min {
			min = sub.ScanRate
		}
	}
	if min <= 0 {
		min = 1000
	}
	return time.Duration(min) * time.Millisecond
}

// backoffDuration returns the exponential backoff duration based on failure count.
// Formula: 2^failures * 1000ms, capped at 60s.
func (d *DeviceState) backoffDuration() time.Duration {
	if d.failures <= 0 {
		return 0
	}
	ms := 1000 << d.failures // 2^failures * 1000
	if ms > 60000 {
		ms = 60000
	}
	return time.Duration(ms) * time.Millisecond
}
