//go:build plc || all

package lsp

// The Starlark parser stops at the first syntax error, which means an
// unclosed `(` on line 5 hides a second unclosed `{` on line 50. This pre-
// pass sweeps the whole source without trying to parse the grammar, so we
// can surface structural issues the parser never gets a chance to report.
//
// Scope is deliberately narrow: bracket balance and mixed-indentation. Any
// category we can't catch reliably without a grammar belongs in the parser,
// not here.

import "fmt"

// prepassStarlark returns additional diagnostics to merge with the parser's.
// The caller is responsible for deduping against parser output if the parser
// already flagged the same line — see mergePrepassDiagnostics.
func prepassStarlark(source string) []Diagnostic {
	if source == "" {
		return nil
	}
	var diags []Diagnostic
	diags = append(diags, scanBrackets(source)...)
	diags = append(diags, scanMixedIndent(source)...)
	return diags
}

type openBracket struct {
	ch   byte
	line int
	col  int // 1-based
}

// scanBrackets walks the source tracking string/comment state and records
// every unmatched opener and unmatched closer.
func scanBrackets(source string) []Diagnostic {
	var stack []openBracket
	var diags []Diagnostic

	line, col := 1, 1
	i := 0
	for i < len(source) {
		c := source[i]

		// Line comment: skip to EOL.
		if c == '#' {
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}

		// Triple-quoted strings: """..."""  or  '''...'''. Must check before
		// single-quoted to avoid eating the opening pair as an empty string.
		if (c == '"' || c == '\'') && i+2 < len(source) && source[i+1] == c && source[i+2] == c {
			quote := c
			i += 3
			col += 3
			for i < len(source) {
				if source[i] == '\\' && i+1 < len(source) {
					// Track newline in escape for accurate positions.
					if source[i+1] == '\n' {
						line++
						col = 1
					} else {
						col += 2
					}
					i += 2
					continue
				}
				if source[i] == quote && i+2 < len(source) && source[i+1] == quote && source[i+2] == quote {
					i += 3
					col += 3
					break
				}
				if source[i] == '\n' {
					line++
					col = 1
				} else {
					col++
				}
				i++
			}
			continue
		}

		// Single-line string. Stops at matching quote or newline (parser will
		// already flag an unterminated single-line string — don't duplicate).
		if c == '"' || c == '\'' {
			quote := c
			i++
			col++
			for i < len(source) && source[i] != quote && source[i] != '\n' {
				if source[i] == '\\' && i+1 < len(source) {
					i += 2
					col += 2
					continue
				}
				i++
				col++
			}
			if i < len(source) && source[i] == quote {
				i++
				col++
			}
			continue
		}

		switch c {
		case '(', '[', '{':
			stack = append(stack, openBracket{ch: c, line: line, col: col})
		case ')', ']', '}':
			if len(stack) == 0 {
				// Unmatched closer. Parser typically catches the first; but if
				// it slipped past (e.g. triggered a different error earlier),
				// surface it.
				diags = append(diags, rangeDiag(line, col, line, col+1,
					fmt.Sprintf("Unexpected closing `%c` — no matching opener.", c)))
			} else {
				top := stack[len(stack)-1]
				if !bracketsMatch(top.ch, c) {
					diags = append(diags, rangeDiag(line, col, line, col+1,
						fmt.Sprintf("Closing `%c` doesn't match opening `%c` on line %d.", c, top.ch, top.line)))
				}
				stack = stack[:len(stack)-1]
			}
		}

		if c == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i++
	}

	// Anything still on the stack is unclosed.
	for _, open := range stack {
		close := closerFor(open.ch)
		diags = append(diags, rangeDiag(open.line, open.col, open.line, open.col+1,
			fmt.Sprintf("Unclosed `%c` — add a matching `%c` to close it.", open.ch, close)))
	}

	return diags
}

func bracketsMatch(open, close byte) bool {
	switch open {
	case '(':
		return close == ')'
	case '[':
		return close == ']'
	case '{':
		return close == '}'
	}
	return false
}

func closerFor(open byte) byte {
	switch open {
	case '(':
		return ')'
	case '[':
		return ']'
	case '{':
		return '}'
	}
	return ' '
}

// scanMixedIndent flags lines whose leading whitespace mixes tabs and spaces.
// Starlark allows either but not both on the same line's indent — mixing is
// a common cause of "unindent does not match" errors.
func scanMixedIndent(source string) []Diagnostic {
	var diags []Diagnostic
	line := 1
	lineStart := 0
	for i := 0; i <= len(source); i++ {
		atEnd := i == len(source)
		if atEnd || source[i] == '\n' {
			hasTab, hasSpace := false, false
			for j := lineStart; j < i; j++ {
				if source[j] == '\t' {
					hasTab = true
				} else if source[j] == ' ' {
					hasSpace = true
				} else {
					break
				}
			}
			if hasTab && hasSpace {
				diags = append(diags, rangeDiag(line, 1, line, 2,
					"Mixed tabs and spaces in indentation. Use one or the other consistently."))
			}
			if !atEnd {
				line++
				lineStart = i + 1
			}
		}
	}
	return diags
}

// mergePrepassDiagnostics combines parser diagnostics with pre-pass findings,
// dropping pre-pass diagnostics that land on a line the parser already
// flagged. The parser's message is more authoritative for the primary error;
// the pre-pass adds value by catching *additional* issues elsewhere.
func mergePrepassDiagnostics(parser, prepass []Diagnostic) []Diagnostic {
	if len(prepass) == 0 {
		return parser
	}
	parserLines := make(map[int]bool, len(parser))
	for _, d := range parser {
		parserLines[d.Range.Start.Line] = true
	}
	out := make([]Diagnostic, 0, len(parser)+len(prepass))
	out = append(out, parser...)
	for _, d := range prepass {
		if parserLines[d.Range.Start.Line] {
			continue
		}
		out = append(out, d)
	}
	return out
}
