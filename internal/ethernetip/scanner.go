//go:build ethernetip || all

package ethernetip

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/rbe"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

// DeviceConnection holds the state for a connected EtherNet/IP device.
type DeviceConnection struct {
	DeviceID        string
	Gateway         string
	Port            int
	Slot            int
	Variables       map[string]*CachedVar
	StructTypes     map[string]string
	Subscribers     map[string]map[string]bool
	ScanRate        time.Duration
	stopChan        chan struct{}
	scanRateChanged chan struct{}
	mu              sync.RWMutex
}

// CachedVar holds the cached state of a single tag.
type CachedVar struct {
	TagName     string
	Datatype    string
	CipType     string
	Value       interface{}
	Quality     string
	LastRead    int64
	LastChanged int64
	TagHandle   *PlcTag
	CreateFails int
	Deadband    *types.DeadBandConfig
	DisableRBE  bool
	rbeState    rbe.State
}

const maxCreateRetries = 3

// Scanner manages EtherNet/IP device connections and Bus request handling.
type Scanner struct {
	b           bus.Bus
	moduleID    string
	log         *slog.Logger
	connections map[string]*DeviceConnection
	mu          sync.RWMutex
	enabled     atomic.Bool

	subs []bus.Subscription

	// Rate tracking (sliding window)
	publishTimes []int64
	pollTimes    []int64
	publishMu    sync.Mutex
	pollMu       sync.Mutex
}

// NewScanner creates a new EtherNet/IP scanner.
func NewScanner(b bus.Bus, moduleID string, log *slog.Logger) *Scanner {
	s := &Scanner{
		b:           b,
		moduleID:    moduleID,
		log:         log,
		connections: make(map[string]*DeviceConnection),
	}
	s.enabled.Store(true)
	return s
}

func (s *Scanner) recordPublish(n int) {
	now := time.Now().UnixMilli()
	s.publishMu.Lock()
	for i := 0; i < n; i++ {
		s.publishTimes = append(s.publishTimes, now)
	}
	s.publishMu.Unlock()
}

// PublishRate returns the current publish rate (metrics/second) over a 10s window.
func (s *Scanner) PublishRate() float64 {
	const windowMs int64 = 10_000
	now := time.Now().UnixMilli()
	cutoff := now - windowMs
	s.publishMu.Lock()
	i := 0
	for i < len(s.publishTimes) && s.publishTimes[i] < cutoff {
		i++
	}
	s.publishTimes = s.publishTimes[i:]
	count := len(s.publishTimes)
	s.publishMu.Unlock()
	return float64(count) / (float64(windowMs) / 1000.0)
}

func (s *Scanner) recordPolls(n int) {
	now := time.Now().UnixMilli()
	s.pollMu.Lock()
	for i := 0; i < n; i++ {
		s.pollTimes = append(s.pollTimes, now)
	}
	s.pollMu.Unlock()
}

// PollRate returns the current poll rate (tags read/second) over a 10s window.
func (s *Scanner) PollRate() float64 {
	const windowMs int64 = 10_000
	now := time.Now().UnixMilli()
	cutoff := now - windowMs
	s.pollMu.Lock()
	i := 0
	for i < len(s.pollTimes) && s.pollTimes[i] < cutoff {
		i++
	}
	s.pollTimes = s.pollTimes[i:]
	count := len(s.pollTimes)
	s.pollMu.Unlock()
	return float64(count) / (float64(windowMs) / 1000.0)
}

// IsEnabled returns whether the scanner is enabled.
func (s *Scanner) IsEnabled() bool {
	return s.enabled.Load()
}

// SetEnabled enables or disables the scanner.
func (s *Scanner) SetEnabled(enabled bool) {
	was := s.enabled.Swap(enabled)
	if was != enabled {
		if enabled {
			s.log.Info("eip: scanner ENABLED — resuming polling")
		} else {
			s.log.Info("eip: scanner DISABLED — pausing polling (connections preserved)")
		}
	}
}

