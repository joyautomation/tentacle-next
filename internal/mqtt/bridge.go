//go:build mqtt || all

package mqtt

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/rbe"
	"github.com/joyautomation/tentacle/internal/sparkplug"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

// Bridge connects NATS data to a Sparkplug B MQTT node.
type Bridge struct {
	mu sync.RWMutex

	b        bus.Bus
	node     *SparkplugNode
	sf       *StoreForwardBuffer
	config   itypes.MqttBridgeConfig
	moduleID string

	// Variable tracking
	variables map[string]*PlcVariable // variableID → tracked var

	// Template registry
	templates *TemplateRegistry

	// Active NATS subscriptions
	dataSubs []bus.Subscription

	// Rebirth debounce
	rebirthTimer *time.Timer
	rebirthMu    sync.Mutex

	// Drain ticker
	drainStop chan struct{}

	enabled bool
}

// NewBridge creates a new NATS-to-MQTT bridge.
func NewBridge(b bus.Bus, moduleID string, config itypes.MqttBridgeConfig) *Bridge {
	return &Bridge{
		b:         b,
		moduleID:  moduleID,
		config:    config,
		variables: make(map[string]*PlcVariable),
		templates: NewTemplateRegistry(),
		enabled:   true,
	}
}

// Start connects to MQTT and begins bridging data.
func (br *Bridge) Start() error {
	br.node = NewSparkplugNode(br.config)

	// Set up DCMD callback
	br.node.OnDeviceCommand(br.handleDeviceCommand)
	br.node.OnNodeCommand(br.handleNodeCommand)

	// Set up store-forward
	br.sf = NewStoreForwardBuffer(br.config.StoreForwardMax, br.config.StoreForwardSize, br.config.DrainRate)
	if br.config.PrimaryHostID != "" {
		br.node.OnHostState(br.handleHostState)
	}

	// Connect to MQTT broker
	if err := br.node.Connect(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}

	// Subscribe to all data from all scanner modules
	br.subscribeToData()

	// Load initial variables from running modules
	br.loadInitialVariables()

	// Start drain goroutine
	br.drainStop = make(chan struct{})
	go br.drainLoop()

	// Subscribe to metrics request
	br.subscribeToMetricsRequest()

	// Subscribe to store-forward status request
	br.subscribeToSFStatus()

	return nil
}

// Stop disconnects from MQTT and cleans up.
func (br *Bridge) Stop() {
	br.mu.Lock()
	for _, sub := range br.dataSubs {
		_ = sub.Unsubscribe()
	}
	br.dataSubs = nil
	br.mu.Unlock()

	if br.drainStop != nil {
		close(br.drainStop)
	}

	if br.node != nil {
		br.node.Disconnect()
	}
}

// SetEnabled enables or disables data publishing.
func (br *Bridge) SetEnabled(enabled bool) {
	br.mu.Lock()
	br.enabled = enabled
	br.mu.Unlock()
	slog.Info("mqtt: bridge enabled state changed", "enabled", enabled)
}

// ═══════════════════════════════════════════════════════════════════════════
// Data subscription
// ═══════════════════════════════════════════════════════════════════════════

func (br *Bridge) subscribeToData() {
	// Subscribe to all data: *.data.>
	sub, err := br.b.Subscribe(topics.AllData(), func(subject string, data []byte, reply bus.ReplyFunc) {
		br.handleDataMessage(subject, data)
	})
	if err != nil {
		slog.Error("mqtt: failed to subscribe to data", "error", err)
		return
	}
	br.mu.Lock()
	br.dataSubs = append(br.dataSubs, sub)
	br.mu.Unlock()
}

