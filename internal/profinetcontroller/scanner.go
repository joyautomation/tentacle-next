//go:build profinetcontroller

package profinetcontroller

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/profinet"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const protocolName = "profinetcontroller"

// Scanner manages connections to PROFINET IO Devices.
type Scanner struct {
	mu        sync.Mutex
	b         bus.Bus
	moduleID  string
	log       *slog.Logger
	devices   map[string]*DeviceState // deviceID -> state
	subs      []bus.Subscription
	transport *profinet.Transport // shared transport for all devices on same interface
	dcpClient *DCPClient
}

func newScanner(b bus.Bus, moduleID string, log *slog.Logger) *Scanner {
	return &Scanner{
		b:        b,
		moduleID: moduleID,
		log:      log,
		devices:  make(map[string]*DeviceState),
	}
}

func (s *Scanner) start() error {
	// Subscribe handler
	subSub, err := s.b.Subscribe(topics.ScannerSubscribe(protocolName), func(subject string, data []byte, reply bus.ReplyFunc) {
		s.handleSubscribe(data, reply)
	})
	if err != nil {
		return fmt.Errorf("subscribe handler: %w", err)
	}
	s.subs = append(s.subs, subSub)

	// Unsubscribe handler
	unsubSub, err := s.b.Subscribe(topics.ScannerUnsubscribe(protocolName), func(subject string, data []byte, reply bus.ReplyFunc) {
		s.handleUnsubscribe(data, reply)
	})
	if err != nil {
		return fmt.Errorf("unsubscribe handler: %w", err)
	}
	s.subs = append(s.subs, unsubSub)

	// Variables handler
	varsSub, err := s.b.Subscribe(topics.ScannerVariables(protocolName), func(subject string, data []byte, reply bus.ReplyFunc) {
		s.handleVariables(reply)
	})
	if err != nil {
		return fmt.Errorf("variables handler: %w", err)
	}
	s.subs = append(s.subs, varsSub)

	// Write commands
	cmdSub, err := s.b.Subscribe(topics.CommandWildcard(protocolName), func(subject string, data []byte, reply bus.ReplyFunc) {
		s.handleCommand(subject, data)
	})
	if err != nil {
		return fmt.Errorf("command handler: %w", err)
	}
	s.subs = append(s.subs, cmdSub)

	// KV bucket for persistent subscriptions
	bucket := topics.BucketScannerProfinetController
	if err := s.b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
		s.log.Warn("profinetcontroller: failed to create scanner config bucket", "error", err)
	}
	kvSub, err := s.b.KVWatchAll(bucket, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpPut {
			s.handleSubscribe(value, nil)
		} else if op == bus.KVOpDelete {
			parts := strings.SplitN(key, ".", 2)
			if len(parts) == 2 {
				unsubReq, _ := json.Marshal(UnsubscribeRequest{SubscriberID: parts[0], DeviceID: parts[1]})
				s.handleUnsubscribe(unsubReq, nil)
			}
		}
	})
	if err != nil {
		s.log.Error("profinetcontroller: failed to watch config bucket", "error", err)
	} else {
		s.subs = append(s.subs, kvSub)
	}

	s.log.Info("profinetcontroller: scanner started", "moduleId", s.moduleID)
	return nil
}

func (s *Scanner) stop() {
	for _, sub := range s.subs {
		sub.Unsubscribe()
	}
	s.subs = nil

	s.mu.Lock()
	for _, dev := range s.devices {
		stopDevice(dev)
	}
	s.devices = make(map[string]*DeviceState)
	if s.transport != nil {
		s.transport.Close()
		s.transport = nil
	}
	s.mu.Unlock()
}