// Start subscribes to all Bus request subjects.
func (s *Scanner) Start() {
	subscribe := func(subject string, handler bus.MessageHandler) {
		sub, err := s.b.Subscribe(subject, handler)
		if err != nil {
			s.log.Error("eip: failed to subscribe", "subject", subject, "error", err)
			return
		}
		s.subs = append(s.subs, sub)
	}

	subscribe("ethernetip.browse", s.handleBrowse)
	subscribe("ethernetip.subscribe", s.handleSubscribe)
	subscribe("ethernetip.unsubscribe", s.handleUnsubscribe)
	subscribe("ethernetip.variables", s.handleVariables)
	subscribe("ethernetip.command.>", s.handleCommand)

	// Watch scanner config KV bucket for subscription configs written by
	// controllers (gateway, plc). This decouples startup ordering — the
	// scanner picks up configs whenever it starts, regardless of whether
	// the controller wrote them before or after.
	bucket := topics.BucketScannerEthernetIP
	if err := s.b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
		s.log.Warn("eip: failed to create scanner config bucket", "error", err)
	}
	kvSub, err := s.b.KVWatchAll(bucket, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpPut {
			s.handleSubscribe("", value, nil)
		} else if op == bus.KVOpDelete {
			// Key format: {subscriberId}.{deviceId} — extract and unsubscribe
			parts := strings.SplitN(key, ".", 2)
			if len(parts) == 2 {
				unsubReq, _ := json.Marshal(UnsubscribeRequest{SubscriberID: parts[0], DeviceID: parts[1]})
				s.handleUnsubscribe("", unsubReq, nil)
			}
		}
	})
	if err != nil {
		s.log.Error("eip: failed to watch scanner config bucket", "error", err)
	} else {
		s.subs = append(s.subs, kvSub)
	}

	s.log.Info("eip: listening for browse/subscribe/variables/command requests")
}

