//go:build plc || all

package lsp

import (
	"fmt"
	"regexp"
	"strings"
)

// humanize translates a parser error message into one a PLC engineer can act
// on without knowing how the parser works. Returns the rewritten message and
// a widened column span covering the offending line's non-whitespace content,
// so CodeMirror renders a visible squiggle instead of a caret.
//
// The translations are deliberately conservative: if we don't recognise the
// pattern we return the original message unchanged. Parser internals will
// evolve; an unrecognised message is a cue to add a new case, never to
// silently mangle the error.
func humanize(raw, source string, line int) (newMsg string, startCol, endCol int) {
	msg := raw
	for _, rule := range humanizeRules {
		if rewritten, ok := rule(msg); ok {
			msg = rewritten
			break
		}
	}
	// Capitalise the first letter so the tooltip reads like a sentence. Parser
	// messages are lowercase by Go convention; users expect sentence case.
	msg = capitalizeFirst(msg)

	startCol, endCol = lineContentSpan(source, line)
	return msg, startCol, endCol
}

// humanizeRules is an ordered list of pattern-matchers. Each returns a
// rewritten message and true on a match, or ("", false) otherwise. Order
// matters: more specific patterns must come before generic fallbacks.
var humanizeRules = []func(string) (string, bool){
	// Common "got X, want Y" patterns from the Starlark parser.
	// Format is typically: got %#v, want %#v   (go's %#v on a kindToken string)
	// which yields messages like: `got "outdent", want ","`
	ruleGotWant,
	ruleUnexpectedNewlineInString,
	ruleUnexpectedEOFInString,
	ruleUnindentMismatch,
	ruleStrayBackslash,
	ruleUnexpectedChar,
	ruleUnexpectedCloser,
	ruleKeywordArgForm,
	ruleInvalidNumeric,
	ruleCompChainingParens,
	ruleTernaryMissingElse,
}

var gotWantRe = regexp.MustCompile(`^got\s+(\S+?)\s*,\s*want\s+(.+)$`)

func ruleGotWant(msg string) (string, bool) {
	m := gotWantRe.FindStringSubmatch(msg)
	if m == nil {
		return "", false
	}
	got, want := unquoteToken(m[1]), strings.TrimSpace(m[2])
	// Normalise common want-lists like `"X", for, or if` into plain phrasing.
	wantPhrase := wantPhrase(want)
	gotPhrase := gotPhrase(got)

	// Specific high-signal combinations first so we can give actionable hints
	// instead of a generic "expected X, got Y".
	switch {
	case got == "outdent" && want == `","`:
		return "Line ended before the statement was finished. Did you miss a value, a closing `)`, or more arguments after `,`?", true
	case got == "outdent" && want == `")"`:
		return "Line ended with an open `(`. Add a `)` to close it.", true
	case got == "outdent" && want == `"]"`:
		return "Line ended with an open `[`. Add a `]` to close it.", true
	case got == "outdent" && want == `"}"`:
		return "Line ended with an open `{`. Add a `}` to close it.", true
	case got == "outdent" && want == `":"`:
		return "Expected `:` at the end of this line. Statements like `def`, `if`, `for`, and `while` must end with `:`.", true
	case got == "newline" && want == `":"`:
		return "Expected `:` at the end of this line. Statements like `def`, `if`, `for`, and `while` must end with `:`.", true
	case got == "newline" && want == "primary expression":
		return "Expected a value on this line. Check for an incomplete expression (e.g. `x =` with nothing after).", true
	case got == "outdent" && want == "primary expression":
		return "Expected a value or expression, but the line ended. Did you forget the right-hand side of an assignment?", true
	case got == "EOF" && strings.HasPrefix(want, "\""):
		return fmt.Sprintf("File ended unexpectedly. Expected %s to close an open construct.", wantPhrase), true
	}

	return fmt.Sprintf("Expected %s, but found %s.", wantPhrase, gotPhrase), true
}

// gotPhrase turns a parser token-kind name into a phrase for a sentence.
func gotPhrase(got string) string {
	switch got {
	case "outdent":
		return "end of the indented block"
	case "indent":
		return "start of a new indented block"
	case "newline":
		return "end of line"
	case "EOF":
		return "end of file"
	case "ident":
		return "an identifier"
	case "int":
		return "a number"
	case "float":
		return "a decimal number"
	case "string":
		return "a string"
	}
	if strings.HasPrefix(got, "\"") {
		return "`" + strings.Trim(got, "\"") + "`"
	}
	return got
}

// wantPhrase renders the "want" side of a got/want error. It passes list
// forms ("X, Y, or Z") through mostly intact but swaps quote-form for code.
func wantPhrase(want string) string {
	want = strings.TrimSpace(want)
	switch want {
	case "primary expression":
		return "a value, variable name, or expression"
	case "':'":
		return "`:`"
	case "','":
		return "`,`"
	case "')'":
		return "`)`"
	case "']'":
		return "`]`"
	case "'}'":
		return "`}`"
	}
	// For composite wants like `"X", for, or if`, pass through.
	// Final polish — replace double-quoted tokens with backtick-wrapped code.
	return replaceQuotedTokens(want)
}

var quotedTokenRe = regexp.MustCompile(`"([^"]+)"`)

func replaceQuotedTokens(s string) string {
	return quotedTokenRe.ReplaceAllString(s, "`$1`")
}

