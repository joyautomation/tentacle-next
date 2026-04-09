//go:build ethernetipserver || all

package ethernetipserver

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/types"
)

// Manager coordinates bus subscriptions, the tag database, and the CIP server.
type Manager struct {
	b         bus.Bus
	moduleID  string
	tagDB     *TagDatabase
	udtDB     *UdtDatabase
	server    *CIPServer
	writeback chan WritebackEvent
	log       *slog.Logger

	// Track bus subscriptions for data sources
	dataSubs map[string]bus.Subscription // Bus subject -> subscription
	busSubs  []bus.Subscription          // Request handler subscriptions

	// Track subscribers
	subscribers map[string]*ServerSubscribeRequest // subscriberID -> request

	mu sync.Mutex
}

// NewManager creates a new manager.
func NewManager(b bus.Bus, moduleID string, log *slog.Logger) *Manager {
	tagDB := NewTagDatabase()
	udtDB := NewUdtDatabase()
	writeback := make(chan WritebackEvent, 256)

	provider := NewTentacleTagProvider(tagDB, udtDB, writeback, log)

	return &Manager{
		b:           b,
		moduleID:    moduleID,
		tagDB:       tagDB,
		udtDB:       udtDB,
		server:      NewCIPServer(provider, 44818, log),
		writeback:   writeback,
		dataSubs:    make(map[string]bus.Subscription),
		subscribers: make(map[string]*ServerSubscribeRequest),
		log:         log,
	}
}

// Start registers bus request handlers and starts the writeback processor.
func (m *Manager) Start() {
	// Register bus request handlers
	m.registerHandler(m.moduleID+".subscribe", m.handleSubscribe)
	m.registerHandler(m.moduleID+".unsubscribe", m.handleUnsubscribe)
	m.registerHandler(m.moduleID+".variables", m.handleVariables)
	m.registerHandler(m.moduleID+".browse", m.handleBrowse)

	// Start writeback processor
	go m.processWritebacks()

	m.log.Info("eipserver: manager started, listening for requests", "prefix", m.moduleID)
}

// Stop cleans up all subscriptions and stops the CIP server.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Unsubscribe from all data sources
	for subj, sub := range m.dataSubs {
		if err := sub.Unsubscribe(); err != nil {
			m.log.Warn("eipserver: failed to unsubscribe from data source", "subject", subj, "error", err)
		}
	}
	m.dataSubs = make(map[string]bus.Subscription)

	// Unsubscribe from request handlers
	for _, sub := range m.busSubs {
		_ = sub.Unsubscribe()
	}
	m.busSubs = nil

	// Stop CIP server
	m.server.Stop()

	m.log.Info("eipserver: manager stopped")
}

// registerHandler subscribes to a bus subject and tracks the subscription.
func (m *Manager) registerHandler(subject string, handler bus.MessageHandler) {
	sub, err := m.b.Subscribe(subject, handler)
	if err != nil {
		m.log.Error("eipserver: failed to subscribe", "subject", subject, "error", err)
		return
	}
	m.busSubs = append(m.busSubs, sub)
}

