//go:build api || all

package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
)

const trafficRingSize = 200

// trafficEntry is a captured NATS message for the traffic viewer.
type trafficEntry struct {
	Timestamp string `json:"timestamp"`
	Subject   string `json:"subject"`
	Size      int    `json:"size"`
	Payload   string `json:"payload"`
}

// trafficCollector captures bus messages into a ring buffer and fans out to SSE listeners.
type trafficCollector struct {
	mu       sync.RWMutex
	ring     []trafficEntry
	head     int
	count    int
	sub      bus.Subscription
	// SSE listeners
	listMu    sync.Mutex
	listeners map[chan trafficEntry]struct{}
}

func newTrafficCollector() *trafficCollector {
	return &trafficCollector{
		ring:      make([]trafficEntry, trafficRingSize),
		listeners: make(map[chan trafficEntry]struct{}),
	}
}

// start subscribes to all bus subjects and begins collecting.
func (tc *trafficCollector) start(b bus.Bus) error {
	sub, err := b.Subscribe(">", func(subject string, data []byte, _ bus.ReplyFunc) {
		// Skip noisy internal subjects.
		if strings.HasPrefix(subject, "$KV.") ||
			strings.HasPrefix(subject, "$JS.") ||
			strings.HasPrefix(subject, "_INBOX.") {
			return
		}

		payload := decodePayload(data, 500)
		entry := trafficEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Subject:   subject,
			Size:      len(data),
			Payload:   payload,
		}

		// Add to ring buffer.
		tc.mu.Lock()
		tc.ring[tc.head] = entry
		tc.head = (tc.head + 1) % trafficRingSize
		if tc.count < trafficRingSize {
			tc.count++
		}
		tc.mu.Unlock()

		// Fan out to SSE listeners (non-blocking).
		tc.listMu.Lock()
		for ch := range tc.listeners {
			select {
			case ch <- entry:
			default:
				// Drop if listener is slow.
			}
		}
		tc.listMu.Unlock()
	})
	if err != nil {
		return err
	}
	tc.sub = sub
	return nil
}

func (tc *trafficCollector) stop() {
	if tc.sub != nil {
		tc.sub.Unsubscribe()
	}
}

// recent returns the last n entries (newest first).
func (tc *trafficCollector) recent(limit int) []trafficEntry {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	if limit <= 0 || limit > tc.count {
		limit = tc.count
	}
	result := make([]trafficEntry, limit)
	for i := 0; i < limit; i++ {
		idx := (tc.head - 1 - i + trafficRingSize) % trafficRingSize
		result[i] = tc.ring[idx]
	}
	return result
}

// addListener registers an SSE listener channel.
func (tc *trafficCollector) addListener(ch chan trafficEntry) {
	tc.listMu.Lock()
	tc.listeners[ch] = struct{}{}
	tc.listMu.Unlock()
}

// removeListener unregisters an SSE listener channel.
func (tc *trafficCollector) removeListener(ch chan trafficEntry) {
	tc.listMu.Lock()
	delete(tc.listeners, ch)
	tc.listMu.Unlock()
}

// decodePayload tries to decode bytes as UTF-8, truncates to max chars.
func decodePayload(data []byte, max int) string {
	s := string(data)
	// Check if it looks like valid text.
	for _, r := range s {
		if r == '\uFFFD' {
			return "<binary " + strconv.Itoa(len(data)) + " bytes>"
		}
	}
	if len(s) > max {
		return s[:max]
	}
	return s
}

// handleGetNatsTraffic returns recent traffic entries.
// GET /api/v1/nats/traffic
func (m *Module) handleGetNatsTraffic(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	entries := m.traffic.recent(limit)
	writeJSON(w, http.StatusOK, entries)
}

// handleStreamNatsTraffic streams NATS traffic entries via SSE.
// GET /api/v1/nats/traffic/stream
func (m *Module) handleStreamNatsTraffic(w http.ResponseWriter, r *http.Request) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ch := make(chan trafficEntry, 64)
	m.traffic.addListener(ch)
	defer m.traffic.removeListener(ch)

	for {
		select {
		case entry := <-ch:
			if err := sse.WriteJSON(entry); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

