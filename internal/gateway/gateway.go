//go:build gateway || all

// Package gateway routes scanner data through the Bus, applying RBE deadband
// filtering and UDT assembly. It subscribes to protocol scanner data
// (ethernetip.data.>, opcua.data.>, etc.) and publishes gateway-level
// variables to plc.data.{gatewayId}.{variableId}.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/rbe"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
)

const serviceType = "gateway"

// TrackedVariable holds the RBE state for a single gateway variable.
type TrackedVariable struct {
	Config     itypes.GatewayVariableConfig
	Deadband   *types.DeadBandConfig
	DisableRBE bool
	Value      interface{}
	rbeState   rbe.State
}

type udtMemberRef struct {
	variableID string
	memberName string
}

// Gateway manages scanner subscriptions, data routing, and variable state.
type Gateway struct {
	b         bus.Bus
	gatewayID string
	log       *slog.Logger

	mu        sync.RWMutex
	config    *itypes.GatewayConfigKV
	variables map[string]*TrackedVariable

	// Maps scanner subject tokens to gateway variable IDs.
	// Key: "{protocol}.{deviceId}.{sanitizedTag}" → []variableId
	tagIndex map[string][]string

	// UDT assemblers: variableId → assembler
	udtAssemblers map[string]*UdtAssembler

	// Maps member tag subject tokens to (variableId, memberName) pairs.
	udtMemberIndex map[string][]udtMemberRef

	// Active subscriptions.
	subs   []bus.Subscription
	varSub bus.Subscription
	cmdSub bus.Subscription

	startedAt     time.Time
	stopHeartbeat func()
}

// New creates a new Gateway module.
func New(gatewayID string) *Gateway {
	if gatewayID == "" {
		gatewayID = "gateway"
	}
	return &Gateway{
		gatewayID:      gatewayID,
		variables:      make(map[string]*TrackedVariable),
		tagIndex:       make(map[string][]string),
		udtAssemblers:  make(map[string]*UdtAssembler),
		udtMemberIndex: make(map[string][]udtMemberRef),
	}
}

func (g *Gateway) ModuleID() string    { return g.gatewayID }
func (g *Gateway) ServiceType() string { return serviceType }

