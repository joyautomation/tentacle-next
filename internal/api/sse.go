//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// sseWriter wraps an http.ResponseWriter for Server-Sent Events.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

// newSSEWriter initializes an SSE connection. Returns nil,false if streaming
// is not supported by the underlying ResponseWriter.
func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseWriter{w: w, flusher: flusher}, true
}

// WriteEvent sends a named SSE event with JSON-encoded data.
func (s *sseWriter) WriteEvent(event string, data interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if event != "" {
		fmt.Fprintf(s.w, "event: %s\n", event)
	}
	fmt.Fprintf(s.w, "data: %s\n\n", b)
	s.flusher.Flush()
	return nil
}

// WriteJSON sends an unnamed SSE event with JSON-encoded data.
func (s *sseWriter) WriteJSON(data interface{}) error {
	return s.WriteEvent("", data)
}
