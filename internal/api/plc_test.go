//go:build api || all

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// newPlcTestModule creates an api.Module wired to an in-memory ChannelBus
// with the PLC KV buckets pre-created. Returns the module and a router.
func newPlcTestModule(t *testing.T) (*Module, http.Handler) {
	t.Helper()
	b := bus.NewChannelBus()
	for _, bucket := range []string{topics.BucketPlcConfig, topics.BucketPlcPrograms} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			t.Fatalf("KVCreate %s: %v", bucket, err)
		}
	}
	m := New("api-test")
	m.bus = b
	return m, m.routes()
}

func doJSON(t *testing.T, h http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestPlcConfig_GetMissingReturns404(t *testing.T) {
	_, h := newPlcTestModule(t)
	rr := doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/config", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPlcConfig_PutThenGetRoundTrip(t *testing.T) {
	_, h := newPlcTestModule(t)

	cfg := itypes.PlcConfigKV{
		PlcID:     "plc",
		Devices:   map[string]itypes.PlcDeviceConfigKV{},
		Variables: map[string]itypes.PlcVariableConfigKV{},
		Tasks:     map[string]itypes.PlcTaskConfigKV{},
	}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/config", cfg)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT config: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET config: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var got itypes.PlcConfigKV
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PlcID != "plc" {
		t.Fatalf("expected plcId=plc, got %q", got.PlcID)
	}
	if got.UpdatedAt == 0 {
		t.Fatalf("expected UpdatedAt to be stamped on PUT")
	}
}

func TestPlcTasks_PutAndDelete(t *testing.T) {
	_, h := newPlcTestModule(t)

	task := itypes.PlcTaskConfigKV{
		Name:        "MainTask",
		Description: "primary scan loop",
		ScanRateMs:  100,
		ProgramRef:  "main",
		Enabled:     true,
	}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/tasks/MainTask", task)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT task: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/tasks", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET tasks: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var tasks map[string]itypes.PlcTaskConfigKV
	if err := json.NewDecoder(rr.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := tasks["MainTask"]; !ok {
		t.Fatalf("expected MainTask in tasks, got %v", tasks)
	}

	rr = doJSON(t, h, http.MethodDelete, "/api/v1/plcs/plc/tasks/MainTask", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE task: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/tasks", nil)
	var afterDelete map[string]itypes.PlcTaskConfigKV
	if err := json.NewDecoder(rr.Body).Decode(&afterDelete); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := afterDelete["MainTask"]; ok {
		t.Fatalf("MainTask should be deleted, got %v", afterDelete)
	}
}

func TestPlcTasks_RejectsInvalidScanRate(t *testing.T) {
	_, h := newPlcTestModule(t)
	bad := itypes.PlcTaskConfigKV{Name: "x", ScanRateMs: 0, ProgramRef: "main"}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/tasks/x", bad)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlcTasks_RejectsMissingProgramRef(t *testing.T) {
	_, h := newPlcTestModule(t)
	bad := itypes.PlcTaskConfigKV{Name: "x", ScanRateMs: 100}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/tasks/x", bad)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlcVariables_PutAndDelete(t *testing.T) {
	_, h := newPlcTestModule(t)

	v := itypes.PlcVariableConfigKV{
		ID:        "tankLevel",
		Datatype:  "number",
		Direction: "internal",
		Default:   0,
	}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/variables/tankLevel", v)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT var: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/variables", nil)
	var vars map[string]itypes.PlcVariableConfigKV
	if err := json.NewDecoder(rr.Body).Decode(&vars); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := vars["tankLevel"]; !ok {
		t.Fatalf("expected tankLevel, got %v", vars)
	}

	rr = doJSON(t, h, http.MethodDelete, "/api/v1/plcs/plc/variables/tankLevel", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE var: expected 200, got %d", rr.Code)
	}
}

func TestPlcVariables_RequiresDatatypeAndDirection(t *testing.T) {
	_, h := newPlcTestModule(t)
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/variables/x", itypes.PlcVariableConfigKV{})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlcPrograms_PutGetListDelete(t *testing.T) {
	_, h := newPlcTestModule(t)

	prog := itypes.PlcProgramKV{
		Name:     "main",
		Language: "starlark",
		Source:   "x = 1\n",
	}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/programs/main", prog)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT program: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// PUT must stamp UpdatedAt and UpdatedBy.
	var saved itypes.PlcProgramKV
	if err := json.NewDecoder(rr.Body).Decode(&saved); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if saved.UpdatedAt == 0 {
		t.Fatalf("expected UpdatedAt stamped")
	}
	if saved.UpdatedBy != "api" {
		t.Fatalf("expected UpdatedBy=api, got %q", saved.UpdatedBy)
	}

	// GET single program returns full source.
	rr = doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/programs/main", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET program: expected 200, got %d", rr.Code)
	}
	var got itypes.PlcProgramKV
	json.NewDecoder(rr.Body).Decode(&got)
	if got.Source != "x = 1\n" {
		t.Fatalf("expected source preserved, got %q", got.Source)
	}

	// LIST omits source.
	rr = doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/programs", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("LIST programs: expected 200, got %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "x = 1") {
		t.Fatalf("LIST response should omit source: %s", rr.Body.String())
	}

	// DELETE.
	rr = doJSON(t, h, http.MethodDelete, "/api/v1/plcs/plc/programs/main", nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE program: expected 204, got %d", rr.Code)
	}
	rr = doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/programs/main", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: expected 404, got %d", rr.Code)
	}
}

func TestPlcPrograms_RejectsInvalidLanguage(t *testing.T) {
	_, h := newPlcTestModule(t)
	bad := itypes.PlcProgramKV{Name: "x", Language: "python", Source: "print('x')"}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/programs/x", bad)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPlcPrograms_RejectsEmptySource(t *testing.T) {
	_, h := newPlcTestModule(t)
	bad := itypes.PlcProgramKV{Name: "x", Language: "starlark", Source: ""}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/programs/x", bad)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