// Start initializes the gateway module with the given Bus.
func (g *Gateway) Start(ctx context.Context, b bus.Bus) error {
	g.b = b
	g.log = slog.Default().With("serviceType", g.ServiceType(), "moduleID", g.ModuleID())

	// Ensure required KV buckets exist
	for _, bucket := range []string{
		topics.BucketGatewayConfig, topics.BucketServiceEnabled,
		topics.BucketScannerEthernetIP, topics.BucketScannerOpcUA,
		topics.BucketScannerModbus, topics.BucketScannerSNMP,
	} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			g.log.Warn("gateway: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	g.startedAt = time.Now()

	// Start heartbeat
	g.stopHeartbeat = heartbeat.Start(b, g.gatewayID, serviceType, func() map[string]interface{} {
		return map[string]interface{}{
			"variableCount": g.VariableCount(),
			"hasConfig":     g.HasConfig(),
		}
	})

	// Subscribe to status browse for module status variables.
	statusVars := []module.StatusVar{
		{Name: "uptime", Datatype: "number"},
		{Name: "variableCount", Datatype: "number"},
		{Name: "udtVariableCount", Datatype: "number"},
		{Name: "deviceCount", Datatype: "number"},
	}
	statusBrowseSub, _ := b.Subscribe(topics.StatusBrowse(serviceType), func(_ string, _ []byte, reply bus.ReplyFunc) {
		module.HandleStatusBrowse(statusVars, reply)
	})
	g.mu.Lock()
	g.subs = append(g.subs, statusBrowseSub)
	g.mu.Unlock()

	// Publish status data every 10s.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.publishStatus()
			}
		}
	}()

	// Load initial config
	if data, _, err := b.KVGet(topics.BucketGatewayConfig, g.gatewayID); err == nil {
		var config itypes.GatewayConfigKV
		if err := json.Unmarshal(data, &config); err != nil {
			g.log.Error("gateway: failed to parse initial config", "error", err)
		} else {
			g.log.Info("gateway: loaded initial config", "updatedAt", config.UpdatedAt)
			g.ApplyConfig(&config)
		}
	} else {
		g.log.Info("gateway: no existing config found, seeding empty config")
		emptyConfig := &itypes.GatewayConfigKV{
			GatewayID:    g.gatewayID,
			Devices:      make(map[string]itypes.GatewayDeviceConfig),
			Variables:    make(map[string]itypes.GatewayVariableConfig),
			UdtTemplates: make(map[string]itypes.GatewayUdtTemplateConfig),
			UdtVariables: make(map[string]itypes.GatewayUdtVariableConfig),
			UpdatedAt:    time.Now().UnixMilli(),
		}
		if data, err := json.Marshal(emptyConfig); err == nil {
			if _, err := b.KVPut(topics.BucketGatewayConfig, g.gatewayID, data); err != nil {
				g.log.Warn("gateway: failed to seed empty config", "error", err)
			}
		}
	}

	// Watch for config changes
	configSub, err := b.KVWatch(topics.BucketGatewayConfig, g.gatewayID, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			g.log.Info("gateway: config deleted, stopping")
			g.ApplyConfig(nil)
			return
		}
		var config itypes.GatewayConfigKV
		if err := json.Unmarshal(value, &config); err != nil {
			g.log.Error("gateway: failed to parse updated config", "error", err)
			return
		}
		g.log.Info("gateway: config updated, rebuilding subscriptions",
			"devices", len(config.Devices),
			"variables", len(config.Variables),
			"udtTemplates", len(config.UdtTemplates),
			"udtVariables", len(config.UdtVariables))
		g.ApplyConfig(&config)
	})
	if err != nil {
		g.log.Error("gateway: failed to watch config KV", "error", err)
	} else {
		g.mu.Lock()
		g.subs = append(g.subs, configSub)
		g.mu.Unlock()
	}

	// Listen for shutdown via NATS
	shutdownSub, _ := b.Subscribe(topics.Shutdown(g.gatewayID), func(subject string, data []byte, reply bus.ReplyFunc) {
		g.log.Info("gateway: received shutdown command via Bus")
		g.Stop()
		os.Exit(0)
	})
	g.mu.Lock()
	g.subs = append(g.subs, shutdownSub)
	g.mu.Unlock()

	// Block until context cancelled or signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down all subscriptions and cleans up.
func (g *Gateway) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.unsubscribeAllLocked()
	g.config = nil
	g.variables = make(map[string]*TrackedVariable)
	g.tagIndex = make(map[string][]string)
	g.udtAssemblers = make(map[string]*UdtAssembler)
	g.udtMemberIndex = make(map[string][]udtMemberRef)
	if g.stopHeartbeat != nil {
		g.stopHeartbeat()
	}
	return nil
}

// VariableCount returns the number of active variables (atomic + UDT).
func (g *Gateway) VariableCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.variables) + len(g.udtAssemblers)
}

// HasConfig returns true if a config is currently applied.
func (g *Gateway) HasConfig() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.config != nil
}

// publishStatus routes gateway status values through the tag index so they
// use configured variable names and don't create duplicates in the MQTT bridge.
// (Using module.PublishStatus would publish on gateway.data.gateway.{name} which
// the gateway also subscribes to, causing a self-routing loop and duplicate metrics.)
func (g *Gateway) publishStatus() {
	g.mu.RLock()
	deviceCount := 0
	if g.config != nil {
		deviceCount = len(g.config.Devices)
	}
	varCount := len(g.variables)
	udtCount := len(g.udtAssemblers)
	g.mu.RUnlock()

	uptimeSeconds := int64(time.Since(g.startedAt).Seconds())

	statusValues := map[string]struct {
		value    interface{}
		datatype string
	}{
		"uptime":           {value: uptimeSeconds, datatype: "number"},
		"variableCount":    {value: varCount, datatype: "number"},
		"udtVariableCount": {value: udtCount, datatype: "number"},
		"deviceCount":      {value: deviceCount, datatype: "number"},
	}
	for name, sv := range statusValues {
		g.routeValue(serviceType, serviceType, name, sv.value, sv.datatype)
	}
}

