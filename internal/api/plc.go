//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/plc"
	"github.com/joyautomation/tentacle/internal/plc/lsp"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// PLC programs are stored in a single global bucket keyed by program name.
// The {plcId} URL segment is preserved for symmetry with config endpoints
// and to leave room for future per-PLC scoping, but it is not part of the KV key.

// ─── PLC Config Helpers ────────────────────────────────────────────────────

func (m *Module) getPlcConfig(plcID string) (*itypes.PlcConfigKV, error) {
	data, _, err := m.bus.KVGet(topics.BucketPlcConfig, plcID)
	if err != nil {
		return nil, err
	}
	var cfg itypes.PlcConfigKV
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (m *Module) putPlcConfig(cfg *itypes.PlcConfigKV) error {
	cfg.UpdatedAt = time.Now().UnixMilli()
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = m.bus.KVPut(topics.BucketPlcConfig, cfg.PlcID, data)
	return err
}

// ensurePlcMaps initializes nil maps so callers can safely write.
func ensurePlcMaps(cfg *itypes.PlcConfigKV) {
	if cfg.Devices == nil {
		cfg.Devices = make(map[string]itypes.PlcDeviceConfigKV)
	}
	if cfg.Variables == nil {
		cfg.Variables = make(map[string]itypes.PlcVariableConfigKV)
	}
	if cfg.UdtTemplates == nil {
		cfg.UdtTemplates = make(map[string]itypes.PlcUdtTemplateConfigKV)
	}
	if cfg.Tasks == nil {
		cfg.Tasks = make(map[string]itypes.PlcTaskConfigKV)
	}
}

func newEmptyPlcConfig(plcID string) *itypes.PlcConfigKV {
	cfg := &itypes.PlcConfigKV{PlcID: plcID}
	ensurePlcMaps(cfg)
	return cfg
}

// ─── PLC Program Helpers ───────────────────────────────────────────────────

func (m *Module) getPlcProgram(name string) (*itypes.PlcProgramKV, error) {
	data, _, err := m.bus.KVGet(topics.BucketPlcPrograms, name)
	if err != nil {
		return nil, err
	}
	var prog itypes.PlcProgramKV
	if err := json.Unmarshal(data, &prog); err != nil {
		return nil, err
	}
	return &prog, nil
}

func (m *Module) putPlcProgram(prog *itypes.PlcProgramKV) error {
	prog.UpdatedAt = time.Now().UnixMilli()
	if prog.UpdatedBy == "" {
		prog.UpdatedBy = "api"
	}
	data, err := json.Marshal(prog)
	if err != nil {
		return err
	}
	_, err = m.bus.KVPut(topics.BucketPlcPrograms, prog.Name, data)
	return err
}

func (m *Module) deletePlcProgram(name string) error {
	return m.bus.KVDelete(topics.BucketPlcPrograms, name)
}

// validProgramLanguages enumerates the supported PLC program languages.
var validProgramLanguages = map[string]bool{
	"ladder":   true,
	"st":       true,
	"starlark": true,
}

// ─── Config CRUD ───────────────────────────────────────────────────────────

func (m *Module) handleGetPlcConfig(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	cfg, err := m.getPlcConfig(plcID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("plc %q not found: %v", plcID, err))
		return
	}
	ensurePlcMaps(cfg)
	writeJSON(w, http.StatusOK, cfg)
}

func (m *Module) handlePutPlcConfig(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")

	var cfg itypes.PlcConfigKV
	if err := readJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	cfg.PlcID = plcID
	ensurePlcMaps(&cfg)

	if err := m.putPlcConfig(&cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put plc config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, &cfg)
}

// ─── Tasks ─────────────────────────────────────────────────────────────────

func (m *Module) handleListPlcTasks(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	cfg, err := m.getPlcConfig(plcID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]itypes.PlcTaskConfigKV{})
		return
	}
	ensurePlcMaps(cfg)
	writeJSON(w, http.StatusOK, cfg.Tasks)
}

