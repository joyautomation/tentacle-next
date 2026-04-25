//go:build plc || all

package lsp

import (
	"strings"
	"testing"
)

// analyzeST should surface semantic-lowering errors (not just parse errors)
// with the source position of the offending statement, so the editor can
// underline the right line.
func TestAnalyzeSTReportsUndeclaredIdentifier(t *testing.T) {
	src := "" +
		"PROGRAM p\n" +
		"VAR_GLOBAL\n" +
		"    x : INT;\n" +
		"END_VAR\n" +
		"x := y;\n" +
		"END_PROGRAM\n"
	diags := Analyze(src, "st")
	if len(diags) == 0 {
		t.Fatalf("expected diagnostic for undeclared identifier, got none")
	}
	d := diags[0]
	// Statement `x := y;` lives on line 5 (1-based). LSP positions are
	// 0-based, so we expect Range.Start.Line == 4.
	if d.Range.Start.Line != 4 {
		t.Errorf("diagnostic line = %d, want 4 (0-based)", d.Range.Start.Line)
	}
	if !strings.Contains(strings.ToLower(d.Message), "undeclared") &&
		!strings.Contains(strings.ToLower(d.Message), "y") {
		t.Errorf("diagnostic should mention the undeclared name, got %q", d.Message)
	}
}

func TestAnalyzeSTReportsTypeMismatchInIfCondition(t *testing.T) {
	src := "" +
		"PROGRAM p\n" +
		"VAR_GLOBAL\n" +
		"    x : INT;\n" +
		"END_VAR\n" +
		"IF x THEN\n" +
		"    x := 1;\n" +
		"END_IF;\n" +
		"END_PROGRAM\n"
	diags := Analyze(src, "st")
	if len(diags) == 0 {
		t.Fatalf("expected diagnostic for non-BOOL IF condition")
	}
	if diags[0].Range.Start.Line != 4 {
		t.Errorf("diagnostic line = %d, want 4 (IF on source line 5)", diags[0].Range.Start.Line)
	}
	if !strings.Contains(diags[0].Message, "BOOL") {
		t.Errorf("expected BOOL mismatch message, got %q", diags[0].Message)
	}
}

func TestHoverSTReportsDeclaredType(t *testing.T) {
	src := "" +
		"PROGRAM p\n" +
		"VAR_GLOBAL\n" +
		"    motor : BOOL := TRUE;\n" +
		"END_VAR\n" +
		"motor := FALSE;\n" +
		"END_PROGRAM\n"
	// Cursor on `motor` in the assignment statement (line 5, char 2).
	hov, ok := hoverST(src, Position{Line: 4, Character: 2})
	if !ok {
		t.Fatalf("expected hover for `motor`")
	}
	if !strings.Contains(hov.Contents.Value, "BOOL") {
		t.Errorf("hover should mention type BOOL, got %q", hov.Contents.Value)
	}
	if !strings.Contains(hov.Contents.Value, "TRUE") {
		t.Errorf("hover should include initial value, got %q", hov.Contents.Value)
	}
}

func TestCompletionSTIncludesVarsAndBuiltins(t *testing.T) {
	src := "" +
		"PROGRAM p\n" +
		"VAR_GLOBAL\n" +
		"    counter : INT;\n" +
		"END_VAR\n" +
		"END_PROGRAM\n"
	list := completeST(src)
	want := map[string]bool{"counter": false, "ABS": false, "TON": false, "IF": false}
	for _, it := range list.Items {
		if _, ok := want[it.Label]; ok {
			want[it.Label] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("completion missing %q", k)
		}
	}
}

func TestAnalyzeSTNoDiagnosticsOnValidProgram(t *testing.T) {
	src := "" +
		"PROGRAM p\n" +
		"VAR_GLOBAL\n" +
		"    x : INT;\n" +
		"    y : INT;\n" +
		"END_VAR\n" +
		"y := x + 1;\n" +
		"END_PROGRAM\n"
	if diags := Analyze(src, "st"); len(diags) != 0 {
		t.Errorf("valid program should produce no diagnostics, got %+v", diags)
	}
}
