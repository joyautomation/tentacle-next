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

// TimelineEntry records a state transition for the status timeline.
type TimelineEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	State     StoreForwardState `json:"-"`
	StateStr  string            `json:"state"`
}

// StoreForwardBuffer implements a circular buffer for Sparkplug DDATA messages
// when the primary host is offline.
type StoreForwardBuffer struct {
	mu sync.Mutex

	log       *slog.Logger
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

	// State timeline (last hour of transitions)
	timeline []TimelineEntry
}

// NewStoreForwardBuffer creates a new S&F buffer with the given limits.
func NewStoreForwardBuffer(maxRecords int, maxBytes int64, drainRate int, log *slog.Logger) *StoreForwardBuffer {
	if maxRecords <= 0 {
		maxRecords = 10000
	}
	if drainRate <= 0 {
		drainRate = 100
	}
	sf := &StoreForwardBuffer{
		log:       log,
		state:     SFOffline,
		records:   make([]BufferedRecord, 0, 256),
		maxCount:  maxRecords,
		maxBytes:  maxBytes,
		drainRate: drainRate,
	}
	// Seed timeline in offline/buffering state. The bridge reconciles to
	// online once the node publishes NBIRTH and (if configured) the primary
	// host reports online.
	sf.timeline = []TimelineEntry{{Timestamp: time.Now(), State: SFOffline, StateStr: "buffering"}}
	return sf
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
		sf.recordTimeline(SFDraining, "draining")
		sf.log.Info("mqtt: store-forward entering drain mode", "buffered", len(sf.records))
	} else {
		sf.state = SFOnline
		sf.recordTimeline(SFOnline, "online")
		sf.log.Info("mqtt: store-forward online")
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
	sf.recordTimeline(SFOffline, "buffering")
	sf.log.Info("mqtt: store-forward entering offline mode")
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
		sf.recordTimeline(SFOnline, "online")
		sf.log.Info("mqtt: store-forward drain complete, now online")
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

// recordTimeline appends a state transition and trims entries older than 1 hour.
// Must be called with sf.mu held.
func (sf *StoreForwardBuffer) recordTimeline(state StoreForwardState, stateStr string) {
	now := time.Now()
	sf.timeline = append(sf.timeline, TimelineEntry{Timestamp: now, State: state, StateStr: stateStr})
	// Trim entries older than 1 hour, but keep the last pre-cutoff entry
	// so we know the state at the left edge of the window.
	cutoff := now.Add(-1 * time.Hour)
	for len(sf.timeline) > 2 && sf.timeline[0].Timestamp.Before(cutoff) && sf.timeline[1].Timestamp.Before(cutoff) {
		sf.timeline = sf.timeline[1:]
	}
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

// TimelinePoint is the JSON-friendly representation of a timeline entry.
type TimelinePoint struct {
	Timestamp string `json:"timestamp"`
	State     string `json:"state"`
}

// StoreForwardStatus is the JSON-friendly status snapshot.
type StoreForwardStatus struct {
	State             string          `json:"state"`
	PrimaryHostID     string          `json:"primaryHostId,omitempty"`
	PrimaryHostOnline bool            `json:"primaryHostOnline"`
	BufferedCount     int             `json:"bufferedCount"`
	MaxRecords        int             `json:"maxRecords"`
	UsagePercent      float64         `json:"usagePercent"`
	Draining          bool            `json:"draining"`
	DrainETA          float64         `json:"drainEtaSeconds"`
	TotalBuffered     int64           `json:"totalBuffered"`
	TotalDrained      int64           `json:"totalDrained"`
	TotalEvicted      int64           `json:"totalEvicted"`
	PublishRate       float64         `json:"publishRate"`
	Timeline          []TimelinePoint `json:"timeline"`
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

	// Compute publish rate inline (same logic as PublishRate method, but already locked)
	now := time.Now()
	cutoff := now.Add(-10 * time.Second)
	for len(sf.publishTimes) > 0 && sf.publishTimes[0].Before(cutoff) {
		sf.publishTimes = sf.publishTimes[1:]
	}
	rate := 0.0
	if len(sf.publishTimes) > 0 {
		rate = float64(len(sf.publishTimes)) / 10.0
	}

	// Build timeline points for the last hour.
	// Keep the most recent entry before the window so we know the state at the left edge.
	hourAgo := now.Add(-1 * time.Hour)
	var tlPoints []TimelinePoint
	var lastBeforeWindow *TimelineEntry
	for i := range sf.timeline {
		te := &sf.timeline[i]
		if te.Timestamp.Before(hourAgo) {
			lastBeforeWindow = te
		} else {
			tlPoints = append(tlPoints, TimelinePoint{
				Timestamp: te.Timestamp.Format(time.RFC3339Nano),
				State:     te.StateStr,
			})
		}
	}
	// Prepend the pre-window entry (clamped to hourAgo) so the bar fills from the left edge.
	if lastBeforeWindow != nil {
		tlPoints = append([]TimelinePoint{{
			Timestamp: hourAgo.Format(time.RFC3339Nano),
			State:     lastBeforeWindow.StateStr,
		}}, tlPoints...)
	}
	// Fallback: at least include the most recent state
	if len(tlPoints) == 0 && len(sf.timeline) > 0 {
		last := sf.timeline[len(sf.timeline)-1]
		tlPoints = []TimelinePoint{{
			Timestamp: last.Timestamp.Format(time.RFC3339Nano),
			State:     last.StateStr,
		}}
	}

	return StoreForwardStatus{
		State:             stateStr,
		PrimaryHostOnline: sf.state != SFOffline,
		BufferedCount:     len(sf.records),
		MaxRecords:        sf.maxCount,
		UsagePercent:      usage,
		Draining:          sf.state == SFDraining,
		DrainETA:          eta,
		TotalBuffered:     sf.totalBuffered,
		TotalDrained:      sf.totalDrained,
		TotalEvicted:      sf.totalEvicted,
		PublishRate:        rate,
		Timeline:          tlPoints,
	}
}
