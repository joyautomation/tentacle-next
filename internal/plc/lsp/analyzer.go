//go:build plc || all

package lsp

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc/st"
	"go.starlark.net/syntax"
)

// Analyze produces diagnostics for the given source under the given language.
// Language IDs follow the convention used by the web editor:
//   - "starlark" / "python" → go.starlark.net/syntax.Parse
//   - "st" / "structured-text" → internal/plc/st.Parse
//
// This function is the single source of diagnostic truth; both the legacy
// HTTP /validate endpoint and the LSP server dispatch to it.
func Analyze(source, languageID string) []Diagnostic {
	switch strings.ToLower(languageID) {
	case "st", "structured-text":
		return analyzeST(source)
	case "starlark", "python", "":
		return analyzeStarlark(source)
	default:
		return nil
	}
}

func analyzeStarlark(source string) []Diagnostic {
	if source == "" {
		return nil
	}
	_, err := syntax.Parse("program.star", source, 0)
	if err == nil {
		return nil
	}
	var se syntax.Error
	if errors.As(err, &se) {
		return []Diagnostic{parseDiag(int(se.Pos.Line), int(se.Pos.Col), se.Msg)}
	}
	return []Diagnostic{parseDiag(1, 1, err.Error())}
}

// st parser errors are formatted as "line N: message"; extract the line.
var stLineRe = regexp.MustCompile(`^line (\d+):\s*(.*)$`)

func analyzeST(source string) []Diagnostic {
	if source == "" {
		return nil
	}
	_, err := st.Parse(source)
	if err == nil {
		return nil
	}
	msg := err.Error()
	line := 1
	if m := stLineRe.FindStringSubmatch(msg); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n
			msg = m[2]
		}
	}
	return []Diagnostic{parseDiag(line, 1, msg)}
}

// parseDiag converts 1-based (line, col) from the parsers into an LSP-native
// 0-based range covering the single offending position. We have no end column
// from the parsers, so we collapse the range to a caret; the editor widens
// it to a full-word squiggle by default.
func parseDiag(line, col int, message string) Diagnostic {
	if line < 1 {
		line = 1
	}
	if col < 1 {
		col = 1
	}
	start := Position{Line: line - 1, Character: col - 1}
	return Diagnostic{
		Range:    Range{Start: start, End: start},
		Severity: SeverityError,
		Source:   "tentacle-plc",
		Message:  message,
	}
}