func (s *Scanner) handleSubscribe(data []byte, reply bus.ReplyFunc) {
	var req SubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.log.Error("profinetcontroller: parse subscribe", "error", err)
		sendReply(reply, false, err.Error())
		return
	}

	if req.DeviceID == "" || req.StationName == "" {
		sendReply(reply, false, "deviceId and stationName are required")
		return
	}
	if req.InterfaceName == "" {
		sendReply(reply, false, "interfaceName is required")
		return
	}
	if req.CycleTimeMs <= 0 {
		req.CycleTimeMs = 1
	}

	// Build tag index
	tagConfigs := make(map[string]ControllerTag)
	for _, slot := range req.Slots {
		for _, sub := range slot.Subslots {
			for _, tag := range sub.Tags {
				tagConfigs[tag.TagID] = tag
			}
		}
	}

	s.mu.Lock()

	// Ensure transport exists for this interface
	if err := s.ensureTransport(req.InterfaceName); err != nil {
		s.mu.Unlock()
		s.log.Error("profinetcontroller: transport failed", "error", err)
		sendReply(reply, false, err.Error())
		return
	}

	dev, exists := s.devices[req.DeviceID]
	if !exists {
		dev = &DeviceState{
			DeviceID:      req.DeviceID,
			StationName:   req.StationName,
			InterfaceName: req.InterfaceName,
			VendorID:      req.VendorID,
			DeviceIDPN:    req.DeviceIDPN,
			CycleTimeMs:   req.CycleTimeMs,
			Subscribers:   make(map[string]*Subscriber),
			Slots:         req.Slots,
			lastValues:    make(map[string]interface{}),
			allTags:       make(map[string]ControllerTag),
			stopChan:      make(chan struct{}),
		}
		if req.IP != "" {
			dev.IP = net.ParseIP(req.IP)
		}
		s.devices[req.DeviceID] = dev
	}

	// Add subscriber
	dev.mu.Lock()
	dev.Subscribers[req.SubscriberID] = &Subscriber{
		SubscriberID: req.SubscriberID,
		Tags:         tagConfigs,
	}
	rebuildDeviceTags(dev)
	dev.mu.Unlock()
	s.mu.Unlock()

	if !exists {
		go s.deviceLoop(dev)
	}

	s.log.Info("profinetcontroller: subscribed",
		"device", req.DeviceID,
		"subscriber", req.SubscriberID,
		"tags", len(tagConfigs),
	)
	sendReply(reply, true, "")
}

func (s *Scanner) handleUnsubscribe(data []byte, reply bus.ReplyFunc) {
	var req UnsubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		sendReply(reply, false, err.Error())
		return
	}

	s.mu.Lock()
	dev, exists := s.devices[req.DeviceID]
	if !exists {
		s.mu.Unlock()
		sendReply(reply, true, "")
		return
	}

	dev.mu.Lock()
	if len(req.TagIDs) > 0 {
		if sub, ok := dev.Subscribers[req.SubscriberID]; ok {
			for _, tagID := range req.TagIDs {
				delete(sub.Tags, tagID)
			}
			if len(sub.Tags) == 0 {
				delete(dev.Subscribers, req.SubscriberID)
			}
		}
	} else {
		delete(dev.Subscribers, req.SubscriberID)
	}

	if len(dev.Subscribers) == 0 {
		dev.mu.Unlock()
		stopDevice(dev)
		delete(s.devices, req.DeviceID)
		s.mu.Unlock()
		s.log.Info("profinetcontroller: device removed", "device", req.DeviceID)
		sendReply(reply, true, "")
		return
	}

	rebuildDeviceTags(dev)
	dev.mu.Unlock()
	s.mu.Unlock()

	s.log.Info("profinetcontroller: unsubscribed", "device", req.DeviceID, "subscriber", req.SubscriberID)
	sendReply(reply, true, "")
}

func (s *Scanner) handleVariables(reply bus.ReplyFunc) {
	if reply == nil {
		return
	}

	s.mu.Lock()
	var vars []types.VariableInfo
	for _, dev := range s.devices {
		dev.mu.Lock()
		for tagID, tag := range dev.allTags {
			val := dev.lastValues[tagID]
			vars = append(vars, types.VariableInfo{
				ModuleID:   s.moduleID,
				DeviceID:   dev.DeviceID,
				VariableID: tagID,
				Value:      val,
				Datatype:   tag.Datatype,
			})
		}
		dev.mu.Unlock()
	}
	s.mu.Unlock()

	resp, _ := json.Marshal(vars)
	_ = reply(resp)
}

