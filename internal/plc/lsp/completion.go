//go:build plc || all

package lsp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.starlark.net/syntax"
)

// completeStarlark builds a CompletionList for the given document at the
// given position. Sources, in order of priority:
//
//   1. PLC builtins (get_var, log, NO, ...) — always available, authoritative
//      docs live in the catalog.
//   2. Local symbols resolved from the AST — function defs, top-level
//      assignments, and names introduced by the containing function's
//      parameters / for-loops. Scoped to the cursor's enclosing function
//      when possible.
//   3. Starlark keywords that make sense in an identifier-expected context.
//
// We return IsIncomplete=false so the client caches the list until the
// next trigger; filtering by prefix happens client-side.
func completeStarlark(source string, pos Position, provider SymbolProvider) CompletionList {
	// Member access (`expr.`) short-circuits the general list: if we can
	// resolve the expression to a template, returning only its fields and
	// methods is more useful than drowning the user in builtins.
	if list, ok := memberCompletion(source, pos, provider); ok {
		return list
	}

	items := make([]CompletionItem, 0, 64)

	// 1. Builtins. These always apply.
	for _, b := range catalog {
		items = append(items, builtinToCompletionItem(b))
	}

	// 2. Local symbols. Parse-and-scan: if parsing fails (common while the
	// user is mid-type), we fall back to the partial AST the parser returns
	// alongside the error.
	locals := collectLocalSymbols(source, pos)
	for _, name := range locals {
		items = append(items, CompletionItem{
			Label:            name,
			Kind:             CompletionKindVariable,
			Detail:           "local",
			InsertText:       name,
			InsertTextFormat: InsertTextFormatPlainText,
			SortText:         "1" + name, // sort after builtins (which use "0")
		})
	}

	// 3. Keywords — small list of the ones users type often.
	for _, kw := range starlarkKeywords {
		items = append(items, CompletionItem{
			Label:            kw,
			Kind:             CompletionKindKeyword,
			Detail:           "keyword",
			InsertText:       kw,
			InsertTextFormat: InsertTextFormatPlainText,
			SortText:         "2" + kw,
		})
	}

	return CompletionList{IsIncomplete: false, Items: items}
}

func builtinToCompletionItem(b Builtin) CompletionItem {
	kind := CompletionKindFunction
	if b.Kind == "ladder" {
		kind = CompletionKindEvent
	}
	insert := b.InsertText
	format := InsertTextFormatSnippet
	if insert == "" {
		insert = b.Name
		format = InsertTextFormatPlainText
	}
	docParts := []string{}
	if b.Category != "" {
		docParts = append(docParts, "*"+b.Category+"*")
	}
	if b.Doc != "" {
		docParts = append(docParts, b.Doc)
	}
	return CompletionItem{
		Label:            b.Name,
		Kind:             kind,
		Detail:           b.Signature,
		Documentation:    strings.Join(docParts, "\n\n"),
		InsertText:       insert,
		InsertTextFormat: format,
		SortText:         "0" + b.Name,
	}
}

var starlarkKeywords = []string{
	"and", "break", "continue", "def", "elif", "else", "for",
	"if", "in", "lambda", "not", "or", "pass", "return", "True", "False", "None",
}