// ApplyConfig stops any current subscriptions and applies a new configuration.
func (g *Gateway) ApplyConfig(config *itypes.GatewayConfigKV) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.unsubscribeDataLocked()

	g.config = config
	g.variables = make(map[string]*TrackedVariable)
	g.tagIndex = make(map[string][]string)
	g.udtAssemblers = make(map[string]*UdtAssembler)
	g.udtMemberIndex = make(map[string][]udtMemberRef)

	if config == nil {
		g.log.Info("gateway: config cleared, no variables active")
		return
	}

	// Build tracked variables and tag index for atomic variables
	for varID, varCfg := range config.Variables {
		device, ok := config.Devices[varCfg.DeviceID]
		if !ok {
			g.log.Warn("gateway: variable references unknown device", "variable", varID, "device", varCfg.DeviceID)
			continue
		}

		deadband := varCfg.Deadband
		if deadband == nil {
			deadband = device.Deadband
		}
		disableRBE := varCfg.DisableRBE
		if !disableRBE && device.DisableRBE != nil && *device.DisableRBE {
			disableRBE = true
		}

		tv := &TrackedVariable{
			Config:     varCfg,
			Deadband:   deadband,
			DisableRBE: disableRBE,
			Value:      varCfg.Default,
		}
		g.variables[varID] = tv

		sanitizedTag := types.SanitizeForSubject(varCfg.Tag)
		key := fmt.Sprintf("%s.%s.%s", device.Protocol, types.SanitizeForSubject(varCfg.DeviceID), sanitizedTag)
		g.tagIndex[key] = append(g.tagIndex[key], varID)
	}

	// Build UDT assemblers and member tag index
	udtVarCount := 0
	for varID, udtVar := range config.UdtVariables {
		device, ok := config.Devices[udtVar.DeviceID]
		if !ok {
			continue
		}
		tmpl, ok := config.UdtTemplates[udtVar.TemplateName]
		if !ok {
			continue
		}

		udtDeadband := udtVar.Deadband
		if udtDeadband == nil {
			udtDeadband = device.Deadband
		}
		udtDisableRBE := udtVar.DisableRBE
		if !udtDisableRBE && device.DisableRBE != nil && *device.DisableRBE {
			udtDisableRBE = true
		}

		memberDeadbands := make(map[string]types.DeadBandConfig)
		for _, member := range tmpl.Members {
			dt := member.Datatype
			if dt == "boolean" || dt == "string" || dt == "BOOL" || dt == "BOOLEAN" || dt == "STRING" {
				continue
			}
			// Resolve base deadband: start from device default, overlay member default.
			base := types.DeadBandConfig{}
			if device.Deadband != nil {
				base = *device.Deadband
			}
			if member.DefaultDeadband != nil {
				// Member default overrides device default per-field.
				// Value is always taken; MinTime/MaxTime only if non-zero (0 = disabled).
				base.Value = member.DefaultDeadband.Value
				if member.DefaultDeadband.MinTime != 0 {
					base.MinTime = member.DefaultDeadband.MinTime
				}
				if member.DefaultDeadband.MaxTime != 0 {
					base.MaxTime = member.DefaultDeadband.MaxTime
				}
			}
			if override, ok := udtVar.MemberDeadbands[member.Name]; ok {
				// Merge sparse per-field override on top of the base.
				memberDeadbands[member.Name] = override.Merge(base)
			} else {
				memberDeadbands[member.Name] = base
			}
		}

		assembler := NewUdtAssembler(g.b, g.gatewayID, udtVar.ID, udtVar, tmpl, udtDeadband, udtDisableRBE, memberDeadbands)
		g.udtAssemblers[varID] = assembler

		sanitizedDevice := types.SanitizeForSubject(udtVar.DeviceID)
		for memberName, memberTag := range udtVar.MemberTags {
			sanitizedTag := types.SanitizeForSubject(memberTag)
			key := fmt.Sprintf("%s.%s.%s", device.Protocol, sanitizedDevice, sanitizedTag)
			g.udtMemberIndex[key] = append(g.udtMemberIndex[key], udtMemberRef{
				variableID: varID,
				memberName: memberName,
			})
		}
		udtVarCount++
	}

	g.log.Info("gateway: config applied",
		"atomicVars", len(g.variables),
		"udtVars", udtVarCount,
		"templates", len(config.UdtTemplates),
		"devices", len(config.Devices))

	g.subscribeToScannersLocked()
	g.setupVariablesHandlerLocked()
	g.setupCommandRoutingLocked()
}

