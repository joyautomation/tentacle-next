//go:build modbus || all

package modbus

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

const maxGap = 10 // max register gap before splitting into separate read blocks

// Scanner manages per-device polling loops and subscriber management.
type Scanner struct {
	mu       sync.Mutex
	b        bus.Bus
	moduleID string
	log      *slog.Logger
	devices  map[string]*DeviceState // deviceID -> state
	subs     []bus.Subscription
}

// newScanner creates a new Scanner.
func newScanner(b bus.Bus, moduleID string, log *slog.Logger) *Scanner {
	return &Scanner{
		b:        b,
		moduleID: moduleID,
		log:      log,
		devices:  make(map[string]*DeviceState),
	}
}

// start sets up bus handlers for subscribe, unsubscribe, variables, and commands.
func (s *Scanner) start() error {
	// Handle subscribe requests (request/reply)
	subSub, err := s.b.Subscribe(topics.ScannerSubscribe("modbus"), func(subject string, data []byte, reply bus.ReplyFunc) {
		s.handleSubscribe(data, reply)
	})
	if err != nil {
		return fmt.Errorf("subscribe handler: %w", err)
	}
	s.subs = append(s.subs, subSub)

	// Handle unsubscribe requests (request/reply)
	unsubSub, err := s.b.Subscribe(topics.ScannerUnsubscribe("modbus"), func(subject string, data []byte, reply bus.ReplyFunc) {
		s.handleUnsubscribe(data, reply)
	})
	if err != nil {
		return fmt.Errorf("unsubscribe handler: %w", err)
	}
	s.subs = append(s.subs, unsubSub)

	// Handle variables requests (request/reply)
	varsSub, err := s.b.Subscribe(topics.ScannerVariables("modbus"), func(subject string, data []byte, reply bus.ReplyFunc) {
		s.handleVariables(reply)
	})
	if err != nil {
		return fmt.Errorf("variables handler: %w", err)
	}
	s.subs = append(s.subs, varsSub)

	// Handle write commands: modbus.command.>
	cmdSub, err := s.b.Subscribe(topics.CommandWildcard("modbus"), func(subject string, data []byte, reply bus.ReplyFunc) {
		s.handleCommand(subject, data)
	})
	if err != nil {
		return fmt.Errorf("command handler: %w", err)
	}
	s.subs = append(s.subs, cmdSub)

	// Watch scanner config KV bucket for subscription configs.
	bucket := topics.BucketScannerModbus
	if err := s.b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
		s.log.Warn("modbus: failed to create scanner config bucket", "error", err)
	}
	kvSub, err := s.b.KVWatchAll(bucket, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpPut {
			s.handleSubscribe(value, nil)
		} else if op == bus.KVOpDelete {
			parts := strings.SplitN(key, ".", 2)
			if len(parts) == 2 {
				unsubReq, _ := json.Marshal(itypes.ModbusScannerUnsubscribeRequest{SubscriberID: parts[0], DeviceID: parts[1]})
				s.handleUnsubscribe(unsubReq, nil)
			}
		}
	})
	if err != nil {
		s.log.Error("modbus: failed to watch scanner config bucket", "error", err)
	} else {
		s.subs = append(s.subs, kvSub)
	}

	s.log.Info("modbus: scanner started", "moduleId", s.moduleID)
	return nil
}

// stop tears down all handlers and device polling loops.
func (s *Scanner) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sub := range s.subs {
		_ = sub.Unsubscribe()
	}
	s.subs = nil

	for _, dev := range s.devices {
		stopDevice(dev)
	}
	s.devices = make(map[string]*DeviceState)
}