func (br *Bridge) handleDataMessage(subject string, rawData []byte) {
	br.mu.RLock()
	enabled := br.enabled
	br.mu.RUnlock()
	if !enabled {
		return
	}

	// Parse the PlcDataMessage
	var msg types.PlcDataMessage
	if err := json.Unmarshal(rawData, &msg); err != nil {
		return
	}

	// Skip messages from the mqtt module itself
	if msg.ModuleID == br.moduleID {
		return
	}

	// Build a unique variable key
	varKey := variableKey(msg.ModuleID, msg.DeviceID, msg.VariableID)

	br.mu.Lock()
	pv, exists := br.variables[varKey]
	if !exists {
		// New variable discovered
		pv = &PlcVariable{
			ID:          msg.VariableID,
			ModuleID:    msg.ModuleID,
			DeviceID:    msg.DeviceID,
			Description: msg.Description,
			Datatype:    msg.Datatype,
			Deadband:    msg.Deadband,
			DisableRBE:  msg.DisableRBE,
			UdtTemplate: msg.UdtTemplate,
		}
		if msg.MemberDeadbands != nil {
			pv.MemberDeadbands = msg.MemberDeadbands
			pv.MemberRBEStates = make(map[string]*rbe.State)
		}
		pv.SparkplugType = sparkplug.NatsToSparkplugType(msg.Datatype)
		br.variables[varKey] = pv
	}

	pv.Value = msg.Value

	// Update deadband if changed
	if msg.Deadband != nil {
		pv.Deadband = msg.Deadband
	}
	if msg.MemberDeadbands != nil {
		pv.MemberDeadbands = msg.MemberDeadbands
		if pv.MemberRBEStates == nil {
			pv.MemberRBEStates = make(map[string]*rbe.State)
		}
	}
	br.mu.Unlock()

	// Register template if new UDT
	if msg.Datatype == "udt" && msg.UdtTemplate != nil {
		br.registerTemplate(msg.UdtTemplate)
	}

	// RBE check
	nowMs := time.Now().UnixMilli()
	if msg.Datatype == "udt" {
		if !br.shouldPublishUDT(pv, msg.Value, nowMs) {
			return
		}
	} else {
		if !rbe.ShouldPublish(&pv.RBEState, msg.Value, nowMs, pv.Deadband, pv.DisableRBE) {
			return
		}
		rbe.RecordPublish(&pv.RBEState, msg.Value, nowMs)
	}

	// Convert to Sparkplug metric and publish
	deviceID := br.sparkplugDeviceID()
	metric := br.valueToMetric(pv, msg.Value, nowMs)

	if br.sf.State() == SFOffline {
		// Buffer the data
		payload := &sparkplug.Payload{
			Timestamp: uint64(nowMs),
			Metrics:   []sparkplug.Metric{metric},
		}
		br.sf.Buffer(deviceID, payload)
		return
	}

	// Publish DDATA
	if err := br.node.PublishDeviceData(deviceID, []sparkplug.Metric{metric}); err != nil {
		slog.Debug("mqtt: failed to publish DDATA", "error", err)
		return
	}
	br.sf.RecordPublish(1)

	// Ensure device metrics are registered (for DBIRTH)
	br.ensureDeviceMetric(deviceID, varKey, metric)
}

// shouldPublishUDT checks per-member deadbands for UDT values.
func (br *Bridge) shouldPublishUDT(pv *PlcVariable, value interface{}, nowMs int64) bool {
	members, ok := value.(map[string]interface{})
	if !ok {
		// Fall back to scalar RBE
		if !rbe.ShouldPublish(&pv.RBEState, value, nowMs, pv.Deadband, pv.DisableRBE) {
			return false
		}
		rbe.RecordPublish(&pv.RBEState, value, nowMs)
		return true
	}

	anyChanged := false
	for memberName, memberVal := range members {
		db, hasDB := pv.MemberDeadbands[memberName]
		state, hasState := pv.MemberRBEStates[memberName]
		if !hasState {
			state = &rbe.State{}
			if pv.MemberRBEStates == nil {
				pv.MemberRBEStates = make(map[string]*rbe.State)
			}
			pv.MemberRBEStates[memberName] = state
		}

		var dbPtr *types.DeadBandConfig
		if hasDB {
			dbPtr = &db
		}

		if rbe.ShouldPublish(state, memberVal, nowMs, dbPtr, pv.DisableRBE) {
			rbe.RecordPublish(state, memberVal, nowMs)
			anyChanged = true
		}
	}
	return anyChanged
}

// valueToMetric converts a PlcVariable value to a Sparkplug metric.
func (br *Bridge) valueToMetric(pv *PlcVariable, value interface{}, nowMs int64) sparkplug.Metric {
	name := pv.ID
	if pv.ModuleID != "" {
		name = pv.ModuleID + "/" + pv.ID
	}

	if pv.Datatype == "udt" && br.config.UseTemplates && pv.UdtTemplate != nil {
		return br.udtToTemplateMetric(pv, value, nowMs)
	}

	if pv.Datatype == "udt" && !br.config.UseTemplates {
		// Flat mode: publish each member as a separate metric
		// This is handled at a higher level; here we just publish the whole value as string
		return sparkplug.Metric{
			Name:      name,
			Datatype:  sparkplug.TypeString,
			Timestamp: uint64(nowMs),
			Value:     fmt.Sprintf("%v", value),
		}
	}

	// Coerce value to proper Go type
	goVal := coerceValue(value, pv.SparkplugType)

	return sparkplug.Metric{
		Name:      name,
		Datatype:  pv.SparkplugType,
		Timestamp: uint64(nowMs),
		Value:     goVal,
	}
}

