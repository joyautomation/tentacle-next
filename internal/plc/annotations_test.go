//go:build plc || all

package plc

import (
	"strings"
	"testing"

	"go.starlark.net/syntax"
)

func TestStripAnnotations_Simple(t *testing.T) {
	src := "def check(thing: str) -> bool:\n    return True\n"
	stripped, sigs := StripAnnotations(src)
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signature, got %d", len(sigs))
	}
	sig := sigs[0]
	if sig.Name != "check" {
		t.Errorf("name = %q, want %q", sig.Name, "check")
	}
	if len(sig.Params) != 1 || sig.Params[0].Name != "thing" || sig.Params[0].Type != "str" {
		t.Errorf("params = %+v", sig.Params)
	}
	if !sig.HasReturn || sig.ReturnType != "bool" {
		t.Errorf("return = %q (has=%v)", sig.ReturnType, sig.HasReturn)
	}
	// Stripped source must be byte-length identical and parse as Starlark.
	if len(stripped) != len(src) {
		t.Fatalf("stripped length %d != original %d", len(stripped), len(src))
	}
	if _, err := syntax.Parse("x.star", stripped, 0); err != nil {
		t.Fatalf("stripped source failed to parse: %v\n--- source ---\n%s", err, stripped)
	}
}

func TestStripAnnotations_MultipleParams(t *testing.T) {
	src := `def foo(a: int, b: str = "hi", c=3):
    return a
`
	stripped, sigs := StripAnnotations(src)
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signature, got %d", len(sigs))
	}
	p := sigs[0].Params
	if len(p) != 3 {
		t.Fatalf("expected 3 params, got %d: %+v", len(p), p)
	}
	if p[0].Name != "a" || p[0].Type != "int" {
		t.Errorf("p[0] = %+v", p[0])
	}
	if p[1].Name != "b" || p[1].Type != "str" || p[1].Default != `"hi"` {
		t.Errorf("p[1] = %+v", p[1])
	}
	if p[2].Name != "c" || p[2].HasType || p[2].Default != "3" {
		t.Errorf("p[2] = %+v", p[2])
	}
	if _, err := syntax.Parse("x.star", stripped, 0); err != nil {
		t.Fatalf("stripped parse failed: %v", err)
	}
}

func TestStripAnnotations_NoAnnotations(t *testing.T) {
	src := "def plain(a, b):\n    return a\n"
	stripped, sigs := StripAnnotations(src)
	if stripped != src {
		t.Errorf("expected identical source, got:\n%s", stripped)
	}
	if len(sigs) != 1 || len(sigs[0].Params) != 2 {
		t.Errorf("sigs = %+v", sigs)
	}
	if sigs[0].Params[0].HasType || sigs[0].Params[1].HasType {
		t.Errorf("expected no types, got %+v", sigs[0].Params)
	}
}

func TestStripAnnotations_PreservesColumns(t *testing.T) {
	// The error token column should survive stripping so LSP diagnostics on
	// the stripped source map to the user's original caret positions.
	src := "def f(x: int) -> bool:\n    badtoken!!\n"
	stripped, _ := StripAnnotations(src)
	if len(stripped) != len(src) {
		t.Fatalf("length changed")
	}
	// Every newline must be at the same position.
	for i, c := range []byte(src) {
		if c == '\n' && stripped[i] != '\n' {
			t.Fatalf("newline at %d lost", i)
		}
	}
}

func TestStripAnnotations_Multiline(t *testing.T) {
	src := `def foo(
    a: int,
    b: str,
) -> bool:
    return True
`
	stripped, sigs := StripAnnotations(src)
	if _, err := syntax.Parse("x.star", stripped, 0); err != nil {
		t.Fatalf("parse failed: %v\n%s", err, stripped)
	}
	if len(sigs) != 1 || len(sigs[0].Params) != 2 {
		t.Fatalf("sigs = %+v", sigs)
	}
	if sigs[0].ReturnType != "bool" {
		t.Errorf("return = %q", sigs[0].ReturnType)
	}
}

func TestStripAnnotations_Variadic(t *testing.T) {
	src := "def f(*args: int, **kwargs: str) -> None:\n    pass\n"
	stripped, sigs := StripAnnotations(src)
	if _, err := syntax.Parse("x.star", stripped, 0); err != nil {
		t.Fatalf("parse failed: %v\n%s", err, stripped)
	}
	if len(sigs[0].Params) != 2 {
		t.Fatalf("params = %+v", sigs[0].Params)
	}
	if !sigs[0].Params[0].Variadic || sigs[0].Params[0].Name != "args" {
		t.Errorf("p[0] = %+v", sigs[0].Params[0])
	}
	if !sigs[0].Params[1].Keyword || sigs[0].Params[1].Name != "kwargs" {
		t.Errorf("p[1] = %+v", sigs[0].Params[1])
	}
}

func TestStripAnnotations_NestedDef(t *testing.T) {
	src := `def outer(a: int):
    def inner(b: str) -> bool:
        return True
    return inner
`
	stripped, sigs := StripAnnotations(src)
	if _, err := syntax.Parse("x.star", stripped, 0); err != nil {
		t.Fatalf("parse failed: %v\n%s", err, stripped)
	}
	if len(sigs) != 2 {
		t.Fatalf("expected 2 sigs (outer+inner), got %d", len(sigs))
	}
}

func TestStripAnnotations_IgnoresDefInString(t *testing.T) {
	src := `x = "def foo(a: int):"
def real(a: int):
    return a
`
	_, sigs := StripAnnotations(src)
	if len(sigs) != 1 || sigs[0].Name != "real" {
		t.Fatalf("expected only `real`, got %+v", sigs)
	}
}

func TestStripAnnotations_IgnoresDefInComment(t *testing.T) {
	src := `# def fake(a: int):
def real(a: int):
    return a
`
	_, sigs := StripAnnotations(src)
	if len(sigs) != 1 || sigs[0].Name != "real" {
		t.Fatalf("expected only `real`, got %+v", sigs)
	}
}

func TestStripAnnotations_UnknownAnnotationTypeRoundTrips(t *testing.T) {
	// Type annotations are free-form text — we don't validate them.
	src := "def f(x: MyCustomTemplate) -> CustomReturn:\n    return x\n"
	stripped, sigs := StripAnnotations(src)
	if _, err := syntax.Parse("x.star", stripped, 0); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sigs[0].Params[0].Type != "MyCustomTemplate" {
		t.Errorf("type = %q", sigs[0].Params[0].Type)
	}
	if sigs[0].ReturnType != "CustomReturn" {
		t.Errorf("return = %q", sigs[0].ReturnType)
	}
}

func TestStripAnnotations_LeavesBodyUntouched(t *testing.T) {
	src := `def f(a: int) -> bool:
    x: int = 5  # this isn't a def header; leave it alone
    return True
`
	stripped, _ := StripAnnotations(src)
	// The line inside the body should still contain the annotation literally —
	// we only strip def headers. The parser will reject `x: int = 5` but that
	// is the user's problem (Starlark doesn't allow variable annotations
	// either; we'd need a separate decision to support them).
	if !strings.Contains(stripped, "x: int = 5") {
		t.Errorf("body annotation was incorrectly stripped:\n%s", stripped)
	}
}
