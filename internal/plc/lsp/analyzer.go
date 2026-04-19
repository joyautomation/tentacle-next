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
	var parserDiags []Diagnostic
	_, err := syntax.Parse("program.star", source, 0)
	if err != nil {
		var se syntax.Error
		if errors.As(err, &se) {
			line := int(se.Pos.Line)
			msg, startCol, endCol := humanize(se.Msg, source, line)
			parserDiags = []Diagnostic{rangeDiag(line, startCol, line, endCol, msg)}
		} else {
			msg, startCol, endCol := humanize(err.Error(), source, 1)
			parserDiags = []Diagnostic{rangeDiag(1, startCol, 1, endCol, msg)}
		}
	}
	// Pre-pass runs whether or not the parser errored. If the source is
	// clean but has, say, a trailing unclosed bracket, the parser may have
	// already flagged it — mergePrepassDiagnostics dedupes by line.
	pre := prepassStarlark(source)
	return mergePrepassDiagnostics(parserDiags, pre)
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
	humanMsg, startCol, endCol := humanize(msg, source, line)
	return []Diagnostic{rangeDiag(line, startCol, line, endCol, humanMsg)}
}

// rangeDiag builds an error Diagnostic covering the given 1-based span.
// Range is converted to LSP's 0-based convention here. If the end position
// is at or before the start, the caller is expected to widen separately.
func rangeDiag(startLine, startCol, endLine, endCol int, message string) Diagnostic {
	if startLine < 1 {
		startLine = 1
	}
	if startCol < 1 {
		startCol = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	if endLine == startLine && endCol < startCol {
		endCol = startCol
	}
	return Diagnostic{
		Range: Range{
			Start: Position{Line: startLine - 1, Character: startCol - 1},
			End:   Position{Line: endLine - 1, Character: endCol - 1},
		},
		Severity: SeverityError,
		Source:   "tentacle-plc",
		Message:  message,
	}
}
