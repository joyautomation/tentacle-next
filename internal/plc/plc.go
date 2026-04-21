//go:build plc || all

// Package plc implements a Starlark-based PLC task runner module.
// It subscribes directly to scanner topics for input variables,
// executes Starlark programs on configurable scan intervals,
// and publishes output variables that the gateway module consumes.
package plc

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"github.com/joyautomation/tentacle/types"
	"go.starlark.net/starlark"
)

const serviceType = "plc"

// Module is the PLC module implementing module.Module.
type Module struct {
	b     bus.Bus
	plcID string
	log   *slog.Logger

	mu        sync.RWMutex
	config    *itypes.PlcConfigKV
	variables *VariableStore
	engine    *Engine
	tasks     map[string]*taskRunner
	pub       *publisher
	bridge    *scannerBridge

	subs          []bus.Subscription
	stopHeartbeat func()
	startedAt     time.Time
}

// New creates a new PLC module instance.
func New(plcID string) *Module {
	if plcID == "" {
		plcID = "plc"
	}
	return &Module{
		plcID:     plcID,
		variables: NewVariableStore(),
	}
}

func (m *Module) ModuleID() string    { return m.plcID }
func (m *Module) ServiceType() string { return serviceType }

// Start initializes the PLC module with the given Bus.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.startedAt = time.Now()
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	// Apply GC tuning for tighter scan-time p99. Lower GOGC means smaller,
	// more frequent collections — trades a few percent of CPU for much
	// shorter pauses. Opt-in via env so users not running the PLC module
	// aren't affected.
	if v := os.Getenv("TENTACLE_PLC_GOGC"); v != "" {
		if pct, err := strconv.Atoi(v); err == nil && pct > 0 {
			debug.SetGCPercent(pct)
			m.log.Info("plc: GOGC tuned", "percent", pct)
		}
	}
	if v := os.Getenv("TENTACLE_PLC_MEMLIMIT_MB"); v != "" {
		if mb, err := strconv.Atoi(v); err == nil && mb > 0 {
			debug.SetMemoryLimit(int64(mb) * 1024 * 1024)
			m.log.Info("plc: soft memory limit set", "mb", mb)
		}
	}

	// Ensure required KV buckets exist.
	for _, bucket := range []string{
		topics.BucketPlcConfig, topics.BucketPlcPrograms, topics.BucketPlcTemplates,
		topics.BucketPlcVariables,
		topics.BucketScannerEthernetIP, topics.BucketScannerOpcUA,
		topics.BucketScannerModbus, topics.BucketScannerSNMP,
	} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("plc: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Start heartbeat.
	m.stopHeartbeat = heartbeat.Start(b, m.plcID, serviceType, func() map[string]interface{} {
		return map[string]interface{}{
			"variableCount": m.variables.Count(),
			"hasConfig":     m.HasConfig(),
		}
	})

	// Load initial config.
	if data, _, err := b.KVGet(topics.BucketPlcConfig, m.plcID); err == nil {
		var config itypes.PlcConfigKV
		if err := json.Unmarshal(data, &config); err != nil {
			m.log.Error("plc: failed to parse initial config", "error", err)
		} else {
			m.log.Info("plc: loaded initial config",
				"variables", len(config.Variables),
				"tasks", len(config.Tasks))
			m.applyConfig(&config)
		}
	} else {
		m.log.Info("plc: no existing config found, seeding empty config")
		emptyConfig := &itypes.PlcConfigKV{
			PlcID:        m.plcID,
			Devices:      make(map[string]itypes.PlcDeviceConfigKV),
			Variables:    make(map[string]itypes.PlcVariableConfigKV),
			UdtTemplates: make(map[string]itypes.PlcUdtTemplateConfigKV),
			Tasks:        make(map[string]itypes.PlcTaskConfigKV),
			UpdatedAt:    time.Now().UnixMilli(),
		}
		if data, err := json.Marshal(emptyConfig); err == nil {
			if _, err := b.KVPut(topics.BucketPlcConfig, m.plcID, data); err != nil {
				m.log.Warn("plc: failed to seed empty config", "error", err)
			}
		}
	}

	// Watch for config changes.
	configSub, err := b.KVWatch(topics.BucketPlcConfig, m.plcID, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			m.log.Info("plc: config deleted")
			m.applyConfig(nil)
			return
		}
		var config itypes.PlcConfigKV
		if err := json.Unmarshal(value, &config); err != nil {
			m.log.Error("plc: failed to parse updated config", "error", err)
			return
		}
		m.log.Info("plc: config updated, rebuilding",
			"devices", len(config.Devices),
			"variables", len(config.Variables),
			"tasks", len(config.Tasks))
		m.applyConfig(&config)
	})
	if err != nil {
		m.log.Error("plc: failed to watch config KV", "error", err)
	} else {
		m.mu.Lock()
		m.subs = append(m.subs, configSub)
		m.mu.Unlock()
	}

	// Watch for template changes so existing StructValue instances pick
	// up new fields (or drop removed ones) without a restart.
	tmplSub, err := b.KVWatchAll(topics.BucketPlcTemplates, func(key string, value []byte, op bus.KVOperation) {
		m.mu.RLock()
		eng := m.engine
		m.mu.RUnlock()
		if eng == nil {
			return
		}
		templates := m.loadTemplates()
		m.log.Info("plc: templates updated, reconciling", "count", len(templates))
		eng.SetTemplates(templates)
	})
	if err != nil {
		m.log.Error("plc: failed to watch templates KV", "error", err)
	} else {
		m.mu.Lock()
		m.subs = append(m.subs, tmplSub)
		m.mu.Unlock()
	}

	// Listen for shutdown via Bus.
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.plcID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("plc: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.mu.Lock()
	m.subs = append(m.subs, shutdownSub)
	m.mu.Unlock()

	// Handle variables request/reply.
	varSub, _ := b.Subscribe(topics.Variables(m.plcID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.handleVariablesRequest(reply)
	})
	m.mu.Lock()
	m.subs = append(m.subs, varSub)
	m.mu.Unlock()

	// Handle task-stats request/reply.
	statsSub, _ := b.Subscribe(topics.PlcTaskStats(m.plcID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.handleTaskStatsRequest(reply)
	})
	m.mu.Lock()
	m.subs = append(m.subs, statsSub)
	m.mu.Unlock()

	// Handle variable write commands. The API publishes {"value": X} to
	// {plcID}.command.{variableID}; variableID may contain dots for
	// nested struct field paths (e.g. motor1.test).
	cmdSub, err := b.Subscribe(topics.CommandWildcard(m.plcID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.handleVariableCommand(subject, data)
	})
	if err != nil {
		m.log.Error("plc: failed to subscribe to commands", "error", err)
	} else {
		m.mu.Lock()
		m.subs = append(m.subs, cmdSub)
		m.mu.Unlock()
	}

	m.log.Info("plc: started")

	// Block until context cancelled or signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down all subscriptions and cleans up.
func (m *Module) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop tasks.
	for _, t := range m.tasks {
		t.stop()
	}
	m.tasks = nil

	// Stop publisher.
	if m.pub != nil {
		m.pub.stop()
		m.pub = nil
	}

	// Stop scanner bridge.
	if m.bridge != nil {
		m.bridge.unsubscribe()
		m.bridge = nil
	}

	// Unsubscribe bus subscriptions.
	for _, sub := range m.subs {
		if sub != nil {
			sub.Unsubscribe()
		}
	}
	m.subs = nil
	m.config = nil
	m.engine = nil
	m.variables.Clear()

	if m.stopHeartbeat != nil {
		m.stopHeartbeat()
	}
	return nil
}

