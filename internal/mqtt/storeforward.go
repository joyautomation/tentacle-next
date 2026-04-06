//go:build mqtt || all

package mqtt

import (
	"log/slog"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/sparkplug"
)

// StoreForwardState represents the current S&F mode.
type StoreForwardState int

const (
	SFOnline   StoreForwardState = iota // Primary host UP — publish directly
	SFOffline                           // Primary host DOWN — buffer data
	SFDraining                          // Primary host back UP — replaying buffer
)

// BufferedRecord holds a single DDATA payload buffered during offline mode.
type BufferedRecord struct {
	DeviceID  string
	Payload   *sparkplug.Payload
	Timestamp time.Time
}

// StoreForwardBuffer implements a circular buffer for Sparkplug DDATA messages
// when the primary host is offline.
type StoreForwardBuffer struct {
	mu sync.Mutex

	state     StoreForwardState
	records   []BufferedRecord
	maxCount  int
	maxBytes  int64
	drainRate int // records per second

	// Stats
	totalBuffered int64
	totalDrained  int64
	totalEvicted  int64

	// Publish rate tracking
	publishTimes []time.Time
}

// NewStoreForwardBuffer creates a new S&F buffer with the given limits.
func NewStoreForwardBuffer(maxRecords int, maxBytes int64, drainRate int) *StoreForwardBuffer {
	if maxRecords <= 0 {
		maxRecords = 10000
	}
	if drainRate <= 0 {
		drainRate = 100
	}
	return &StoreForwardBuffer{
		state:     SFOnline,
		records:   make([]BufferedRecord, 0, 256),
		maxCount:  maxRecords,
		maxBytes:  maxBytes,
		drainRate: drainRate,
	}
}

// SetOnline transitions to online mode and starts draining if there are buffered records.
func (sf *StoreForwardBuffer) SetOnline() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if sf.state == SFOnline {
		return
	}

	if len(sf.records) > 0 {
		sf.state = SFDraining
		slog.Info("mqtt: store-forward entering drain mode", "buffered", len(sf.records))
	} else {
		sf.state = SFOnline
		slog.Info("mqtt: store-forward online")
	}
}

// SetOffline transitions to offline mode (primary host is down).
func (sf *StoreForwardBuffer) SetOffline() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if sf.state == SFOffline {
		return
	}
	sf.state = SFOffline
	slog.Info("mqtt: store-forward entering offline mode")
}

// State returns the current S&F state.
func (sf *StoreForwardBuffer) State() StoreForwardState {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	return sf.state
}

// Buffer adds a record to the circular buffer. Evicts oldest if full.
func (sf *StoreForwardBuffer) Buffer(deviceID string, payload *sparkplug.Payload) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	rec := BufferedRecord{
		DeviceID:  deviceID,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	// Evict if at capacity
	for len(sf.records) >= sf.maxCount {
		sf.records = sf.records[1:]
		sf.totalEvicted++
	}

	sf.records = append(sf.records, rec)
	sf.totalBuffered++
}

// Drain returns up to drainRate records and marks them as historical.
// Returns nil when buffer is empty (and transitions to online).
func (sf *StoreForwardBuffer) Drain() []BufferedRecord {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if sf.state != SFDraining {
		return nil
	}

	if len(sf.records) == 0 {
		sf.state = SFOnline
		slog.Info("mqtt: store-forward drain complete, now online")
		return nil
	}

	count := sf.drainRate
	if count > len(sf.records) {
		count = len(sf.records)
	}

	batch := make([]BufferedRecord, count)
	copy(batch, sf.records[:count])
	sf.records = sf.records[count:]
	sf.totalDrained += int64(count)

	// Mark all metrics as historical
	for i := range batch {
		if batch[i].Payload != nil {
			for j := range batch[i].Payload.Metrics {
				batch[i].Payload.Metrics[j].IsHistorical = true
			}
		}
	}

	return batch
}

// BufferedCount returns the number of records currently buffered.
func (sf *StoreForwardBuffer) BufferedCount() int {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	return len(sf.records)
}

// RecordPublish tracks a publish event for rate calculation.
func (sf *StoreForwardBuffer) RecordPublish(metricCount int) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	now := time.Now()
	for i := 0; i < metricCount; i++ {
		sf.publishTimes = append(sf.publishTimes, now)
	}

	// Trim to 10-second sliding window
	cutoff := now.Add(-10 * time.Second)
	for len(sf.publishTimes) > 0 && sf.publishTimes[0].Before(cutoff) {
		sf.publishTimes = sf.publishTimes[1:]
	}
}

// PublishRate returns the current publish rate in metrics/second.
func (sf *StoreForwardBuffer) PublishRate() float64 {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-10 * time.Second)
	for len(sf.publishTimes) > 0 && sf.publishTimes[0].Before(cutoff) {
		sf.publishTimes = sf.publishTimes[1:]
	}

	if len(sf.publishTimes) == 0 {
		return 0
	}
	return float64(len(sf.publishTimes)) / 10.0
}

// Status returns a JSON-friendly status snapshot.
type StoreForwardStatus struct {
	State         string  `json:"state"`
	BufferedCount int     `json:"bufferedCount"`
	MaxRecords    int     `json:"maxRecords"`
	UsagePercent  float64 `json:"usagePercent"`
	TotalBuffered int64   `json:"totalBuffered"`
	TotalDrained  int64   `json:"totalDrained"`
	TotalEvicted  int64   `json:"totalEvicted"`
	PublishRate   float64 `json:"publishRate"`
	DrainETA      float64 `json:"drainEtaSeconds"`
}

func (sf *StoreForwardBuffer) Status() StoreForwardStatus {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	stateStr := "online"
	switch sf.state {
	case SFOffline:
		stateStr = "offline"
	case SFDraining:
		stateStr = "draining"
	}

	usage := 0.0
	if sf.maxCount > 0 {
		usage = float64(len(sf.records)) / float64(sf.maxCount) * 100
	}

	eta := 0.0
	if sf.state == SFDraining && sf.drainRate > 0 {
		eta = float64(len(sf.records)) / float64(sf.drainRate)
	}

	return StoreForwardStatus{
		State:         stateStr,
		BufferedCount: len(sf.records),
		MaxRecords:    sf.maxCount,
		UsagePercent:  usage,
		TotalBuffered: sf.totalBuffered,
		TotalDrained:  sf.totalDrained,
		TotalEvicted:  sf.totalEvicted,
		DrainETA:      eta,
	}
}
