//go:build plc || all

package lsp

import (
	"strings"
	"testing"
)

func TestCompletionIncludesBuiltins(t *testing.T) {
	list := completeStarlark("", Position{Line: 0, Character: 0})
	if len(list.Items) == 0 {
		t.Fatalf("expected completion items, got 0")
	}
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	for _, name := range []string{"get_var", "set_var", "log", "NO", "TON", "clamp"} {
		if !seen[name] {
			t.Errorf("expected %q in completion list", name)
		}
	}
}

func TestCompletionBuiltinHasSnippet(t *testing.T) {
	list := completeStarlark("", Position{Line: 0, Character: 0})
	for _, it := range list.Items {
		if it.Label == "get_var" {
			if it.InsertTextFormat != InsertTextFormatSnippet {
				t.Errorf("expected snippet format for get_var, got %d", it.InsertTextFormat)
			}
			if !strings.Contains(it.InsertText, "$1") {
				t.Errorf("expected snippet placeholder in get_var insert text, got %q", it.InsertText)
			}
			if it.Detail == "" {
				t.Errorf("expected signature in Detail")
			}
			return
		}
	}
	t.Errorf("get_var not found in completion list")
}

func TestCompletionIncludesLocalDef(t *testing.T) {
	src := "def cool_helper():\n    pass\n\nx = 1\n"
	list := completeStarlark(src, Position{Line: 3, Character: 0})
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["cool_helper"] {
		t.Errorf("expected `cool_helper` in completions, got %v", completionLabels(list))
	}
	if !seen["x"] {
		t.Errorf("expected `x` in completions")
	}
}

func TestCompletionIncludesDefParams(t *testing.T) {
	src := "def main(motor_speed, setpoint):\n    y = 1\n"
	// Cursor on the `y = 1` line — inside def main's body.
	list := completeStarlark(src, Position{Line: 1, Character: 4})
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["motor_speed"] || !seen["setpoint"] {
		t.Errorf("expected def params in completions, got %v", completionLabels(list))
	}
}

func TestCompletionKeywordsPresent(t *testing.T) {
	list := completeStarlark("", Position{Line: 0, Character: 0})
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	for _, kw := range []string{"if", "else", "for", "def", "True", "False"} {
		if !seen[kw] {
			t.Errorf("expected keyword %q in completions", kw)
		}
	}
}

func completionLabels(list CompletionList) []string {
	out := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, it.Label)
	}
	return out
}
