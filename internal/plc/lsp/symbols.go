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

// FunctionParam mirrors a single parameter declared on a function
// signature. Optional params carry Required=false; when a default value is
// known it's stored separately so completion can render it.
type FunctionParam struct {
	Name        string
	Type        string // "number" | "boolean" | "string" | UDT template name
	Description string
	Required    bool
}

// FunctionReturn describes the shape returned by a function.
type FunctionReturn struct {
	Type        string
	Description string
}

// FunctionInfo captures the cross-program view of a top-level callable.
// The LSP uses this for completion, hover, and arity diagnostics.
//
// Program is the owning program's name — useful for disambiguating in
// hover text and for skipping self-references at the call site.
//
// HasSignature distinguishes "function declared with empty param list" from
// "function has no declared signature at all" — the latter should skip
// arity checking so users aren't punished for not filling in metadata.
type FunctionInfo struct {
	Name         string
	Program      string
	Description  string
	Params       []FunctionParam
	Returns      *FunctionReturn
	HasSignature bool
}

// SymbolProvider surfaces PLC-scoped knowledge — variables, templates, and
// other programs' top-level functions — to the LSP. Implementations
// typically read from the KV bus; calls happen on completion and hover, so
// lightweight caching is welcome but not required.
//
// All lookups return nil when the name is unknown. A nil SymbolProvider
// is a valid configuration: completion degrades to builtins and locals
// only, which matches the pre-provider behavior.
type SymbolProvider interface {
	Variable(name string) *VariableInfo
	Template(name string) *TemplateInfo
	// VariableNames returns all configured PLC variable names for the
	// current PLC. Order is unspecified; callers that need stable ordering
	// should sort.
	VariableNames() []string
	// Function returns the signature for a top-level function exported by
	// any program in the current PLC. nil when no such function is known
	// or the owning program has no saved signature.
	Function(name string) *FunctionInfo
	// FunctionNames returns every function name with a known signature,
	// across all programs. Order unspecified.
	FunctionNames() []string
}
