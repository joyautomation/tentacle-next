//go:build plc || all

package lsp

import (
	"sort"

	"github.com/joyautomation/tentacle/internal/plc/ir"
	"github.com/joyautomation/tentacle/internal/plc/st"
)

// completeST returns a completion list for ST source. It mixes three
// categories so the popup behaves like other LSP-driven editors users
// already know:
//
//   - Variables declared in any VAR block (with their type as detail).
//   - Built-in stateless functions (ABS, MIN, MAX, conversions, ...).
//   - Built-in function-block types (TON, R_TRIG, CTU, ...).
//   - The handful of ST keywords that are useful inside a body.
//
// The list is intentionally not context-sensitive yet — it doesn't try
// to filter by what's reachable from the cursor. That refinement comes
// when we wire the typechecker's symbol scope into the request handler;
// for now a flat list is far better than the empty stub it replaces.
func completeST(source string) CompletionList {
	items := []CompletionItem{}
	prog, err := st.Parse(source)
	if err == nil && prog != nil {
		seen := map[string]bool{}
		for _, vb := range prog.VarBlocks {
			for _, vd := range vb.Variables {
				if seen[vd.Name] {
					continue
				}
				seen[vd.Name] = true
				items = append(items, CompletionItem{
					Label:    vd.Name,
					Kind:     CompletionKindVariable,
					Detail:   vd.Datatype,
					SortText: "0_" + vd.Name,
				})
			}
		}
	}
	for name := range ir.Builtins {
		items = append(items, CompletionItem{
			Label:            name,
			Kind:             CompletionKindFunction,
			Detail:           "built-in function",
			InsertText:       name + "($0)",
			InsertTextFormat: InsertTextFormatSnippet,
			SortText:         "1_" + name,
		})
	}
	for name := range ir.FBs {
		items = append(items, CompletionItem{
			Label:    name,
			Kind:     CompletionKindStruct,
			Detail:   "function block",
			SortText: "2_" + name,
		})
	}
	for _, kw := range stKeywords {
		items = append(items, CompletionItem{
			Label:    kw,
			Kind:     CompletionKindKeyword,
			SortText: "3_" + kw,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].SortText < items[j].SortText })
	return CompletionList{IsIncomplete: false, Items: items}
}

// stKeywords are the ST keywords most likely to appear mid-program. The
// intent isn't to be exhaustive — VAR / END_PROGRAM live in fixed slots
// users don't usually type by hand once a file exists.
var stKeywords = []string{
	"IF", "THEN", "ELSE", "ELSIF", "END_IF",
	"FOR", "TO", "BY", "DO", "END_FOR",
	"WHILE", "END_WHILE",
	"REPEAT", "UNTIL", "END_REPEAT",
	"CASE", "OF", "END_CASE",
	"RETURN", "EXIT", "CONTINUE",
	"AND", "OR", "XOR", "NOT", "MOD",
	"TRUE", "FALSE",
}