// handleSubscribe processes a modbus.subscribe request.
func (s *Scanner) handleSubscribe(data []byte, reply bus.ReplyFunc) {
	var req itypes.ModbusScannerSubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.log.Error("modbus: failed to parse subscribe request", "error", err)
		sendReply(reply, false, err.Error())
		return
	}

	if req.DeviceID == "" || req.Host == "" {
		sendReply(reply, false, "deviceId and host are required")
		return
	}

	port := req.Port
	if port == 0 {
		port = 502
	}
	unitID := req.UnitID
	if unitID == 0 {
		unitID = 1
	}
	scanRate := req.ScanRate
	if scanRate <= 0 {
		scanRate = 1000
	}

	// Build tag configs
	tagConfigs := make(map[string]itypes.ModbusTagConfig, len(req.Tags))
	for _, tag := range req.Tags {
		fc := tag.FunctionCode
		if fc == "" {
			fc = "holding"
		}
		dt := tag.Datatype
		if dt == "" {
			dt = "uint16"
		}
		bo := tag.ByteOrder
		if bo == "" {
			bo = req.ByteOrder
		}
		tagConfigs[tag.ID] = itypes.ModbusTagConfig{
			ID:           tag.ID,
			Description:  tag.Description,
			Address:      tag.Address,
			FunctionCode: fc,
			Datatype:     dt,
			ByteOrder:    bo,
			Writable:     tag.Writable,
		}
	}

	s.mu.Lock()
	dev, exists := s.devices[req.DeviceID]
	if !exists {
		dev = &DeviceState{
			DeviceID:    req.DeviceID,
			Host:        req.Host,
			Port:        port,
			UnitID:      byte(unitID),
			ByteOrder:   req.ByteOrder,
			Subscribers: make(map[string]*Subscriber),
			lastValues:  make(map[string]interface{}),
			allTags:     make(map[string]itypes.ModbusTagConfig),
			stopChan:    make(chan struct{}),
		}
		s.devices[req.DeviceID] = dev
	}

	// Add or update subscriber
	dev.mu.Lock()
	dev.Subscribers[req.SubscriberID] = &Subscriber{
		SubscriberID: req.SubscriberID,
		ScanRate:     scanRate,
		Tags:         tagConfigs,
	}
	rebuildDevicePlan(dev)
	wasRunning := !dev.stopped && exists
	dev.mu.Unlock()
	s.mu.Unlock()

	// Start polling if this is a new device
	if !exists {
		go s.pollDevice(dev)
	} else if wasRunning {
		// Device is already polling; plan rebuild is picked up on next cycle.
		s.log.Info("modbus: updated subscriber", "device", req.DeviceID, "subscriber", req.SubscriberID)
	}

	s.log.Info("modbus: subscribed", "device", req.DeviceID, "subscriber", req.SubscriberID, "tags", len(tagConfigs))
	sendReply(reply, true, "")
}

// handleUnsubscribe processes a modbus.unsubscribe request.
func (s *Scanner) handleUnsubscribe(data []byte, reply bus.ReplyFunc) {
	var req itypes.ModbusScannerUnsubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.log.Error("modbus: failed to parse unsubscribe request", "error", err)
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
		// Remove specific tags from the subscriber
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
		s.log.Info("modbus: device removed (no subscribers)", "device", req.DeviceID)
		sendReply(reply, true, "")
		return
	}

	rebuildDevicePlan(dev)
	dev.mu.Unlock()
	s.mu.Unlock()

	s.log.Info("modbus: unsubscribed", "device", req.DeviceID, "subscriber", req.SubscriberID)
	sendReply(reply, true, "")
}

// DeviceStatuses returns a snapshot of communication status for every tracked device.
func (s *Scanner) DeviceStatuses() []itypes.DeviceCommStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]itypes.DeviceCommStatus, 0, len(s.devices))
	for _, dev := range s.devices {
		dev.mu.Lock()
		state := dev.state
		if state == "" {
			state = "disconnected"
		}
		out = append(out, itypes.DeviceCommStatus{
			DeviceID:            dev.DeviceID,
			State:               state,
			LastReadAt:          dev.lastReadAt,
			LastErrorAt:         dev.lastErrorAt,
			LastError:           dev.lastError,
			ConsecutiveFailures: dev.failures,
		})
		dev.mu.Unlock()
	}
	return out
}

// handleVariables responds with all current tag values.
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
			dt := modbusToNatsDatatype(tag.Datatype)
			vars = append(vars, types.VariableInfo{
				ModuleID:    s.moduleID,
				DeviceID:    dev.DeviceID,
				VariableID:  tagID,
				Value:       val,
				Datatype:    dt,
				Description: tag.Description,
			})
		}
		dev.mu.Unlock()
	}
	s.mu.Unlock()

	resp, err := json.Marshal(vars)
	if err != nil {
		return
	}
	_ = reply(resp)
}

