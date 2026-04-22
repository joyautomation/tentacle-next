//go:build plc || all

package lsp

// Builtin describes a PLC-specific Starlark builtin that program authors
// can call. The catalog drives completion, hover, and (later) signature
// help. It is deliberately hand-authored rather than reflected from the
// runtime registration: LSP needs human-written docs, not function names.
//
// Keep entries terse — one or two sentences. Author-facing text is
// rendered in CodeMirror tooltips and completion popups, so it must be
// legible at a glance.
type Builtin struct {
	Name       string   // identifier as seen in user code, e.g. "get_var"
	Kind       string   // "function", "ladder", "constant" — maps to LSP CompletionItemKind
	Signature  string   // one-line signature, e.g. `get_var(name: str) -> value`
	Params     []string // parameter names, for signature help later
	Category   string   // category label shown in completion detail
	Doc        string   // brief description, rendered in completion/hover popup
	InsertText string   // optional snippet; empty means insert Name as-is
	// Context limits where this builtin is surfaced. "" means everywhere
	// (all starlark documents). "test" means only in unit-test documents —
	// the client signals this by opening with languageId "starlark-test".
	Context string
}

// catalog is the single source of truth. Other packages must not mutate
// this slice — the LSP server treats it as read-only.
var catalog = []Builtin{
	// --- Tags / Variable Access ------------------------------------------
	{
		Name: "get_var", Kind: "function", Category: "Tags",
		Signature:  `get_var(name: str) -> value`,
		Params:     []string{"name"},
		InsertText: `get_var("$1")`,
		Doc:        "Read a PLC variable by name. Returns the current value with automatic type conversion (bool, number, string).",
	},
	{
		Name: "get_num", Kind: "function", Category: "Tags",
		Signature:  `get_num(name: str) -> float`,
		Params:     []string{"name"},
		InsertText: `get_num("$1")`,
		Doc:        "Read a numeric PLC variable by name. Returns the value as a float64.",
	},
	{
		Name: "get_bool", Kind: "function", Category: "Tags",
		Signature:  `get_bool(name: str) -> bool`,
		Params:     []string{"name"},
		InsertText: `get_bool("$1")`,
		Doc:        "Read a boolean PLC variable by name.",
	},
	{
		Name: "get_str", Kind: "function", Category: "Tags",
		Signature:  `get_str(name: str) -> str`,
		Params:     []string{"name"},
		InsertText: `get_str("$1")`,
		Doc:        "Read a string PLC variable by name.",
	},
	{
		Name: "set_var", Kind: "function", Category: "Tags",
		Signature:  `set_var(name: str, value: any)`,
		Params:     []string{"name", "value"},
		InsertText: `set_var("$1", $2)`,
		Doc:        "Write a value to a PLC variable. The value is coerced to the variable's declared type.",
	},

	// --- Logging --------------------------------------------------------
	{
		Name: "log", Kind: "function", Category: "Logging",
		Signature:  `log(*args)`,
		InsertText: `log($1)`,
		Doc:        "Log at INFO level. Arguments are stringified and joined with spaces.",
	},
	{
		Name: "log_debug", Kind: "function", Category: "Logging",
		Signature: `log_debug(*args)`,
		Doc:       "Log at DEBUG level.",
	},
	{
		Name: "log_info", Kind: "function", Category: "Logging",
		Signature: `log_info(*args)`,
		Doc:       "Log at INFO level.",
	},
	{
		Name: "log_warn", Kind: "function", Category: "Logging",
		Signature: `log_warn(*args)`,
		Doc:       "Log at WARN level.",
	},
	{
		Name: "log_error", Kind: "function", Category: "Logging",
		Signature: `log_error(*args)`,
		Doc:       "Log at ERROR level.",
	},

	// --- Math -----------------------------------------------------------
	{
		Name: "abs", Kind: "function", Category: "Math",
		Signature:  `abs(x: float) -> float`,
		Params:     []string{"x"},
		InsertText: `abs($1)`,
		Doc:        "Absolute value.",
	},
	{
		Name: "clamp", Kind: "function", Category: "Math",
		Signature:  `clamp(val: float, lo: float, hi: float) -> float`,
		Params:     []string{"val", "lo", "hi"},
		InsertText: `clamp($1, $2, $3)`,
		Doc:        "Constrain `val` to the inclusive range [lo, hi].",
	},
	{
		Name: "sqrt", Kind: "function", Category: "Math",
		Signature:  `sqrt(x: float) -> float`,
		Params:     []string{"x"},
		InsertText: `sqrt($1)`,
		Doc:        "Square root.",
	},
	{
		Name: "pow", Kind: "function", Category: "Math",
		Signature:  `pow(base: float, exp: float) -> float`,
		Params:     []string{"base", "exp"},
		InsertText: `pow($1, $2)`,
		Doc:        "Raise `base` to `exp`.",
	},

	// --- Ladder: Contacts ----------------------------------------------
	{
		Name: "NO", Kind: "ladder", Category: "Ladder · Contact",
		Signature: `NO(tag: str)`,
		Params:    []string{"tag"},
		Doc:       "Normally-Open contact. Energizes when `tag` is true.",
	},
	{
		Name: "NC", Kind: "ladder", Category: "Ladder · Contact",
		Signature: `NC(tag: str)`,
		Params:    []string{"tag"},
		Doc:       "Normally-Closed contact. Energizes when `tag` is false.",
	},

	// --- Ladder: Coils --------------------------------------------------
	{
		Name: "OTE", Kind: "ladder", Category: "Ladder · Coil",
		Signature: `OTE(tag: str)`,
		Params:    []string{"tag"},
		Doc:       "Output Energize. Sets `tag` true when the rung is energized, false when de-energized.",
	},
	{
		Name: "OTL", Kind: "ladder", Category: "Ladder · Coil",
		Signature: `OTL(tag: str)`,
		Params:    []string{"tag"},
		Doc:       "Output Latch. Sets `tag` true when the rung energizes; held until an `OTU` fires.",
	},
	{
		Name: "OTU", Kind: "ladder", Category: "Ladder · Coil",
		Signature: `OTU(tag: str)`,
		Params:    []string{"tag"},
		Doc:       "Output Unlatch. Sets `tag` false when the rung energizes.",
	},

	// --- Ladder: Timers & Counters --------------------------------------
	{
		Name: "TON", Kind: "ladder", Category: "Ladder · Timer",
		Signature: `TON(tag: str, preset_ms: int)`,
		Params:    []string{"tag", "preset_ms"},
		Doc:       "Timer-On-Delay. Counts up while enabled; sets `DN` when `ACC >= preset_ms`.",
	},
	{
		Name: "TOF", Kind: "ladder", Category: "Ladder · Timer",
		Signature: `TOF(tag: str, preset_ms: int)`,
		Params:    []string{"tag", "preset_ms"},
		Doc:       "Timer-Off-Delay. Counts while disabled; clears `DN` when `ACC >= preset_ms`.",
	},
	{
		Name: "CTU", Kind: "ladder", Category: "Ladder · Counter",
		Signature: `CTU(tag: str, preset: int)`,
		Params:    []string{"tag", "preset"},
		Doc:       "Count-Up counter. Increments on rising edge; sets `DN` when `ACC >= preset`.",
	},
	{
		Name: "CTD", Kind: "ladder", Category: "Ladder · Counter",
		Signature: `CTD(tag: str, preset: int)`,
		Params:    []string{"tag", "preset"},
		Doc:       "Count-Down counter. Decrements on rising edge; sets `DN` when `ACC <= 0`.",
	},
	{
		Name: "RES", Kind: "ladder", Category: "Ladder · Reset",
		Signature: `RES(tag: str)`,
		Params:    []string{"tag"},
		Doc:       "Reset. Clears `ACC` and `DN` for a timer or counter when the rung energizes.",
	},

	// --- Test assertions (only in test files) --------------------------
	{
		Name: "assert_eq", Kind: "function", Category: "Test · Assertion", Context: "test",
		Signature:  `assert_eq(actual, expected, msg: str = "")`,
		Params:     []string{"actual", "expected", "msg"},
		InsertText: `assert_eq($1, $2)`,
		Doc:        "Fail the test if `actual != expected`. Optional `msg` is prepended to the failure message.",
	},
	{
		Name: "assert_ne", Kind: "function", Category: "Test · Assertion", Context: "test",
		Signature:  `assert_ne(actual, expected, msg: str = "")`,
		Params:     []string{"actual", "expected", "msg"},
		InsertText: `assert_ne($1, $2)`,
		Doc:        "Fail the test if `actual == expected`.",
	},
	{
		Name: "assert_true", Kind: "function", Category: "Test · Assertion", Context: "test",
		Signature:  `assert_true(value, msg: str = "")`,
		Params:     []string{"value", "msg"},
		InsertText: `assert_true($1)`,
		Doc:        "Fail the test if `value` is falsy.",
	},
	{
		Name: "assert_false", Kind: "function", Category: "Test · Assertion", Context: "test",
		Signature:  `assert_false(value, msg: str = "")`,
		Params:     []string{"value", "msg"},
		InsertText: `assert_false($1)`,
		Doc:        "Fail the test if `value` is truthy.",
	},
	{
		Name: "assert_near", Kind: "function", Category: "Test · Assertion", Context: "test",
		Signature:  `assert_near(actual: float, expected: float, tolerance: float = 1e-9, msg: str = "")`,
		Params:     []string{"actual", "expected", "tolerance", "msg"},
		InsertText: `assert_near($1, $2, $3)`,
		Doc:        "Fail if `abs(actual - expected) > tolerance`. Use for floating-point comparisons where bit-exact equality is not reliable.",
	},
	{
		Name: "assert_raises", Kind: "function", Category: "Test · Assertion", Context: "test",
		Signature:  `assert_raises(fn: callable, msg: str = "")`,
		Params:     []string{"fn", "msg"},
		InsertText: `assert_raises(lambda: $1)`,
		Doc:        "Fail if `fn()` does NOT raise. Use `lambda` to defer invocation — e.g. `assert_raises(lambda: divide(1, 0))`.",
	},
	{
		Name: "fail", Kind: "function", Category: "Test · Assertion", Context: "test",
		Signature:  `fail(msg: str = "")`,
		Params:     []string{"msg"},
		InsertText: `fail($1)`,
		Doc:        "Fail the test immediately with the given message.",
	},

	// --- Ladder: Structure ---------------------------------------------
	{
		Name: "branch", Kind: "ladder", Category: "Ladder · Structure",
		Signature: `branch(*elements)`,
		Doc:       "Parallel paths (logical OR). Accepts any number of contacts or series/branch groupings.",
	},
	{
		Name: "series", Kind: "ladder", Category: "Ladder · Structure",
		Signature: `series(*elements)`,
		Doc:       "Sequential chain (logical AND). Accepts any number of contacts or branch groupings.",
	},
	{
		Name: "rung", Kind: "ladder", Category: "Ladder · Execution",
		Signature: `rung(*elements)`,
		Doc:       "Evaluate a complete ladder rung. Conditions are AND-evaluated; outputs fire when the rung is energized.",
	},
}

// BuiltinsByName returns the catalog as a map keyed by Name.
func BuiltinsByName() map[string]Builtin {
	m := make(map[string]Builtin, len(catalog))
	for _, b := range catalog {
		m[b.Name] = b
	}
	return m
}

// Builtins returns a read-only copy of the catalog.
func Builtins() []Builtin {
	out := make([]Builtin, len(catalog))
	copy(out, catalog)
	return out
}

// builtinAvailable reports whether a builtin should be surfaced for a given
// document language. Test-scoped builtins (assert_eq, fail, ...) only appear
// in "starlark-test" documents; everything else shows everywhere.
func builtinAvailable(b Builtin, lang string) bool {
	if b.Context == "" {
		return true
	}
	return b.Context == "test" && lang == "starlark-test"
}