// HasConfig returns true if a config is currently applied.
func (m *Module) HasConfig() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config != nil
}

// applyConfig rebuilds the variable store, engine, tasks, publisher, and scanner bridge.
func (m *Module) applyConfig(config *itypes.PlcConfigKV) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing tasks.
	for _, t := range m.tasks {
		t.stop()
	}
	m.tasks = nil

	// Stop existing publisher.
	if m.pub != nil {
		m.pub.stop()
		m.pub = nil
	}

	// Stop existing scanner bridge.
	if m.bridge != nil {
		m.bridge.unsubscribe()
		m.bridge = nil
	}

	m.config = config
	m.variables.Clear()
	m.engine = nil

	if config == nil {
		return
	}

	// Create engine and load templates before variables so template-typed
	// variables can be instantiated as StructValues at register time.
	m.engine = NewEngine(m.variables, m.log)
	templates := m.loadTemplates()
	m.engine.SetTemplates(templates)

	// Register variables.
	now := time.Now().UnixMilli()
	for id, vcfg := range config.Variables {
		rv := &RuntimeVariable{
			ID:          id,
			Datatype:    vcfg.Datatype,
			Direction:   vcfg.Direction,
			Value:       initialVariableValue(m.engine, &vcfg, m.log),
			Quality:     "good",
			LastUpdated: now,
		}
		m.variables.Add(rv)
	}

	// Load and compile programs from KV.
	for _, taskCfg := range config.Tasks {
		if !taskCfg.Enabled || taskCfg.ProgramRef == "" {
			continue
		}
		if m.engine.HasProgram(taskCfg.ProgramRef) {
			continue
		}
		data, _, err := m.b.KVGet(topics.BucketPlcPrograms, taskCfg.ProgramRef)
		if err != nil {
			m.log.Error("plc: failed to load program", "program", taskCfg.ProgramRef, "error", err)
			continue
		}
		var prog itypes.PlcProgramKV
		if err := json.Unmarshal(data, &prog); err != nil {
			m.log.Error("plc: failed to parse program", "program", taskCfg.ProgramRef, "error", err)
			continue
		}
		if err := m.engine.Compile(taskCfg.ProgramRef, prog.Source); err != nil {
			m.log.Error("plc: failed to compile program", "program", taskCfg.ProgramRef, "error", err)
			continue
		}
		m.log.Info("plc: compiled program", "program", taskCfg.ProgramRef, "language", prog.Language)
	}

	// Start scanner bridge.
	m.bridge = newScannerBridge(m.b, m.plcID, m.variables, m.log)
	m.bridge.subscribe(config)

	// Start publisher.
	m.pub = newPublisher(m.b, m.plcID, m.variables, m.log)
	m.pub.start()

	// Start task runners.
	m.tasks = make(map[string]*taskRunner)
	for taskID, taskCfg := range config.Tasks {
		if !taskCfg.Enabled || taskCfg.ProgramRef == "" {
			continue
		}
		if !m.engine.HasProgram(taskCfg.ProgramRef) {
			m.log.Warn("plc: skipping task, program not compiled", "task", taskID, "program", taskCfg.ProgramRef)
			continue
		}
		runner := newTaskRunner(taskID, taskCfg.ProgramRef, taskCfg.ScanRateMs, m.engine, m.log)
		m.tasks[taskID] = runner
		runner.start()
	}

	m.log.Info("plc: config applied",
		"variables", m.variables.Count(),
		"programs", m.engine.ProgramCount(),
		"tasks", len(m.tasks))
}

