//go:build plc || all

package lsp

import (
	"strings"
	"testing"
)

func TestHoverOnBuiltin(t *testing.T) {
	// `get_var` appears on line 0, columns 0–7.
	src := `x = get_var("foo")`
	h, ok := hoverStarlark(src, Position{Line: 0, Character: 6}, nil, "") // middle of "get_var"
	if !ok || h == nil {
		t.Fatalf("expected hover on get_var, got nothing")
	}
	if !strings.Contains(h.Contents.Value, "get_var") {
		t.Errorf("hover missing signature, got %q", h.Contents.Value)
	}
	if !strings.Contains(h.Contents.Value, "PLC variable") {
		t.Errorf("hover missing doc prose, got %q", h.Contents.Value)
	}
	// Range should cover the word (cols 4..11 → start.character=4).
	if h.Range == nil || h.Range.Start.Character != 4 {
		t.Errorf("expected Range starting at char 4, got %+v", h.Range)
	}
}

func TestHoverOnNonBuiltinReturnsNothing(t *testing.T) {
	src := "my_local_var = 1\n"
	_, ok := hoverStarlark(src, Position{Line: 0, Character: 4}, nil, "")
	if ok {
		t.Errorf("expected no hover for non-builtin identifier")
	}
}

func TestHoverOffIdentifierReturnsNothing(t *testing.T) {
	src := "x = 1"
	// Cursor on the `=` character.
	_, ok := hoverStarlark(src, Position{Line: 0, Character: 2}, nil, "")
	if ok {
		t.Errorf("expected no hover on `=`")
	}
}

func TestHoverAtEndOfIdentifier(t *testing.T) {
	// Cursor one past the last char of a builtin (common "cursor after word").
	src := "log"
	h, ok := hoverStarlark(src, Position{Line: 0, Character: 3}, nil, "")
	if !ok || h == nil {
		t.Fatalf("expected hover when cursor is immediately after builtin")
	}
	if !strings.Contains(h.Contents.Value, "log") {
		t.Errorf("hover payload did not mention log, got %q", h.Contents.Value)
	}
}
