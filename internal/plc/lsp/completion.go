//go:build plc || all

package lsp

import (
	"fmt"
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
func completeStarlark(source string, pos Position) CompletionList {
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
