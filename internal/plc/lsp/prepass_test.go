//go:build plc || all

package lsp

import (
	"strings"
	"testing"
)

func TestPrepassUnclosedParen(t *testing.T) {
	diags := prepassStarlark("def foo(\n    pass\n")
	if len(diags) == 0 {
		t.Fatalf("expected at least one diagnostic, got 0")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "Unclosed `(`") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Unclosed `(` diagnostic, got %+v", diags)
	}
}

func TestPrepassMultipleUnclosedBrackets(t *testing.T) {
	// Parser stops at the first error; pre-pass should flag BOTH openers.
	src := "a = (\n    b = [\n"
	diags := prepassStarlark(src)
	unclosed := 0
	for _, d := range diags {
		if strings.HasPrefix(d.Message, "Unclosed") {
			unclosed++
		}
	}
	if unclosed != 2 {
		t.Errorf("expected 2 unclosed diagnostics, got %d: %+v", unclosed, diags)
	}
}

func TestPrepassMismatchedBrackets(t *testing.T) {
	diags := prepassStarlark("x = (1, 2]\n")
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "doesn't match") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected mismatch diagnostic, got %+v", diags)
	}
}

func TestPrepassIgnoresBracketsInStrings(t *testing.T) {
	// Brackets inside strings must not affect the stack.
	diags := prepassStarlark(`x = "(((" + '[' + """{"""` + "\n")
	for _, d := range diags {
		if strings.Contains(d.Message, "Unclosed") {
			t.Errorf("did not expect unclosed diagnostic, got %+v", diags)
		}
	}
}

func TestPrepassIgnoresBracketsInComments(t *testing.T) {
	diags := prepassStarlark("x = 1  # ignored (((\ny = 2\n")
	for _, d := range diags {
		if strings.Contains(d.Message, "Unclosed") {
			t.Errorf("did not expect unclosed diagnostic, got %+v", diags)
		}
	}
}

func TestPrepassMixedIndent(t *testing.T) {
	// Line 2 starts with tab-then-space — mixed.
	src := "def foo():\n\t pass\n"
	diags := prepassStarlark(src)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "Mixed tabs and spaces") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected mixed-indent diagnostic, got %+v", diags)
	}
}

func TestMergeDropsPrepassOnParserLines(t *testing.T) {
	parser := []Diagnostic{rangeDiag(3, 1, 3, 5, "parser says bad")}
	pre := []Diagnostic{
		rangeDiag(3, 1, 3, 2, "prepass also on line 3 — should drop"),
		rangeDiag(7, 1, 7, 2, "prepass on different line — should keep"),
	}
	merged := mergePrepassDiagnostics(parser, pre)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged diagnostics, got %d: %+v", len(merged), merged)
	}
	// Ensure the kept prepass is the line-7 one.
	kept := merged[1]
	if kept.Range.Start.Line != 6 { // 0-based
		t.Errorf("expected kept diagnostic on line 7 (0-based 6), got %d", kept.Range.Start.Line)
	}
}

func TestAnalyzeStarlarkSurfacesMultipleErrors(t *testing.T) {
	// Parser will stop at the first structural problem; pre-pass adds the
	// second unclosed bracket so the user sees both.
	src := "x = (\n\ny = [\n"
	diags := analyzeStarlark(src, nil, "")
	if len(diags) < 2 {
		t.Fatalf("expected at least 2 diagnostics, got %d: %+v", len(diags), diags)
	}
}