// handleSubscribe processes ethernetip-server.subscribe requests.
func (m *Manager) handleSubscribe(subject string, data []byte, reply bus.ReplyFunc) {
	var req ServerSubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		m.sendReply(reply, map[string]interface{}{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	if req.SubscriberID == "" {
		m.sendReply(reply, map[string]interface{}{"error": "subscriberId is required"})
		return
	}

	listenPort := req.ListenPort
	if listenPort == 0 {
		listenPort = 44818
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store subscriber
	m.subscribers[req.SubscriberID] = &req

	// Register UDT types
	for _, udt := range req.Udts {
		m.udtDB.RegisterType(udt)
		m.log.Info("eipserver: registered UDT type", "name", udt.Name, "members", len(udt.Members))
	}

	// Register tags
	tagsAdded := 0
	for _, tag := range req.Tags {
		// Determine if this is a UDT tag
		isUdt := false
		udtType := ""
		if _, ok := m.udtDB.GetType(tag.CipType); ok {
			isUdt = true
			udtType = tag.CipType
		}

		// Create tag entry in database
		var defaultVal interface{}
		if isUdt {
			if err := m.udtDB.CreateInstance(tag.Name, tag.CipType); err != nil {
				m.log.Warn("eipserver: failed to create UDT instance", "tag", tag.Name, "error", err)
				continue
			}
			defaultVal = m.udtDB.ReadAll(tag.Name)
		} else {
			defaultVal = cipDefaultValue(tag.CipType)
		}

		datatype := cipToNatsDatatype(tag.CipType)
		if isUdt {
			datatype = "udt"
		}

		m.tagDB.Set(tag.Name, &TagEntry{
			Name:        tag.Name,
			CipType:     tag.CipType,
			Datatype:    datatype,
			Value:       defaultVal,
			Source:      tag.Source,
			Writable:    tag.Writable,
			LastUpdated: time.Now().UnixMilli(),
			IsUdt:       isUdt,
			UdtType:     udtType,
		})

		// Subscribe to source bus subject if not already subscribed
		if tag.Source != "" {
			if err := m.subscribeToSource(tag.Name, tag.Source, tag.CipType, isUdt); err != nil {
				m.log.Warn("eipserver: failed to subscribe to source", "source", tag.Source, "tag", tag.Name, "error", err)
			}
		}

		tagsAdded++
		m.log.Debug("eipserver: registered tag", "name", tag.Name, "type", tag.CipType, "source", tag.Source, "writable", tag.Writable, "udt", isUdt)
	}

	// Start CIP server if not running
	if err := m.server.Start(); err != nil {
		m.log.Error("eipserver: failed to start CIP server", "error", err)
		m.sendReply(reply, map[string]interface{}{"error": fmt.Sprintf("failed to start CIP server: %v", err)})
		return
	}

	m.log.Info("eipserver: subscriber registered", "subscriberId", req.SubscriberID, "tags", tagsAdded, "udts", len(req.Udts), "port", listenPort)

	m.sendReply(reply, map[string]interface{}{
		"success":  true,
		"tags":     tagsAdded,
		"udts":     len(req.Udts),
		"port":     listenPort,
		"serverId": m.moduleID,
	})
}

// handleUnsubscribe processes ethernetip-server.unsubscribe requests.
func (m *Manager) handleUnsubscribe(subject string, data []byte, reply bus.ReplyFunc) {
	var req ServerUnsubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		m.sendReply(reply, map[string]interface{}{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.subscribers[req.SubscriberID]; !ok {
		m.sendReply(reply, map[string]interface{}{"error": "subscriber not found"})
		return
	}

	delete(m.subscribers, req.SubscriberID)

	// If no subscribers remain, clean up
	if len(m.subscribers) == 0 {
		// Unsubscribe from all data sources
		for subj, sub := range m.dataSubs {
			_ = sub.Unsubscribe()
			delete(m.dataSubs, subj)
		}
		m.server.Stop()
		m.log.Info("eipserver: all subscribers removed, CIP server stopped")
	}

	m.sendReply(reply, map[string]interface{}{"success": true})
}

// handleVariables returns current tag state.
func (m *Manager) handleVariables(_ string, _ []byte, reply bus.ReplyFunc) {
	tags := m.tagDB.All()
	result := make([]TagInfo, 0, len(tags))
	for _, entry := range tags {
		result = append(result, TagInfo{
			Name:        entry.Name,
			CipType:     entry.CipType,
			Value:       entry.Value,
			Datatype:    entry.Datatype,
			Writable:    entry.Writable,
			Source:      entry.Source,
			LastUpdated: entry.LastUpdated,
		})
	}
	m.sendReply(reply, result)
}

// handleBrowse returns the list of exposed tags.
func (m *Manager) handleBrowse(_ string, _ []byte, reply bus.ReplyFunc) {
	tags := m.tagDB.All()
	result := make([]map[string]interface{}, 0, len(tags))
	for _, entry := range tags {
		tagInfo := map[string]interface{}{
			"name":     entry.Name,
			"cipType":  entry.CipType,
			"datatype": entry.Datatype,
			"writable": entry.Writable,
		}
		if entry.IsUdt {
			tagInfo["udtType"] = entry.UdtType
		}
		result = append(result, tagInfo)
	}
	m.sendReply(reply, result)
}

// subscribeToSource subscribes to a bus subject for tag value updates.
func (m *Manager) subscribeToSource(tagName, source, cipType string, isUdt bool) error {
	// For UDT tags, subscribe to wildcard to catch member updates
	// e.g., source = "plc.data.project1.MyTimer" -> subscribe to "plc.data.project1.MyTimer.>"
	if isUdt {
		wildcardSource := source + ".>"
		if _, ok := m.dataSubs[wildcardSource]; ok {
			return nil // Already subscribed
		}

		sub, err := m.b.Subscribe(wildcardSource, func(subj string, msgData []byte, _ bus.ReplyFunc) {
			m.handleUdtMemberUpdate(tagName, source, subj, msgData)
		})
		if err != nil {
			return err
		}
		m.dataSubs[wildcardSource] = sub

		// Also subscribe to the base subject for whole-UDT updates
		if _, ok := m.dataSubs[source]; !ok {
			sub2, err := m.b.Subscribe(source, func(subj string, msgData []byte, _ bus.ReplyFunc) {
				m.handleUdtWholeUpdate(tagName, msgData)
			})
			if err != nil {
				return err
			}
			m.dataSubs[source] = sub2
		}
		return nil
	}

	// Scalar tag: subscribe to exact subject
	if _, ok := m.dataSubs[source]; ok {
		return nil // Already subscribed
	}

	sub, err := m.b.Subscribe(source, func(subj string, msgData []byte, _ bus.ReplyFunc) {
		m.handleScalarUpdate(tagName, cipType, msgData)
	})
	if err != nil {
		return err
	}
	m.dataSubs[source] = sub
	return nil
}

// handleScalarUpdate processes a bus message for a scalar tag.
func (m *Manager) handleScalarUpdate(tagName, cipType string, data []byte) {
	var msg types.PlcDataMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		m.log.Debug("eipserver: failed to unmarshal data", "tag", tagName, "error", err)
		return
	}

	coerced := coerceValue(cipType, msg.Value)
	m.tagDB.UpdateValue(tagName, coerced)
}

// handleUdtMemberUpdate processes a bus message for a UDT member update.
// The subject suffix after the base source indicates the member path.
func (m *Manager) handleUdtMemberUpdate(tagName, baseSource, subject string, data []byte) {
	// Extract member path from subject
	// e.g., subject = "plc.data.project1.MyTimer.ACC", baseSource = "plc.data.project1.MyTimer"
	// -> memberPath = "ACC"
	memberPath := strings.TrimPrefix(subject, baseSource+".")
	if memberPath == "" || memberPath == subject {
		return
	}

	var msg types.PlcDataMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		m.log.Debug("eipserver: failed to unmarshal UDT member data", "tag", tagName, "member", memberPath, "error", err)
		return
	}

	// Look up the UDT member's CIP type for proper coercion
	coerced := m.coerceUdtMemberValue(tagName, memberPath, msg.Value)

	if m.udtDB.UpdateMember(tagName, memberPath, coerced) {
		m.udtDB.SyncToTagDatabase(tagName, m.tagDB)
	}
}

// handleUdtWholeUpdate processes a bus message for a whole UDT value update.
func (m *Manager) handleUdtWholeUpdate(tagName string, data []byte) {
	var msg types.PlcDataMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		m.log.Debug("eipserver: failed to unmarshal whole UDT data", "tag", tagName, "error", err)
		return
	}

	// For whole UDT updates, value should be a map
	valueMap, ok := msg.Value.(map[string]interface{})
	if !ok {
		return
	}

	for memberName, memberValue := range valueMap {
		coerced := m.coerceUdtMemberValue(tagName, memberName, memberValue)
		m.udtDB.UpdateMember(tagName, memberName, coerced)
	}
	m.udtDB.SyncToTagDatabase(tagName, m.tagDB)
}

// coerceUdtMemberValue looks up the CIP type for a UDT member and coerces the value.
func (m *Manager) coerceUdtMemberValue(tagName, memberPath string, value interface{}) interface{} {
	entry, ok := m.tagDB.Get(tagName)
	if !ok {
		return value
	}

	udtType, ok := m.udtDB.GetType(entry.UdtType)
	if !ok {
		return value
	}

	// Find the member definition
	parts := strings.SplitN(memberPath, ".", 2)
	for _, member := range udtType.Members {
		if member.Name == parts[0] {
			if len(parts) > 1 && member.TemplateRef != "" {
				// Nested: look up nested type's member
				nestedType, ok := m.udtDB.GetType(member.TemplateRef)
				if ok {
					for _, nm := range nestedType.Members {
						if nm.Name == parts[1] {
							return coerceValue(nm.CipType, value)
						}
					}
				}
			}
			return coerceValue(member.CipType, value)
		}
	}
	return value
}

// processWritebacks reads from the writeback channel and publishes to the bus.
func (m *Manager) processWritebacks() {
	for event := range m.writeback {
		// Find the tag's source subject to derive the command subject
		parts := strings.SplitN(event.TagName, ".", 2)
		baseName := parts[0]

		entry, ok := m.tagDB.Get(baseName)
		if !ok {
			continue
		}

		// Derive command subject from source subject
		// source: "plc.data.project1.MyVar" -> command: "plc.command.project1.MyVar"
		cmdSubject := deriveCommandSubject(entry.Source)
		if cmdSubject == "" {
			m.log.Warn("eipserver: cannot derive command subject", "tag", event.TagName, "source", entry.Source)
			continue
		}

		// If writing a UDT member, append the member path to the command subject
		if len(parts) > 1 {
			cmdSubject = cmdSubject + "." + parts[1]
		}

		// Publish write command
		cmdMsg := types.PlcDataMessage{
			ModuleID:   m.moduleID,
			DeviceID:   m.moduleID,
			VariableID: event.TagName,
			Value:      event.Value,
			Timestamp:  time.Now().UnixMilli(),
			Datatype:   cipToNatsDatatype(event.CipType),
		}

		data, err := json.Marshal(cmdMsg)
		if err != nil {
			m.log.Warn("eipserver: failed to marshal writeback", "tag", event.TagName, "error", err)
			continue
		}

		if err := m.b.Publish(cmdSubject, data); err != nil {
			m.log.Warn("eipserver: failed to publish writeback", "tag", event.TagName, "subject", cmdSubject, "error", err)
		} else {
			m.log.Debug("eipserver: CIP write -> bus", "tag", event.TagName, "value", event.Value, "subject", cmdSubject)
		}
	}
}

// deriveCommandSubject converts a data source subject to a command subject.
// "plc.data.project1.MyVar" -> "plc.command.project1.MyVar"
// "{moduleId}.data.{rest}" -> "{moduleId}.command.{rest}"
func deriveCommandSubject(source string) string {
	parts := strings.SplitN(source, ".", 3)
	if len(parts) < 3 {
		return ""
	}
	if parts[1] != "data" {
		return ""
	}
	return parts[0] + ".command." + parts[2]
}

// sendReply marshals a payload to JSON and sends it via the reply function.
func (m *Manager) sendReply(reply bus.ReplyFunc, payload interface{}) {
	if reply == nil {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		m.log.Warn("eipserver: failed to marshal reply", "error", err)
		return
	}
	if err := reply(data); err != nil {
		m.log.Warn("eipserver: failed to send reply", "error", err)
	}
}
