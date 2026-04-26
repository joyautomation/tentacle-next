//go:build plc || all

// Package lad implements the parser and IR lowering for Ladder Diagram
// (LAD) programs, the second of tentacle-next's first-class PLC languages.
//
// LAD targets simple, readable logic: interlocks, latching, motor starters.
// Programs that outgrow LAD's expressiveness should move to ST — the two
// languages share the IR backend and a unified user-FB registry, so the
// transition is incremental.
//
// A LAD program is a list of rungs evaluated top-to-bottom each scan.
// Every rung is a contact network (returning a power-flow boolean) plus
// one or more outputs (coils or FB calls) driven by that boolean.
//
// The contact network is a series-parallel tree, not an arbitrary DAG:
// bridge networks are rejected at parse. Users wanting non-series-parallel
// logic should duplicate contacts or use ST. See the LAD scope memory for
// the full rationale.
package lad

// Diagram is one parsed LAD program. The serialized form is canonical JSON;
// a future commit will add a text round-trip for grep/diff readability.
type Diagram struct {
	Name      string    `json:"name,omitempty"`
	Variables []VarDecl `json:"variables,omitempty"`
	Rungs     []*Rung   `json:"rungs"`
}

// VarDecl is a single variable declaration in a LAD program. It mirrors a
// row from an ST VAR block but in flat form: Kind names the block, "" is
// the default VAR (local + retained-by-default-no).
type VarDecl struct {
	Name   string `json:"name"`
	Type   string `json:"type"`             // built-in scalar, FB type, or UDT name
	Kind   string `json:"kind,omitempty"`   // "", "input", "output", "global"
	Init   string `json:"init,omitempty"`   // optional literal source ("0", "TRUE", "T#5s", etc.)
	Retain bool   `json:"retain,omitempty"` // RETAIN modifier
}

// Rung is one logical row of the diagram: a contact network on the left
// (Logic) drives any number of outputs on the right (Outputs). An empty
// Outputs list is permitted so a rung can be authored before its coils
// are wired up — lowering treats it as a no-op (the contact expression
// is still type-checked).
type Rung struct {
	Comment string   `json:"comment,omitempty"`
	Logic   Element  `json:"logic"`
	Outputs []Output `json:"outputs,omitempty"`
}

// Element is a node in the contact network tree. Concrete kinds:
// Contact (leaf), Series (AND fold of children), Parallel (OR fold).
type Element interface{ elementNode() }

// Contact is a single NO or NC contact. The operand is an identifier,
// optionally dotted (e.g. "t1.Q") to read a function-block output or a
// struct field.
type Contact struct {
	Form    string `json:"form"` // "NO" | "NC"
	Operand string `json:"operand"`
}

func (*Contact) elementNode() {}

// Series is an AND fold: all child elements must conduct.
type Series struct {
	Items []Element `json:"items"`
}

func (*Series) elementNode() {}

// Parallel is an OR fold: any conducting child carries power flow.
type Parallel struct {
	Items []Element `json:"items"`
}

func (*Parallel) elementNode() {}

// Output is a sink on the right side of a rung. v1 covers coils
// (OTE/OTL/OTU) and FB invocations.
type Output interface{ outputNode() }

// Coil writes the rung's power flow to Operand:
//
//	OTE: Operand := powerFlow            (non-retentive — every scan)
//	OTL: IF powerFlow THEN Operand := TRUE  (latch — set on rising flow)
//	OTU: IF powerFlow THEN Operand := FALSE (unlatch — clear on rising flow)
type Coil struct {
	Form    string `json:"form"` // "OTE" | "OTL" | "OTU"
	Operand string `json:"operand"`
}

func (*Coil) outputNode() {}

// FBCall invokes a function-block instance declared in this program's
// VAR block. PowerInput names the input that receives the rung's power
// flow boolean; when empty, the FB's first input is used (TON.IN, etc.).
// Other named inputs bind through Inputs.
//
// EN/ENO is intentionally omitted in v1 — power flow gating is achieved
// by binding it to the chosen power input. If a real EN/ENO is needed
// later the schema will gain a top-of-block flag.
type FBCall struct {
	Instance   string          `json:"instance"`
	PowerInput string          `json:"powerInput,omitempty"`
	Inputs     map[string]Expr `json:"inputs,omitempty"`
}

func (*FBCall) outputNode() {}

// Expr is a value supplied to an FB input. v1 keeps these simple: bare
// references (identifier or dotted) and scalar literals. Compound
// expressions (arithmetic, comparisons) belong in ST — declare a temp
// variable and wire it in if you need one in LAD.
type Expr interface{ exprNode() }

// Ref reads a variable by name (identifier or dotted, e.g. "t1.Q").
type Ref struct {
	Name string `json:"name"`
}

func (*Ref) exprNode() {}

// IntLit is a 64-bit signed integer literal.
type IntLit struct {
	V int64 `json:"value"`
}

func (*IntLit) exprNode() {}

// RealLit is a 64-bit float literal.
type RealLit struct {
	V float64 `json:"value"`
}

func (*RealLit) exprNode() {}

// BoolLit is TRUE or FALSE.
type BoolLit struct {
	V bool `json:"value"`
}

func (*BoolLit) exprNode() {}

// TimeLit is a duration in milliseconds. The serialized form may carry
// either a Raw IEC literal ("T#5s") or a pre-parsed Ms value; Parse
// resolves Raw → Ms.
type TimeLit struct {
	Ms  int64  `json:"ms,omitempty"`
	Raw string `json:"raw,omitempty"`
}

func (*TimeLit) exprNode() {}

// StringLit is a UTF-8 string literal.
type StringLit struct {
	V string `json:"value"`
}

func (*StringLit) exprNode() {}