func unquoteToken(s string) string {
	s = strings.TrimSpace(s)
	return strings.Trim(s, "\"")
}

func ruleUnexpectedNewlineInString(msg string) (string, bool) {
	if strings.Contains(msg, "unexpected newline in string") {
		return "String literal runs off the end of the line. Close the string with `\"` or use `\"\"\"...\"\"\"` for multi-line strings.", true
	}
	return "", false
}

func ruleUnexpectedEOFInString(msg string) (string, bool) {
	if strings.Contains(msg, "unexpected EOF in string") {
		return "String literal never closes. Add the matching `\"` before the end of the file.", true
	}
	return "", false
}

func ruleUnindentMismatch(msg string) (string, bool) {
	if strings.Contains(msg, "unindent does not match") {
		return "Indentation on this line doesn't match any outer block. Make sure you're using the same indent (usually 4 spaces) throughout, and not mixing spaces with tabs.", true
	}
	return "", false
}

func ruleStrayBackslash(msg string) (string, bool) {
	if strings.Contains(msg, "stray backslash") {
		return "Stray `\\` in source. A backslash is only valid at the very end of a line to continue a statement.", true
	}
	return "", false
}

var unexpectedCharRe = regexp.MustCompile(`unexpected input character\s+(.+)`)

func ruleUnexpectedChar(msg string) (string, bool) {
	m := unexpectedCharRe.FindStringSubmatch(msg)
	if m == nil {
		return "", false
	}
	ch := strings.TrimSpace(m[1])
	if ch == "'!'" {
		return "`!` is not a Starlark operator. Use `!=` for \"not equal\" or `not` to negate.", true
	}
	return fmt.Sprintf("Unexpected character %s in the source.", ch), true
}

var unexpectedCloserRe = regexp.MustCompile(`^unexpected\s+"(.)"$`)

func ruleUnexpectedCloser(msg string) (string, bool) {
	m := unexpectedCloserRe.FindStringSubmatch(msg)
	if m == nil {
		return "", false
	}
	ch := m[1]
	return fmt.Sprintf("Unexpected closing `%s` — there's no matching opening bracket for it.", ch), true
}

func ruleKeywordArgForm(msg string) (string, bool) {
	if strings.Contains(msg, "keyword argument must have form name=expr") {
		return "Keyword arguments must be of the form `name=value`. The left side of `=` must be a simple name.", true
	}
	return "", false
}

func ruleInvalidNumeric(msg string) (string, bool) {
	switch {
	case strings.Contains(msg, "invalid hex literal"):
		return "Invalid hex literal. Use `0x` followed by hex digits, e.g. `0xFF`.", true
	case strings.Contains(msg, "invalid octal literal"):
		return "Invalid octal literal. Use `0o` followed by octal digits, e.g. `0o755`.", true
	case strings.Contains(msg, "invalid binary literal"):
		return "Invalid binary literal. Use `0b` followed by `0`s and `1`s, e.g. `0b1010`.", true
	case strings.Contains(msg, "invalid float literal"):
		return "Invalid decimal number. Check for a trailing `e`, `.`, or `_` with no digits after.", true
	case strings.Contains(msg, "obsolete form of octal literal"):
		return "Old-style octal literal (e.g. `0755`). Use the modern form `0o755` instead.", true
	}
	return "", false
}

func ruleCompChainingParens(msg string) (string, bool) {
	if strings.Contains(msg, "does not associate") {
		return "Comparison operators don't chain. Use `and` to join two comparisons: `a < b and b < c`.", true
	}
	return "", false
}

func ruleTernaryMissingElse(msg string) (string, bool) {
	if strings.Contains(msg, "conditional expression without else clause") {
		return "`x if cond` needs an `else` branch. Write `x if cond else y`.", true
	}
	return "", false
}

// capitalizeFirst uppercases the first rune of s if it's a lowercase letter.
// We keep this simple because parser messages are ASCII.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	first := s[0]
	if first >= 'a' && first <= 'z' {
		return string(first-32) + s[1:]
	}
	return s
}

// lineContentSpan returns 1-based (startCol, endCol) covering the non-
// whitespace content of the given 1-based line in source. If the line is
// empty or out of range, returns (1, 2) so diagnostics still render a
// caret rather than collapsing to zero width.
func lineContentSpan(source string, line int) (int, int) {
	if line < 1 || source == "" {
		return 1, 2
	}
	current := 1
	lineStart := 0
	for i := 0; i <= len(source); i++ {
		atEnd := i == len(source)
		if current == line && (atEnd || source[i] == '\n') {
			// Found end of the target line — [lineStart, i) is its content.
			content := source[lineStart:i]
			leading := 0
			for leading < len(content) && (content[leading] == ' ' || content[leading] == '\t') {
				leading++
			}
			// Trim trailing whitespace (usually none before \n, but be safe).
			trailing := len(content)
			for trailing > leading && (content[trailing-1] == ' ' || content[trailing-1] == '\t') {
				trailing--
			}
			if leading == trailing {
				// Line is blank. Use (1, 2) so there's a one-char caret.
				return 1, 2
			}
			return leading + 1, trailing + 1
		}
		if !atEnd && source[i] == '\n' {
			current++
			lineStart = i + 1
		}
	}
	return 1, 2
}