// ═══════════════════════════════════════════════════════════════════════════
// Internal: subscription management
// ═══════════════════════════════════════════════════════════════════════════

func (g *Gateway) unsubscribeAllLocked() {
	g.unsubscribeDataLocked()
	// Also unsub lifecycle subs
	for _, sub := range g.subs {
		_ = sub.Unsubscribe()
	}
	g.subs = nil
}

func (g *Gateway) unsubscribeDataLocked() {
	if g.varSub != nil {
		_ = g.varSub.Unsubscribe()
		g.varSub = nil
	}
	if g.cmdSub != nil {
		_ = g.cmdSub.Unsubscribe()
		g.cmdSub = nil
	}
	for _, a := range g.udtAssemblers {
		a.Stop()
	}
	if g.config != nil {
		g.deleteSubscriptionConfigsLocked()
	}
}

func (g *Gateway) subscribeToScannersLocked() {
	if g.config == nil {
		return
	}

	type deviceGroup struct {
		device    itypes.GatewayDeviceConfig
		deviceID  string
		variables map[string]itypes.GatewayVariableConfig
	}
	groups := make(map[string]*deviceGroup)

	for varID, varCfg := range g.config.Variables {
		device, ok := g.config.Devices[varCfg.DeviceID]
		if !ok {
			continue
		}
		grp, ok := groups[varCfg.DeviceID]
		if !ok {
			grp = &deviceGroup{device: device, deviceID: varCfg.DeviceID, variables: make(map[string]itypes.GatewayVariableConfig)}
			groups[varCfg.DeviceID] = grp
		}
		grp.variables[varID] = varCfg
	}

	// Add UDT member tags as synthetic variables for subscription
	for _, udtVar := range g.config.UdtVariables {
		device, ok := g.config.Devices[udtVar.DeviceID]
		if !ok {
			continue
		}
		grp, ok := groups[udtVar.DeviceID]
		if !ok {
			grp = &deviceGroup{device: device, deviceID: udtVar.DeviceID, variables: make(map[string]itypes.GatewayVariableConfig)}
			groups[udtVar.DeviceID] = grp
		}
		for memberName, memberTag := range udtVar.MemberTags {
			syntheticID := fmt.Sprintf("__udt__%s__%s", udtVar.ID, memberName)
			cipType := ""
			if udtVar.MemberCipTypes != nil {
				cipType = udtVar.MemberCipTypes[memberName]
			}
			syntheticVar := itypes.GatewayVariableConfig{
				ID: syntheticID, DeviceID: udtVar.DeviceID, Tag: memberTag, Datatype: "number", CipType: cipType,
			}
			// For Modbus devices, populate register metadata from template + instance.
			if device.Protocol == "modbus" {
				if tmpl, tmplOk := g.config.UdtTemplates[udtVar.TemplateName]; tmplOk {
					for _, m := range tmpl.Members {
						if m.Name == memberName {
							if m.FunctionCode != "" {
								fc := modbusStringFCToInt(m.FunctionCode)
								syntheticVar.FunctionCode = &fc
							}
							if m.ModbusDatatype != "" {
								syntheticVar.ModbusDatatype = m.ModbusDatatype
							}
							break
						}
					}
				}
				if udtVar.MemberAddresses != nil {
					if addr, ok := udtVar.MemberAddresses[memberName]; ok {
						addrCopy := addr
						syntheticVar.Address = &addrCopy
					}
				}
				if udtVar.MemberByteOrders != nil {
					if bo, ok := udtVar.MemberByteOrders[memberName]; ok {
						syntheticVar.ByteOrder = bo
					}
				}
			}
			grp.variables[syntheticID] = syntheticVar
		}
	}

	for _, grp := range groups {
		// Skip devices whose protocol matches the gateway's own output prefix.
		// Their data is routed directly (e.g. via publishStatus → routeValue)
		// and subscribing would create a self-routing loop.
		if grp.device.Protocol == g.gatewayID {
			continue
		}
		g.subscribeToDeviceLocked(grp.deviceID, grp.device, grp.variables)
	}
}

