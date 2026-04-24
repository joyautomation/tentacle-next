//go:build plc || all

package plc

import (
	"log/slog"
	"testing"
)

// addRuntimeNumber registers a numeric variable on the store so an ST
// program's VAR_GLOBAL reference can find it. Mirrors what applyConfig
// does at startup, just without the Bus plumbing.
func addRuntimeNumber(vs *VariableStore, id string, val float64) {
	vs.Add(&RuntimeVariable{
		ID:        id,
		Datatype:  "number",
		Direction: "internal",
		Value:     val,
	})
}

func addRuntimeBool(vs *VariableStore, id string, val bool) {
	vs.Add(&RuntimeVariable{
		ID:        id,
		Datatype:  "boolean",
		Direction: "internal",
		Value:     val,
	})
}

func TestEngineExecutesST(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "input_a", 7)
	addRuntimeNumber(vs, "input_b", 3)
	addRuntimeNumber(vs, "result", 0)

	src := `
PROGRAM mathprog
VAR_GLOBAL
    input_a : INT;
    input_b : INT;
    result : INT;
END_VAR
result := input_a + input_b;
END_PROGRAM`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("mathprog", src); err != nil {
		t.Fatalf("CompileST: %v", err)
	}
	if !eng.HasProgram("mathprog") {
		t.Fatalf("HasProgram returned false for ST program")
	}

	state := NewTaskState()
	if err := eng.Execute("mathprog", "", state); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := vs.GetNumber("result"); got != 10 {
		t.Errorf("result = %v, want 10", got)
	}
}

func TestEngineSTRetainsAcrossScans(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "counter", 0)

	src := `
PROGRAM accum
VAR RETAIN
    n : INT := 0;
END_VAR
VAR_GLOBAL
    counter : INT;
END_VAR
n := n + 1;
counter := n;
END_PROGRAM`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("accum", src); err != nil {
		t.Fatalf("CompileST: %v", err)
	}

	state := NewTaskState()
	for i := 0; i < 5; i++ {
		if err := eng.Execute("accum", "", state); err != nil {
			t.Fatalf("scan %d: %v", i, err)
		}
	}
	if got := vs.GetNumber("counter"); got != 5 {
		t.Errorf("counter = %v after 5 scans, want 5", got)
	}
}

func TestEngineSTAndStarlarkCoexist(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "from_st", 0)
	addRuntimeNumber(vs, "from_starlark", 0)

	stSrc := `
PROGRAM s
VAR_GLOBAL
    from_st : INT;
END_VAR
from_st := 42;
END_PROGRAM`

	starlarkSrc := `
def main():
    set_var("from_starlark", 99)
`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("s", stSrc); err != nil {
		t.Fatalf("CompileST: %v", err)
	}
	if err := eng.Compile("k", starlarkSrc); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if err := eng.Execute("s", "", NewTaskState()); err != nil {
		t.Fatalf("execute st: %v", err)
	}
	if err := eng.Execute("k", "main", NewTaskState()); err != nil {
		t.Fatalf("execute starlark: %v", err)
	}

	if got := vs.GetNumber("from_st"); got != 42 {
		t.Errorf("from_st = %v, want 42", got)
	}
	if got := vs.GetNumber("from_starlark"); got != 99 {
		t.Errorf("from_starlark = %v, want 99", got)
	}
}

func TestEngineSwitchLanguageDropsOldEngine(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "x", 0)

	starlarkSrc := `
def main():
    set_var("x", 1)
`
	stSrc := `
PROGRAM p
VAR_GLOBAL
    x : INT;
END_VAR
x := 2;
END_PROGRAM`

	eng := NewEngine(vs, slog.Default())
	if err := eng.Compile("p", starlarkSrc); err != nil {
		t.Fatalf("compile starlark: %v", err)
	}
	state := NewTaskState()
	if err := eng.Execute("p", "main", state); err != nil {
		t.Fatalf("starlark execute: %v", err)
	}
	if got := vs.GetNumber("x"); got != 1 {
		t.Errorf("x after starlark = %v, want 1", got)
	}

	// Recompile the same name as ST — the Starlark version should be evicted.
	if err := eng.CompileST("p", stSrc); err != nil {
		t.Fatalf("compile st: %v", err)
	}
	stState := NewTaskState()
	if err := eng.Execute("p", "", stState); err != nil {
		t.Fatalf("st execute: %v", err)
	}
	if got := vs.GetNumber("x"); got != 2 {
		t.Errorf("x after st = %v, want 2", got)
	}
}

func TestEngineSTCompileError(t *testing.T) {
	vs := NewVariableStore()
	eng := NewEngine(vs, slog.Default())
	// Undeclared identifier should error during Lower.
	src := `
PROGRAM bad
nope := 1;
END_PROGRAM`
	if err := eng.CompileST("bad", src); err == nil {
		t.Fatalf("expected compile error, got nil")
	}
	if eng.HasProgram("bad") {
		t.Errorf("failed compile should not register program")
	}
}

func TestEngineSTBoolAndArith(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeBool(vs, "enabled", true)
	addRuntimeBool(vs, "fault", false)
	addRuntimeBool(vs, "motor_run", false)

	src := `
PROGRAM motor
VAR_GLOBAL
    enabled : BOOL;
    fault : BOOL;
    motor_run : BOOL;
END_VAR
motor_run := enabled AND NOT fault;
END_PROGRAM`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("motor", src); err != nil {
		t.Fatalf("CompileST: %v", err)
	}
	if err := eng.Execute("motor", "", NewTaskState()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := vs.GetBool("motor_run"); !got {
		t.Errorf("motor_run = %v, want true", got)
	}
}
