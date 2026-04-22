//go:build plc || all

package lsp

import (
	"strings"

	"github.com/joyautomation/tentacle/internal/plc"
	"go.starlark.net/syntax"
)

// SourceReference is one call-site within a Starlark source. Positions are
// 1-based and measured in bytes within the original (un-stripped) source;
// LineText is the full line so UI result lists can render a preview.
type SourceReference struct {
	Line     int    `json:"line"`
	StartCol int    `json:"startCol"`
	EndCol   int    `json:"endCol"`
	LineText string `json:"lineText"`
}

// FindProgramReferences walks src for call sites whose callee is a bare
// ident named target. The check skips programs that define a local function
// of the same name — those shadow any cross-program function, so a rename
// of the cross-program function must not rewrite them.
//
// Returns positions in the original (un-stripped) source; annotation
// stripping preserves byte offsets, so AST positions line up.
func FindProgramReferences(src, target string) []SourceReference {
	if src == "" || target == "" {
		return nil
	}
	stripped, _ := plc.StripAnnotations(src)
	file, err := syntax.Parse("program.star", stripped, 0)
	if file == nil || err != nil {
		return nil
	}
	localDefs := collectLocalDefNames(file)
	if _, shadowed := localDefs[target]; shadowed {
		return nil
	}
	lines := splitLines(src)
	var out []SourceReference
	syntax.Walk(file, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fn.(*syntax.Ident)
		if !ok {
			return true
		}
		if ident.Name != target {
			return true
		}
		out = append(out, identReference(ident, lines))
		return true
	})
	return out
}

// FindVariableReferences walks src for variable-name-taking builtin calls
// (get_var, set_var, get_num, …) whose first positional argument is a
// string literal equal to target. The whole string literal — quotes
// included — is returned as the reference span so a rename replace can
// cover the entire token.
func FindVariableReferences(src, target string) []SourceReference {
	if src == "" || target == "" {
		return nil
	}
	stripped, _ := plc.StripAnnotations(src)
	file, err := syntax.Parse("program.star", stripped, 0)
	if file == nil || err != nil {
		return nil
	}
	lines := splitLines(src)
	var out []SourceReference
	syntax.Walk(file, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fn.(*syntax.Ident)
		if !ok {
			return true
		}
		if !variableArgFuncs[ident.Name] {
			return true
		}
		if len(call.Args) == 0 {
			return true
		}
		lit, ok := firstPositionalLiteral(call.Args)
		if !ok {
			return true
		}
		if s, ok := lit.Value.(string); !ok || s != target {
			return true
		}
		out = append(out, literalReference(lit, lines))
		return true
	})
	return out
}

// firstPositionalLiteral returns the first argument when it is a bare
// string literal (i.e. not a keyword `name=` form). Keyword first args are
// legal Starlark but nobody writes `get_var(name="x")` in practice.
func firstPositionalLiteral(args []syntax.Expr) (*syntax.Literal, bool) {
	if len(args) == 0 {
		return nil, false
	}
	if _, isKw := args[0].(*syntax.BinaryExpr); isKw {
		return nil, false
	}
	lit, ok := args[0].(*syntax.Literal)
	if !ok || lit.Token != syntax.STRING {
		return nil, false
	}
	return lit, true
}

func identReference(ident *syntax.Ident, lines []string) SourceReference {
	line := int(ident.NamePos.Line)
	startCol := int(ident.NamePos.Col)
	return SourceReference{
		Line:     line,
		StartCol: startCol,
		EndCol:   startCol + len(ident.Name),
		LineText: lineAt(lines, line),
	}
}

func literalReference(lit *syntax.Literal, lines []string) SourceReference {
	start, end := lit.Span()
	return SourceReference{
		Line:     int(start.Line),
		StartCol: int(start.Col),
		EndCol:   int(end.Col),
		LineText: lineAt(lines, int(start.Line)),
	}
}

// splitLines splits on \n but keeps carriage returns off the end so the
// line text rendered in the UI matches what the user sees.
func splitLines(s string) []string {
	raw := strings.Split(s, "\n")
	for i, l := range raw {
		raw[i] = strings.TrimRight(l, "\r")
	}
	return raw
}

func lineAt(lines []string, line1Based int) string {
	i := line1Based - 1
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}