func (s *Scanner) handleCommand(subject string, data []byte) {
	// Subject format: profinetcontroller.command.{deviceID}.{tagID}
	parts := strings.Split(subject, ".")
	if len(parts) < 4 {
		return
	}
	// deviceID is parts[2], tagID is everything after
	deviceID := parts[2]
	tagID := strings.Join(parts[3:], ".")

	s.mu.Lock()
	dev, exists := s.devices[deviceID]
	s.mu.Unlock()
	if !exists {
		return
	}

	dev.mu.Lock()
	tag, ok := dev.allTags[tagID]
	if !ok || tag.Direction != "output" {
		dev.mu.Unlock()
		return
	}

	// Parse write value and pack into output buffer
	if dev.cyclic != nil {
		var msg types.PlcDataMessage
		if err := json.Unmarshal(data, &msg); err == nil {
			packed := packTagValue(tag, msg.Value)
			if packed != nil {
				dev.cyclic.WriteOutputTag(tag.ByteOffset, packed)
			}
		}
	}
	dev.mu.Unlock()
}

// deviceLoop manages the lifecycle of a single PROFINET device connection.
func (s *Scanner) deviceLoop(dev *DeviceState) {
	s.log.Info("profinetcontroller: starting device loop",
		"device", dev.DeviceID,
		"stationName", dev.StationName,
	)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-dev.stopChan
		cancel()
	}()

	for {
		select {
		case <-dev.stopChan:
			s.disconnectDevice(dev)
			s.log.Info("profinetcontroller: device loop stopped", "device", dev.DeviceID)
			return
		default:
		}

		// Backoff on failures
		dev.mu.Lock()
		bo := backoffDuration(dev.failures)
		dev.mu.Unlock()
		if bo > 0 {
			select {
			case <-dev.stopChan:
				return
			case <-time.After(bo):
			}
		}

		// Step 1: Discover device if no IP
		if dev.IP == nil || dev.IP.Equal(net.IPv4zero) {
			discovered, err := s.dcpClient.IdentifyByName(ctx, dev.StationName, 2*time.Second)
			if err != nil || discovered == nil {
				dev.mu.Lock()
				dev.failures++
				dev.mu.Unlock()
				s.log.Warn("profinetcontroller: DCP discovery failed",
					"device", dev.DeviceID, "stationName", dev.StationName, "error", err)
				continue
			}
			dev.mu.Lock()
			dev.IP = discovered.IP
			dev.MAC = discovered.MAC
			dev.mu.Unlock()
			s.log.Info("profinetcontroller: discovered device",
				"device", dev.DeviceID, "ip", discovered.IP, "mac", discovered.MAC)
		}

		// Step 2: Establish AR
		dev.mu.Lock()
		localMAC := s.transport.LocalMAC()
		deviceMAC := dev.MAC
		if deviceMAC == nil {
			// If we have IP but no MAC, use broadcast (will be resolved by ARP)
			deviceMAC = net.HardwareAddr{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		}
		ar, err := NewControllerAR(dev.IP, deviceMAC, localMAC,
			dev.StationName, dev.Slots, dev.CycleTimeMs, s.log)
		if err != nil {
			dev.failures++
			dev.mu.Unlock()
			s.log.Warn("profinetcontroller: AR creation failed", "device", dev.DeviceID, "error", err)
			continue
		}
		dev.ar = ar
		dev.mu.Unlock()

		if err := ar.Establish(ctx); err != nil {
			dev.mu.Lock()
			dev.failures++
			dev.ar.Close()
			dev.ar = nil
			dev.mu.Unlock()
			s.log.Warn("profinetcontroller: AR establishment failed", "device", dev.DeviceID, "error", err)
			continue
		}

		// Step 3: Start cyclic exchange
		dev.mu.Lock()
		dev.failures = 0
		cyclic := NewControllerCyclic(s.transport, ar, func(data []byte) {
			s.handleInputData(dev, data)
		}, s.log)
		dev.cyclic = cyclic
		dev.mu.Unlock()

		go cyclic.Start(ctx)

		s.log.Info("profinetcontroller: device connected",
			"device", dev.DeviceID, "ip", dev.IP)

		// Step 4: Monitor connection
		s.monitorDevice(dev, ctx)

		// Connection lost — clean up and retry
		s.disconnectDevice(dev)
		s.log.Warn("profinetcontroller: device disconnected, will retry", "device", dev.DeviceID)
	}
}

