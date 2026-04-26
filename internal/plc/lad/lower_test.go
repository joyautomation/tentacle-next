//go:build plc || all

package lad

import (
	"strings"
	"testing"

	"github.com/joyautomation/tentacle/internal/plc/ir"
)

type stubHost struct {
	globals map[string]ir.Value
	now     int64
}

func newStubHost() *stubHost { return &stubHost{globals: map[string]ir.Value{}} }

func (h *stubHost) ReadGlobal(name string) (ir.Value, error)  { return h.globals[name], nil }
func (h *stubHost) WriteGlobal(name string, v ir.Value) error { h.globals[name] = v; return nil }
func (h *stubHost) NowMs() int64                              { return h.now }

func compile(t *testing.T, src string, userFBs ...map[string]*ir.FBDef) *ir.Program {
	t.Helper()
	diag, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	prog, err := Lower(diag, userFBs...)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	return prog
}

func compileExpectErr(t *testing.T, src, want string) {
	t.Helper()
	diag, err := Parse(src)
	if err == nil {
		_, err = Lower(diag)
	}
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

// TestLatchOTL_OTU exercises the canonical motor-starter rung:
// (start OR latch) AND NOT stop drives both the latch coil (OTL) and a
// motor coil (OTE). A second rung uses OTU on a master-stop release.
func TestLatchOTL_OTU(t *testing.T) {
	src := `{
		"name": "motor",
		"variables": [
			{"name": "start",  "type": "BOOL"},
			{"name": "latch",  "type": "BOOL"},
			{"name": "stop",   "type": "BOOL"},
			{"name": "motor",  "type": "BOOL"},
			{"name": "reset",  "type": "BOOL"}
		],
		"rungs": [
			{
				"comment": "Latch start, gate with NC stop",
				"logic": {"kind": "series", "items": [
					{"kind": "parallel", "items": [
						{"kind": "contact", "form": "NO", "operand": "start"},
						{"kind": "contact", "form": "NO", "operand": "latch"}
					]},
					{"kind": "contact", "form": "NC", "operand": "stop"}
				]},
				"outputs": [
					{"kind": "coil", "form": "OTL", "operand": "latch"},
					{"kind": "coil", "form": "OTE", "operand": "motor"}
				]
			},
			{
				"comment": "Manual reset clears the latch",
				"logic": {"kind": "contact", "form": "NO", "operand": "reset"},
				"outputs": [
					{"kind": "coil", "form": "OTU", "operand": "latch"}
				]
			}
		]
	}`
	prog := compile(t, src)
	frame := ir.NewFrame(prog)
	host := newStubHost()
	startIdx := prog.SlotIndex["start"]
	stopIdx := prog.SlotIndex["stop"]
	latchIdx := prog.SlotIndex["latch"]
	motorIdx := prog.SlotIndex["motor"]
	resetIdx := prog.SlotIndex["reset"]

	// Press start: latch latches, motor energises.
	frame.Slots[startIdx] = ir.BoolVal(true)
	if err := ir.Run(prog, frame, host); err != nil {
		t.Fatal(err)
	}
	if !frame.Slots[latchIdx].B {
		t.Fatalf("latch should be true after start press")
	}
	if !frame.Slots[motorIdx].B {
		t.Fatalf("motor should be true after start press")
	}
	// Release start: latch holds, motor stays on.
	frame.Slots[startIdx] = ir.BoolVal(false)
	if err := ir.Run(prog, frame, host); err != nil {
		t.Fatal(err)
	}
	if !frame.Slots[latchIdx].B || !frame.Slots[motorIdx].B {
		t.Fatalf("latch should hold and motor stay on after start release")
	}
	// Press stop: rung-1 power flow drops, motor de-energises (OTE) but
	// the latch remains set (OTL only sets, never clears).
	frame.Slots[stopIdx] = ir.BoolVal(true)
	if err := ir.Run(prog, frame, host); err != nil {
		t.Fatal(err)
	}
	if !frame.Slots[latchIdx].B {
		t.Fatalf("OTL latch should not clear from stop alone")
	}
	if frame.Slots[motorIdx].B {
		t.Fatalf("motor should be off while stop is held")
	}
	// Press reset to clear the latch via OTU.
	frame.Slots[resetIdx] = ir.BoolVal(true)
	if err := ir.Run(prog, frame, host); err != nil {
		t.Fatal(err)
	}
	if frame.Slots[latchIdx].B {
		t.Fatalf("OTU should clear the latch on reset")
	}
}

// TestSeriesParallelNesting validates AND/OR folds at multiple levels:
// (a AND b) OR (c AND d AND e) drives a coil; truth-table sampling
// covers each productive path.
func TestSeriesParallelNesting(t *testing.T) {
	src := `{
		"variables": [
			{"name": "a", "type": "BOOL"},
			{"name": "b", "type": "BOOL"},
			{"name": "c", "type": "BOOL"},
			{"name": "d", "type": "BOOL"},
			{"name": "e", "type": "BOOL"},
			{"name": "out", "type": "BOOL"}
		],
		"rungs": [{
			"logic": {"kind": "parallel", "items": [
				{"kind": "series", "items": [
					{"kind": "contact", "form": "NO", "operand": "a"},
					{"kind": "contact", "form": "NO", "operand": "b"}
				]},
				{"kind": "series", "items": [
					{"kind": "contact", "form": "NO", "operand": "c"},
					{"kind": "contact", "form": "NO", "operand": "d"},
					{"kind": "contact", "form": "NO", "operand": "e"}
				]}
			]},
			"outputs": [{"kind": "coil", "form": "OTE", "operand": "out"}]
		}]
	}`
	prog := compile(t, src)
	cases := []struct {
		a, b, c, d, e, want bool
	}{
		{false, false, false, false, false, false},
		{true, false, false, false, false, false},
		{true, true, false, false, false, true},
		{false, false, true, true, false, false},
		{false, false, true, true, true, true},
		{true, true, true, true, true, true},
	}
	for _, tc := range cases {
		frame := ir.NewFrame(prog)
		frame.Slots[prog.SlotIndex["a"]] = ir.BoolVal(tc.a)
		frame.Slots[prog.SlotIndex["b"]] = ir.BoolVal(tc.b)
		frame.Slots[prog.SlotIndex["c"]] = ir.BoolVal(tc.c)
		frame.Slots[prog.SlotIndex["d"]] = ir.BoolVal(tc.d)
		frame.Slots[prog.SlotIndex["e"]] = ir.BoolVal(tc.e)
		if err := ir.Run(prog, frame, newStubHost()); err != nil {
			t.Fatal(err)
		}
		if got := frame.Slots[prog.SlotIndex["out"]].B; got != tc.want {
			t.Errorf("inputs=%v: out=%v, want %v", tc, got, tc.want)
		}
	}
}

// TestTONFromLAD wires the rung's power flow into TON.IN and reads the
// timer's Q output via a contact in a downstream rung.
func TestTONFromLAD(t *testing.T) {
	src := `{
		"variables": [
			{"name": "run",   "type": "BOOL"},
			{"name": "t1",    "type": "TON"},
			{"name": "ready", "type": "BOOL"}
		],
		"rungs": [
			{
				"logic": {"kind": "contact", "form": "NO", "operand": "run"},
				"outputs": [{"kind": "fb", "instance": "t1",
					"inputs": {"PT": {"kind": "time", "raw": "100ms"}}}]
			},
			{
				"logic": {"kind": "contact", "form": "NO", "operand": "t1.Q"},
				"outputs": [{"kind": "coil", "form": "OTE", "operand": "ready"}]
			}
		]
	}`
	prog := compile(t, src)
	frame := ir.NewFrame(prog)
	host := newStubHost()
	runIdx := prog.SlotIndex["run"]
	readyIdx := prog.SlotIndex["ready"]

	frame.Slots[runIdx] = ir.BoolVal(true)
	host.now = 0
	if err := ir.Run(prog, frame, host); err != nil {
		t.Fatal(err)
	}
	if frame.Slots[readyIdx].B {
		t.Fatalf("ready should still be false at t=0")
	}
	host.now = 50
	if err := ir.Run(prog, frame, host); err != nil {
		t.Fatal(err)
	}
	if frame.Slots[readyIdx].B {
		t.Fatalf("ready should still be false at t=50ms")
	}
	host.now = 150
	if err := ir.Run(prog, frame, host); err != nil {
		t.Fatal(err)
	}
	if !frame.Slots[readyIdx].B {
		t.Fatalf("ready should be true after PT=100ms elapses")
	}
	// Drop run: TON.Q clears, ready de-energises (OTE follows).
	frame.Slots[runIdx] = ir.BoolVal(false)
	if err := ir.Run(prog, frame, host); err != nil {
		t.Fatal(err)
	}
	if frame.Slots[readyIdx].B {
		t.Fatalf("ready should clear when run drops")
	}
}

// TestUserFBFromLAD calls a user-defined FB declared (mocked here) in
// the shared registry. Engine-level tests cover the cross-language
// integration; this one verifies the Lower path resolves a non-builtin
// FB type via the resolver argument.
func TestUserFBFromLAD(t *testing.T) {
	// Synthesize a user-defined FB whose Step doubles X into Y.
	doubler := &ir.FBDef{
		Name:      "Doubler",
		Inputs:    []ir.FBSlot{{Name: "EN", Type: ir.BoolT}, {Name: "X", Type: ir.IntT}},
		Outputs:   []ir.FBSlot{{Name: "Y", Type: ir.IntT}},
		SlotIndex: map[string]int{"EN": 0, "X": 1, "Y": 2},
		Step: func(inst *ir.FBInstance, _ ir.FBStepCtx) error {
			if inst.Slots[0].B {
				inst.Slots[2] = ir.IntVal(inst.Slots[1].I * 2)
			}
			return nil
		},
	}
	registry := map[string]*ir.FBDef{"Doubler": doubler}

	src := `{
		"variables": [
			{"name": "go",     "type": "BOOL"},
			{"name": "d",      "type": "Doubler"},
			{"name": "input",  "type": "INT", "init": "7"},
			{"name": "result", "type": "INT"}
		],
		"rungs": [
			{
				"logic": {"kind": "contact", "form": "NO", "operand": "go"},
				"outputs": [{"kind": "fb", "instance": "d",
					"powerInput": "EN",
					"inputs": {"X": {"kind": "ref", "name": "input"}}}]
			}
		]
	}`
	prog := compile(t, src, registry)
	frame := ir.NewFrame(prog)
	frame.Slots[prog.SlotIndex["go"]] = ir.BoolVal(true)
	if err := ir.Run(prog, frame, newStubHost()); err != nil {
		t.Fatal(err)
	}
	d := frame.Slots[prog.SlotIndex["d"]].FB
	if got := d.Slots[2].I; got != 14 {
		t.Errorf("Doubler.Y = %d, want 14", got)
	}
	// LAD doesn't yet have a way to assign FB outputs back to a coil, but
	// reading via member access in a downstream rung's contact would let
	// the result flow through the coil network. Verify the value is
	// observable through resolveOperand by re-using the same diagram with
	// an extra rung — sanity-check covered by TestTONFromLAD already, so
	// this test focuses on the lowering path resolving the user FB.
	_ = prog.SlotIndex["result"]
}

// TestUndeclaredOperand surfaces a clear error when a contact references
// a variable that wasn't declared.
func TestUndeclaredOperand(t *testing.T) {
	src := `{
		"variables": [{"name": "out", "type": "BOOL"}],
		"rungs": [{
			"logic": {"kind": "contact", "form": "NO", "operand": "ghost"},
			"outputs": [{"kind": "coil", "form": "OTE", "operand": "out"}]
		}]
	}`
	compileExpectErr(t, src, `undeclared identifier "ghost"`)
}

// TestNonBoolContact rejects a contact whose operand isn't BOOL.
func TestNonBoolContact(t *testing.T) {
	src := `{
		"variables": [
			{"name": "n", "type": "INT"},
			{"name": "out", "type": "BOOL"}
		],
		"rungs": [{
			"logic": {"kind": "contact", "form": "NO", "operand": "n"},
			"outputs": [{"kind": "coil", "form": "OTE", "operand": "out"}]
		}]
	}`
	compileExpectErr(t, src, "must be BOOL")
}

// TestCoilTypeMismatch rejects coiling onto a non-BOOL operand.
func TestCoilTypeMismatch(t *testing.T) {
	src := `{
		"variables": [
			{"name": "x", "type": "BOOL"},
			{"name": "n", "type": "INT"}
		],
		"rungs": [{
			"logic": {"kind": "contact", "form": "NO", "operand": "x"},
			"outputs": [{"kind": "coil", "form": "OTE", "operand": "n"}]
		}]
	}`
	compileExpectErr(t, src, "must target BOOL")
}