func (m *Module) handlePutPlcTask(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	taskName := chi.URLParam(r, "taskName")

	var task itypes.PlcTaskConfigKV
	if err := readJSON(r, &task); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	task.Name = taskName

	if task.ScanRateMs <= 0 {
		writeError(w, http.StatusBadRequest, "scanRateMs must be > 0")
		return
	}
	if task.ProgramRef == "" {
		writeError(w, http.StatusBadRequest, "programRef is required")
		return
	}

	cfg, err := m.getPlcConfig(plcID)
	if err != nil {
		cfg = newEmptyPlcConfig(plcID)
	}
	ensurePlcMaps(cfg)

	cfg.Tasks[taskName] = task

	if err := m.putPlcConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put plc config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (m *Module) handleDeletePlcTask(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	taskName := chi.URLParam(r, "taskName")

	cfg, err := m.getPlcConfig(plcID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("plc %q not found: %v", plcID, err))
		return
	}
	ensurePlcMaps(cfg)

	delete(cfg.Tasks, taskName)

	if err := m.putPlcConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put plc config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── Variables ─────────────────────────────────────────────────────────────

func (m *Module) handleListPlcConfigVariables(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	cfg, err := m.getPlcConfig(plcID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]itypes.PlcVariableConfigKV{})
		return
	}
	ensurePlcMaps(cfg)
	writeJSON(w, http.StatusOK, cfg.Variables)
}

func (m *Module) handlePutPlcConfigVariable(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	variableID := chi.URLParam(r, "variableId")

	var v itypes.PlcVariableConfigKV
	if err := readJSON(r, &v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	v.ID = variableID

	if v.Datatype == "" {
		writeError(w, http.StatusBadRequest, "datatype is required")
		return
	}
	if v.Direction == "" {
		writeError(w, http.StatusBadRequest, "direction is required")
		return
	}

	cfg, err := m.getPlcConfig(plcID)
	if err != nil {
		cfg = newEmptyPlcConfig(plcID)
	}
	ensurePlcMaps(cfg)

	cfg.Variables[variableID] = v

	if err := m.putPlcConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put plc config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (m *Module) handleDeletePlcConfigVariable(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	variableID := chi.URLParam(r, "variableId")

	cfg, err := m.getPlcConfig(plcID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("plc %q not found: %v", plcID, err))
		return
	}
	ensurePlcMaps(cfg)

	delete(cfg.Variables, variableID)

	if err := m.putPlcConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put plc config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ─── Programs ──────────────────────────────────────────────────────────────

// programListItem is the program list response (omits source for payload size).
type programListItem struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description,omitempty"`
	Module           string                 `json:"module,omitempty"`
	Language         string                 `json:"language"`
	Signature        *itypes.PlcFunctionSig `json:"signature,omitempty"`
	UpdatedAt        int64                  `json:"updatedAt"`
	UpdatedBy        string                 `json:"updatedBy,omitempty"`
	HasPending       bool                   `json:"hasPending,omitempty"`
	PendingUpdatedAt int64                  `json:"pendingUpdatedAt,omitempty"`
	PendingUpdatedBy string                 `json:"pendingUpdatedBy,omitempty"`
}

func (m *Module) handleListPlcPrograms(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketPlcPrograms)
	if err != nil {
		// Empty bucket — return empty list rather than 500.
		writeJSON(w, http.StatusOK, []programListItem{})
		return
	}
	out := make([]programListItem, 0, len(keys))
	for _, k := range keys {
		prog, err := m.getPlcProgram(k)
		if err != nil {
			m.log.Warn("skipping plc program", "key", k, "err", err)
			continue
		}
		out = append(out, programListItem{
			Name:             prog.Name,
			Description:      prog.Description,
			Module:           prog.Module,
			Language:         prog.Language,
			Signature:        prog.Signature,
			UpdatedAt:        prog.UpdatedAt,
			UpdatedBy:        prog.UpdatedBy,
			HasPending:       prog.HasPending(),
			PendingUpdatedAt: prog.PendingUpdatedAt,
			PendingUpdatedBy: prog.PendingUpdatedBy,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (m *Module) handleGetPlcProgram(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	prog, err := m.getPlcProgram(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("program %q not found: %v", name, err))
		return
	}
	writeJSON(w, http.StatusOK, prog)
}

func (m *Module) handlePutPlcProgram(w http.ResponseWriter, r *http.Request) {
	urlName := chi.URLParam(r, "name")

	var prog itypes.PlcProgramKV
	if err := readJSON(r, &prog); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if prog.Name == "" {
		prog.Name = urlName
	}

	if !validProgramLanguages[prog.Language] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("language must be one of ladder, st, starlark (got %q)", prog.Language))
		return
	}
	if prog.Source == "" && prog.Language != "st" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}
	if !isValidProgramName(prog.Name) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("name %q is not a valid identifier", prog.Name))
		return
	}

	// Rename: URL key differs from body name. The old entry is deleted
	// after the new entry is successfully persisted. A collision with an
	// existing program at the new name is rejected with 409.
	rename := urlName != prog.Name
	if rename {
		if existing, _ := m.getPlcProgram(prog.Name); existing != nil {
			writeError(w, http.StatusConflict, fmt.Sprintf("a program named %q already exists", prog.Name))
			return
		}
	}

	// Signatures are derived from Python-style annotations on the entry
	// function's def header — the client no longer supplies them directly.
	// Sources that don't declare annotations leave Signature nil, which is
	// the right default (completion still offers the name as a bare call).
	if prog.Language == "starlark" {
		prog.Signature = plc.DeriveProgramSignature(prog.Source, prog.Name)
	}

	if err := m.putPlcProgram(&prog); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put plc program: %v", err))
		return
	}
	if rename {
		if err := m.deletePlcProgram(urlName); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete old program %q after rename: %v", urlName, err))
			return
		}
	}
	writeJSON(w, http.StatusOK, &prog)
}

