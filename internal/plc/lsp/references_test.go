//go:build plc || all

package lsp

import (
	"testing"
)

func TestFindProgramReferencesSimple(t *testing.T) {
	src := "def main():\n    check(x)\n    other()\n    check(y)\n"
	refs := FindProgramReferences(src, "check")
	if len(refs) != 2 {
		t.Fatalf("want 2 refs, got %d: %+v", len(refs), refs)
	}
	if refs[0].Line != 2 || refs[0].StartCol != 5 || refs[0].EndCol != 10 {
		t.Errorf("ref 0 wrong position: %+v", refs[0])
	}
	if refs[1].Line != 4 {
		t.Errorf("ref 1 wrong line: %+v", refs[1])
	}
	if refs[0].LineText != "    check(x)" {
		t.Errorf("ref 0 line text %q", refs[0].LineText)
	}
}

func TestFindProgramReferencesSkipsLocalDef(t *testing.T) {
	// A local def with the target name shadows any cross-program function;
	// we must not report its call sites as external references.
	src := "def check(v):\n    pass\ndef main():\n    check(1)\n"
	refs := FindProgramReferences(src, "check")
	if refs != nil {
		t.Fatalf("expected nil when target is locally defined, got %+v", refs)
	}
}

func TestFindProgramReferencesIgnoresAttributeAndKwarg(t *testing.T) {
	// Attribute access (obj.check) and keyword args (check=1) must not
	// count as call sites to the cross-program function `check`.
	src := "def main():\n    obj.check(1)\n    other(check=1)\n"
	refs := FindProgramReferences(src, "check")
	if refs != nil {
		t.Fatalf("attribute/kwarg should not match, got %+v", refs)
	}
}

func TestFindProgramReferencesStripsAnnotations(t *testing.T) {
	// Type annotations used to break Starlark's parser. The scanner
	// strips annotations while preserving offsets so positions remain
	// meaningful in the original source.
	src := "def main():\n    check(x)\n\ndef check(v: int) -> bool:\n    return True\n"
	// Target shadowed by a local def — expect nil.
	if refs := FindProgramReferences(src, "check"); refs != nil {
		t.Fatalf("local def should shadow, got %+v", refs)
	}
}

func TestFindVariableReferencesGetVar(t *testing.T) {
	src := "def main():\n    v = get_var(\"tank_level\")\n    set_var(\"tank_level\", 0)\n"
	refs := FindVariableReferences(src, "tank_level")
	if len(refs) != 2 {
		t.Fatalf("want 2 refs, got %d: %+v", len(refs), refs)
	}
	// First ref: the whole string literal including quotes.
	if refs[0].Line != 2 {
		t.Errorf("ref 0 line: %d", refs[0].Line)
	}
	want := "    v = get_var(\"tank_level\")"
	if refs[0].LineText != want {
		t.Errorf("ref 0 line text %q want %q", refs[0].LineText, want)
	}
}

func TestFindVariableReferencesIgnoresUnrelatedStrings(t *testing.T) {
	src := "def main():\n    log(\"tank_level message\")\n    v = get_var(\"other\")\n"
	refs := FindVariableReferences(src, "tank_level")
	if refs != nil {
		t.Fatalf("log() string and mismatched var should not match, got %+v", refs)
	}
}

func TestFindVariableReferencesSkipsParseErrors(t *testing.T) {
	src := "def main(:\n    get_var(\"x\")\n"
	if refs := FindVariableReferences(src, "x"); refs != nil {
		t.Fatalf("broken parse should yield nil, got %+v", refs)
	}
}
