//go:build history || all

package history

import (
	"sync"

	"github.com/joyautomation/tentacle/internal/rbe"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

// batchBuffer accumulates history records and flushes them periodically or
// when the buffer reaches a threshold.
type batchBuffer struct {
	mu      sync.Mutex
	records []itypes.HistoryRecord
}

func newBatchBuffer() *batchBuffer {
	return &batchBuffer{
		records: make([]itypes.HistoryRecord, 0, batchMaxSize),
	}
}

// add appends a record to the buffer and returns true if the buffer has
// reached the flush threshold.
func (bb *batchBuffer) add(rec itypes.HistoryRecord) bool {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bb.records = append(bb.records, rec)
	return len(bb.records) >= batchMaxSize
}

// drain returns all buffered records and resets the buffer.
func (bb *batchBuffer) drain() []itypes.HistoryRecord {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	if len(bb.records) == 0 {
		return nil
	}
	out := bb.records
	bb.records = make([]itypes.HistoryRecord, 0, batchMaxSize)
	return out
}

// len returns the current number of buffered records.
func (bb *batchBuffer) len() int {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	return len(bb.records)
}

// variableRBEState tracks per-variable RBE state for deadband filtering.
type variableRBEState struct {
	mu    sync.RWMutex
	state map[string]*rbe.State // key: "moduleId:variableId"
}

func newVariableRBEState() *variableRBEState {
	return &variableRBEState{
		state: make(map[string]*rbe.State),
	}
}

// get returns the RBE state for a variable, creating it if needed.
func (v *variableRBEState) get(key string) *rbe.State {
	v.mu.RLock()
	s, ok := v.state[key]
	v.mu.RUnlock()
	if ok {
		return s
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	// Double-check after acquiring write lock.
	if s, ok := v.state[key]; ok {
		return s
	}
	s = &rbe.State{}
	v.state[key] = s
	return s
}

// historyStats tracks metrics for heartbeat metadata.
type historyStats struct {
	mu             sync.Mutex
	recordsWritten int64
	batchesWritten int64
	lastFlushTime  int64
}

func (s *historyStats) addBatch(count int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordsWritten += count
	s.batchesWritten++
}

func (s *historyStats) setLastFlush(ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFlushTime = ts
}

func (s *historyStats) snapshot() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]interface{}{
		"recordsWritten": s.recordsWritten,
		"batchesWritten": s.batchesWritten,
		"lastFlushTime":  s.lastFlushTime,
	}
}

// deadbandForMessage returns a DeadBandConfig pointer from a PlcDataMessage.
// Returns nil if no deadband is configured.
func deadbandForMessage(msg *types.PlcDataMessage) *types.DeadBandConfig {
	return msg.Deadband
}