// udtToTemplateMetric converts a UDT value to a Sparkplug Template metric.
func (br *Bridge) udtToTemplateMetric(pv *PlcVariable, value interface{}, nowMs int64) sparkplug.Metric {
	tmplName := pv.UdtTemplate.Name
	name := pv.ID
	if pv.ModuleID != "" {
		name = pv.ModuleID + "/" + pv.ID
	}

	members, _ := value.(map[string]interface{})
	var tmplMetrics []sparkplug.Metric
	if members != nil && pv.UdtTemplate != nil {
		for _, mdef := range pv.UdtTemplate.Members {
			mval, ok := members[mdef.Name]
			if !ok {
				continue
			}
			mtype := sparkplug.NatsToSparkplugType(mdef.Datatype)
			tmplMetrics = append(tmplMetrics, sparkplug.Metric{
				Name:      mdef.Name,
				Datatype:  mtype,
				Timestamp: uint64(nowMs),
				Value:     coerceValue(mval, mtype),
			})
		}
	}

	return sparkplug.Metric{
		Name:      name,
		Datatype:  sparkplug.TypeTemplate,
		Timestamp: uint64(nowMs),
		Value: &sparkplug.Template{
			TemplateRef: tmplName,
			Metrics:     tmplMetrics,
		},
	}
}

// registerTemplate adds a UDT template definition to the node's NBIRTH metrics.
func (br *Bridge) registerTemplate(tmpl *types.UdtTemplateDefinition) {
	if br.templates.Has(tmpl.Name) {
		return
	}

	// Build Sparkplug Template definition
	var members []sparkplug.Metric
	for _, m := range tmpl.Members {
		dt := sparkplug.NatsToSparkplugType(m.Datatype)
		members = append(members, sparkplug.Metric{
			Name:     m.Name,
			Datatype: dt,
		})
	}

	spTmpl := &sparkplug.Template{
		Version:      tmpl.Version,
		IsDefinition: true,
		Metrics:      members,
	}
	br.templates.Register(tmpl.Name, spTmpl)

	slog.Info("mqtt: registered template", "name", tmpl.Name, "members", len(tmpl.Members))

	// Schedule rebirth to include new template definition in NBIRTH
	br.scheduleRebirth()
}

// scheduleRebirth debounces rebirth requests (500ms window).
func (br *Bridge) scheduleRebirth() {
	br.rebirthMu.Lock()
	defer br.rebirthMu.Unlock()

	if br.rebirthTimer != nil {
		br.rebirthTimer.Stop()
	}
	br.rebirthTimer = time.AfterFunc(500*time.Millisecond, func() {
		br.updateNodeMetrics()
		br.node.Rebirth()
	})
}

// updateNodeMetrics rebuilds the node-level metrics (template definitions).
func (br *Bridge) updateNodeMetrics() {
	var nodeMetrics []sparkplug.Metric
	for name, tmpl := range br.templates.All() {
		nodeMetrics = append(nodeMetrics, sparkplug.Metric{
			Name:      name,
			Datatype:  sparkplug.TypeTemplate,
			Timestamp: uint64(time.Now().UnixMilli()),
			Value:     tmpl,
		})
	}
	br.node.SetNodeMetrics(nodeMetrics)
}

// ensureDeviceMetric tracks a metric for device DBIRTH purposes.
func (br *Bridge) ensureDeviceMetric(deviceID, varKey string, metric sparkplug.Metric) {
	br.mu.Lock()
	defer br.mu.Unlock()

	// We don't duplicate-check here for performance; the node handles it via SetDeviceMetrics
	// This is called on every publish, but the DBIRTH is only sent on birth/rebirth
}

// ═══════════════════════════════════════════════════════════════════════════
// DCMD handling: MQTT → NATS
// ═══════════════════════════════════════════════════════════════════════════

func (br *Bridge) handleDeviceCommand(deviceID string, metrics []sparkplug.Metric) {
	for _, m := range metrics {
		br.routeCommand(m)
	}
}

func (br *Bridge) handleNodeCommand(metrics []sparkplug.Metric) {
	for _, m := range metrics {
		if m.Name == "Node Control/Rebirth" {
			continue // Handled by the node itself
		}
		slog.Info("mqtt: received NCMD", "metric", m.Name)
	}
}

