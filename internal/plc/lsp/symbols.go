//go:build plc || all

package lsp

// VariableInfo is the PLC-scoped view of a user-declared variable. Only the
// fields the completion engine needs are surfaced; everything else stays in
// the KV config.
//
// TemplateName is non-empty when the variable is typed to a template
// (struct). For atomic variables (number/boolean/string/bytes) it is empty
// and Datatype holds the primitive name.
type VariableInfo struct {
	Name         string
	Datatype     string
	TemplateName string
}

// TemplateField mirrors the subset of itypes.PlcTemplateField that
// completion cares about. Kept here (rather than importing itypes) so the
// lsp package stays free of PLC-internal types and remains trivially
// testable.
type TemplateField struct {
	Name        string
	Type        string
	Description string
	Unit        string
}

// TemplateMethod mirrors the subset of itypes.PlcTemplateMethod that
// completion cares about.
type TemplateMethod struct {
	Name string
}

// TemplateInfo is a minimal description of a template's shape.
type TemplateInfo struct {
	Name    string
	Fields  []TemplateField
	Methods []TemplateMethod
}

// SymbolProvider surfaces PLC-scoped knowledge — currently variables and
// templates — to the LSP. Implementations typically read from the KV bus;
// calls happen on completion and hover, so lightweight caching is welcome
// but not required.
//
// Both lookups return nil when the name is unknown. A nil SymbolProvider
// is a valid configuration: completion degrades to builtins and locals
// only, which matches the pre-provider behavior.
type SymbolProvider interface {
	Variable(name string) *VariableInfo
	Template(name string) *TemplateInfo
	// VariableNames returns all configured PLC variable names for the
	// current PLC. Order is unspecified; callers that need stable ordering
	// should sort.
	VariableNames() []string
}