func (g *Gateway) subscribeToDeviceLocked(deviceID string, device itypes.GatewayDeviceConfig, vars map[string]itypes.GatewayVariableConfig) {
	sanitizedDevice := types.SanitizeForSubject(deviceID)
	protocol := device.Protocol

	// Subscribe to wildcard: {protocol}.data.{deviceId}.>
	subject := fmt.Sprintf("%s.data.%s.>", protocol, sanitizedDevice)
	sub, err := g.b.Subscribe(subject, func(subj string, data []byte, reply bus.ReplyFunc) {
		g.handleScannerData(subj, data, protocol, deviceID)
	})
	if err != nil {
		g.log.Error("gateway: failed to subscribe", "subject", subject, "error", err)
	} else {
		g.subs = append(g.subs, sub)
	}

	// Also subscribe to exact device subject for batch messages
	batchSubject := fmt.Sprintf("%s.data.%s", protocol, sanitizedDevice)
	batchSub, err := g.b.Subscribe(batchSubject, func(subj string, data []byte, reply bus.ReplyFunc) {
		g.handleScannerBatchData(data, protocol, deviceID)
	})
	if err != nil {
		g.log.Error("gateway: failed to subscribe", "subject", batchSubject, "error", err)
	} else {
		g.subs = append(g.subs, batchSub)
	}

	g.writeSubscriptionConfig(deviceID, device, vars)
}

// ═══════════════════════════════════════════════════════════════════════════
// Scanner subscribe/unsubscribe requests
// ═══════════════════════════════════════════════════════════════════════════