// handleCommand processes a write command from modbus.command.{tagId}.
func (s *Scanner) handleCommand(subject string, data []byte) {
	// Parse tag ID from subject: modbus.command.{tagId}
	parts := strings.SplitN(subject, ".", 3)
	if len(parts) < 3 {
		s.log.Warn("modbus: invalid command subject", "subject", subject)
		return
	}
	tagID := parts[2]

	// Parse the write value
	var cmdMsg struct {
		Value interface{} `json:"value"`
	}
	if err := json.Unmarshal(data, &cmdMsg); err != nil {
		s.log.Error("modbus: failed to parse command", "tag", tagID, "error", err)
		return
	}

	// Find the device and tag
	s.mu.Lock()
	var targetDev *DeviceState
	var targetTag itypes.ModbusTagConfig
	var found bool
	for _, dev := range s.devices {
		dev.mu.Lock()
		if tag, ok := dev.allTags[tagID]; ok {
			if tag.Writable {
				targetDev = dev
				targetTag = tag
				found = true
			}
		}
		dev.mu.Unlock()
		if found {
			break
		}
	}
	s.mu.Unlock()

	if !found {
		s.log.Warn("modbus: command for unknown or non-writable tag", "tag", tagID)
		return
	}

	targetDev.mu.Lock()
	defer targetDev.mu.Unlock()

	if targetDev.conn == nil {
		s.log.Warn("modbus: cannot write, no connection", "device", targetDev.DeviceID, "tag", tagID)
		return
	}

	fc := fcNameToCode(targetTag.FunctionCode)
	addr := uint16(targetTag.Address)

	byteOrder := targetTag.ByteOrder
	if byteOrder == "" {
		byteOrder = targetDev.ByteOrder
	}
	if byteOrder == "" {
		byteOrder = "ABCD"
	}

	var writeErr error

	if fc == fcReadCoils {
		// Write coil using FC05
		bval, ok := toBool(cmdMsg.Value)
		if !ok {
			s.log.Error("modbus: invalid coil write value", "tag", tagID, "value", cmdMsg.Value)
			return
		}
		writeErr = writeSingleCoil(targetDev, addr, bval)
	} else {
		// Register write
		encoded, regCount, err := encodeValue(cmdMsg.Value, targetTag.Datatype, byteOrder)
		if err != nil {
			s.log.Error("modbus: encode error for write", "tag", tagID, "error", err)
			return
		}
		if regCount == 1 {
			// FC06 single register
			val := uint16(encoded[0])<<8 | uint16(encoded[1])
			writeErr = writeSingleRegister(targetDev, addr, val)
		} else {
			// FC16 multiple registers
			writeErr = writeMultipleRegisters(targetDev, addr, encoded)
		}
	}

	if writeErr != nil {
		s.log.Error("modbus: write failed", "device", targetDev.DeviceID, "tag", tagID, "error", writeErr)
	} else {
		s.log.Info("modbus: write succeeded", "device", targetDev.DeviceID, "tag", tagID)
	}
}

// pollDevice runs the polling loop for a single device.
func (s *Scanner) pollDevice(dev *DeviceState) {
	s.log.Info("modbus: starting poll loop", "device", dev.DeviceID, "host", dev.Host, "port", dev.Port)

	for {
		select {
		case <-dev.stopChan:
			s.log.Info("modbus: poll loop stopped", "device", dev.DeviceID)
			disconnect(dev)
			return
		default:
		}

		// Check for backoff
		dev.mu.Lock()
		bo := dev.backoffDuration()
		dev.mu.Unlock()
		if bo > 0 {
			select {
			case <-dev.stopChan:
				disconnect(dev)
				return
			case <-time.After(bo):
			}
		}

		// Ensure connection
		dev.mu.Lock()
		if dev.conn == nil {
			dev.state = "connecting"
			if err := connect(dev); err != nil {
				dev.failures++
				dev.state = "disconnected"
				dev.lastError = err.Error()
				dev.lastErrorAt = time.Now().UnixMilli()
				dev.mu.Unlock()
				s.log.Warn("modbus: connection failed", "device", dev.DeviceID, "error", err, "backoff", dev.backoffDuration())
				continue
			}
			dev.state = "connected"
			dev.lastError = ""
			s.log.Info("modbus: connected", "device", dev.DeviceID)
		}
		blocks := make([]ReadBlock, len(dev.blocks))
		copy(blocks, dev.blocks)
		dev.mu.Unlock()

		// Read all blocks
		allValues := make(map[string]interface{})
		var readErr error
		for _, block := range blocks {
			dev.mu.Lock()
			vals, err := readBlock(dev, block, s.log)
			dev.mu.Unlock()
			if err != nil {
				readErr = err
				break
			}
			for k, v := range vals {
				allValues[k] = v
			}
		}

		if readErr != nil {
			dev.mu.Lock()
			dev.failures++
			dev.state = "error"
			dev.lastError = readErr.Error()
			dev.lastErrorAt = time.Now().UnixMilli()
			disconnect(dev)
			dev.mu.Unlock()
			s.log.Warn("modbus: read failed", "device", dev.DeviceID, "error", readErr, "backoff", dev.backoffDuration())
			continue
		}

		// Successful read — reset failure count
		dev.mu.Lock()
		dev.failures = 0
		dev.state = "connected"
		dev.lastReadAt = time.Now().UnixMilli()
		dev.lastError = ""
		dev.mu.Unlock()

		// Publish changed values
		s.publishChanges(dev, allValues)

		// Wait for next scan
		scanRate := dev.effectiveScanRate()
		select {
		case <-dev.stopChan:
			disconnect(dev)
			return
		case <-time.After(scanRate):
		}
	}
}

