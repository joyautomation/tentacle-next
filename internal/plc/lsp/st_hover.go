//go:build plc || all

package lsp

import (
	"fmt"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc/st"
)

// hoverST resolves the identifier under `pos` to its declared type. The
// editor's hover popup ends up showing something like `motor : BOOL` —
// just enough to let the user double-check that they're poking the right
// variable without flipping back to the VAR block.
//
// The implementation is intentionally cheap: parse, find the ident token
// at `pos` by walking the source line, then look up its declaration in
// the program's VarBlocks. It does not run the type checker, so it
// works even when the program has lowering errors elsewhere.
func hoverST(source string, pos Position) (*Hover, bool) {
	name, span, ok := identAt(source, pos)
	if !ok {
		return nil, false
	}
	prog, err := st.Parse(source)
	if err != nil || prog == nil {
		return nil, false
	}
	decl, fbField, found := lookupSTSymbol(prog, name)
	if !found {
		return nil, false
	}
	body := formatSTHover(name, decl, fbField)
	return &Hover{
		Contents: MarkupContent{Kind: "markdown", Value: body},
		Range:    &span,
	}, true
}

// lookupSTSymbol walks every VAR block looking for `name`. Returns the
// matching VarDecl; if the match is a FB instance and `name` looked like
// a member access (handled by the caller pre-splitting on `.`), the
// caller asks again with the member name.
func lookupSTSymbol(prog *st.Program, name string) (*st.VarDecl, string, bool) {
	for _, vb := range prog.VarBlocks {
		for i, vd := range vb.Variables {
			if vd.Name == name {
				return &vb.Variables[i], "", true
			}
		}
	}
	return nil, "", false
}

func formatSTHover(name string, decl *st.VarDecl, fbField string) string {
	var b strings.Builder
	b.WriteString("```st\n")
	if decl != nil {
		fmt.Fprintf(&b, "%s : %s", name, decl.Datatype)
		if decl.Initial != nil {
			b.WriteString(" := ")
			b.WriteString(exprPreview(decl.Initial))
		}
	} else if fbField != "" {
		fmt.Fprintf(&b, "%s : %s", name, fbField)
	} else {
		b.WriteString(name)
	}
	b.WriteString("\n```")
	return b.String()
}

// exprPreview renders a literal initial value as a short string for the
// hover popup. Falls back to "…" for shapes we don't pretty-print.
func exprPreview(e st.Expression) string {
	switch v := e.(type) {
	case *st.NumberLit:
		return v.Value
	case *st.BoolLit:
		if v.Value {
			return "TRUE"
		}
		return "FALSE"
	case *st.StringLit:
		return "'" + v.Value + "'"
	case *st.TimeLit:
		return "T#" + v.Raw
	}
	return "…"
}

// identAt locates the identifier token covering the given LSP position
// (0-based line/character). It returns the identifier text and a Range
// suitable for setting Hover.Range so the editor highlights the whole
// word, not just the click point.
func identAt(source string, pos Position) (string, Range, bool) {
	lines := strings.Split(source, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return "", Range{}, false
	}
	line := lines[pos.Line]
	if pos.Character < 0 || pos.Character > len(line) {
		return "", Range{}, false
	}
	isIdent := func(b byte) bool {
		return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
	}
	start := pos.Character
	for start > 0 && isIdent(line[start-1]) {
		start--
	}
	end := pos.Character
	for end < len(line) && isIdent(line[end]) {
		end++
	}
	if start == end {
		return "", Range{}, false
	}
	name := line[start:end]
	// Reject pure-numeric tokens.
	if name[0] >= '0' && name[0] <= '9' {
		return "", Range{}, false
	}
	return name, Range{
		Start: Position{Line: pos.Line, Character: start},
		End:   Position{Line: pos.Line, Character: end},
	}, true
}
