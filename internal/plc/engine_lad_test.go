//go:build plc || all

package plc

import (
	"log/slog"
	"testing"
)

// TestEngineLAD_BasicLatch compiles a LAD program through the engine and
// drives a global through the IR host: pressing start latches motor on,
// pressing stop drops it.
func TestEngineLAD_BasicLatch(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeBool(vs, "start", false)
	addRuntimeBool(vs, "stop", false)
	addRuntimeBool(vs, "motor", false)

	src := `{
		"name": "motor",
		"variables": [
			{"name": "start", "type": "BOOL", "kind": "global"},
			{"name": "stop",  "type": "BOOL", "kind": "global"},
			{"name": "motor", "type": "BOOL", "kind": "global"},
			{"name": "latch", "type": "BOOL"}
		],
		"rungs": [{
			"logic": {"kind": "series", "items": [
				{"kind": "parallel", "items": [
					{"kind": "contact", "form": "NO", "operand": "start"},
					{"kind": "contact", "form": "NO", "operand": "latch"}
				]},
				{"kind": "contact", "form": "NC", "operand": "stop"}
			]},
			"outputs": [
				{"kind": "coil", "form": "OTE", "operand": "latch"},
				{"kind": "coil", "form": "OTE", "operand": "motor"}
			]
		}]
	}`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileLAD("motor", src); err != nil {
		t.Fatalf("CompileLAD: %v", err)
	}
	state := NewTaskState()

	// Press start → motor on.
	vs.Set("start", true, 0)
	if err := eng.Execute("motor", "", state); err != nil {
		t.Fatal(err)
	}
	if !vs.GetBool("motor") {
		t.Fatalf("motor should be on after start press")
	}
	// Release start, latch holds motor.
	vs.Set("start", false, 0)
	if err := eng.Execute("motor", "", state); err != nil {
		t.Fatal(err)
	}
	if !vs.GetBool("motor") {
		t.Fatalf("motor should still be on (latched)")
	}
	// Press stop → motor off.
	vs.Set("stop", true, 0)
	if err := eng.Execute("motor", "", state); err != nil {
		t.Fatal(err)
	}
	if vs.GetBool("motor") {
		t.Fatalf("motor should be off after stop")
	}
}

// TestEngineLAD_CallsSTUserFB defines a FUNCTION_BLOCK in ST and calls
// it from a LAD program, exercising the unified user-FB registry.
func TestEngineLAD_CallsSTUserFB(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeBool(vs, "go", false)
	addRuntimeNumber(vs, "input", 0)
	addRuntimeNumber(vs, "result", 0)

	libSrc := `
FUNCTION_BLOCK Doubler
VAR_INPUT
  EN : BOOL;
  X  : INT;
END_VAR
VAR_OUTPUT
  Y : INT;
END_VAR
  IF EN THEN
    Y := X * 2;
  END_IF;
END_FUNCTION_BLOCK
`
	ladSrc := `{
		"variables": [
			{"name": "go",     "type": "BOOL", "kind": "global"},
			{"name": "input",  "type": "INT",  "kind": "global"},
			{"name": "result", "type": "INT",  "kind": "global"},
			{"name": "d",      "type": "Doubler"},
			{"name": "tmp",    "type": "INT"}
		],
		"rungs": [{
			"logic": {"kind": "contact", "form": "NO", "operand": "go"},
			"outputs": [{
				"kind": "fb", "instance": "d", "powerInput": "EN",
				"inputs": {"X": {"kind": "ref", "name": "input"}}
			}]
		}]
	}`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("lib", libSrc); err != nil {
		t.Fatalf("CompileST lib: %v", err)
	}
	if err := eng.CompileLAD("ladprog", ladSrc); err != nil {
		t.Fatalf("CompileLAD: %v", err)
	}

	vs.Set("go", true, 0)
	vs.Set("input", float64(9), 0)
	state := NewTaskState()
	if err := eng.Execute("ladprog", "", state); err != nil {
		t.Fatalf("execute lad: %v", err)
	}
	// The FB instance is internal to the LAD frame; assert through the
	// frame directly.
	if state.irFrame == nil {
		t.Fatal("irFrame not initialised")
	}
	prog := eng.ladPrograms["ladprog"]
	dSlot := prog.SlotIndex["d"]
	d := state.irFrame.Slots[dSlot].FB
	if d == nil {
		t.Fatal("doubler instance not allocated")
	}
	// Doubler output Y is at slot index 2 (EN, X, Y).
	if got := d.Slots[2].I; got != 18 {
		t.Errorf("d.Y = %d, want 18", got)
	}
}

// TestEngineLAD_CrossLanguageGlobal: ST writes a global, LAD reads it.
// Demonstrates that ST and LAD coordinate through the shared variable
// store, satisfying the smoke checklist's cross-language item.
func TestEngineLAD_CrossLanguageGlobal(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeBool(vs, "trigger", false)
	addRuntimeBool(vs, "out", false)

	stSrc := `
PROGRAM writer
VAR_GLOBAL
  trigger : BOOL;
END_VAR
  trigger := TRUE;
END_PROGRAM
`
	ladSrc := `{
		"variables": [
			{"name": "trigger", "type": "BOOL", "kind": "global"},
			{"name": "out",     "type": "BOOL", "kind": "global"}
		],
		"rungs": [{
			"logic": {"kind": "contact", "form": "NO", "operand": "trigger"},
			"outputs": [{"kind": "coil", "form": "OTE", "operand": "out"}]
		}]
	}`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("writer", stSrc); err != nil {
		t.Fatalf("CompileST: %v", err)
	}
	if err := eng.CompileLAD("reader", ladSrc); err != nil {
		t.Fatalf("CompileLAD: %v", err)
	}
	if err := eng.Execute("writer", "", NewTaskState()); err != nil {
		t.Fatalf("execute writer: %v", err)
	}
	if err := eng.Execute("reader", "", NewTaskState()); err != nil {
		t.Fatalf("execute reader: %v", err)
	}
	if !vs.GetBool("out") {
		t.Fatalf("out should be true after ST write + LAD read")
	}
}
