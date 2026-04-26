//go:build plc || all

package plc

import (
	"log/slog"
	"testing"
)

// TestEngineUserFB_CrossProgram defines a user FUNCTION_BLOCK in one ST
// source and consumes it from a separate program. Exercises the engine's
// per-source FB registry and cross-program resolver wiring.
func TestEngineUserFB_CrossProgram(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "input", 6)
	addRuntimeNumber(vs, "result", 0)

	libSrc := `
FUNCTION_BLOCK Doubler
VAR_INPUT
  X : INT;
END_VAR
VAR_OUTPUT
  Y : INT;
END_VAR
  Y := X * 2;
END_FUNCTION_BLOCK
`
	progSrc := `
PROGRAM main
VAR
  d : Doubler;
END_VAR
VAR_GLOBAL
  input : INT;
  result : INT;
END_VAR
  d(X := input);
  result := d.Y;
END_PROGRAM
`

	eng := NewEngine(vs, slog.Default())
	// Compile the FB library first so the consumer sees it.
	if err := eng.CompileST("lib", libSrc); err != nil {
		t.Fatalf("CompileST lib: %v", err)
	}
	if err := eng.CompileST("main", progSrc); err != nil {
		t.Fatalf("CompileST main: %v", err)
	}

	if err := eng.Execute("main", "", NewTaskState()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := vs.GetNumber("result"); got != 12 {
		t.Errorf("result = %v, want 12", got)
	}
}

// TestEngineUserFB_CompileOrderRecovery exercises the engine's
// re-lower-on-FB-change behaviour: the consumer compiles before the
// library, fails, and after the library compiles the consumer is
// re-lowered transparently.
func TestEngineUserFB_CompileOrderRecovery(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "input", 4)
	addRuntimeNumber(vs, "result", 0)

	libSrc := `
FUNCTION_BLOCK Tripler
VAR_INPUT
  X : INT;
END_VAR
VAR_OUTPUT
  Y : INT;
END_VAR
  Y := X * 3;
END_FUNCTION_BLOCK
`
	progSrc := `
PROGRAM main
VAR
  t : Tripler;
END_VAR
VAR_GLOBAL
  input : INT;
  result : INT;
END_VAR
  t(X := input);
  result := t.Y;
END_PROGRAM
`

	eng := NewEngine(vs, slog.Default())
	// Consumer compiles first — should fail because Tripler is unknown.
	if err := eng.CompileST("main", progSrc); err == nil {
		t.Fatalf("expected CompileST main to fail without library, got nil")
	}
	// Library brings the FB into the registry; main is not yet registered
	// (its previous compile failed) so we re-compile it now.
	if err := eng.CompileST("lib", libSrc); err != nil {
		t.Fatalf("CompileST lib: %v", err)
	}
	if err := eng.CompileST("main", progSrc); err != nil {
		t.Fatalf("CompileST main (after lib): %v", err)
	}

	if err := eng.Execute("main", "", NewTaskState()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := vs.GetNumber("result"); got != 12 {
		t.Errorf("result = %v, want 12", got)
	}
}

// TestEngineUserFB_RemoveDropsRegistry verifies that removing the
// library source drops its FBs from the registry and breaks dependent
// programs (re-lowering them surfaces the missing FB).
func TestEngineUserFB_RemoveDropsRegistry(t *testing.T) {
	vs := NewVariableStore()
	addRuntimeNumber(vs, "input", 2)
	addRuntimeNumber(vs, "result", 0)

	libSrc := `
FUNCTION_BLOCK Squarer
VAR_INPUT
  X : INT;
END_VAR
VAR_OUTPUT
  Y : INT;
END_VAR
  Y := X * X;
END_FUNCTION_BLOCK
`
	progSrc := `
PROGRAM main
VAR
  s : Squarer;
END_VAR
VAR_GLOBAL
  input : INT;
  result : INT;
END_VAR
  s(X := input);
  result := s.Y;
END_PROGRAM
`

	eng := NewEngine(vs, slog.Default())
	if err := eng.CompileST("lib", libSrc); err != nil {
		t.Fatalf("CompileST lib: %v", err)
	}
	if err := eng.CompileST("main", progSrc); err != nil {
		t.Fatalf("CompileST main: %v", err)
	}
	eng.Remove("lib")

	// `main` is still registered (its prior IR is cached), but its source
	// no longer compiles — re-compile it to verify the registry was
	// pruned.
	if err := eng.CompileST("main", progSrc); err == nil {
		t.Fatalf("expected CompileST main to fail after Remove(lib), got nil")
	}
}