func (g *Gateway) writeSubscriptionConfig(deviceID string, device itypes.GatewayDeviceConfig, vars map[string]itypes.GatewayVariableConfig) {
	// Auto-managed devices (network, module status) self-publish their data
	// and don't need scanner subscription config.
	if device.AutoManaged {
		return
	}

	subscriberID := fmt.Sprintf("gateway-%s", g.gatewayID)

	bucket := topics.ScannerBucket(device.Protocol)
	if bucket == "" {
		g.log.Debug("gateway: no scanner bucket for protocol (self-publishing source)", "protocol", device.Protocol, "device", deviceID)
		return
	}

	var payload []byte
	var err error

	scanRate := func(defaultRate int) int {
		if device.ScanRate != nil {
			return *device.ScanRate
		}
		return defaultRate
	}

	switch device.Protocol {
	case "ethernetip":
		tags := make([]string, 0, len(vars))
		cipTypes := make(map[string]string)
		structTypes := make(map[string]string)
		deadbands := make(map[string]types.DeadBandConfig)
		disableRBE := make(map[string]bool)
		for _, v := range vars {
			tags = append(tags, v.Tag)
			if v.CipType != "" {
				cipTypes[v.Tag] = v.CipType
			}
			if v.Deadband != nil {
				deadbands[v.Tag] = *v.Deadband
			}
			if v.DisableRBE {
				disableRBE[v.Tag] = true
			}
		}
		if g.config != nil {
			for _, udtVar := range g.config.UdtVariables {
				if udtVar.DeviceID == deviceID {
					structTypes[udtVar.Tag] = udtVar.TemplateName
				}
			}
		}
		port := 44818
		if device.Port != nil {
			port = *device.Port
		}
		slot := 0
		if device.Slot != nil {
			slot = *device.Slot
		}
		req := itypes.EthernetIPSubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, Host: device.Host, Port: port, Slot: slot,
			Tags: tags, ScanRate: scanRate(1000), CipTypes: cipTypes, StructTypes: structTypes,
			Deadbands: deadbands, DisableRBE: disableRBE,
		}
		payload, err = json.Marshal(req)

	case "opcua":
		nodeIDs := make([]string, 0, len(vars))
		for _, v := range vars {
			nodeIDs = append(nodeIDs, v.Tag)
		}
		req := itypes.OpcUASubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, EndpointURL: device.EndpointURL,
			NodeIDs: nodeIDs, ScanRate: scanRate(1000),
		}
		payload, err = json.Marshal(req)

	case "snmp":
		oids := make([]string, 0, len(vars))
		for _, v := range vars {
			oids = append(oids, v.Tag)
		}
		port := 161
		if device.Port != nil {
			port = *device.Port
		}
		req := itypes.SNMPSubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, Host: device.Host, Port: port,
			Version: device.Version, Community: device.Community, V3Auth: device.V3Auth,
			OIDs: oids, ScanRate: scanRate(5000),
		}
		payload, err = json.Marshal(req)

	case "modbus":
		tags := make([]itypes.ModbusTagConfig, 0, len(vars))
		for _, v := range vars {
			fc := "holding"
			if v.FunctionCode != nil {
				fc = itypes.FunctionCodeToString(*v.FunctionCode)
			}
			addr := 0
			if v.Address != nil {
				addr = *v.Address
			}
			dt := "uint16"
			if v.ModbusDatatype != "" {
				dt = v.ModbusDatatype
			}
			bo := device.ByteOrder
			if v.ByteOrder != "" {
				bo = v.ByteOrder
			}
			tags = append(tags, itypes.ModbusTagConfig{ID: v.Tag, Address: addr, FunctionCode: fc, Datatype: dt, ByteOrder: bo})
		}
		port := 502
		if device.Port != nil {
			port = *device.Port
		}
		unitID := 1
		if device.UnitID != nil {
			unitID = *device.UnitID
		}
		req := itypes.ModbusScannerSubscribeRequest{
			SubscriberID: subscriberID, DeviceID: deviceID, Host: device.Host, Port: port,
			UnitID: unitID, ByteOrder: device.ByteOrder, Tags: tags, ScanRate: scanRate(1000),
		}
		payload, err = json.Marshal(req)
	}

	if err != nil {
		g.log.Error("gateway: failed to marshal subscribe config", "device", deviceID, "error", err)
		return
	}

	// Write to the scanner's KV bucket — fire and forget.
	// The scanner watches this bucket and will pick it up whenever it's ready.
	key := fmt.Sprintf("%s.%s", subscriberID, deviceID)
	if _, err := g.b.KVPut(bucket, key, payload); err != nil {
		g.log.Error("gateway: failed to write scanner config", "bucket", bucket, "key", key, "error", err)
		return
	}
	g.log.Info("gateway: wrote scanner config", "protocol", device.Protocol, "device", deviceID, "bucket", bucket)
}

