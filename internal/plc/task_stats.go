//go:build plc || all

package plc

import (
	"sort"
	"sync"
	"time"
)

// scanStatsWindow is the number of recent scan durations kept in the
// ring buffer used to compute percentiles. At a 1 ms scan rate this is
// roughly one second of samples; at 100 ms it's ~100 s.
const scanStatsWindow = 1024

// scanStats is a per-task ring buffer of recent scan durations plus
// lifetime counters. Safe for concurrent access.
type scanStats struct {
	mu          sync.Mutex
	buf         [scanStatsWindow]time.Duration
	idx         int
	filled      int
	totalRuns   uint64
	totalErrors uint64
	lastRun     time.Time
	lastErr     string
	firstRun    time.Time
}

func newScanStats() *scanStats {
	return &scanStats{}
}

// record appends a scan duration and updates counters. If err is non-nil
// the error counter increments and the message is retained for display.
func (s *scanStats) record(d time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf[s.idx] = d
	s.idx = (s.idx + 1) % scanStatsWindow
	if s.filled < scanStatsWindow {
		s.filled++
	}
	s.totalRuns++
	now := time.Now()
	if s.firstRun.IsZero() {
		s.firstRun = now
	}
	s.lastRun = now
	if err != nil {
		s.totalErrors++
		s.lastErr = err.Error()
	}
}

// TaskStatsSnapshot is a JSON-serialisable snapshot of a task's runtime
// scan statistics.
type TaskStatsSnapshot struct {
	Samples       int     `json:"samples"`
	TotalRuns     uint64  `json:"totalRuns"`
	TotalErrors   uint64  `json:"totalErrors"`
	P50Us         float64 `json:"p50Us"`
	P95Us         float64 `json:"p95Us"`
	P99Us         float64 `json:"p99Us"`
	MinUs         float64 `json:"minUs"`
	MaxUs         float64 `json:"maxUs"`
	MeanUs        float64 `json:"meanUs"`
	LastUs        float64 `json:"lastUs"`
	LastRunMs     int64   `json:"lastRunMs"`
	LastError     string  `json:"lastError,omitempty"`
	EffectiveHz   float64 `json:"effectiveHz"`
}

func (s *scanStats) snapshot() TaskStatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := TaskStatsSnapshot{
		Samples:     s.filled,
		TotalRuns:   s.totalRuns,
		TotalErrors: s.totalErrors,
		LastError:   s.lastErr,
	}
	if !s.lastRun.IsZero() {
		snap.LastRunMs = s.lastRun.UnixMilli()
	}
	if s.filled == 0 {
		return snap
	}

	samples := make([]time.Duration, s.filled)
	copy(samples, s.buf[:s.filled])
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

	lastIdx := (s.idx - 1 + scanStatsWindow) % scanStatsWindow
	snap.LastUs = float64(s.buf[lastIdx]) / float64(time.Microsecond)
	snap.MinUs = float64(samples[0]) / float64(time.Microsecond)
	snap.MaxUs = float64(samples[len(samples)-1]) / float64(time.Microsecond)
	snap.P50Us = float64(samples[percentileIdx(len(samples), 0.50)]) / float64(time.Microsecond)
	snap.P95Us = float64(samples[percentileIdx(len(samples), 0.95)]) / float64(time.Microsecond)
	snap.P99Us = float64(samples[percentileIdx(len(samples), 0.99)]) / float64(time.Microsecond)

	var sum time.Duration
	for _, d := range samples {
		sum += d
	}
	snap.MeanUs = float64(sum) / float64(len(samples)) / float64(time.Microsecond)

	if !s.firstRun.IsZero() && s.totalRuns > 0 {
		elapsed := s.lastRun.Sub(s.firstRun).Seconds()
		if elapsed > 0 {
			snap.EffectiveHz = float64(s.totalRuns-1) / elapsed
		}
	}
	return snap
}

// percentileIdx returns the nearest-rank index for percentile p (0..1)
// in a sorted slice of length n. Matches the common "nearest rank"
// definition so percentiles are always existing samples.
func percentileIdx(n int, p float64) int {
	if n <= 0 {
		return 0
	}
	i := int(float64(n)*p + 0.5)
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	return i
}
