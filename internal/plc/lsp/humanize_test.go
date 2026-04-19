//go:build plc || all

package lsp

import (
	"strings"
	"testing"
)

func TestHumanizeGotOutdentWantComma(t *testing.T) {
	// The message the user saw in the IDE: "got outdent, want ','"
	got, _, _ := humanize(`got outdent, want ","`, "def main():\n  x=\n", 2)
	if !strings.Contains(got, "Line ended before the statement was finished") {
		t.Errorf("expected human rewrite, got %q", got)
	}
	if strings.Contains(got, "outdent") {
		t.Errorf("humanised message still contains parser jargon: %q", got)
	}
}

func TestHumanizeGotOutdentWantColon(t *testing.T) {
	got, _, _ := humanize(`got outdent, want ":"`, "if x\n", 1)
	if !strings.Contains(got, ":") || !strings.Contains(got, "def") {
		t.Errorf("expected colon-hint message, got %q", got)
	}
}

func TestHumanizeNewlineInString(t *testing.T) {
	got, _, _ := humanize(`unexpected newline in string`, `x = "hi`+"\n", 1)
	if !strings.Contains(got, "String literal") || !strings.Contains(got, "multi-line") {
		t.Errorf("expected string-literal hint, got %q", got)
	}
}

func TestHumanizeUnindent(t *testing.T) {
	got, _, _ := humanize(`unindent does not match any outer indentation level`, "", 3)
	if !strings.Contains(got, "Indentation") || !strings.Contains(got, "tabs") {
		t.Errorf("expected indentation hint, got %q", got)
	}
}

func TestHumanizeBangCharacter(t *testing.T) {
	got, _, _ := humanize(`unexpected input character '!'`, "x != y", 1)
	if !strings.Contains(got, "!=") || !strings.Contains(got, "not") {
		t.Errorf("expected ! rewrite, got %q", got)
	}
}

func TestHumanizeUnknownMessageFallsThrough(t *testing.T) {
	// Parser internals can change; a message we don't recognise should be
	// capitalised and returned — never silently swallowed or mangled.
	got, _, _ := humanize(`something weird happened`, "", 1)
	if got != "Something weird happened" {
		t.Errorf("expected capitalised passthrough, got %q", got)
	}
}

func TestHumanizeWidensRangeToEndOfLine(t *testing.T) {
	source := "def main():\n    x=\n    pass\n"
	// Error is on line 2 (the "x=" line).
	_, _, endCol := humanize(`got outdent, want ","`, source, 2)
	// Line 2 is "    x=" — 6 chars — so end column should be 7 (1-based after last char).
	if endCol != 7 {
		t.Errorf("expected endCol 7 for '    x=', got %d", endCol)
	}
}

func TestLineContentSpanBounds(t *testing.T) {
	// Out-of-range line returns caret span (1, 2) rather than panicking.
	if s, e := lineContentSpan("abc\n", 5); s != 1 || e != 2 {
		t.Errorf("out-of-range line should return (1,2), got (%d,%d)", s, e)
	}
	// Empty source returns caret span.
	if s, e := lineContentSpan("", 1); s != 1 || e != 2 {
		t.Errorf("empty source should return (1,2), got (%d,%d)", s, e)
	}
}

// Integration: verify analyzeStarlark now produces a widened, humanised diagnostic.
func TestAnalyzeStarlarkHumanised(t *testing.T) {
	diags := analyzeStarlark("def main():\n    x=\n")
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	d := diags[0]
	if strings.Contains(d.Message, "outdent") {
		t.Errorf("diagnostic message still parser-jargon: %q", d.Message)
	}
	if d.Range.End.Character <= d.Range.Start.Character && d.Range.End.Line == d.Range.Start.Line {
		t.Errorf("expected widened range, got start=%+v end=%+v", d.Range.Start, d.Range.End)
	}
}
