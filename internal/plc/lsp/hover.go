//go:build plc || all

package lsp

// hoverStarlark resolves the identifier under `pos` and returns a Hover
// payload. Only PLC builtins get hover in this first cut — local symbol
// hover (with inferred type/docstring) arrives once the resolver pass
// lands.
//
// Returns (nil, false) when there's nothing useful at the cursor. The
// server turns a false into a JSON `null` result, which tells the client
// to suppress the tooltip.
func hoverStarlark(source string, pos Position) (*Hover, bool) {
	word, startCol, endCol := identifierAt(source, pos.Line, pos.Character)
	if word == "" {
		return nil, false
	}
	b, ok := BuiltinsByName()[word]
	if !ok {
		return nil, false
	}
	return &Hover{
		Contents: MarkupContent{Kind: "markdown", Value: formatHoverMarkdown(b)},
		Range: &Range{
			Start: Position{Line: pos.Line, Character: startCol},
			End:   Position{Line: pos.Line, Character: endCol},
		},
	}, true
}

// identifierAt returns the word under (line, character) in source, plus
// the 0-based [startCol, endCol) span it occupies on that line. Returns
// an empty string if the cursor is not on an identifier character.
//
// An "identifier character" matches Starlark's rules: letters, digits,
// underscore — with the first char not a digit.
func identifierAt(source string, line, character int) (string, int, int) {
	lineStart := 0
	current := 0
	for i := 0; i <= len(source); i++ {
		if current == line {
			// Find line end.
			end := len(source)
			for j := i; j < len(source); j++ {
				if source[j] == '\n' {
					end = j
					break
				}
			}
			return extractIdent(source[i:end], character)
		}
		if i < len(source) && source[i] == '\n' {
			current++
			lineStart = i + 1
			_ = lineStart
		}
	}
	return "", 0, 0
}

func extractIdent(lineText string, character int) (string, int, int) {
	if character < 0 {
		character = 0
	}
	// Hover requests arrive with character pointing at the first char
	// after the cursor; normalise so we look at the char under the caret.
	if character > len(lineText) {
		character = len(lineText)
	}

	// If the cursor is immediately after an identifier (end of word), step
	// back once so we pick up the identifier the user just finished typing.
	probe := character
	if probe == len(lineText) || !isIdentChar(lineText[probe]) {
		if probe > 0 && isIdentChar(lineText[probe-1]) {
			probe--
		} else {
			return "", 0, 0
		}
	}
	start := probe
	for start > 0 && isIdentChar(lineText[start-1]) {
		start--
	}
	end := probe + 1
	for end < len(lineText) && isIdentChar(lineText[end]) {
		end++
	}
	// First char must not be a digit.
	if start < len(lineText) && isDigit(lineText[start]) {
		return "", 0, 0
	}
	return lineText[start:end], start, end
}

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