// collectLocalSymbols parses source and walks the AST, collecting names
// that would be visible at the given cursor position. We deliberately
// over-include rather than compute exact scope — wrong scope in completion
// is annoying (missing names), over-including at most shows a stale name
// that won't resolve at runtime (user notices immediately).
func collectLocalSymbols(source string, pos Position) []string {
	// 1-based line for Starlark positions.
	cursorLine := pos.Line + 1

	f, err := syntax.Parse("program.star", source, 0)
	if f == nil && err != nil {
		// Parsing completely failed — give up on locals; builtins are still
		// useful. A smarter strategy (truncate to pos line and retry) can be
		// added if this produces noticeably worse completion.
		return nil
	}

	seen := map[string]struct{}{}
	add := func(n string) {
		if n == "" {
			return
		}
		seen[n] = struct{}{}
	}

	syntax.Walk(f, func(n syntax.Node) bool {
		if n == nil {
			return false
		}
		switch x := n.(type) {
		case *syntax.DefStmt:
			add(x.Name.Name)
			// Include parameters only if the cursor is inside this def's body.
			if containsLine(x, cursorLine) {
				for _, p := range x.Params {
					add(paramName(p))
				}
			}
		case *syntax.AssignStmt:
			collectAssignTargets(x.LHS, add)
		case *syntax.ForStmt:
			collectAssignTargets(x.Vars, add)
		case *syntax.Comprehension:
			for _, c := range x.Clauses {
				if fc, ok := c.(*syntax.ForClause); ok {
					collectAssignTargets(fc.Vars, add)
				}
			}
		}
		return true
	})

	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// collectAssignTargets extracts identifier names from the left-hand side
// of an assignment. Handles plain names and tuple unpacking.
func collectAssignTargets(e syntax.Expr, add func(string)) {
	switch x := e.(type) {
	case *syntax.Ident:
		add(x.Name)
	case *syntax.TupleExpr:
		for _, sub := range x.List {
			collectAssignTargets(sub, add)
		}
	case *syntax.ListExpr:
		for _, sub := range x.List {
			collectAssignTargets(sub, add)
		}
	case *syntax.ParenExpr:
		collectAssignTargets(x.X, add)
	}
}

func paramName(p syntax.Expr) string {
	switch x := p.(type) {
	case *syntax.Ident:
		return x.Name
	case *syntax.BinaryExpr: // default value: NAME = expr
		if id, ok := x.X.(*syntax.Ident); ok {
			return id.Name
		}
	case *syntax.UnaryExpr: // *args, **kwargs
		if id, ok := x.X.(*syntax.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func containsLine(n syntax.Node, line int) bool {
	start, end := n.Span()
	return int(start.Line) <= line && int(end.Line) >= line
}

// ─── Member access completion ───────────────────────────────────────────

// getVarCallRe matches a single complete `get_var("NAME")` call with no
// trailing junk. Used to recognize the direct pattern
// `get_var("motor1").`.
var getVarCallRe = regexp.MustCompile(`^get_var\s*\(\s*["']([^"']+)["']\s*\)$`)

// identRe matches a bare identifier, for the indirect case where the user
// writes `m = get_var("motor1")` and later `m.`.
var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// memberCompletion returns the fields + methods of the template on the
// left-hand side of a member access immediately preceding the cursor.
// Returns (_, false) when the context isn't a member access we can
// resolve — callers fall back to the general completion list.
//
// We deliberately use text-based detection rather than the Starlark AST:
// by the time the user types `.`, the document almost always fails to
// parse (the attr name is missing), so the AST loses the expression we
// need. Looking backwards from the cursor over a single line is both
// simpler and more robust here.
func memberCompletion(source string, pos Position, provider SymbolProvider) (CompletionList, bool) {
	if provider == nil {
		return CompletionList{}, false
	}
	line := lineAtIndex(source, pos.Line)
	if pos.Character < 0 || pos.Character > len(line) {
		return CompletionList{}, false
	}
	prefix := line[:pos.Character]
	// Strip any partial attribute name the user has already typed after
	// the dot (`motor1.spe|` → trim back to `motor1.`).
	end := len(prefix)
	for end > 0 && isIdentByte(prefix[end-1]) {
		end--
	}
	if end == 0 || prefix[end-1] != '.' {
		return CompletionList{}, false
	}
	beforeDot := strings.TrimRight(prefix[:end-1], " \t")
	templateName, ok := resolveExprTemplate(source, beforeDot, provider)
	if !ok {
		return CompletionList{}, false
	}
	tmpl := provider.Template(templateName)
	if tmpl == nil {
		return CompletionList{}, false
	}
	items := make([]CompletionItem, 0, len(tmpl.Fields)+len(tmpl.Methods))
	for _, f := range tmpl.Fields {
		detail := f.Name + ": " + f.Type
		if f.Unit != "" {
			detail += " (" + f.Unit + ")"
		}
		items = append(items, CompletionItem{
			Label:            f.Name,
			Kind:             CompletionKindVariable,
			Detail:           detail,
			Documentation:    f.Description,
			InsertText:       f.Name,
			InsertTextFormat: InsertTextFormatPlainText,
			SortText:         "0" + f.Name,
		})
	}
	for _, m := range tmpl.Methods {
		items = append(items, CompletionItem{
			Label:            m.Name,
			Kind:             CompletionKindFunction,
			Detail:           m.Name + "()",
			InsertText:       m.Name + "($0)",
			InsertTextFormat: InsertTextFormatSnippet,
			SortText:         "1" + m.Name,
		})
	}
	return CompletionList{IsIncomplete: false, Items: items}, true
}

// resolveExprTemplate maps a simple expression (the text just before a
// member-access dot) to a template name. Two forms are recognized:
//
//   - A direct `get_var("NAME")` call — look up NAME.
//   - A bare identifier — search the source for an assignment of the
//     form `IDENT = get_var("NAME")` and resolve that.
//
// Anything more complex (chained calls, attribute access, arithmetic)
// returns false; supporting those would require real type inference and
// is a follow-up.
func resolveExprTemplate(source, expr string, provider SymbolProvider) (string, bool) {
	expr = strings.TrimSpace(expr)
	if m := getVarCallRe.FindStringSubmatch(expr); m != nil {
		return variableTemplate(provider, m[1])
	}
	if identRe.MatchString(expr) {
		if name, ok := findGetVarAssignment(source, expr); ok {
			return variableTemplate(provider, name)
		}
	}
	return "", false
}

func variableTemplate(provider SymbolProvider, varName string) (string, bool) {
	v := provider.Variable(varName)
	if v == nil || v.TemplateName == "" {
		return "", false
	}
	return v.TemplateName, true
}

// findGetVarAssignment scans source for `ident = get_var("NAME")` and
// returns NAME. Picks the last assignment if several exist — that matches
// ordinary Python/Starlark shadowing semantics well enough for
// completion. Returns ("", false) when no matching assignment is found.
func findGetVarAssignment(source, ident string) (string, bool) {
	// Build a dedicated regex per ident; identRe upstream already
	// guarantees `ident` is a safe identifier, so regex-quoting is
	// unnecessary.
	re := regexp.MustCompile(`(?m)(?:^|[^A-Za-z0-9_])` + ident + `\s*=\s*get_var\s*\(\s*["']([^"']+)["']\s*\)`)
	matches := re.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return "", false
	}
	last := matches[len(matches)-1]
	return last[1], true
}

// lineAtIndex returns the 0-indexed source line. Out-of-range indices
// return an empty string; the caller will simply produce no completion.
func lineAtIndex(source string, line int) string {
	if line < 0 {
		return ""
	}
	start := 0
	current := 0
	for i := 0; i < len(source); i++ {
		if source[i] == '\n' {
			if current == line {
				return source[start:i]
			}
			current++
			start = i + 1
		}
	}
	if current == line {
		return source[start:]
	}
	return ""
}

func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// formatHoverMarkdown renders a builtin as a markdown block suitable for a
// hover tooltip. Declared here so hover.go and completion code share the
// same shape.
func formatHoverMarkdown(b Builtin) string {
	var sb strings.Builder
	sb.WriteString("```starlark\n")
	sb.WriteString(b.Signature)
	sb.WriteString("\n```")
	if b.Category != "" {
		fmt.Fprintf(&sb, "\n\n*%s*", b.Category)
	}
	if b.Doc != "" {
		sb.WriteString("\n\n")
		sb.WriteString(b.Doc)
	}
	return sb.String()
}
