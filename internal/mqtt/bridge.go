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
	log      *slog.Logger
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

	enabled  bool
	stopping bool

	// Inputs to the store-forward state reconciler. hostOnline starts true
	// when no primary host is configured (we publish whenever the node is
	// born); otherwise it flips with STATE messages from the primary host.
	// hostSeen records whether we've ever received a STATE message — it
	// lets the UI distinguish "waiting for primary host" (never seen) from
	// "primary host went offline" (seen then lost).
	hostOnline bool
	hostSeen   bool
}

// NewBridge creates a new NATS-to-MQTT bridge.
func NewBridge(b bus.Bus, moduleID string, config itypes.MqttBridgeConfig, log *slog.Logger) *Bridge {
	return &Bridge{
		b:         b,
		log:       log,
		moduleID:  moduleID,
		config:    config,
		variables: make(map[string]*PlcVariable),
		templates: NewTemplateRegistry(),
		enabled:   true,
	}
}

// Start connects to MQTT and begins bridging data.
// Bus subscriptions for metrics, store-forward status, and data are set up
// regardless of broker connectivity so the module stays responsive and
// buffers data when the broker is unreachable.
func (br *Bridge) Start() error {
	br.node = NewSparkplugNode(br.config, br.log)

	// Set up callbacks
	br.node.OnDeviceCommand(br.handleDeviceCommand)
	br.node.OnNodeCommand(br.handleNodeCommand)
	br.node.OnBeforeBirth(func() {
		br.buildDeviceMetrics()
		br.resetRBEStates()
	})

	// Set up store-forward
	br.sf = NewStoreForwardBuffer(br.config.StoreForwardMax, br.config.StoreForwardSize, br.config.DrainRate, br.log)

	// If no primary host is configured, the "host online" input is held high
	// so SF state is driven entirely by node state. Otherwise it starts low
	// and flips when a STATE message arrives.
	br.hostOnline = br.config.PrimaryHostID == ""
	if br.config.PrimaryHostID != "" {
		br.node.OnHostState(br.handleHostState)
	}
	br.node.OnStateChange(br.handleNodeStateChange)

	// SF starts offline until we've both connected (node Born) and, if
	// configured, learned the primary host is online.
	br.sf.SetOffline()

	// Set up bus subscriptions BEFORE connecting so the module responds to
	// status queries and buffers incoming data even when the broker is down.
	br.subscribeToMetricsRequest()
	br.subscribeToSFStatus()
	br.subscribeToData()

	// Start drain goroutine
	br.drainStop = make(chan struct{})
	go br.drainLoop()

	// Connect to MQTT broker
	if err := br.node.Connect(); err != nil {
		br.log.Warn("mqtt: broker unreachable, starting in offline/buffering mode", "error", err)
		go br.reconnectLoop()
		return nil
	}

	// Load initial variables and publish DBIRTH BEFORE data starts flowing.
	// This prevents a race where DDATA is sent before DBIRTH — Ignition
	// ignores DDATA for unknown metrics, and RBE marks them as published,
	// so slowly-changing values never get republished after DBIRTH.
	br.loadInitialVariables()

	return nil
}

// reconnectLoop retries the broker connection until either the node is
// connected again or the bridge is stopping. Used after initial connect
// failure and after a host-offline-driven Disconnect — the TCK expects
// the edge node to re-establish the session promptly, so the first
// retry happens after a short delay rather than the long idle interval.
func (br *Bridge) reconnectLoop() {
	delay := 1 * time.Second
	for {
		if br.isStopping() {
			return
		}
		select {
		case <-br.drainStop:
			return
		case <-time.After(delay):
		}
		if br.node.State() != StateDisconnected {
			return // already connected (e.g. via paho auto-reconnect)
		}
		br.log.Info("mqtt: attempting to reconnect to broker")
		if err := br.node.Connect(); err != nil {
			br.log.Warn("mqtt: reconnect failed", "error", err)
			if delay < 30*time.Second {
				delay *= 2
			}
			continue
		}
		br.loadInitialVariables()
		return
	}
}

