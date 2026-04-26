//go:build plc || all

package plc

import (
	"log/slog"
	"strings"
	"testing"
)

// TestRunTest_RunSTBuiltin exercises the run_st test-harness builtin:
// a Starlark test sets a global, ticks an ST program, then asserts on
// the resulting value.
func TestRunTest_RunSTBuiltin(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "in", 0)
	addRuntimeNumber(vs, "out", 0)

	stSrc := `
PROGRAM doubler
VAR_GLOBAL
  in : INT;
  out : INT;
END_VAR
  out := in * 2;
END_PROGRAM
`
	testSrc := `
set_var("in", 21)
run_st("doubler")
assert_eq(get_num("out"), 42)
`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("doubler", stSrc); err != nil {
		t.Fatalf("CompileST: %v", err)
	}
	res := eng.RunTest("doubler_test", testSrc)
	if res.Status != "pass" {
		t.Fatalf("expected pass, got %q (%s)", res.Status, res.Message)
	}
}

// TestRunTest_RunSTRetainsState verifies retain semantics across
// multiple run_st calls in the same test.
func TestRunTest_RunSTRetainsState(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "counter", 0)

	stSrc := `
PROGRAM accum
VAR RETAIN
  n : INT := 0;
END_VAR
VAR_GLOBAL
  counter : INT;
END_VAR
  n := n + 1;
  counter := n;
END_PROGRAM
`
	testSrc := `
run_st("accum")
run_st("accum")
run_st("accum")
assert_eq(get_num("counter"), 3)
reset_st("accum")
run_st("accum")
assert_eq(get_num("counter"), 1)
`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("accum", stSrc); err != nil {
		t.Fatalf("CompileST: %v", err)
	}
	res := eng.RunTest("accum_test", testSrc)
	if res.Status != "pass" {
		t.Fatalf("expected pass, got %q (%s)", res.Status, res.Message)
	}
}

// TestRunTest_RunSTUnknownProgram surfaces a clear error when the
// referenced program isn't registered.
func TestRunTest_RunSTUnknownProgram(t *testing.T) {
	vs := NewVariableStore()
	eng := NewEngine(vs, slog.Default())
	res := eng.RunTest("missing_test", `run_st("nope")`)
	if res.Status != "error" {
		t.Fatalf("expected error status, got %q", res.Status)
	}
	if !strings.Contains(res.Message, "not found") {
		t.Errorf("expected 'not found' in message, got %q", res.Message)
	}
}