func (g *Gateway) deleteSubscriptionConfigsLocked() {
	subscriberID := fmt.Sprintf("gateway-%s", g.gatewayID)

	type devKey struct{ protocol, deviceID string }
	seen := make(map[devKey]bool)

	for _, varCfg := range g.config.Variables {
		device, ok := g.config.Devices[varCfg.DeviceID]
		if !ok {
			continue
		}
		dk := devKey{device.Protocol, varCfg.DeviceID}
		if seen[dk] {
			continue
		}
		seen[dk] = true

		bucket := topics.ScannerBucket(device.Protocol)
		if bucket == "" {
			continue
		}
		key := fmt.Sprintf("%s.%s", subscriberID, varCfg.DeviceID)
		_ = g.b.KVDelete(bucket, key)
	}

	// Also clean up UDT variable devices
	for _, udtVar := range g.config.UdtVariables {
		device, ok := g.config.Devices[udtVar.DeviceID]
		if !ok {
			continue
		}
		dk := devKey{device.Protocol, udtVar.DeviceID}
		if seen[dk] {
			continue
		}
		seen[dk] = true

		bucket := topics.ScannerBucket(device.Protocol)
		if bucket == "" {
			continue
		}
		key := fmt.Sprintf("%s.%s", subscriberID, udtVar.DeviceID)
		_ = g.b.KVDelete(bucket, key)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Data handling: scanner → gateway → plc.data.*
// ═══════════════════════════════════════════════════════════════════════════

func (g *Gateway) handleScannerData(subject string, rawData []byte, protocol, deviceID string) {
	var dataMsg types.PlcDataMessage
	if err := json.Unmarshal(rawData, &dataMsg); err != nil {
		return
	}

	parts := strings.SplitN(subject, ".", 4)
	if len(parts) < 4 {
		return
	}
	tag := parts[3]

	g.routeValue(protocol, deviceID, tag, dataMsg.Value, dataMsg.Datatype)
}

func (g *Gateway) handleScannerBatchData(rawData []byte, protocol, deviceID string) {
	var batch types.ScannerBatchMessage
	if err := json.Unmarshal(rawData, &batch); err != nil {
		return
	}

	for _, val := range batch.Values {
		sanitizedTag := types.SanitizeForSubject(val.VariableID)
		g.routeValue(protocol, deviceID, sanitizedTag, val.Value, val.Datatype)
	}
}

func (g *Gateway) routeValue(protocol, deviceID, sanitizedTag string, value interface{}, datatype string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	sanitizedDevice := types.SanitizeForSubject(deviceID)
	key := fmt.Sprintf("%s.%s.%s", protocol, sanitizedDevice, sanitizedTag)

	// Route to atomic variables
	if varIDs, ok := g.tagIndex[key]; ok {
		nowMs := time.Now().UnixMilli()
		for _, varID := range varIDs {
			tv, ok := g.variables[varID]
			if !ok {
				continue
			}
			tv.Value = value

			if !rbe.ShouldPublish(&tv.rbeState, value, nowMs, tv.Deadband, tv.DisableRBE) {
				continue
			}

			dt := datatype
			if dt == "" {
				dt = tv.Config.Datatype
			}

			outMsg := types.PlcDataMessage{
				ModuleID:    g.gatewayID,
				DeviceID:    tv.Config.DeviceID,
				VariableID:  tv.Config.ID,
				Value:       value,
				Timestamp:   nowMs,
				Datatype:    dt,
				Description: tv.Config.Description,
			}
			if tv.Deadband != nil {
				outMsg.Deadband = tv.Deadband
			}
			if tv.DisableRBE {
				outMsg.DisableRBE = true
			}
			if tv.Config.HistoryEnabled {
				outMsg.HistoryEnabled = true
			}

			data, err := json.Marshal(outMsg)
			if err != nil {
				continue
			}

			pubSubject := topics.Data(g.gatewayID, types.SanitizeForSubject(tv.Config.DeviceID), types.SanitizeForSubject(tv.Config.Tag))
			_ = g.b.Publish(pubSubject, data)

			rbe.RecordPublish(&tv.rbeState, value, nowMs)
		}
	}

	// Route to UDT assemblers
	if memberRefs, ok := g.udtMemberIndex[key]; ok {
		for _, ref := range memberRefs {
			if assembler, ok := g.udtAssemblers[ref.variableID]; ok {
				assembler.SetMember(ref.memberName, value)
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Variables request handler
// ═══════════════════════════════════════════════════════════════════════════

func (g *Gateway) setupVariablesHandlerLocked() {
	subject := topics.Variables(g.gatewayID)
	sub, err := g.b.Subscribe(subject, func(subj string, data []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}
		g.mu.RLock()
		defer g.mu.RUnlock()

		vars := make([]types.VariableInfo, 0, len(g.variables)+len(g.udtAssemblers))
		for _, tv := range g.variables {
			vars = append(vars, types.VariableInfo{
				ModuleID: g.gatewayID, DeviceID: tv.Config.DeviceID, VariableID: tv.Config.ID,
				Value: tv.Value, Datatype: tv.Config.Datatype, Description: tv.Config.Description,
				Deadband: tv.Deadband, DisableRBE: tv.DisableRBE,
			})
		}

		for _, assembler := range g.udtAssemblers {
			tmpl := assembler.template
			members := make([]types.UdtMemberDefinition, len(tmpl.Members))
			for i, m := range tmpl.Members {
				datatype := m.Datatype
				if assembler.config.MemberCipTypes != nil {
					if cipType, ok := assembler.config.MemberCipTypes[m.Name]; ok {
						datatype = itypes.CipToNatsDatatype(cipType)
					}
				}
				members[i] = types.UdtMemberDefinition{Name: m.Name, Datatype: datatype, TemplateRef: m.TemplateRef}
			}
			vars = append(vars, types.VariableInfo{
				ModuleID: g.gatewayID, DeviceID: assembler.config.DeviceID, VariableID: assembler.config.ID,
				Value: assembler.Value(), Datatype: "udt",
				UdtTemplate: &types.UdtTemplateDefinition{Name: tmpl.Name, Version: tmpl.Version, Members: members},
			})
		}

		resp, err := json.Marshal(vars)
		if err != nil {
			return
		}
		_ = reply(resp)
	})
	if err != nil {
		g.log.Error("gateway: failed to subscribe to variables", "subject", subject, "error", err)
		return
	}
	g.varSub = sub
}

// ═══════════════════════════════════════════════════════════════════════════
// Command routing
// ═══════════════════════════════════════════════════════════════════════════

func (g *Gateway) setupCommandRoutingLocked() {
	subject := topics.CommandWildcard(g.gatewayID)
	sub, err := g.b.Subscribe(subject, func(subj string, data []byte, reply bus.ReplyFunc) {
		parts := strings.SplitN(subj, ".", 3)
		if len(parts) < 3 {
			return
		}
		cmdTag := parts[2]

		g.mu.RLock()
		defer g.mu.RUnlock()

		// Try atomic variable lookup (bidirectional tags only)
		for _, v := range g.variables {
			if types.SanitizeForSubject(v.Config.Tag) == cmdTag && v.Config.Bidirectional {
				device, ok := g.config.Devices[v.Config.DeviceID]
				if !ok {
					return
				}
				cmdSubject := fmt.Sprintf("%s.command.%s", device.Protocol, v.Config.Tag)
				_ = g.b.Publish(cmdSubject, data)
				return
			}
		}

		// Try UDT member command: cmdTag = "udtID/memberName"
		if idx := strings.Index(cmdTag, "/"); idx >= 0 && g.config != nil {
			udtID := cmdTag[:idx]
			memberName := cmdTag[idx+1:]

			udtVar, ok := g.config.UdtVariables[udtID]
			if !ok {
				return
			}
			memberTag, ok := udtVar.MemberTags[memberName]
			if !ok {
				g.log.Warn("gateway: DCMD for unknown UDT member", "udt", udtID, "member", memberName)
				return
			}
			device, ok := g.config.Devices[udtVar.DeviceID]
			if !ok {
				return
			}
			cmdSubject := fmt.Sprintf("%s.command.%s", device.Protocol, memberTag)
			_ = g.b.Publish(cmdSubject, data)
		}
	})
	if err != nil {
		g.log.Error("gateway: failed to subscribe to commands", "subject", subject, "error", err)
		return
	}
	g.cmdSub = sub
}

// modbusStringFCToInt converts a Modbus function code string to its numeric value.
func modbusStringFCToInt(fc string) int {
	switch fc {
	case "coil":
		return 1
	case "discrete":
		return 2
	case "holding":
		return 3
	case "input":
		return 4
	default:
		return 3
	}
}