// Stop disconnects from MQTT and cleans up.
func (br *Bridge) Stop() {
	br.mu.Lock()
	br.stopping = true
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
	br.log.Info("mqtt: bridge enabled state changed", "enabled", enabled)
}

// ═══════════════════════════════════════════════════════════════════════════
// Data subscription
// ═══════════════════════════════════════════════════════════════════════════

func (br *Bridge) subscribeToData() {
	// Subscribe only to gateway module data — the gateway applies config
	// transforms (UDT assembly, deadbands, etc.) before re-publishing.
	sub, err := br.b.Subscribe(topics.DataWildcard("gateway"), func(subject string, data []byte, reply bus.ReplyFunc) {
		br.handleDataMessage(subject, data)
	})
	if err != nil {
		br.log.Error("mqtt: failed to subscribe to data", "error", err)
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
	isNew := !exists

	pv.Value = msg.Value
	pv.LastUpdated = msg.Timestamp

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
	// Device ID: use source device from message, fall back to module ID
	deviceID := msg.DeviceID
	if deviceID == "" {
		deviceID = br.moduleID
	}
	metric := br.valueToMetric(pv, msg.Value, nowMs)

	// New variables MUST first appear in DBIRTH. Publishing DDATA before the
	// metric is birthed violates Sparkplug B `principles-birth-certificates-
	// order` and the TCK rejects it. Schedule a rebirth — once the next DBIRTH
	// includes this variable, future updates flow as DDATA.
	if isNew {
		br.scheduleRebirth()
		return
	}

	// Buffer data when broker is unreachable or store-forward is offline
	if br.sf.State() == SFOffline || br.node.State() != StateBorn {
		payload := &sparkplug.Payload{
			Timestamp: uint64(nowMs),
			Metrics:   []sparkplug.Metric{metric},
		}
		br.sf.Buffer(deviceID, payload)
		return
	}

	// Publish DDATA
	if err := br.node.PublishDeviceData(deviceID, []sparkplug.Metric{metric}); err != nil {
		br.log.Debug("mqtt: failed to publish DDATA", "error", err)
		return
	}
	br.sf.RecordPublish(1)
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

	br.log.Info("mqtt: registered template", "name", tmpl.Name, "members", len(tmpl.Members))

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
		// Rebirth() → Birth() triggers onBeforeBirth which rebuilds device
		// metrics from current values and resets RBE states.
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

// resetRBEStates clears all RBE tracking so the next value for every variable
// passes the RBE check. Called before rebirth so that DDATA flows after DBIRTH.
func (br *Bridge) resetRBEStates() {
	br.mu.Lock()
	defer br.mu.Unlock()
	for _, pv := range br.variables {
		pv.RBEState = rbe.State{}
		for k := range pv.MemberRBEStates {
			pv.MemberRBEStates[k] = &rbe.State{}
		}
	}
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
		br.log.Info("mqtt: received NCMD", "metric", m.Name)
	}
}

func (br *Bridge) routeCommand(m sparkplug.Metric) {
	br.mu.RLock()
	defer br.mu.RUnlock()

	// Find the source variable by metric name
	var sourceVar *PlcVariable
	for _, pv := range br.variables {
		if pv.ID == m.Name {
			sourceVar = pv
			break
		}
	}

	if sourceVar == nil {
		// Try matching just the last segment (for folder/name patterns)
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
		br.log.Warn("mqtt: DCMD for unknown variable", "name", m.Name)
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
		br.log.Info("mqtt: primary host online", "host", hostID)
	} else {
		br.log.Info("mqtt: primary host offline", "host", hostID)
	}
	br.mu.Lock()
	br.hostOnline = online
	br.hostSeen = true
	br.mu.Unlock()
	br.reconcileSFState()
}

// handleNodeStateChange is invoked whenever the Sparkplug node transitions
// (Disconnected ↔ Dead ↔ Born). It drives the store-forward state so the
// buffer flips offline the moment we lose our publish session — not only
// when a primary host STATE message says so.
func (br *Bridge) handleNodeStateChange(s NodeState) {
	br.reconcileSFState()
	// When the node tears the session down because the primary host went
	// offline, kick off a reconnect attempt — Sparkplug B
	// `operational-behavior-edge-node-termination-host-offline-reconnect`
	// requires the edge node to start session establishment over again.
	// (Paho only auto-reconnects on transport loss, not after an explicit
	// Disconnect call.)
	if s == StateDisconnected && br.config.PrimaryHostID != "" && !br.isStopping() {
		go br.reconnectLoop()
	}
}

// isStopping reports whether Stop() has begun tearing the bridge down. We use
// it to gate the host-offline reconnect goroutine — no point reconnecting if
// the surrounding module is already shutting down.
func (br *Bridge) isStopping() bool {
	br.mu.RLock()
	defer br.mu.RUnlock()
	return br.stopping
}

// reconcileSFState combines node + host inputs into the SF state. Online
// requires the node to have published NBIRTH AND (no primary host configured
// OR the primary host's last STATE message was ONLINE). Any other condition
// → offline (buffer). Reads state fresh so goroutine ordering doesn't matter.
func (br *Bridge) reconcileSFState() {
	nodeBorn := br.node != nil && br.node.State() == StateBorn

	br.mu.RLock()
	hostOnline := br.hostOnline
	br.mu.RUnlock()

	if nodeBorn && hostOnline {
		br.sf.SetOnline()
	} else {
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
					br.log.Warn("mqtt: drain publish failed", "error", err)
				}
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Initial variable loading
// ═══════════════════════════════════════════════════════════════════════════

func (br *Bridge) loadInitialVariables() {
	// Only load variables from the gateway module, since the bridge subscribes
	// exclusively to gateway.data.> for runtime data.
	const gatewayModuleID = "gateway"

	subject := topics.Variables(gatewayModuleID)
	resp, err := br.b.Request(subject, nil, 3*time.Second)
	if err != nil {
		br.log.Warn("mqtt: no variables response from gateway (will auto-discover from data stream)", "error", err)
		return
	}

	var vars []types.VariableInfo
	if err := json.Unmarshal(resp, &vars); err != nil {
		br.log.Warn("mqtt: failed to parse variables from gateway", "error", err)
		return
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

	br.log.Info("mqtt: loaded initial variables from gateway", "count", len(vars))

	// Build initial device metrics for DBIRTH
	br.buildDeviceMetrics()
}

func (br *Bridge) buildDeviceMetrics() {
	br.mu.RLock()
	defer br.mu.RUnlock()

	nowMs := time.Now().UnixMilli()

	// Group metrics by source device ID.
	byDevice := make(map[string][]sparkplug.Metric)
	for _, pv := range br.variables {
		devID := pv.DeviceID
		if devID == "" {
			devID = br.moduleID
		}
		m := br.valueToMetric(pv, pv.Value, nowMs)
		byDevice[devID] = append(byDevice[devID], m)
	}
	for devID, metrics := range byDevice {
		isNew := br.node.SetDeviceMetrics(devID, metrics)
		if isNew && br.node.State() == StateBorn {
			br.node.PublishDeviceBirth(devID)
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
				DeviceID:     pv.DeviceID,
				Datatype:     pv.Datatype,
				LastUpdated:  pv.LastUpdated,
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
		br.log.Error("mqtt: failed to subscribe to metrics request", "error", err)
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
		status.PrimaryHostID = br.config.PrimaryHostID
		status.PrimaryHostConfigured = br.config.PrimaryHostID != ""
		br.mu.RLock()
		status.PrimaryHostSeen = br.hostSeen
		br.mu.RUnlock()
		if br.node != nil {
			status.BrokerReachable = br.node.IsBrokerReachable()
			status.NodeBorn = br.node.State() == StateBorn
		}
		respData, err := json.Marshal(status)
		if err != nil {
			return
		}
		_ = reply(respData)
	})
	if err != nil {
		br.log.Error("mqtt: failed to subscribe to store-forward status request", "error", err)
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
