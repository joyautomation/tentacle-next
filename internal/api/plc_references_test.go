//go:build api || all

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// seedProgram PUTs a starlark program into the test module's KV.
func seedProgram(t *testing.T, h http.Handler, name, source string) {
	t.Helper()
	prog := itypes.PlcProgramKV{Name: name, Language: "starlark", Source: source}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/programs/"+name, prog)
	if rr.Code != http.StatusOK {
		t.Fatalf("seed program %s: %d %s", name, rr.Code, rr.Body.String())
	}
}

func TestPlcReferences_ProgramKindAcrossPrograms(t *testing.T) {
	_, h := newPlcTestModule(t)
	seedProgram(t, h, "check", "def check(v):\n    return v\n")
	seedProgram(t, h, "main", "def main():\n    check(1)\n    check(2)\n")
	seedProgram(t, h, "other", "def other():\n    pass\n")

	rr := doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/references?name=check&kind=program", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var sites []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &sites); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("want 2 sites (both in main), got %d: %+v", len(sites), sites)
	}
	for _, s := range sites {
		if s["program"] != "main" {
			t.Errorf("site not in main: %+v", s)
		}
		if s["source"] != "program" {
			t.Errorf("wrong source: %+v", s)
		}
	}
}

func TestPlcReferences_ProgramKindIncludesTaskRefs(t *testing.T) {
	_, h := newPlcTestModule(t)
	seedProgram(t, h, "fast", "def main():\n    pass\n")

	task := itypes.PlcTaskConfigKV{Name: "FastScan", ScanRateMs: 100, ProgramRef: "fast", EntryFn: "main", Enabled: true}
	rr := doJSON(t, h, http.MethodPut, "/api/v1/plcs/plc/tasks/FastScan", task)
	if rr.Code != http.StatusOK {
		t.Fatalf("seed task: %d %s", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/references?name=fast&kind=program", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var sites []map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &sites)
	found := false
	for _, s := range sites {
		if s["source"] == "taskProgramRef" && s["task"] == "FastScan" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected taskProgramRef site for FastScan, got %+v", sites)
	}
}

func TestPlcReferences_VariableKind(t *testing.T) {
	_, h := newPlcTestModule(t)
	seedProgram(t, h, "p1", "def main():\n    v = get_var(\"tank\")\n    set_var(\"tank\", 0)\n")
	seedProgram(t, h, "p2", "def main():\n    v = get_var(\"other\")\n")

	rr := doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/references?name=tank&kind=variable", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var sites []map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &sites)
	if len(sites) != 2 {
		t.Fatalf("want 2 sites in p1, got %d: %+v", len(sites), sites)
	}
	for _, s := range sites {
		if s["program"] != "p1" {
			t.Errorf("unexpected program: %+v", s)
		}
	}
}

func TestPlcReferences_BadKind(t *testing.T) {
	_, h := newPlcTestModule(t)
	rr := doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/references?name=x&kind=nope", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
}

func TestPlcReferences_MissingName(t *testing.T) {
	_, h := newPlcTestModule(t)
	rr := doJSON(t, h, http.MethodGet, "/api/v1/plcs/plc/references?kind=program", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
}