func (br *Bridge) routeCommand(m sparkplug.Metric) {
	br.mu.RLock()
	defer br.mu.RUnlock()

	// Find the source variable by metric name
	var sourceVar *PlcVariable
	for _, pv := range br.variables {
		fullName := pv.ModuleID + "/" + pv.ID
		if fullName == m.Name || pv.ID == m.Name {
			sourceVar = pv
			break
		}
	}

	if sourceVar == nil {
		// Try matching just the last segment
		lastSeg := m.Name
		if idx := strings.LastIndex(m.Name, "/"); idx >= 0 {
			lastSeg = m.Name[idx+1:]
		}
		for _, pv := range br.variables {
			if pv.ID == lastSeg {
				sourceVar = pv
				break
			}
		}
	}

	if sourceVar == nil {
		slog.Warn("mqtt: DCMD for unknown variable", "name", m.Name)
		return
	}

	// Handle template commands (write to each member)
	if tmpl, ok := m.Value.(*sparkplug.Template); ok && tmpl != nil {
		for _, member := range tmpl.Metrics {
			cmdSubject := topics.Command(sourceVar.ModuleID, sourceVar.ID+"/"+member.Name)
			val := sparkplugValueToJSON(member.Value)
			data, _ := json.Marshal(val)
			_ = br.b.Publish(cmdSubject, data)
		}
		return
	}

	// Scalar command
	cmdSubject := topics.Command(sourceVar.ModuleID, sourceVar.ID)
	val := sparkplugValueToJSON(m.Value)
	data, _ := json.Marshal(val)
	_ = br.b.Publish(cmdSubject, data)
}

// ═══════════════════════════════════════════════════════════════════════════
// Store & Forward
// ═══════════════════════════════════════════════════════════════════════════

func (br *Bridge) handleHostState(hostID string, online bool) {
	if online {
		slog.Info("mqtt: primary host online", "host", hostID)
		br.sf.SetOnline()
	} else {
		slog.Info("mqtt: primary host offline", "host", hostID)
		br.sf.SetOffline()
	}
}