// monitorDevice watches for watchdog expiry or stop signal.
func (s *Scanner) monitorDevice(dev *DeviceState, ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-dev.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			dev.mu.Lock()
			cyclic := dev.cyclic
			dev.mu.Unlock()
			if cyclic != nil && cyclic.WatchdogExpired() {
				s.log.Warn("profinetcontroller: watchdog expired", "device", dev.DeviceID)
				return
			}
		}
	}
}

// handleInputData processes input cyclic data from a device.
func (s *Scanner) handleInputData(dev *DeviceState, data []byte) {
	dev.mu.Lock()
	defer dev.mu.Unlock()

	nowMs := time.Now().UnixMilli()
	sanitizedDevice := types.SanitizeForSubject(dev.DeviceID)

	// Unpack tags from input data
	for tagID, tag := range dev.allTags {
		if tag.Direction != "input" {
			continue
		}

		val := unpackTagValue(tag, data)
		if val == nil {
			continue
		}

		// Change detection
		lastVal, exists := dev.lastValues[tagID]
		if exists && fmt.Sprintf("%v", lastVal) == fmt.Sprintf("%v", val) {
			continue
		}
		dev.lastValues[tagID] = val

		sanitizedTag := types.SanitizeForSubject(tagID)
		msg := types.PlcDataMessage{
			ModuleID:   s.moduleID,
			DeviceID:   dev.DeviceID,
			VariableID: tagID,
			Value:      val,
			Timestamp:  nowMs,
			Datatype:   tag.Datatype,
		}
		msgData, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		subject := topics.Data(protocolName, sanitizedDevice, sanitizedTag)
		if err := s.b.Publish(subject, msgData); err != nil {
			s.log.Warn("profinetcontroller: publish failed", "subject", subject, "error", err)
		}
	}
}

// disconnectDevice cleans up an active device connection.
func (s *Scanner) disconnectDevice(dev *DeviceState) {
	dev.mu.Lock()
	if dev.cyclic != nil {
		dev.cyclic.Stop()
		dev.cyclic = nil
	}
	if dev.ar != nil {
		_ = dev.ar.Release(context.Background())
		dev.ar = nil
	}
	dev.mu.Unlock()
}

// ensureTransport creates the shared transport if not already open.
func (s *Scanner) ensureTransport(ifaceName string) error {
	if s.transport != nil {
		return nil
	}

	transport, err := profinet.NewTransport(ifaceName, s.log)
	if err != nil {
		return err
	}
	s.transport = transport
	s.dcpClient = NewDCPClient(transport, s.log)

	// Start frame dispatcher
	go s.frameLoop()

	return nil
}

// frameLoop reads all incoming Ethernet frames and dispatches to handlers.
func (s *Scanner) frameLoop() {
	ctx := context.Background()
	for {
		_, payload, srcMAC, err := s.transport.RecvFrame(ctx)
		if err != nil {
			return // transport closed
		}

		frameID, err := profinet.ParseFrameID(payload)
		if err != nil {
			continue
		}

		switch {
		case profinet.IsDCPFrame(frameID):
			if s.dcpClient != nil {
				s.dcpClient.HandleResponse(payload, srcMAC)
			}

		case profinet.IsRTCyclicFrame(frameID):
			s.mu.Lock()
			for _, dev := range s.devices {
				dev.mu.Lock()
				if dev.cyclic != nil {
					dev.cyclic.HandleInputFrame(payload)
				}
				dev.mu.Unlock()
			}
			s.mu.Unlock()

		case profinet.IsAlarmFrame(frameID):
			// TODO: route alarms to appropriate device
		}
	}
}

// Helpers

func stopDevice(dev *DeviceState) {
	dev.mu.Lock()
	if !dev.stopped {
		dev.stopped = true
		close(dev.stopChan)
	}
	dev.mu.Unlock()
}

func rebuildDeviceTags(dev *DeviceState) {
	dev.allTags = make(map[string]ControllerTag)
	for _, sub := range dev.Subscribers {
		for tagID, tag := range sub.Tags {
			dev.allTags[tagID] = tag
		}
	}
}

