//go:build plc || all

// Package plc implements a Starlark-based PLC task runner module.
// It subscribes directly to scanner topics for input variables,
// executes Starlark programs on configurable scan intervals,
// and publishes output variables that the gateway module consumes.
package plc

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/joyautomation/tentacle/internal/scanner"
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
	sources   *scanner.Registry
	variables *VariableStore
	engine    *Engine
	tasks     map[string]*taskRunner
	pub       *publisher
	bridge    *scannerBridge
	tryMgr    *TrySessionManager

	persist *persister

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
		topics.BucketPlcVariables, topics.BucketPlcValues,
		topics.BucketSources,
		topics.BucketScannerEthernetIP, topics.BucketScannerOpcUA,
		topics.BucketScannerModbus, topics.BucketScannerSNMP,
	} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("plc: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Migrate any legacy plc_config.devices to the shared sources bucket.
	scanner.MigrateLegacyDevices(b, m.log, topics.BucketPlcConfig, m.plcID)

	// Start the shared sources registry watcher.
	m.sources = scanner.NewRegistry(b, m.log)
	if err := m.sources.Start(m.onSourcesChanged); err != nil {
		m.log.Error("plc: failed to start sources registry", "error", err)
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

	// Watch for program changes so edits assembled via the API hot-swap
	// into the running engine without a config reapply. Pending-only
	// writes don't change Source so they won't recompile.
	progSub, err := b.KVWatchAll(topics.BucketPlcPrograms, func(key string, value []byte, op bus.KVOperation) {
		m.mu.RLock()
		eng := m.engine
		m.mu.RUnlock()
		if eng == nil {
			return
		}
		if op == bus.KVOpDelete {
			eng.Remove(key)
			m.log.Info("plc: program removed, engine updated", "program", key)
			return
		}
		var prog itypes.PlcProgramKV
		if err := json.Unmarshal(value, &prog); err != nil {
			m.log.Error("plc: failed to parse updated program", "program", key, "error", err)
			return
		}
		if eng.Source(prog.Name) == prog.Source {
			return // pending-only write or no-op: live source unchanged
		}
		if err := eng.Compile(prog.Name, prog.Source); err != nil {
			m.log.Error("plc: failed to recompile program", "program", prog.Name, "error", err)
			return
		}
		m.log.Info("plc: program recompiled", "program", prog.Name)
	})
	if err != nil {
		m.log.Error("plc: failed to watch programs KV", "error", err)
	} else {
		m.mu.Lock()
		m.subs = append(m.subs, progSub)
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

	// Handle try-session commands (start/stop/status).
	trySub, _ := b.Subscribe(topics.PlcTry(m.plcID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.handleTryRequest(data, reply)
	})
	m.mu.Lock()
	m.subs = append(m.subs, trySub)
	m.mu.Unlock()

	// Handle test-run commands.
	testSub, _ := b.Subscribe(topics.PlcTest(m.plcID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.handleTestRequest(data, reply)
	})
	m.mu.Lock()
	m.subs = append(m.subs, testSub)
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

	// Stop persister (flushes pending writes before exiting).
	if m.persist != nil {
		m.persist.stop()
		m.persist = nil
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
	if m.tryMgr != nil {
		m.tryMgr.Close()
		m.tryMgr = nil
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

// onSourcesChanged is invoked by the shared sources Registry whenever a source
// is added, updated, or deleted. It re-applies the current config so the
// scanner bridge picks up the new source set.
func (m *Module) onSourcesChanged() {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()
	if cfg == nil {
		return
	}
	m.applyConfig(cfg)
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

	// Stop existing persister. Its run loop flushes any pending writes
	// on close, so values in flight when config reloads are preserved.
	if m.persist != nil {
		m.persist.stop()
		m.persist = nil
	}

	// Stop existing scanner bridge.
	if m.bridge != nil {
		m.bridge.unsubscribe()
		m.bridge = nil
	}

	// Tear down any active try session manager — it holds an error
	// observer on the old engine that must be released.
	if m.tryMgr != nil {
		m.tryMgr.Close()
		m.tryMgr = nil
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

	// Wire try-session manager. onEvent publishes terminal events to the
	// bus so API subscribers (SSE streams) can forward to the UI.
	m.tryMgr = NewTrySessionManager(m.engine, m.log, func(ev TryEvent) {
		if data, err := json.Marshal(ev); err == nil {
			_ = m.b.Publish(topics.PlcTryEvents(m.plcID), data)
		}
	})

	// Register variables. Persisted snapshots override config defaults
	// so values survive restart/redeploy; a variable missing from the
	// snapshot (newly added in config) falls back to its configured
	// default.
	persisted := loadPersistedValues(m.b, m.log)
	now := time.Now().UnixMilli()
	restored := 0
	for id, vcfg := range config.Variables {
		var initial interface{}
		if snap, ok := persisted[id]; ok {
			initial = restoreVariableValue(m.engine, &vcfg, snap.Value, m.log)
			restored++
		} else {
			initial = initialVariableValue(m.engine, &vcfg, m.log)
		}
		rv := &RuntimeVariable{
			ID:          id,
			Datatype:    vcfg.Datatype,
			Direction:   vcfg.Direction,
			Value:       initial,
			Quality:     "good",
			LastUpdated: now,
		}
		m.variables.Add(rv)
	}
	if restored > 0 {
		m.log.Info("plc: restored persisted values", "count", restored)
	}

	// Load and compile every program in the KV bucket (not just the ones
	// referenced by a task). The Starlark engine cross-links top-level
	// defs across programs at compile time, so a task program can call
	// helper functions defined in sibling programs — even if those
	// siblings aren't themselves run as tasks.
	progKeys, err := m.b.KVKeys(topics.BucketPlcPrograms)
	if err != nil {
		m.log.Error("plc: failed to list programs", "error", err)
	}
	for _, key := range progKeys {
		data, _, err := m.b.KVGet(topics.BucketPlcPrograms, key)
		if err != nil {
			m.log.Error("plc: failed to load program", "program", key, "error", err)
			continue
		}
		var prog itypes.PlcProgramKV
		if err := json.Unmarshal(data, &prog); err != nil {
			m.log.Error("plc: failed to parse program", "program", key, "error", err)
			continue
		}
		if err := m.engine.Compile(prog.Name, prog.Source); err != nil {
			m.log.Error("plc: failed to compile program", "program", prog.Name, "error", err)
			continue
		}
		m.log.Info("plc: compiled program", "program", prog.Name, "language", prog.Language)
	}

	// Start scanner bridge.
	m.bridge = newScannerBridge(m.b, m.plcID, m.variables, m.sources, m.log)
	m.bridge.subscribe(config)

	// Start publisher.
	m.pub = newPublisher(m.b, m.plcID, m.variables, m.log)
	m.pub.start()

	// Start persister. Snapshots changed values to KV on a 1s debounce
	// and flushes on shutdown so the last value survives restart.
	m.persist = newPersister(m.b, m.variables, m.log)
	m.persist.start()

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
		runner := newTaskRunner(taskID, taskCfg.ProgramRef, taskCfg.EntryFn, taskCfg.ScanRateMs, m.engine, m.log)
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

// restoreVariableValue builds the runtime value from a persisted
// snapshot. For atomic types the snapshot's value is returned as-is.
// For template-typed variables the snapshot is a JSON object with a
// `_type` discriminator and field values; we instantiate a fresh
// StructValue (so methods/attrs bind to the current template) and
// overlay the saved field values. Fields removed from the template
// since the snapshot are dropped; fields added since get the
// template's default.
func restoreVariableValue(e *Engine, vcfg *itypes.PlcVariableConfigKV, snap interface{}, log *slog.Logger) interface{} {
	if _, ok := e.getTemplate(vcfg.Datatype); !ok {
		return snap
	}
	m, ok := snap.(map[string]interface{})
	if !ok {
		log.Warn("plc: template snapshot is not an object, using defaults",
			"variable", vcfg.ID, "type", vcfg.Datatype)
		return initialVariableValue(e, vcfg, log)
	}
	values := make(map[string]starlark.Value, len(m))
	for k, v := range m {
		if k == "_type" {
			continue
		}
		values[k] = goToStarlark(v)
	}
	sv, err := e.NewStruct(vcfg.Datatype, values)
	if err != nil {
		// NewStruct rejects unknown fields — retry dropping any field
		// that isn't on the current template. This handles the case
		// where a field was removed from the template between snapshot
		// and restore.
		if tmpl, ok := e.getTemplate(vcfg.Datatype); ok {
			valid := make(map[string]bool, len(tmpl.Fields))
			for _, f := range tmpl.Fields {
				valid[f.Name] = true
			}
			filtered := make(map[string]starlark.Value, len(values))
			for k, v := range values {
				if valid[k] {
					filtered[k] = v
				}
			}
			sv, err = e.NewStruct(vcfg.Datatype, filtered)
		}
	}
	if err != nil {
		log.Warn("plc: failed to restore template variable, using defaults",
			"variable", vcfg.ID, "type", vcfg.Datatype, "error", err)
		return initialVariableValue(e, vcfg, log)
	}
	return sv
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

// tryCommand is the request body decoded from PlcTry messages. The Op
// selects one of start/stop/status; other fields apply only to start.
type tryCommand struct {
	Op             string `json:"op"`
	Program        string `json:"program"`
	Source         string `json:"source,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// tryResponse is the reply envelope for PlcTry requests.
type tryResponse struct {
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Session   *TrySessionInfo `json:"session,omitempty"`
	LastEvent *TryEvent       `json:"lastEvent,omitempty"`
}

// testRunCommand is the request body decoded from PlcTest messages.
// A single run request names the test + supplies its source; the runner
// does not read the KV bucket directly so tests can be executed without
// round-tripping through NATS KV.
type testRunCommand struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// testRunResponse is the reply envelope for PlcTest run requests.
type testRunResponse struct {
	OK     bool                  `json:"ok"`
	Error  string                `json:"error,omitempty"`
	Result *itypes.PlcTestResult `json:"result,omitempty"`
}

// handleTestRequest services the test-run command topic. It executes the
// supplied test source against the live engine and returns the result.
// A terminal event is also published to PlcTestEvents so any streaming
// subscribers see the same outcome.
func (m *Module) handleTestRequest(data []byte, reply bus.ReplyFunc) {
	if reply == nil {
		return
	}

	respond := func(r testRunResponse) {
		payload, err := json.Marshal(r)
		if err != nil {
			m.log.Error("plc: failed to marshal test response", "error", err)
			return
		}
		reply(payload)
	}

	var cmd testRunCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		respond(testRunResponse{OK: false, Error: fmt.Sprintf("invalid body: %v", err)})
		return
	}
	if cmd.Name == "" {
		respond(testRunResponse{OK: false, Error: "name is required"})
		return
	}

	m.mu.RLock()
	engine := m.engine
	m.mu.RUnlock()
	if engine == nil {
		respond(testRunResponse{OK: false, Error: "engine not initialized (config not applied yet)"})
		return
	}

	result := engine.RunTest(cmd.Name, cmd.Source)
	if ev, err := json.Marshal(result); err == nil {
		_ = m.b.Publish(topics.PlcTestEvents(m.plcID), ev)
	}
	respond(testRunResponse{OK: true, Result: &result})
}

// handleTryRequest services the try-session command topic. Callers
// multiplex start/stop/status via the request body's Op field.
func (m *Module) handleTryRequest(data []byte, reply bus.ReplyFunc) {
	if reply == nil {
		return
	}

	respond := func(r tryResponse) {
		payload, err := json.Marshal(r)
		if err != nil {
			m.log.Error("plc: failed to marshal try response", "error", err)
			return
		}
		reply(payload)
	}

	var cmd tryCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		respond(tryResponse{OK: false, Error: fmt.Sprintf("invalid body: %v", err)})
		return
	}
	if cmd.Program == "" {
		respond(tryResponse{OK: false, Error: "program is required"})
		return
	}

	m.mu.RLock()
	mgr := m.tryMgr
	m.mu.RUnlock()
	if mgr == nil {
		respond(tryResponse{OK: false, Error: "try manager not initialized (config not applied yet)"})
		return
	}

	switch cmd.Op {
	case "start":
		timeout := time.Duration(cmd.TimeoutSeconds) * time.Second
		info, err := mgr.Start(cmd.Program, cmd.Source, timeout)
		if err != nil {
			respond(tryResponse{OK: false, Error: err.Error()})
			return
		}
		respond(tryResponse{OK: true, Session: &info})
	case "stop":
		_ = mgr.Stop(cmd.Program)
		respond(tryResponse{OK: true, LastEvent: mgr.LastEvent(cmd.Program)})
	case "status":
		respond(tryResponse{
			OK:        true,
			Session:   mgr.Active(cmd.Program),
			LastEvent: mgr.LastEvent(cmd.Program),
		})
	default:
		respond(tryResponse{OK: false, Error: fmt.Sprintf("unknown op %q", cmd.Op)})
	}
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