func (br *Bridge) drainLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-br.drainStop:
			return
		case <-ticker.C:
			records := br.sf.Drain()
			for _, rec := range records {
				if err := br.node.PublishDeviceDataPayload(rec.DeviceID, rec.Payload); err != nil {
					slog.Warn("mqtt: drain publish failed", "error", err)
				}
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Initial variable loading
// ═══════════════════════════════════════════════════════════════════════════

func (br *Bridge) loadInitialVariables() {
	// Query all heartbeats to find running modules
	keys, err := br.b.KVKeys(topics.BucketHeartbeats)
	if err != nil {
		slog.Warn("mqtt: failed to list heartbeats", "error", err)
		return
	}

	for _, moduleID := range keys {
		if moduleID == br.moduleID {
			continue // Skip self
		}

		// Request variables from each module
		subject := topics.Variables(moduleID)
		resp, err := br.b.Request(subject, nil, 3*time.Second)
		if err != nil {
			slog.Debug("mqtt: no variables response from module", "module", moduleID)
			continue
		}

		var vars []types.VariableInfo
		if err := json.Unmarshal(resp, &vars); err != nil {
			slog.Warn("mqtt: failed to parse variables from module", "module", moduleID, "error", err)
			continue
		}

		for _, v := range vars {
			varKey := variableKey(v.ModuleID, v.DeviceID, v.VariableID)
			pv := &PlcVariable{
				ID:            v.VariableID,
				ModuleID:      v.ModuleID,
				DeviceID:      v.DeviceID,
				Description:   v.Description,
				Datatype:      v.Datatype,
				Value:         v.Value,
				Deadband:      v.Deadband,
				DisableRBE:    v.DisableRBE,
				SparkplugType: sparkplug.NatsToSparkplugType(v.Datatype),
			}
			if v.UdtTemplate != nil {
				pv.UdtTemplate = v.UdtTemplate
				br.registerTemplate(v.UdtTemplate)
			}
			br.mu.Lock()
			br.variables[varKey] = pv
			br.mu.Unlock()
		}

		slog.Info("mqtt: loaded initial variables", "module", moduleID, "count", len(vars))
	}

	// Build initial device metrics for DBIRTH
	br.buildDeviceMetrics()
}

func (br *Bridge) buildDeviceMetrics() {
	br.mu.RLock()
	defer br.mu.RUnlock()

	deviceID := br.sparkplugDeviceID()
	nowMs := time.Now().UnixMilli()

	var metrics []sparkplug.Metric
	for _, pv := range br.variables {
		m := br.valueToMetric(pv, pv.Value, nowMs)
		metrics = append(metrics, m)
	}

	if len(metrics) > 0 {
		isNew := br.node.SetDeviceMetrics(deviceID, metrics)
		if isNew && br.node.State() == StateBorn {
			br.node.PublishDeviceBirth(deviceID)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Request handlers
// ═══════════════════════════════════════════════════════════════════════════

func (br *Bridge) subscribeToMetricsRequest() {
	sub, err := br.b.Subscribe(topics.MqttMetrics, func(subject string, data []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}

		br.mu.RLock()
		defer br.mu.RUnlock()

		var metricInfos []itypes.MqttMetricInfo
		for _, pv := range br.variables {
			info := itypes.MqttMetricInfo{
				Name:         pv.ID,
				SparkplugType: sparkplug.TypeToString(pv.SparkplugType),
				Value:        pv.Value,
				ModuleID:     pv.ModuleID,
				Datatype:     pv.Datatype,
			}
			if pv.UdtTemplate != nil {
				info.TemplateRef = pv.UdtTemplate.Name
			}
			metricInfos = append(metricInfos, info)
		}

		var tmplInfos []itypes.MqttTemplateInfo
		for name, tmpl := range br.templates.All() {
			info := itypes.MqttTemplateInfo{Name: name, Version: tmpl.Version}
			for _, m := range tmpl.Metrics {
				info.Members = append(info.Members, struct {
					Name        string `json:"name"`
					Datatype    string `json:"datatype"`
					TemplateRef string `json:"templateRef,omitempty"`
				}{
					Name:     m.Name,
					Datatype: sparkplug.TypeToString(m.Datatype),
				})
			}
			tmplInfos = append(tmplInfos, info)
		}

		resp := itypes.MqttMetricsResponse{
			Metrics:   metricInfos,
			Templates: tmplInfos,
			DeviceID:  br.sparkplugDeviceID(),
			Timestamp: time.Now().UnixMilli(),
		}

		respData, err := json.Marshal(resp)
		if err != nil {
			return
		}
		_ = reply(respData)
	})
	if err != nil {
		slog.Error("mqtt: failed to subscribe to metrics request", "error", err)
		return
	}
	br.mu.Lock()
	br.dataSubs = append(br.dataSubs, sub)
	br.mu.Unlock()
}

func (br *Bridge) subscribeToSFStatus() {
	sub, err := br.b.Subscribe("mqtt.store-forward", func(subject string, data []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}
		status := br.sf.Status()
		respData, err := json.Marshal(status)
		if err != nil {
			return
		}
		_ = reply(respData)
	})
	if err != nil {
		slog.Error("mqtt: failed to subscribe to store-forward status request", "error", err)
		return
	}
	br.mu.Lock()
	br.dataSubs = append(br.dataSubs, sub)
	br.mu.Unlock()
}

// ═══════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════

func (br *Bridge) sparkplugDeviceID() string {
	if br.config.DeviceID != "" {
		return br.config.DeviceID
	}
	return br.moduleID
}

func variableKey(moduleID, deviceID, variableID string) string {
	return moduleID + ":" + deviceID + ":" + variableID
}

// coerceValue converts a JSON-deserialized interface{} to a proper Go type for protobuf.
func coerceValue(value interface{}, spType uint32) interface{} {
	if value == nil {
		return nil
	}

	switch spType {
	case sparkplug.TypeDouble:
		switch v := value.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case json.Number:
			f, _ := v.Float64()
			return f
		case int:
			return float64(v)
		case int64:
			return float64(v)
		default:
			return 0.0
		}

	case sparkplug.TypeFloat:
		switch v := value.(type) {
		case float64:
			return float32(v)
		case float32:
			return v
		default:
			return float32(0)
		}

	case sparkplug.TypeBoolean:
		switch v := value.(type) {
		case bool:
			return v
		case float64:
			return v != 0
		case string:
			return v == "true" || v == "1" || v == "on" || v == "yes"
		default:
			return false
		}

	case sparkplug.TypeString, sparkplug.TypeText:
		switch v := value.(type) {
		case string:
			return v
		default:
			return fmt.Sprintf("%v", v)
		}

	case sparkplug.TypeInt32:
		switch v := value.(type) {
		case float64:
			return uint32(int32(v))
		default:
			return uint32(0)
		}

	case sparkplug.TypeUInt64:
		switch v := value.(type) {
		case float64:
			return uint64(v)
		default:
			return uint64(0)
		}

	default:
		return value
	}
}

// sparkplugValueToJSON converts a Sparkplug metric value back to a JSON-friendly value.
func sparkplugValueToJSON(value interface{}) interface{} {
	switch v := value.(type) {
	case uint32:
		return int(v)
	case uint64:
		return v
	case float32:
		return float64(v)
	default:
		return v
	}
}