// isValidProgramName returns true when s is a non-empty identifier of the
// form [A-Za-z_][A-Za-z0-9_]* — the same shape Starlark accepts for a def
// name, which is what users end up typing in the editor.
func isValidProgramName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// ─── References ────────────────────────────────────────────────────────────

// referenceSite is one cross-reference hit. When Source == "program" the
// site lives inside a Starlark program's source and Line/StartCol/EndCol
// pinpoint the token. When Source == "taskProgramRef" the site is a task's
// programRef field — Task identifies which task, and no text positions
// apply (the field is structured config, not source).
type referenceSite struct {
	Source   string `json:"source"`
	Program  string `json:"program,omitempty"`
	Task     string `json:"task,omitempty"`
	Line     int    `json:"line,omitempty"`
	StartCol int    `json:"startCol,omitempty"`
	EndCol   int    `json:"endCol,omitempty"`
	LineText string `json:"lineText,omitempty"`
}

// handleFindPlcReferences returns every place the given name is referenced
// across the PLC. kind=program matches bare-ident call sites in Starlark
// sources plus task programRefs; kind=variable matches the first string
// argument of variable-taking builtins (get_var, set_var, NO, TON, …).
//
// Only Starlark programs are scanned for source-level references — ST and
// ladder sources are not parsed by the same walker.
func (m *Module) handleFindPlcReferences(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	name := r.URL.Query().Get("name")
	kind := r.URL.Query().Get("kind")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name query parameter is required")
		return
	}
	if kind != "program" && kind != "variable" {
		writeError(w, http.StatusBadRequest, "kind must be program or variable")
		return
	}

	keys, err := m.bus.KVKeys(topics.BucketPlcPrograms)
	if err != nil {
		// Empty bucket is not an error — no programs, no references.
		keys = nil
	}

	sites := make([]referenceSite, 0)
	for _, k := range keys {
		prog, err := m.getPlcProgram(k)
		if err != nil || prog == nil {
			continue
		}
		if prog.Language != "starlark" {
			continue
		}
		// Skip the program itself when looking up references to it — the
		// def header isn't a "use", and local recursive calls are part of
		// the owning program's body and handled by rename separately.
		if kind == "program" && prog.Name == name {
			continue
		}
		var refs []lsp.SourceReference
		if kind == "program" {
			refs = lsp.FindProgramReferences(prog.Source, name)
		} else {
			refs = lsp.FindVariableReferences(prog.Source, name)
		}
		for _, ref := range refs {
			sites = append(sites, referenceSite{
				Source:   "program",
				Program:  prog.Name,
				Line:     ref.Line,
				StartCol: ref.StartCol,
				EndCol:   ref.EndCol,
				LineText: ref.LineText,
			})
		}
	}

	if kind == "program" {
		if cfg, err := m.getPlcConfig(plcID); err == nil && cfg != nil {
			ensurePlcMaps(cfg)
			taskNames := make([]string, 0, len(cfg.Tasks))
			for taskName := range cfg.Tasks {
				taskNames = append(taskNames, taskName)
			}
			sort.Strings(taskNames)
			for _, taskName := range taskNames {
				task := cfg.Tasks[taskName]
				if task.ProgramRef == name {
					sites = append(sites, referenceSite{
						Source:  "taskProgramRef",
						Task:    taskName,
						Program: task.ProgramRef,
					})
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, sites)
}

func (m *Module) handleDeletePlcProgram(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := m.deletePlcProgram(name); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete plc program: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Online Edit: pending / assemble / cancel ──────────────────────────────
//
// RSLogix-style online-edit flow. The live program keeps running while the
// user edits; changes accumulate in the Pending* fields on the KV record
// and only swap into the engine on assemble.
//
//   PUT    /programs/{name}/pending   — write/update pending edit
//   POST   /programs/{name}/assemble  — promote pending → live (hot-swap)
//   POST   /programs/{name}/cancel    — discard pending

// handlePutPlcProgramPending stores an uncommitted edit on an existing
// program. Does not recompile — the engine keeps running the live source
// until assemble. Refuses when no live program exists yet (first save
// still goes through PUT /programs/{name}).
func (m *Module) handlePutPlcProgramPending(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var body struct {
		Source      string `json:"source"`
		StSource    string `json:"stSource,omitempty"`
		Language    string `json:"language,omitempty"`
		Description string `json:"description,omitempty"`
		UpdatedBy   string `json:"updatedBy,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	existing, err := m.getPlcProgram(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("program %q not found (create via PUT /programs/%s first)", name, name))
		return
	}

	lang := body.Language
	if lang == "" {
		lang = existing.Language
	}
	if !validProgramLanguages[lang] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("language must be one of ladder, st, starlark (got %q)", lang))
		return
	}
	if body.Source == "" && lang != "st" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	existing.PendingSource = body.Source
	existing.PendingStSource = body.StSource
	existing.PendingLanguage = lang
	existing.PendingUpdatedAt = time.Now().UnixMilli()
	existing.PendingUpdatedBy = body.UpdatedBy
	if existing.PendingUpdatedBy == "" {
		existing.PendingUpdatedBy = "api"
	}
	if lang == "starlark" {
		existing.PendingSignature = plc.DeriveProgramSignature(body.Source, name)
	} else {
		existing.PendingSignature = nil
	}

	// Persist pending edit without touching the live Source/UpdatedAt so
	// the program-bucket watcher treats this as a no-op for the engine.
	data, err := json.Marshal(existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("marshal program: %v", err))
		return
	}
	if _, err := m.bus.KVPut(topics.BucketPlcPrograms, name, data); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put pending program: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

// handleAssemblePlcProgram promotes the pending edit to live. The KV write
// bumps Source/UpdatedAt; the PLC module's program-bucket watcher picks it
// up and hot-recompiles the engine.
func (m *Module) handleAssemblePlcProgram(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	existing, err := m.getPlcProgram(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("program %q not found", name))
		return
	}
	if !existing.HasPending() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("program %q has no pending edit", name))
		return
	}

	existing.Source = existing.PendingSource
	existing.StSource = existing.PendingStSource
	if existing.PendingLanguage != "" {
		existing.Language = existing.PendingLanguage
	}
	if existing.PendingSignature != nil {
		existing.Signature = existing.PendingSignature
	} else if existing.Language == "starlark" {
		existing.Signature = plc.DeriveProgramSignature(existing.Source, name)
	}
	if existing.PendingUpdatedBy != "" {
		existing.UpdatedBy = existing.PendingUpdatedBy
	}

	existing.PendingSource = ""
	existing.PendingStSource = ""
	existing.PendingLanguage = ""
	existing.PendingSignature = nil
	existing.PendingUpdatedAt = 0
	existing.PendingUpdatedBy = ""

	if err := m.putPlcProgram(existing); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("assemble plc program: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

// handleCancelPlcProgram discards the pending edit. Live source untouched.
func (m *Module) handleCancelPlcProgram(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	existing, err := m.getPlcProgram(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("program %q not found", name))
		return
	}
	if !existing.HasPending() {
		writeJSON(w, http.StatusOK, existing)
		return
	}

	existing.PendingSource = ""
	existing.PendingStSource = ""
	existing.PendingLanguage = ""
	existing.PendingSignature = nil
	existing.PendingUpdatedAt = 0
	existing.PendingUpdatedBy = ""

	data, err := json.Marshal(existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("marshal program: %v", err))
		return
	}
	if _, err := m.bus.KVPut(topics.BucketPlcPrograms, name, data); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("cancel pending program: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

// ─── Online Edit: Try Mode ─────────────────────────────────────────────────
//
// "Try" hot-swaps a candidate source into the live engine. If a task
// raises an error during the session — or a watchdog timer expires — the
// PLC module auto-reverts to the pre-try source. The live KV record is
// never touched; try mode is ephemeral.
//
// Named "try" (not "test") so the latter stays free for the unit-test
// framework.
//
//   POST   /programs/{name}/try        — start session { source, timeoutSeconds? }
//   POST   /programs/{name}/try/stop   — end session (revert now)
//   GET    /programs/{name}/try        — session status + last event
//   GET    /plcs/{plcId}/programs/try/events — SSE stream of try events

type tryStartRequest struct {
	Source         string `json:"source"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// plcTryCommand is the wire format the PLC module decodes on PlcTry.
// Mirrors plc.tryCommand — duplicated here to avoid the api package
// importing the plc package (which the build tags keep independent).
type plcTryCommand struct {
	Op             string `json:"op"`
	Program        string `json:"program"`
	Source         string `json:"source,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

func (m *Module) doTryRequest(plcID string, cmd plcTryCommand) ([]byte, int, error) {
	body, err := json.Marshal(cmd)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	resp, err := m.bus.Request(topics.PlcTry(plcID), body, busTimeout)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	// Peek at ok=false so we can surface the engine error with a 4xx.
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &env); err == nil && !env.OK {
		return resp, http.StatusBadRequest, fmt.Errorf("%s", env.Error)
	}
	return resp, http.StatusOK, nil
}

func (m *Module) handleStartPlcProgramTry(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	name := chi.URLParam(r, "name")

	var body tryStartRequest
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if body.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	resp, status, err := m.doTryRequest(plcID, plcTryCommand{
		Op:             "start",
		Program:        name,
		Source:         body.Source,
		TimeoutSeconds: body.TimeoutSeconds,
	})
	if err != nil && status != http.StatusBadRequest {
		writeError(w, status, fmt.Sprintf("start try session: %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

func (m *Module) handleStopPlcProgramTry(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	name := chi.URLParam(r, "name")

	resp, status, err := m.doTryRequest(plcID, plcTryCommand{Op: "stop", Program: name})
	if err != nil && status != http.StatusBadRequest {
		writeError(w, status, fmt.Sprintf("stop try session: %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

func (m *Module) handleGetPlcProgramTry(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	name := chi.URLParam(r, "name")

	resp, status, err := m.doTryRequest(plcID, plcTryCommand{Op: "status", Program: name})
	if err != nil && status != http.StatusBadRequest {
		writeError(w, status, fmt.Sprintf("get try session: %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

// handleStreamPlcProgramTryEvents forwards try-session events from the
// bus to an SSE stream. One shared stream per PLC — the client filters by
// program name client-side.
func (m *Module) handleStreamPlcProgramTryEvents(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	events := make(chan json.RawMessage, 32)
	sub, err := m.bus.Subscribe(topics.PlcTryEvents(plcID), func(_ string, data []byte, _ bus.ReplyFunc) {
		select {
		case events <- append(json.RawMessage(nil), data...):
		default:
			// Drop if the client is too slow — try events are low-volume
			// and a lost revert notification is still surfaced by the
			// next GET /try poll.
		}
	})
	if err != nil {
		m.log.Warn("plc: failed to subscribe to try events", "error", err)
		return
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events:
			if err := sse.WriteEvent("try", ev); err != nil {
				return
			}
		}
	}
}

// ─── Unit Tests ─────────────────────────────────────────────────────────────
//
// Tests are Starlark scripts persisted in the plc_tests KV bucket. A test
// exercises live program code through the same cross-program call proxies
// used at runtime and uses the assert_* builtins to verify behavior.
//
//   GET    /plcs/{plcId}/tests            — list
//   GET    /plcs/{plcId}/tests/{name}     — get
//   PUT    /plcs/{plcId}/tests/{name}     — create/update
//   DELETE /plcs/{plcId}/tests/{name}     — delete
//   POST   /plcs/{plcId}/tests/{name}/run — execute single test
//   POST   /plcs/{plcId}/tests/run        — execute all tests
//   GET    /plcs/{plcId}/tests/events     — SSE of test results

func (m *Module) getPlcTest(name string) (*itypes.PlcTestKV, error) {
	data, _, err := m.bus.KVGet(topics.BucketPlcTests, name)
	if err != nil {
		return nil, err
	}
	var t itypes.PlcTestKV
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (m *Module) putPlcTest(t *itypes.PlcTestKV) error {
	t.UpdatedAt = time.Now().UnixMilli()
	if t.UpdatedBy == "" {
		t.UpdatedBy = "api"
	}
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = m.bus.KVPut(topics.BucketPlcTests, t.Name, data)
	return err
}

type testListItem struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	UpdatedAt   int64                 `json:"updatedAt"`
	UpdatedBy   string                `json:"updatedBy,omitempty"`
	LastResult  *itypes.PlcTestResult `json:"lastResult,omitempty"`
}

func (m *Module) handleListPlcTests(w http.ResponseWriter, r *http.Request) {
	keys, err := m.bus.KVKeys(topics.BucketPlcTests)
	if err != nil {
		writeJSON(w, http.StatusOK, []testListItem{})
		return
	}
	out := make([]testListItem, 0, len(keys))
	for _, k := range keys {
		t, err := m.getPlcTest(k)
		if err != nil {
			m.log.Warn("skipping plc test", "key", k, "err", err)
			continue
		}
		out = append(out, testListItem{
			Name:        t.Name,
			Description: t.Description,
			UpdatedAt:   t.UpdatedAt,
			UpdatedBy:   t.UpdatedBy,
			LastResult:  t.LastResult,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (m *Module) handleGetPlcTest(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	t, err := m.getPlcTest(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("test %q not found: %v", name, err))
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (m *Module) handlePutPlcTest(w http.ResponseWriter, r *http.Request) {
	urlName := chi.URLParam(r, "name")

	var t itypes.PlcTestKV
	if err := readJSON(r, &t); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if t.Name == "" {
		t.Name = urlName
	}
	if t.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}
	if !isValidProgramName(t.Name) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("name %q is not a valid identifier", t.Name))
		return
	}

	rename := urlName != t.Name
	if rename {
		if existing, _ := m.getPlcTest(t.Name); existing != nil {
			writeError(w, http.StatusConflict, fmt.Sprintf("a test named %q already exists", t.Name))
			return
		}
	}

	if err := m.putPlcTest(&t); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put plc test: %v", err))
		return
	}
	if rename {
		if err := m.bus.KVDelete(topics.BucketPlcTests, urlName); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete old test %q after rename: %v", urlName, err))
			return
		}
	}
	writeJSON(w, http.StatusOK, &t)
}

func (m *Module) handleDeletePlcTest(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := m.bus.KVDelete(topics.BucketPlcTests, name); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete plc test: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// plcTestRunCommand mirrors plc.testRunCommand.
type plcTestRunCommand struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// doTestRunRequest issues a run command to the PLC module and returns the
// unmarshalled result (or an error + matching HTTP status).
func (m *Module) doTestRunRequest(plcID string, cmd plcTestRunCommand) (*itypes.PlcTestResult, int, error) {
	body, err := json.Marshal(cmd)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	resp, err := m.bus.Request(topics.PlcTest(plcID), body, busTimeout)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	var env struct {
		OK     bool                  `json:"ok"`
		Error  string                `json:"error,omitempty"`
		Result *itypes.PlcTestResult `json:"result,omitempty"`
	}
	if err := json.Unmarshal(resp, &env); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if !env.OK {
		return nil, http.StatusBadRequest, fmt.Errorf("%s", env.Error)
	}
	return env.Result, http.StatusOK, nil
}

// handleRunPlcTest executes a single test and persists its result on the
// KV record so the next list call reflects the latest status.
func (m *Module) handleRunPlcTest(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	name := chi.URLParam(r, "name")

	t, err := m.getPlcTest(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("test %q not found: %v", name, err))
		return
	}

	result, status, err := m.doTestRunRequest(plcID, plcTestRunCommand{Name: t.Name, Source: t.Source})
	if err != nil {
		writeError(w, status, fmt.Sprintf("run test: %v", err))
		return
	}

	t.LastResult = result
	if perr := m.putPlcTest(t); perr != nil {
		m.log.Warn("plc: failed to persist test result", "name", name, "error", perr)
	}
	writeJSON(w, http.StatusOK, result)
}

// handleRunAllPlcTests runs every test in the bucket sequentially and
// returns their results. Each run persists its own last-result snapshot.
func (m *Module) handleRunAllPlcTests(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")

	keys, err := m.bus.KVKeys(topics.BucketPlcTests)
	if err != nil {
		writeJSON(w, http.StatusOK, []itypes.PlcTestResult{})
		return
	}
	sort.Strings(keys)

	results := make([]itypes.PlcTestResult, 0, len(keys))
	for _, k := range keys {
		t, err := m.getPlcTest(k)
		if err != nil {
			continue
		}
		result, _, err := m.doTestRunRequest(plcID, plcTestRunCommand{Name: t.Name, Source: t.Source})
		if err != nil {
			results = append(results, itypes.PlcTestResult{
				Name:    t.Name,
				Status:  "error",
				Message: err.Error(),
			})
			continue
		}
		t.LastResult = result
		if perr := m.putPlcTest(t); perr != nil {
			m.log.Warn("plc: failed to persist test result", "name", t.Name, "error", perr)
		}
		results = append(results, *result)
	}
	writeJSON(w, http.StatusOK, results)
}

// handleStreamPlcTestEvents forwards test-run events from the PLC module
// bus topic to an SSE stream. One shared stream per PLC — the client
// filters by test name.
func (m *Module) handleStreamPlcTestEvents(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")
	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	events := make(chan json.RawMessage, 32)
	sub, err := m.bus.Subscribe(topics.PlcTestEvents(plcID), func(_ string, data []byte, _ bus.ReplyFunc) {
		select {
		case events <- append(json.RawMessage(nil), data...):
		default:
		}
	})
	if err != nil {
		m.log.Warn("plc: failed to subscribe to test events", "error", err)
		return
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events:
			if err := sse.WriteEvent("test", ev); err != nil {
				return
			}
		}
	}
}


type plcImportItem struct {
	VariableID     string      `json:"variableId"`
	DeviceID       string      `json:"deviceId"`
	Tag            string      `json:"tag"`
	Datatype       string      `json:"datatype"`
	Protocol       string      `json:"protocol"`
	CipType        string      `json:"cipType,omitempty"`
	Direction      string      `json:"direction"`
	Default        interface{} `json:"default"`
	FunctionCode   *int        `json:"functionCode,omitempty"`
	ModbusDatatype string      `json:"modbusDatatype,omitempty"`
	ByteOrder      string      `json:"byteOrder,omitempty"`
	Address        *int        `json:"address,omitempty"`
}

type plcImportRequest struct {
	GatewayID string           `json:"gatewayId"`
	Imports   []plcImportItem  `json:"imports"`
}

func (m *Module) handleBatchImportPlcVariables(w http.ResponseWriter, r *http.Request) {
	plcID := chi.URLParam(r, "plcId")

	var req plcImportRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if len(req.Imports) == 0 {
		writeError(w, http.StatusBadRequest, "imports is required and must not be empty")
		return
	}
	if req.GatewayID == "" {
		req.GatewayID = "gateway"
	}

	gwCfg, err := m.getGatewayConfig(req.GatewayID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("read gateway config: %v", err))
		return
	}

	cfg, err := m.getPlcConfig(plcID)
	if err != nil {
		cfg = &itypes.PlcConfigKV{PlcID: plcID}
	}
	ensurePlcMaps(cfg)

	for _, imp := range req.Imports {
		if imp.VariableID == "" || imp.DeviceID == "" || imp.Tag == "" || imp.Protocol == "" {
			writeError(w, http.StatusBadRequest, "each import requires variableId, deviceId, tag, and protocol")
			return
		}
		if imp.Datatype == "" {
			imp.Datatype = "number"
		}
		if imp.Direction == "" {
			imp.Direction = "input"
		}

		if _, exists := cfg.Devices[imp.DeviceID]; !exists {
			if gwDev, ok := gwCfg.Devices[imp.DeviceID]; ok {
				cfg.Devices[imp.DeviceID] = itypes.PlcDeviceConfigKV{
					Protocol:    gwDev.Protocol,
					Host:        gwDev.Host,
					Port:        gwDev.Port,
					Slot:        gwDev.Slot,
					EndpointURL: gwDev.EndpointURL,
					Version:     gwDev.Version,
					Community:   gwDev.Community,
					UnitID:      gwDev.UnitID,
					ScanRate:    gwDev.ScanRate,
				}
			} else {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("device %q not found in gateway config", imp.DeviceID))
				return
			}
		}

		cfg.Variables[imp.VariableID] = itypes.PlcVariableConfigKV{
			ID:       imp.VariableID,
			Datatype: imp.Datatype,
			Default:  imp.Default,
			Direction: imp.Direction,
			Source: &itypes.PlcVariableSourceKV{
				Protocol:       imp.Protocol,
				DeviceID:       imp.DeviceID,
				Tag:            imp.Tag,
				CipType:        imp.CipType,
				FunctionCode:   imp.FunctionCode,
				ModbusDatatype: imp.ModbusDatatype,
				ByteOrder:      imp.ByteOrder,
				Address:        imp.Address,
			},
		}
	}

	if err := m.putPlcConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save plc config: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}