// ActiveDevice describes a currently connected device for heartbeat metadata.
type ActiveDevice struct {
	DeviceID string `json:"deviceId"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TagCount int    `json:"tagCount"`
}

// ActiveDevices returns info about all currently connected devices.
func (s *Scanner) ActiveDevices() []ActiveDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()

	devices := make([]ActiveDevice, 0, len(s.connections))
	for _, conn := range s.connections {
		conn.mu.RLock()
		devices = append(devices, ActiveDevice{
			DeviceID: conn.DeviceID,
			Host:     conn.Gateway,
			Port:     conn.Port,
			TagCount: len(conn.Variables),
		})
		conn.mu.RUnlock()
	}
	return devices
}

// Stop unsubscribes and closes all connections.
func (s *Scanner) Stop() {
	for _, sub := range s.subs {
		_ = sub.Unsubscribe()
	}
	s.subs = nil

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.connections {
		close(conn.stopChan)
		conn.mu.Lock()
		for _, v := range conn.Variables {
			if v.TagHandle != nil {
				v.TagHandle.Close()
				v.TagHandle = nil
			}
		}
		conn.mu.Unlock()
	}
	s.connections = make(map[string]*DeviceConnection)
	s.log.Info("eip: all connections closed")
}

// ═══════════════════════════════════════════════════════════════════════════
// Browse handler
// ═══════════════════════════════════════════════════════════════════════════

func (s *Scanner) handleBrowse(subject string, data []byte, reply bus.ReplyFunc) {
	var req BrowseRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.respondJSON(reply, BrowseResult{Variables: []VariableInfo{}, Udts: map[string]UdtExport{}, StructTags: map[string]string{}})
		return
	}

	if req.DeviceID == "" || req.Host == "" {
		s.respondJSON(reply, BrowseResult{Variables: []VariableInfo{}, Udts: map[string]UdtExport{}, StructTags: map[string]string{}})
		return
	}

	browseID := req.BrowseID
	if browseID == "" {
		browseID = uuid.New().String()
	}

	// Track terminal events so a defer can guarantee one is emitted even if
	// browseDevice panics or exits without publishing (a hung cgo call the
	// context can't interrupt, a worker goroutine panic, etc.). Without this
	// the client polls progress indefinitely with no resolution.
	var terminalEmitted atomic.Bool
	publishProgress := func(progress types.BrowseProgressMessage) {
		switch progress.Phase {
		case "completed", "failed", "cancelled":
			terminalEmitted.Store(true)
		}
		subj := fmt.Sprintf("ethernetip.browse.progress.%s", browseID)
		d, _ := json.Marshal(progress)
		_ = s.b.Publish(subj, d)
	}

	// Overall browse ceiling — even if an upstream cgo call wedges a worker
	// goroutine, this guarantees browseDevice returns.
	ctx, cancel := context.WithTimeout(context.Background(), browseOverallTimeout)

	// Listen for cancel messages from the API layer.
	cancelSubject := topics.BrowseCancel("ethernetip", browseID)
	cancelSub, _ := s.b.Subscribe(cancelSubject, func(_ string, _ []byte, _ bus.ReplyFunc) {
		s.log.Info("eip: browse cancelled by user", "device", req.DeviceID, "browseId", browseID)
		cancel()
	})

	// Respond immediately with browseID, run browse in background
	s.respondJSON(reply, map[string]string{"browseId": browseID})
	go func() {
		defer cancel()
		if cancelSub != nil {
			defer cancelSub.Unsubscribe()
		}
		// Last-resort terminal emitter: covers panics and any path that
		// somehow returns without a terminal phase.
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("eip: browse goroutine panic", "device", req.DeviceID, "panic", r)
				if !terminalEmitted.Load() {
					publishProgress(types.BrowseProgressMessage{
						BrowseID:  browseID,
						ModuleID:  s.moduleID,
						DeviceID:  req.DeviceID,
						Phase:     "failed",
						Message:   fmt.Sprintf("Browse panic: %v", r),
						Timestamp: time.Now().UTC().Format(time.RFC3339),
					})
				}
				return
			}
			if !terminalEmitted.Load() {
				s.log.Warn("eip: browse ended without terminal phase", "device", req.DeviceID)
				publishProgress(types.BrowseProgressMessage{
					BrowseID:  browseID,
					ModuleID:  s.moduleID,
					DeviceID:  req.DeviceID,
					Phase:     "failed",
					Message:   "Browse ended without completion",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				})
			}
		}()

		result, err := browseDevice(ctx, req.Host, req.Port, req.Slot, req.DeviceID, s.moduleID, browseID, publishProgress)
		if err != nil {
			if ctx.Err() == context.Canceled {
				s.log.Info("eip: browse cancelled", "device", req.DeviceID)
				publishProgress(types.BrowseProgressMessage{
					BrowseID:  browseID,
					ModuleID:  s.moduleID,
					DeviceID:  req.DeviceID,
					Phase:     "cancelled",
					Message:   "Browse cancelled by user",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				})
				return
			}
			if ctx.Err() == context.DeadlineExceeded {
				s.log.Error("eip: browse timed out", "device", req.DeviceID, "timeout", browseOverallTimeout)
				publishProgress(types.BrowseProgressMessage{
					BrowseID:  browseID,
					ModuleID:  s.moduleID,
					DeviceID:  req.DeviceID,
					Phase:     "failed",
					Message:   fmt.Sprintf("Browse timed out after %s", browseOverallTimeout),
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				})
				return
			}
			s.log.Error("eip: browse failed", "device", req.DeviceID, "error", err)
			publishProgress(types.BrowseProgressMessage{
				BrowseID:  browseID,
				ModuleID:  s.moduleID,
				DeviceID:  req.DeviceID,
				Phase:     "failed",
				Message:   fmt.Sprintf("Browse failed: %v", err),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		s.log.Info("eip: browse complete", "device", req.DeviceID, "vars", len(result.Variables), "udts", len(result.Udts))
		resultSubject := fmt.Sprintf("ethernetip.browse.result.%s", browseID)
		resultData, err := json.Marshal(result)
		if err != nil {
			s.log.Error("eip: failed to marshal browse result", "device", req.DeviceID, "error", err)
			publishProgress(types.BrowseProgressMessage{
				BrowseID:  browseID,
				ModuleID:  s.moduleID,
				DeviceID:  req.DeviceID,
				Phase:     "failed",
				Message:   fmt.Sprintf("Browse result too large to encode: %v", err),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		if err := s.b.Publish(resultSubject, resultData); err != nil {
			s.log.Error("eip: failed to publish browse result", "device", req.DeviceID, "bytes", len(resultData), "error", err)
			publishProgress(types.BrowseProgressMessage{
				BrowseID:  browseID,
				ModuleID:  s.moduleID,
				DeviceID:  req.DeviceID,
				Phase:     "failed",
				Message:   fmt.Sprintf("Browse result publish failed (%d bytes): %v", len(resultData), err),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
	}()
}

// ═══════════════════════════════════════════════════════════════════════════
// Subscribe handler
// ═══════════════════════════════════════════════════════════════════════════

func (s *Scanner) handleSubscribe(subject string, data []byte, reply bus.ReplyFunc) {
	var req SubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	if req.DeviceID == "" || req.Host == "" || req.SubscriberID == "" || len(req.Tags) == 0 {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": "missing required fields"})
		return
	}

	scanRate := req.ScanRate
	if scanRate <= 0 {
		scanRate = 1000
	}

	s.mu.Lock()
	conn, exists := s.connections[req.DeviceID]
	if !exists {
		conn = &DeviceConnection{
			DeviceID:        req.DeviceID,
			Gateway:         req.Host,
			Port:            req.Port,
			Slot:            req.Slot,
			Variables:       make(map[string]*CachedVar),
			StructTypes:     make(map[string]string),
			Subscribers:     make(map[string]map[string]bool),
			ScanRate:        time.Duration(scanRate) * time.Millisecond,
			stopChan:        make(chan struct{}),
			scanRateChanged: make(chan struct{}, 1),
		}
		s.connections[req.DeviceID] = conn
		s.log.Info("eip: created connection", "device", req.DeviceID, "host", req.Host, "port", req.Port, "slot", req.Slot)
	}
	s.mu.Unlock()

	newRate := time.Duration(scanRate) * time.Millisecond
	conn.mu.Lock()
	if conn.ScanRate != newRate {
		conn.ScanRate = newRate
		select {
		case conn.scanRateChanged <- struct{}{}:
		default:
		}
	}
	conn.mu.Unlock()

	conn.mu.Lock()
	if req.StructTypes != nil {
		for baseName, udtName := range req.StructTypes {
			conn.StructTypes[baseName] = udtName
		}
	}
	if conn.Subscribers[req.SubscriberID] == nil {
		conn.Subscribers[req.SubscriberID] = make(map[string]bool)
	}

	for _, tagName := range req.Tags {
		conn.Subscribers[req.SubscriberID][tagName] = true

		if _, tagExists := conn.Variables[tagName]; !tagExists {
			cipType := ""
			if req.CipTypes != nil {
				cipType = req.CipTypes[tagName]
			}
			cv := &CachedVar{
				TagName: tagName,
				CipType: cipType,
				Quality: "unknown",
			}
			if req.Deadbands != nil {
				if db, ok := req.Deadbands[tagName]; ok {
					cv.Deadband = &db
				}
			}
			if req.DisableRBE != nil {
				if disable, ok := req.DisableRBE[tagName]; ok {
					cv.DisableRBE = disable
				}
			}
			conn.Variables[tagName] = cv
		} else {
			existing := conn.Variables[tagName]
			if req.CipTypes != nil {
				if ct, ok := req.CipTypes[tagName]; ok && ct != "" {
					existing.CipType = ct
				}
			}
			if req.Deadbands != nil {
				if db, ok := req.Deadbands[tagName]; ok {
					existing.Deadband = &db
				}
			}
			if req.DisableRBE != nil {
				if disable, ok := req.DisableRBE[tagName]; ok {
					existing.DisableRBE = disable
				}
			}
			// Reset RBE state so re-subscribers get current values
			existing.rbeState = rbe.State{}
		}
	}
	conn.mu.Unlock()

	if !exists {
		go s.pollDevice(conn)
	}

	s.log.Info("eip: subscriber added tags", "subscriber", req.SubscriberID, "device", req.DeviceID, "tags", len(req.Tags))

	s.respondJSON(reply, map[string]interface{}{
		"success": true,
		"count":   len(req.Tags),
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// Unsubscribe handler
// ═══════════════════════════════════════════════════════════════════════════

func (s *Scanner) handleUnsubscribe(subject string, data []byte, reply bus.ReplyFunc) {
	var req UnsubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	s.mu.RLock()
	conn, exists := s.connections[req.DeviceID]
	s.mu.RUnlock()

	if !exists {
		s.respondJSON(reply, map[string]interface{}{"success": true, "count": 0})
		return
	}

	conn.mu.Lock()
	if subs, ok := conn.Subscribers[req.SubscriberID]; ok {
		if len(req.Tags) == 0 {
			delete(conn.Subscribers, req.SubscriberID)
		} else {
			for _, tag := range req.Tags {
				delete(subs, tag)
			}
			if len(subs) == 0 {
				delete(conn.Subscribers, req.SubscriberID)
			}
		}
	}
	shouldClose := len(conn.Subscribers) == 0
	conn.mu.Unlock()

	if shouldClose {
		s.mu.Lock()
		close(conn.stopChan)
		conn.mu.Lock()
		for _, v := range conn.Variables {
			if v.TagHandle != nil {
				v.TagHandle.Close()
				v.TagHandle = nil
			}
		}
		conn.mu.Unlock()
		delete(s.connections, req.DeviceID)
		s.mu.Unlock()
		s.log.Info("eip: closed connection (no subscribers)", "device", req.DeviceID)
	}

	s.respondJSON(reply, map[string]interface{}{
		"success": true,
		"count":   len(req.Tags),
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// Variables handler
// ═══════════════════════════════════════════════════════════════════════════

func (s *Scanner) handleVariables(subject string, data []byte, reply bus.ReplyFunc) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var allVars []VariableInfo
	for _, conn := range s.connections {
		conn.mu.RLock()
		for _, v := range conn.Variables {
			vi := VariableInfo{
				ModuleID:    s.moduleID,
				DeviceID:    conn.DeviceID,
				VariableID:  v.TagName,
				Value:       v.Value,
				Datatype:    v.Datatype,
				CipType:     v.CipType,
				Quality:     v.Quality,
				Origin:      "plc",
				LastUpdated: v.LastChanged,
			}
			baseName := v.TagName
			if dotIdx := strings.IndexByte(v.TagName, '.'); dotIdx != -1 {
				baseName = v.TagName[:dotIdx]
			}
			if udtName, ok := conn.StructTypes[baseName]; ok {
				vi.StructType = udtName
			}
			allVars = append(allVars, vi)
		}
		conn.mu.RUnlock()
	}

	s.respondJSON(reply, allVars)
}

// ═══════════════════════════════════════════════════════════════════════════
// Command (write) handler
// ═══════════════════════════════════════════════════════════════════════════

func (s *Scanner) handleCommand(subj string, data []byte, reply bus.ReplyFunc) {
	prefix := "ethernetip.command."
	if len(subj) <= len(prefix) {
		return
	}
	tagName := subj[len(prefix):]

	s.mu.RLock()
	var targetConn *DeviceConnection
	for _, conn := range s.connections {
		conn.mu.RLock()
		if _, exists := conn.Variables[tagName]; exists {
			targetConn = conn
			conn.mu.RUnlock()
			break
		}
		conn.mu.RUnlock()
	}
	s.mu.RUnlock()

	if targetConn == nil {
		return
	}

	attrs := buildTagAttrs(targetConn.Gateway, targetConn.Port, targetConn.Slot, tagName, 0)
	tag, err := createTag(attrs, 10*time.Second)
	if err != nil {
		s.log.Error("eip: failed to create tag for write", "tag", tagName, "error", err)
		return
	}
	defer tag.Close()

	valueStr := string(data)
	targetConn.mu.RLock()
	cachedVar := targetConn.Variables[tagName]
	cipType := ""
	if cachedVar != nil {
		cipType = cachedVar.CipType
	}
	targetConn.mu.RUnlock()

	if err := writeTagValue(tag, cipType, valueStr); err != nil {
		s.log.Error("eip: failed to write", "tag", tagName, "error", err)
		return
	}

	s.log.Info("eip: wrote tag", "tag", tagName, "value", valueStr)
}

// ═══════════════════════════════════════════════════════════════════════════
// Polling loop
// ═══════════════════════════════════════════════════════════════════════════

func (s *Scanner) pollDevice(conn *DeviceConnection) {
	s.log.Info("eip: starting poll loop", "device", conn.DeviceID, "rate", conn.ScanRate)

	ticker := time.NewTicker(conn.ScanRate)
	defer ticker.Stop()

	for {
		select {
		case <-conn.stopChan:
			s.log.Info("eip: poll loop stopped", "device", conn.DeviceID)
			return
		case <-conn.scanRateChanged:
			conn.mu.RLock()
			newRate := conn.ScanRate
			conn.mu.RUnlock()
			ticker.Reset(newRate)
		case <-ticker.C:
			if !s.enabled.Load() {
				continue
			}
			s.pollOnce(conn)
		}
	}
}

func (s *Scanner) pollOnce(conn *DeviceConnection) {
	conn.mu.RLock()
	type tagWork struct {
		name    string
		cipType string
		handle  *PlcTag
	}
	var work []tagWork
	var needsHandle []string
	for name, v := range conn.Variables {
		if v.TagHandle == nil {
			if v.CreateFails < maxCreateRetries {
				needsHandle = append(needsHandle, name)
			}
		} else {
			work = append(work, tagWork{name: name, cipType: v.CipType, handle: v.TagHandle})
		}
	}
	conn.mu.RUnlock()

	for _, tagName := range needsHandle {
		attrs := buildTagAttrs(conn.Gateway, conn.Port, conn.Slot, tagName, 0)
		handle, err := createTag(attrs, 10*time.Second)
		conn.mu.Lock()
		v := conn.Variables[tagName]
		if err != nil {
			if v != nil {
				v.CreateFails++
				v.Quality = "bad"
			}
		} else if v != nil {
			v.TagHandle = handle
			v.CreateFails = 0
			work = append(work, tagWork{name: tagName, cipType: v.CipType, handle: handle})
		}
		conn.mu.Unlock()
	}

	if len(work) == 0 {
		return
	}

	const maxConcurrent = 64
	sem := make(chan struct{}, maxConcurrent)
	type tagResult struct {
		name     string
		value    interface{}
		cipType  string
		natsType string
	}
	results := make(chan tagResult, len(work))

	var wg sync.WaitGroup
	for _, tw := range work {
		wg.Add(1)
		go func(tw tagWork) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := tw.handle.Read(10 * time.Second); err != nil {
				conn.mu.Lock()
				if v, ok := conn.Variables[tw.name]; ok {
					v.Quality = "bad"
					if v.TagHandle != nil {
						v.TagHandle.Close()
						v.TagHandle = nil
					}
					v.CreateFails = 0
				}
				conn.mu.Unlock()
				return
			}

			var value interface{}
			var cipType string
			var readErr error
			if tw.cipType != "" {
				value, cipType, readErr = readByKnownType(tw.handle, tw.cipType)
			} else {
				value, cipType, readErr = readBySize(tw.handle)
			}
			if readErr != nil {
				conn.mu.Lock()
				if v, ok := conn.Variables[tw.name]; ok {
					v.Quality = "bad"
				}
				conn.mu.Unlock()
				return
			}

			results <- tagResult{name: tw.name, value: value, cipType: cipType, natsType: cipToNatsDatatype(cipType)}
		}(tw)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	now := time.Now().UnixMilli()
	published := 0
	for r := range results {
		conn.mu.Lock()
		v, ok := conn.Variables[r.name]
		if ok {
			v.Value = r.value
			v.Datatype = r.natsType
			if v.CipType == "" {
				v.CipType = r.cipType
			}
			v.Quality = "good"
			v.LastRead = now
		}

		if ok && !rbe.ShouldPublish(&v.rbeState, r.value, now, v.Deadband, v.DisableRBE) {
			conn.mu.Unlock()
			continue
		}

		if ok {
			rbe.RecordPublish(&v.rbeState, r.value, now)
			v.LastChanged = now
		}
		conn.mu.Unlock()

		dataMsg := types.PlcDataMessage{
			ModuleID:   s.moduleID,
			DeviceID:   conn.DeviceID,
			VariableID: r.name,
			Value:      r.value,
			Timestamp:  now,
			Datatype:   r.natsType,
		}
		if ok && v.Deadband != nil {
			dataMsg.Deadband = v.Deadband
		}
		if ok && v.DisableRBE {
			dataMsg.DisableRBE = true
		}
		d, _ := json.Marshal(dataMsg)
		pubSubject := fmt.Sprintf("ethernetip.data.%s.%s", types.SanitizeForSubject(conn.DeviceID), sanitizeTagForSubject(r.name))
		_ = s.b.Publish(pubSubject, d)
		published++
	}

	if published > 0 {
		s.recordPublish(published)
	}
	s.recordPolls(len(work))
}

// ═══════════════════════════════════════════════════════════════════════════
// Tag read/write helpers
// ═══════════════════════════════════════════════════════════════════════════

func readBySize(tag TagAccessor) (interface{}, string, error) {
	size := tag.Size()
	switch {
	case size == 1:
		val := tag.GetInt8(0)
		if val == 0 || val == 1 {
			return val != 0, "BOOL", nil
		}
		return int64(val), "SINT", nil
	case size == 2:
		return int64(tag.GetInt16(0)), "INT", nil
	case size == 4:
		return int64(tag.GetInt32(0)), "DINT", nil
	case size == 8:
		return tag.GetFloat64(0), "LREAL", nil
	default:
		if size >= 4 {
			strLen := int(tag.GetInt32(0))
			if strLen > 0 && strLen <= size-4 {
				return tag.GetString(0), "STRING", nil
			}
		}
		return nil, "UNKNOWN", fmt.Errorf("unknown tag size: %d", size)
	}
}

func readByKnownType(tag TagAccessor, cipType string) (interface{}, string, error) {
	switch cipType {
	case "BOOL":
		return tag.GetBit(0), "BOOL", nil
	case "SINT":
		return int64(tag.GetInt8(0)), "SINT", nil
	case "INT":
		return int64(tag.GetInt16(0)), "INT", nil
	case "DINT":
		return int64(tag.GetInt32(0)), "DINT", nil
	case "LINT":
		return tag.GetInt64(0), "LINT", nil
	case "USINT":
		return int64(tag.GetUint8(0)), "USINT", nil
	case "UINT":
		return int64(tag.GetUint16(0)), "UINT", nil
	case "UDINT":
		return int64(tag.GetUint32(0)), "UDINT", nil
	case "REAL":
		return float64(tag.GetFloat32(0)), "REAL", nil
	case "LREAL":
		return tag.GetFloat64(0), "LREAL", nil
	case "STRING":
		return tag.GetString(0), "STRING", nil
	default:
		size := tag.Size()
		if size == 4 {
			return int64(tag.GetInt32(0)), cipType, nil
		}
		return nil, cipType, fmt.Errorf("unsupported CIP type: %s", cipType)
	}
}

func writeTagValue(tag TagAccessor, cipType string, valueStr string) error {
	if err := tag.Read(10 * time.Second); err != nil {
		return fmt.Errorf("pre-read failed: %w", err)
	}

	switch cipType {
	case "BOOL":
		val := valueStr == "true" || valueStr == "1"
		tag.SetBit(0, val)
	case "SINT":
		var n int64
		fmt.Sscanf(valueStr, "%d", &n)
		tag.SetInt32(0, int32(int8(n)))
	case "INT":
		var n int64
		fmt.Sscanf(valueStr, "%d", &n)
		tag.SetInt32(0, int32(int16(n)))
	case "DINT", "LINT":
		var n int64
		fmt.Sscanf(valueStr, "%d", &n)
		tag.SetInt32(0, int32(n))
	case "REAL":
		var f float64
		fmt.Sscanf(valueStr, "%f", &f)
		tag.SetFloat32(0, float32(f))
	case "LREAL":
		var f float64
		fmt.Sscanf(valueStr, "%f", &f)
		tag.SetFloat64(0, f)
	default:
		return fmt.Errorf("unsupported write type: %s", cipType)
	}

	return tag.Write(10 * time.Second)
}

// respondJSON marshals v and calls the reply function.
func (s *Scanner) respondJSON(reply bus.ReplyFunc, v interface{}) {
	if reply == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		s.log.Error("eip: failed to marshal response", "error", err)
		return
	}
	if err := reply(data); err != nil {
		s.log.Error("eip: failed to respond", "error", err)
	}
}
