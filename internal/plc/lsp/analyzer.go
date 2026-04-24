//go:build plc || all

package lsp

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc"
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
	return AnalyzeWithProvider(source, languageID, nil, "")
}

// AnalyzeWithProvider is the provider-aware variant. When provider is
// non-nil, Starlark analysis additionally flags arity mismatches at call
// sites that reference a known cross-program function (i.e. another
// saved program whose signature is declared).
func AnalyzeWithProvider(source, languageID string, provider SymbolProvider, currentProgram string) []Diagnostic {
	switch strings.ToLower(languageID) {
	case "st", "structured-text":
		return analyzeST(source)
	case "starlark", "python", "":
		return analyzeStarlark(source, provider, currentProgram)
	default:
		return nil
	}
}

func analyzeStarlark(source string, provider SymbolProvider, currentProgram string) []Diagnostic {
	if source == "" {
		return nil
	}
	// Strip Python-style annotations so Starlark's parser doesn't complain
	// about them. Byte offsets are preserved, so any diagnostics the parser
	// produces still land at the user's original caret. The extracted
	// signatures carry the return-type annotations (blanked from source),
	// which the return-type checker needs.
	source, sigs := plc.StripAnnotations(source)
	var parserDiags []Diagnostic
	file, err := syntax.Parse("program.star", source, 0)
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
	diags := mergePrepassDiagnostics(parserDiags, pre)
	// Signature-aware call-site diagnostics only run when the parse
	// succeeded (file != nil) and there's a provider to ask.
	if file != nil && provider != nil {
		diags = append(diags, analyzeCallSites(file, provider, currentProgram)...)
		diags = append(diags, analyzeReturnTypes(file, sigs, provider, currentProgram)...)
	}
	return diags
}

// analyzeCallSites walks CallExpr nodes whose callee is a bare identifier.
// When that identifier names a cross-program function with a declared
// signature, we check that the positional + keyword argument count falls
// within the signature's [required, total] range. Mis-naming a keyword
// argument is also flagged.
//
// Local defs take precedence — when the current source defines a function
// with the same name, we skip the check to avoid arguing with the user's
// own helper.
func analyzeCallSites(file *syntax.File, provider SymbolProvider, currentProgram string) []Diagnostic {
	localDefs := collectLocalDefNames(file)
	env := newTypeEnv(file, provider, currentProgram)
	var diags []Diagnostic
	syntax.Walk(file, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fn.(*syntax.Ident)
		if !ok {
			return true
		}
		name := ident.Name
		if name == currentProgram {
			return true
		}
		if _, shadowed := localDefs[name]; shadowed {
			return true
		}
		info := provider.Function(name)
		if info == nil || !info.HasSignature {
			return true
		}
		if d, ok := checkCallArity(call, ident, info); ok {
			diags = append(diags, d...)
		}
		diags = append(diags, checkCallTypes(call, info, env)...)
		return true
	})
	return diags
}

func collectLocalDefNames(file *syntax.File) map[string]struct{} {
	out := make(map[string]struct{})
	syntax.Walk(file, func(n syntax.Node) bool {
		if def, ok := n.(*syntax.DefStmt); ok && def.Name != nil {
			out[def.Name.Name] = struct{}{}
		}
		return true
	})
	return out
}

// checkCallArity returns arity/keyword-name diagnostics for a call whose
// callee is known to be `info`. The second return is false when the call
// should be skipped entirely (e.g. it uses *args/**kwargs which we can't
// statically verify against a param list without variadics).
func checkCallArity(call *syntax.CallExpr, ident *syntax.Ident, info *FunctionInfo) ([]Diagnostic, bool) {
	required := 0
	paramByName := make(map[string]struct{}, len(info.Params))
	for _, p := range info.Params {
		if p.Required {
			required++
		}
		paramByName[p.Name] = struct{}{}
	}
	total := len(info.Params)

	positional := 0
	keyword := 0
	var unknownKwargs []*syntax.BinaryExpr
	for _, arg := range call.Args {
		switch a := arg.(type) {
		case *syntax.UnaryExpr:
			// `*args` / `**kwargs` — can't statically check.
			return nil, false
		case *syntax.BinaryExpr:
			if a.Op == syntax.EQ {
				keyword++
				if idk, ok := a.X.(*syntax.Ident); ok {
					if _, known := paramByName[idk.Name]; !known {
						unknownKwargs = append(unknownKwargs, a)
					}
				}
				continue
			}
			positional++
		default:
			positional++
		}
	}

	var diags []Diagnostic
	callStart, callEnd := call.Span()
	given := positional + keyword
	sig := formatFunctionSignature(info)
	if given < required {
		diags = append(diags, warningDiag(
			int(ident.NamePos.Line), int(ident.NamePos.Col),
			int(callEnd.Line), int(callEnd.Col),
			fmt.Sprintf("`%s` expects at least %d argument(s) but got %d. Signature: %s",
				info.Name, required, given, sig),
		))
	} else if given > total {
		diags = append(diags, warningDiag(
			int(ident.NamePos.Line), int(ident.NamePos.Col),
			int(callEnd.Line), int(callEnd.Col),
			fmt.Sprintf("`%s` expects at most %d argument(s) but got %d. Signature: %s",
				info.Name, total, given, sig),
		))
	}
	for _, kw := range unknownKwargs {
		kwStart, kwEnd := kw.Span()
		idk := kw.X.(*syntax.Ident)
		diags = append(diags, warningDiag(
			int(kwStart.Line), int(kwStart.Col),
			int(kwEnd.Line), int(kwEnd.Col),
			fmt.Sprintf("`%s` has no parameter named `%s`. Signature: %s",
				info.Name, idk.Name, sig),
		))
	}
	_ = callStart
	return diags, true
}

func warningDiag(startLine, startCol, endLine, endCol int, message string) Diagnostic {
	d := rangeDiag(startLine, startCol, endLine, endCol, message)
	d.Severity = SeverityWarning
	return d
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
