//go:build plc || all

package lsp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc"
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
func completeStarlark(source string, pos Position, provider SymbolProvider, currentProgram, lang string) CompletionList {
	// Inside a variable-name string argument (e.g. `get_num("|")`), return
	// the list of known PLC variables instead of the builtin-heavy default.
	if list, ok := argumentCompletion(source, pos, provider); ok {
		return list
	}
	// Member access (`expr.`) short-circuits the general list: if we can
	// resolve the expression to a template, returning only its fields and
	// methods is more useful than drowning the user in builtins.
	if list, ok := memberCompletion(source, pos, provider); ok {
		return list
	}

	items := make([]CompletionItem, 0, 64)

	// 1. Builtins. Test-scoped ones are skipped unless this is a test doc.
	for _, b := range catalog {
		if !builtinAvailable(b, lang) {
			continue
		}
		items = append(items, builtinToCompletionItem(b))
	}

	// 2. Cross-program functions. Offer any other saved program with a
	// declared signature as a callable — detail shows the call form so the
	// user can read the signature without hovering.
	localNames := make(map[string]struct{})
	locals := collectLocalSymbols(source, pos)
	for _, name := range locals {
		localNames[name] = struct{}{}
	}
	if provider != nil {
		for _, fname := range provider.FunctionNames() {
			if fname == currentProgram {
				continue
			}
			if _, shadowed := localNames[fname]; shadowed {
				continue
			}
			info := provider.Function(fname)
			if info == nil {
				continue
			}
			items = append(items, functionToCompletionItem(info))
		}
	}

	// 3. Local symbols. Parse-and-scan: if parsing fails (common while the
	// user is mid-type), we fall back to the partial AST the parser returns
	// alongside the error.
	for _, name := range locals {
		items = append(items, CompletionItem{
			Label:            name,
			Kind:             CompletionKindVariable,
			Detail:           "local",
			InsertText:       name,
			InsertTextFormat: InsertTextFormatPlainText,
			SortText:         "2" + name, // sort after builtins (0) and fns (1)
		})
	}

	// 4. Keywords — small list of the ones users type often.
	for _, kw := range starlarkKeywords {
		items = append(items, CompletionItem{
			Label:            kw,
			Kind:             CompletionKindKeyword,
			Detail:           "keyword",
			InsertText:       kw,
			InsertTextFormat: InsertTextFormatPlainText,
			SortText:         "3" + kw,
		})
	}

	return CompletionList{IsIncomplete: false, Items: items}
}

// functionToCompletionItem renders a cross-program function as a snippet
// that lands the cursor in the first required param. Detail shows the
// call signature; documentation shows the description.
func functionToCompletionItem(info *FunctionInfo) CompletionItem {
	signature := formatFunctionSignature(info)
	insert := formatFunctionSnippet(info)
	doc := info.Description
	if info.Program != "" && info.Description == "" {
		doc = "Defined in program `" + info.Program + "`."
	} else if info.Program != "" {
		doc = info.Description + "\n\n*Defined in program `" + info.Program + "`.*"
	}
	if !info.HasSignature {
		// Land the cursor between the parens so the user can type args even
		// though we have no declared param list.
		insert = info.Name + "(${0})"
	}
	return CompletionItem{
		Label:            info.Name,
		Kind:             CompletionKindFunction,
		Detail:           signature,
		Documentation:    doc,
		InsertText:       insert,
		InsertTextFormat: InsertTextFormatSnippet,
		SortText:         "1" + info.Name,
	}
}

// formatFunctionSignature returns `name(p1: number, p2: string?) -> bool`.
// Optional params are suffixed with `?` so the user can see at a glance
// which args can be omitted.
func formatFunctionSignature(info *FunctionInfo) string {
	var sb strings.Builder
	sb.WriteString(info.Name)
	sb.WriteByte('(')
	for i, p := range info.Params {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(p.Name)
		if p.Type != "" {
			sb.WriteString(": ")
			sb.WriteString(p.Type)
		}
		if !p.Required {
			sb.WriteByte('?')
		}
	}
	sb.WriteByte(')')
	if info.Returns != nil && info.Returns.Type != "" {
		sb.WriteString(" -> ")
		sb.WriteString(info.Returns.Type)
	}
	return sb.String()
}

// formatFunctionSnippet emits `name($1, $2)` with placeholders for each
// required param. Optional params are omitted from the snippet — the user
// can add them explicitly if needed.
func formatFunctionSnippet(info *FunctionInfo) string {
	var sb strings.Builder
	sb.WriteString(info.Name)
	sb.WriteByte('(')
	placeholder := 1
	for _, p := range info.Params {
		if !p.Required {
			continue
		}
		if placeholder > 1 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "${%d:%s}", placeholder, p.Name)
		placeholder++
	}
	sb.WriteString(")$0")
	return sb.String()
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

	// Annotations are stripped before parsing (positions preserved) so
	// Starlark's parser isn't thrown by `def f(x: int):`.
	source, _ = plc.StripAnnotations(source)
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

// ─── Variable-name argument completion ─────────────────────────────────

// variableArgFuncs lists the builtins whose first positional argument is a
// PLC variable name. When the cursor sits inside the first string literal
// of a call to one of these, completion suggests variable names.
var variableArgFuncs = map[string]bool{
	"get_var": true, "get_num": true, "get_bool": true, "get_str": true,
	"set_var": true,
	"NO":      true, "NC": true,
	"OTE":     true, "OTL": true, "OTU": true,
	"TON":     true, "TOF": true,
	"CTU":     true, "CTD": true,
	"RES":     true,
}

