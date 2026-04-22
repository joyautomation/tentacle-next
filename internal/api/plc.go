//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/plc"
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
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Language    string                 `json:"language"`
	Signature   *itypes.PlcFunctionSig `json:"signature,omitempty"`
	UpdatedAt   int64                  `json:"updatedAt"`
	UpdatedBy   string                 `json:"updatedBy,omitempty"`
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
			Name:        prog.Name,
			Description: prog.Description,
			Language:    prog.Language,
			Signature:   prog.Signature,
			UpdatedAt:   prog.UpdatedAt,
			UpdatedBy:   prog.UpdatedBy,
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
	name := chi.URLParam(r, "name")

	var prog itypes.PlcProgramKV
	if err := readJSON(r, &prog); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	prog.Name = name

	if !validProgramLanguages[prog.Language] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("language must be one of ladder, st, starlark (got %q)", prog.Language))
		return
	}
	if prog.Source == "" && prog.Language != "st" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
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
	writeJSON(w, http.StatusOK, &prog)
}

func (m *Module) handleDeletePlcProgram(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := m.deletePlcProgram(name); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete plc program: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── PLC Browse Import ─────────────────────────────────────────────────────

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