// loadTemplates reads every template from the plc_templates KV bucket.
// Returns an empty map if the bucket is empty or unreachable; template-
// typed variables referencing missing templates will log a warning and
// fall back to None at instantiation time.
func (m *Module) loadTemplates() map[string]*itypes.PlcTemplate {
	out := map[string]*itypes.PlcTemplate{}
	keys, err := m.b.KVKeys(topics.BucketPlcTemplates)
	if err != nil {
		return out
	}
	for _, k := range keys {
		data, _, err := m.b.KVGet(topics.BucketPlcTemplates, k)
		if err != nil {
			m.log.Warn("plc: failed to read template", "name", k, "error", err)
			continue
		}
		var tmpl itypes.PlcTemplate
		if err := json.Unmarshal(data, &tmpl); err != nil {
			m.log.Warn("plc: failed to parse template", "name", k, "error", err)
			continue
		}
		out[tmpl.Name] = &tmpl
	}
	return out
}

// initialVariableValue derives the runtime value for a newly-registered
// variable. Atomic variables get vcfg.Default; template-typed variables
// get a freshly instantiated StructValue (with vcfg.Default merged into
// its fields) so programs can use dot-access / method dispatch on them.
func initialVariableValue(e *Engine, vcfg *itypes.PlcVariableConfigKV, log *slog.Logger) interface{} {
	if _, ok := e.getTemplate(vcfg.Datatype); !ok {
		return vcfg.Default
	}
	var values map[string]starlark.Value
	if m, ok := vcfg.Default.(map[string]interface{}); ok {
		values = make(map[string]starlark.Value, len(m))
		for k, v := range m {
			values[k] = goToStarlark(v)
		}
	}
	sv, err := e.NewStruct(vcfg.Datatype, values)
	if err != nil {
		log.Warn("plc: failed to instantiate template variable",
			"variable", vcfg.ID, "type", vcfg.Datatype, "error", err)
		return nil
	}
	return sv
}