// argumentCompletion returns known PLC variable names when the cursor sits
// inside the first string literal of a variable-name-taking builtin call
// (e.g. `get_num("|")`, `set_var("|", 1)`).
//
// Text-based: the AST is usually broken mid-type, so we walk the current
// line's string state forward to decide whether we're inside a string, and
// then look at whatever precedes the opening quote to identify the call.
func argumentCompletion(source string, pos Position, provider SymbolProvider) (CompletionList, bool) {
	if provider == nil {
		return CompletionList{}, false
	}
	line := lineAtIndex(source, pos.Line)
	if pos.Character < 0 || pos.Character > len(line) {
		return CompletionList{}, false
	}
	inStr, _, openCol := stringContextAt(line, pos.Character)
	if !inStr {
		return CompletionList{}, false
	}
	before := strings.TrimRight(line[:openCol], " \t")
	if !strings.HasSuffix(before, "(") {
		return CompletionList{}, false
	}
	before = strings.TrimRight(before[:len(before)-1], " \t")
	end := len(before)
	start := end
	for start > 0 && isIdentByte(before[start-1]) {
		start--
	}
	if start == end {
		return CompletionList{}, false
	}
	funcName := before[start:end]
	if !variableArgFuncs[funcName] {
		return CompletionList{}, false
	}

	names := provider.VariableNames()
	sort.Strings(names)
	items := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		detail := "variable"
		if info := provider.Variable(name); info != nil {
			if info.TemplateName != "" {
				detail = info.TemplateName
			} else if info.Datatype != "" {
				detail = info.Datatype
			}
		}
		items = append(items, CompletionItem{
			Label:            name,
			Kind:             CompletionKindVariable,
			Detail:           detail,
			InsertText:       name,
			InsertTextFormat: InsertTextFormatPlainText,
			SortText:         "0" + name,
		})
	}
	return CompletionList{IsIncomplete: false, Items: items}, true
}

// stringContextAt reports whether col on line is inside a single-line
// string literal. When inside, it also returns the quote character and the
// 0-based column of the opening quote. Handles `\`-escaped quotes.
func stringContextAt(line string, col int) (inside bool, quote byte, openCol int) {
	if col > len(line) {
		col = len(line)
	}
	var q byte
	open := -1
	for i := 0; i < col; i++ {
		c := line[i]
		if q != 0 {
			if c == '\\' && i+1 < col {
				i++
				continue
			}
			if c == q {
				q = 0
				open = -1
			}
			continue
		}
		if c == '"' || c == '\'' {
			q = c
			open = i
		}
	}
	if q == 0 {
		return false, 0, -1
	}
	return true, q, open
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
	templateName, ok := resolveExprTemplate(source, pos, beforeDot, provider)
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
// member-access dot) to a template name. Recognized forms:
//
//   - A direct `get_var("NAME")` call — look up NAME.
//   - A bare identifier — first, check whether the identifier is a
//     parameter of the containing def whose type annotation names a
//     template (`def f(motor: Motor): motor.`). Failing that, search for
//     an assignment `IDENT = get_var("NAME")` in the same source.
//
// Anything more complex (chained calls, attribute access, arithmetic)
// returns false; supporting those would require real type inference and
// is a follow-up.
func resolveExprTemplate(source string, pos Position, expr string, provider SymbolProvider) (string, bool) {
	expr = strings.TrimSpace(expr)
	if m := getVarCallRe.FindStringSubmatch(expr); m != nil {
		return variableTemplate(provider, m[1])
	}
	if identRe.MatchString(expr) {
		if tmpl, ok := findParamTemplate(source, pos, expr, provider); ok {
			return tmpl, true
		}
		if name, ok := findGetVarAssignment(source, expr); ok {
			return variableTemplate(provider, name)
		}
	}
	return "", false
}

// findParamTemplate resolves a bare identifier to a template name by
// looking at the param-type annotation on the enclosing def header.
// Returns the template name only when it refers to a template known to
// the provider — unknown annotation types (`int`, `str`, or a typo)
// return false so the caller can keep searching.
func findParamTemplate(source string, pos Position, ident string, provider SymbolProvider) (string, bool) {
	cursorLine := pos.Line + 1
	stripped, sigs := plc.StripAnnotations(source)
	if len(sigs) == 0 {
		return "", false
	}
	f, err := syntax.Parse("program.star", stripped, 0)
	if f == nil && err != nil {
		return "", false
	}
	// Walk to find the innermost def whose body contains the cursor and
	// whose parameter list includes ident. Nested defs are rare in PLC
	// code but handled correctly — each matching def overwrites the
	// previous hit as Walk descends.
	var hit string
	syntax.Walk(f, func(n syntax.Node) bool {
		if n == nil {
			return false
		}
		d, ok := n.(*syntax.DefStmt)
		if !ok {
			return true
		}
		if !containsLine(d, cursorLine) {
			return true
		}
		for _, p := range d.Params {
			if paramName(p) == ident {
				hit = d.Name.Name
				break
			}
		}
		return true
	})
	if hit == "" {
		return "", false
	}
	for _, sig := range sigs {
		if sig.Name != hit {
			continue
		}
		for _, p := range sig.Params {
			if p.Name != ident || !p.HasType {
				continue
			}
			if provider.Template(p.Type) == nil {
				return "", false
			}
			return p.Type, true
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
