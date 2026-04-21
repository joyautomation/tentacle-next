//go:build plc || all

package lsp

import (
	"strings"
	"testing"
)

func newFnProvider() *fakeProvider {
	return &fakeProvider{
		fns: map[string]*FunctionInfo{
			"motor_on": {
				Name:        "motor_on",
				Program:     "motor_on",
				Description: "Starts the motor.",
				Params: []FunctionParam{
					{Name: "speed", Type: "number", Required: true},
					{Name: "ramp", Type: "number", Required: false},
				},
				Returns: &FunctionReturn{Type: "boolean"},
			},
		},
	}
}

func TestCompletionIncludesCrossProgramFunctions(t *testing.T) {
	list := completeStarlark("", Position{Line: 0, Character: 0}, newFnProvider(), "")
	var found *CompletionItem
	for i, it := range list.Items {
		if it.Label == "motor_on" {
			found = &list.Items[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected motor_on in completion list, got %v", completionLabels(list))
	}
	if found.Kind != CompletionKindFunction {
		t.Errorf("expected function kind, got %d", found.Kind)
	}
	if !strings.Contains(found.Detail, "speed: number") {
		t.Errorf("expected signature in Detail, got %q", found.Detail)
	}
	if found.InsertTextFormat != InsertTextFormatSnippet {
		t.Errorf("expected snippet format, got %d", found.InsertTextFormat)
	}
}

func TestCompletionSkipsCurrentProgramFunction(t *testing.T) {
	list := completeStarlark("", Position{Line: 0, Character: 0}, newFnProvider(), "motor_on")
	for _, it := range list.Items {
		if it.Label == "motor_on" && it.Kind == CompletionKindFunction {
			t.Errorf("current program should not offer its own function, got %v", completionLabels(list))
		}
	}
}

func TestHoverResolvesCrossProgramFunction(t *testing.T) {
	src := "def main():\n    motor_on(100)\n"
	h, ok := hoverStarlark(src, Position{Line: 1, Character: 8}, newFnProvider(), "")
	if !ok {
		t.Fatalf("expected hover for motor_on")
	}
	if !strings.Contains(h.Contents.Value, "motor_on(speed: number") {
		t.Errorf("expected signature in hover, got %q", h.Contents.Value)
	}
	if !strings.Contains(h.Contents.Value, "Starts the motor.") {
		t.Errorf("expected description in hover, got %q", h.Contents.Value)
	}
}

func TestAnalyzeFlagsTooFewArguments(t *testing.T) {
	src := "def main():\n    motor_on()\n"
	diags := AnalyzeWithProvider(src, "starlark", newFnProvider(), "")
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %+v", len(diags), diags)
	}
	if diags[0].Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %d", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Message, "at least 1") {
		t.Errorf("expected arity message, got %q", diags[0].Message)
	}
}

func TestAnalyzeFlagsTooManyArguments(t *testing.T) {
	src := "def main():\n    motor_on(1, 2, 3)\n"
	diags := AnalyzeWithProvider(src, "starlark", newFnProvider(), "")
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %+v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Message, "at most 2") {
		t.Errorf("expected arity message, got %q", diags[0].Message)
	}
}

func TestAnalyzeFlagsUnknownKeywordArgument(t *testing.T) {
	src := "def main():\n    motor_on(speed=100, bogus=1)\n"
	diags := AnalyzeWithProvider(src, "starlark", newFnProvider(), "")
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %+v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Message, "bogus") {
		t.Errorf("expected bogus in message, got %q", diags[0].Message)
	}
}

func TestAnalyzeAcceptsValidCall(t *testing.T) {
	src := "def main():\n    motor_on(100, 5)\n"
	diags := AnalyzeWithProvider(src, "starlark", newFnProvider(), "")
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %+v", diags)
	}
}

func TestAnalyzeSkipsLocalShadowing(t *testing.T) {
	// If the current source defines `motor_on`, cross-program arity
	// checking should step aside — the user's local def wins.
	src := "def motor_on():\n    pass\ndef main():\n    motor_on()\n"
	diags := AnalyzeWithProvider(src, "starlark", newFnProvider(), "")
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics when local def shadows, got %+v", diags)
	}
}

func TestProgramNameFromURI(t *testing.T) {
	cases := []struct {
		uri, want string
	}{
		{"tentacle-plc://programs/main.star", "main"},
		{"tentacle-plc://programs/motor_on.st", "motor_on"},
		{"tentacle-plc://programs/no-ext", "no-ext"},
		{"file:///tmp/foo.star", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := programNameFromURI(tc.uri); got != tc.want {
			t.Errorf("programNameFromURI(%q) = %q; want %q", tc.uri, got, tc.want)
		}
	}
}