// publishChanges publishes values that have changed since the last scan.
func (s *Scanner) publishChanges(dev *DeviceState, values map[string]interface{}) {
	dev.mu.Lock()
	defer dev.mu.Unlock()

	nowMs := time.Now().UnixMilli()
	sanitizedDevice := types.SanitizeForSubject(dev.DeviceID)

	for tagID, val := range values {
		// Change detection
		lastVal, exists := dev.lastValues[tagID]
		if exists && fmt.Sprintf("%v", lastVal) == fmt.Sprintf("%v", val) {
			continue
		}
		dev.lastValues[tagID] = val

		tag, ok := dev.allTags[tagID]
		if !ok {
			continue
		}

		dt := modbusToNatsDatatype(tag.Datatype)
		sanitizedTag := types.SanitizeForSubject(tagID)

		msg := types.PlcDataMessage{
			ModuleID:   s.moduleID,
			DeviceID:   dev.DeviceID,
			VariableID: tagID,
			Value:      val,
			Timestamp:  nowMs,
			Datatype:   dt,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		subject := topics.Data("modbus", sanitizedDevice, sanitizedTag)
		if err := s.b.Publish(subject, data); err != nil {
			s.log.Warn("modbus: publish failed", "subject", subject, "error", err)
		}
	}
}

// stopDevice stops a device's polling loop and disconnects.
func stopDevice(dev *DeviceState) {
	if dev.stopped {
		return
	}
	dev.stopped = true
	close(dev.stopChan)
}

// rebuildDevicePlan merges all subscriber tags and builds optimized read blocks.
// Must be called with dev.mu held.
func rebuildDevicePlan(dev *DeviceState) {
	dev.allTags = make(map[string]itypes.ModbusTagConfig)
	for _, sub := range dev.Subscribers {
		for tagID, tag := range sub.Tags {
			dev.allTags[tagID] = tag
		}
	}
	dev.blocks = buildReadBlocks(dev.allTags, dev.ByteOrder)
}

// buildReadBlocks groups tags by function code and merges close tags into contiguous
// read blocks, with a maximum gap of maxGap registers between tags.
func buildReadBlocks(tags map[string]itypes.ModbusTagConfig, defaultByteOrder string) []ReadBlock {
	// Group tags by function code
	type tagEntry struct {
		tag     itypes.ModbusTagConfig
		fc      int
		addr    int
		regSize int
	}

	groups := make(map[int][]tagEntry)
	for _, tag := range tags {
		fc := fcNameToCode(tag.FunctionCode)
		regSize := 1
		if !fcIsCoilOrDiscrete(fc) {
			regSize = registerCount(tag.Datatype)
		}
		groups[fc] = append(groups[fc], tagEntry{
			tag:     tag,
			fc:      fc,
			addr:    tag.Address,
			regSize: regSize,
		})
	}

	var blocks []ReadBlock

	for fc, entries := range groups {
		// Sort by address
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].addr < entries[j].addr
		})

		// Merge into contiguous blocks
		var current *ReadBlock
		for _, e := range entries {
			if current == nil {
				current = &ReadBlock{
					FunctionCode: fc,
					StartAddr:    e.addr,
					Count:        e.regSize,
					Tags: []TagInBlock{{
						Tag:    e.tag,
						Offset: 0,
					}},
				}
				continue
			}

			endOfCurrent := current.StartAddr + current.Count
			gap := e.addr - endOfCurrent

			if gap <= maxGap {
				// Extend current block
				offset := e.addr - current.StartAddr
				newEnd := e.addr + e.regSize
				if newEnd-current.StartAddr > current.Count {
					current.Count = newEnd - current.StartAddr
				}
				current.Tags = append(current.Tags, TagInBlock{
					Tag:    e.tag,
					Offset: offset,
				})
			} else {
				// Start a new block
				blocks = append(blocks, *current)
				current = &ReadBlock{
					FunctionCode: fc,
					StartAddr:    e.addr,
					Count:        e.regSize,
					Tags: []TagInBlock{{
						Tag:    e.tag,
						Offset: 0,
					}},
				}
			}
		}
		if current != nil {
			blocks = append(blocks, *current)
		}
	}

	return blocks
}

// modbusToNatsDatatype converts a modbus datatype to a NATS datatype.
func modbusToNatsDatatype(dt string) string {
	switch dt {
	case "boolean":
		return "boolean"
	default:
		return "number"
	}
}

// sendReply sends a JSON reply with success/error fields.
func sendReply(reply bus.ReplyFunc, success bool, errMsg string) {
	if reply == nil {
		return
	}
	resp := struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}{
		Success: success,
		Error:   errMsg,
	}
	data, _ := json.Marshal(resp)
	_ = reply(data)
}