func backoffDuration(failures int) time.Duration {
	if failures == 0 {
		return 0
	}
	d := time.Duration(failures) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

func sendReply(reply bus.ReplyFunc, success bool, errMsg string) {
	if reply == nil {
		return
	}
	resp := struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}{Success: success, Error: errMsg}
	data, _ := json.Marshal(resp)
	_ = reply(data)
}

// unpackTagValue extracts a typed value from raw cyclic input data.
func unpackTagValue(tag ControllerTag, data []byte) interface{} {
	offset := int(tag.ByteOffset)
	switch tag.Datatype {
	case "bool":
		if offset >= len(data) {
			return nil
		}
		return (data[offset] >> tag.BitOffset) & 1 == 1
	case "uint8":
		if offset >= len(data) {
			return nil
		}
		return data[offset]
	case "int8":
		if offset >= len(data) {
			return nil
		}
		return int8(data[offset])
	case "uint16":
		if offset+2 > len(data) {
			return nil
		}
		return binary.BigEndian.Uint16(data[offset : offset+2])
	case "int16":
		if offset+2 > len(data) {
			return nil
		}
		return int16(binary.BigEndian.Uint16(data[offset : offset+2]))
	case "uint32":
		if offset+4 > len(data) {
			return nil
		}
		return binary.BigEndian.Uint32(data[offset : offset+4])
	case "int32":
		if offset+4 > len(data) {
			return nil
		}
		return int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	case "float32":
		if offset+4 > len(data) {
			return nil
		}
		return math.Float32frombits(binary.BigEndian.Uint32(data[offset : offset+4]))
	case "uint64":
		if offset+8 > len(data) {
			return nil
		}
		return binary.BigEndian.Uint64(data[offset : offset+8])
	case "int64":
		if offset+8 > len(data) {
			return nil
		}
		return int64(binary.BigEndian.Uint64(data[offset : offset+8]))
	case "float64":
		if offset+8 > len(data) {
			return nil
		}
		return math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
	}
	return nil
}

// packTagValue converts a typed value to raw bytes for the output buffer.
func packTagValue(tag ControllerTag, val interface{}) []byte {
	switch tag.Datatype {
	case "bool":
		// For bools, return the byte with the bit set/cleared
		b := byte(0)
		if v, ok := val.(bool); ok && v {
			b = 1 << tag.BitOffset
		}
		return []byte{b}
	case "uint8":
		if v, ok := toFloat64(val); ok {
			return []byte{byte(v)}
		}
	case "int8":
		if v, ok := toFloat64(val); ok {
			return []byte{byte(int8(v))}
		}
	case "uint16":
		if v, ok := toFloat64(val); ok {
			buf := make([]byte, 2)
			binary.BigEndian.PutUint16(buf, uint16(v))
			return buf
		}
	case "int16":
		if v, ok := toFloat64(val); ok {
			buf := make([]byte, 2)
			binary.BigEndian.PutUint16(buf, uint16(int16(v)))
			return buf
		}
	case "uint32":
		if v, ok := toFloat64(val); ok {
			buf := make([]byte, 4)
			binary.BigEndian.PutUint32(buf, uint32(v))
			return buf
		}
	case "int32":
		if v, ok := toFloat64(val); ok {
			buf := make([]byte, 4)
			binary.BigEndian.PutUint32(buf, uint32(int32(v)))
			return buf
		}
	case "float32":
		if v, ok := toFloat64(val); ok {
			buf := make([]byte, 4)
			binary.BigEndian.PutUint32(buf, math.Float32bits(float32(v)))
			return buf
		}
	case "float64":
		if v, ok := toFloat64(val); ok {
			buf := make([]byte, 8)
			binary.BigEndian.PutUint64(buf, math.Float64bits(v))
			return buf
		}
	case "uint64":
		if v, ok := toFloat64(val); ok {
			buf := make([]byte, 8)
			binary.BigEndian.PutUint64(buf, uint64(v))
			return buf
		}
	case "int64":
		if v, ok := toFloat64(val); ok {
			buf := make([]byte, 8)
			binary.BigEndian.PutUint64(buf, uint64(int64(v)))
			return buf
		}
	}
	return nil
}

func toFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}