// handleVariablesRequest responds to {plcId}.variables request/reply
// with the current variable state for downstream consumers.
func (m *Module) handleVariablesRequest(reply bus.ReplyFunc) {
	if reply == nil {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	allVars := m.variables.All()
	result := make([]types.VariableInfo, 0, len(allVars))
	for _, v := range allVars {
		result = append(result, types.VariableInfo{
			ModuleID:   m.plcID,
			VariableID: v.ID,
			Datatype:   v.Datatype,
			Value:      v.Value,
		})
	}

	data, err := json.Marshal(result)
	if err != nil {
		m.log.Error("plc: failed to marshal variables response", "error", err)
		return
	}
	reply(data)
}

// handleVariableCommand processes a write command published by the API.
// Subject format: {plcID}.command.{variableID}. For atomic variables,
// variableID is a simple name (e.g. "counter"). For struct fields, it
// is dot-separated (e.g. "motor1.test" or "motor1.nested.flag").
func (m *Module) handleVariableCommand(subject string, data []byte) {
	prefix := m.plcID + ".command."
	if !strings.HasPrefix(subject, prefix) {
		return
	}
	path := strings.TrimPrefix(subject, prefix)
	if path == "" {
		return
	}

	var body struct {
		Value interface{} `json:"value"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		m.log.Warn("plc: invalid command payload", "subject", subject, "error", err)
		return
	}

	now := time.Now().UnixMilli()
	parts := strings.Split(path, ".")

	// Atomic variable write.
	if len(parts) == 1 {
		if !m.variables.Set(parts[0], body.Value, now) {
			m.log.Warn("plc: command for unknown variable", "variable", parts[0])
		}
		return
	}

	// Struct field write: walk from the root StructValue through any
	// intermediate struct fields, then SetField on the leaf.
	rootID := parts[0]
	rv := m.variables.GetVariable(rootID)
	if rv == nil {
		m.log.Warn("plc: command for unknown variable", "variable", rootID)
		return
	}
	sv, ok := rv.Value.(*StructValue)
	if !ok {
		m.log.Warn("plc: nested write to non-struct variable", "variable", rootID)
		return
	}
	for i := 1; i < len(parts)-1; i++ {
		attr, err := sv.Attr(parts[i])
		if err != nil || attr == nil {
			m.log.Warn("plc: nested path not found", "variable", rootID, "field", parts[i])
			return
		}
		next, ok := attr.(*StructValue)
		if !ok {
			m.log.Warn("plc: nested path is not a struct", "variable", rootID, "field", parts[i])
			return
		}
		sv = next
	}
	leaf := parts[len(parts)-1]
	if err := sv.SetField(leaf, goToStarlark(body.Value)); err != nil {
		m.log.Warn("plc: struct field write failed", "path", path, "error", err)
		return
	}
	m.variables.MarkChanged(rootID)
}

// handleTaskStatsRequest returns per-task scan-time statistics.
// The response is a map of taskID -> TaskStatsSnapshot, plus the task's
// configured scan rate so the UI can compute headroom.
func (m *Module) handleTaskStatsRequest(reply bus.ReplyFunc) {
	if reply == nil {
		return
	}
	m.mu.RLock()
	type taskStatsResponse struct {
		TaskStatsSnapshot
		ScanRateMs int `json:"scanRateMs"`
	}
	out := make(map[string]taskStatsResponse, len(m.tasks))
	for id, t := range m.tasks {
		snap := t.stats.snapshot()
		out[id] = taskStatsResponse{
			TaskStatsSnapshot: snap,
			ScanRateMs:        int(t.scanRate / time.Millisecond),
		}
	}
	m.mu.RUnlock()

	data, err := json.Marshal(out)
	if err != nil {
		m.log.Error("plc: failed to marshal task stats response", "error", err)
		return
	}
	reply(data)
}
