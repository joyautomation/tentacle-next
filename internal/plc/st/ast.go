//go:build plc || all

package st

// Node is the interface for all AST nodes.
type Node interface {
	nodeType() string
}

// ─── Top-level ──────────────────────────────────────────────────────────────

// Program is the top-level PROGRAM ... END_PROGRAM block.
// TypeDecls are file-scope TYPE ... END_TYPE declarations; they live on
// Program for convenience even though they conceptually sit outside it.
type Program struct {
	Name       string
	TypeDecls  []TypeDecl
	VarBlocks  []VarBlock
	Statements []Statement
}

func (p *Program) nodeType() string { return "Program" }

// VarBlock is a VAR / VAR_INPUT / VAR_OUTPUT / VAR_IN_OUT / VAR_TEMP / VAR_GLOBAL / VAR_EXTERNAL block.
type VarBlock struct {
	Kind      string // "VAR", "VAR_INPUT", "VAR_OUTPUT", "VAR_IN_OUT", "VAR_TEMP", "VAR_GLOBAL", "VAR_EXTERNAL"
	Retain    bool   // VAR RETAIN
	Constant  bool   // VAR CONSTANT
	Variables []VarDecl
}

// VarDecl is a single variable declaration.
// Datatype is the textual form (scalar name, UDT name, or a rendered array
// signature like "ARRAY[1..10] OF INT") and is preserved so the LSP /
// /transpile endpoint can expose a simple string to existing consumers.
// Type carries the structured type expression used by the lowering pass.
type VarDecl struct {
	Name     string
	Datatype string
	Type     TypeExpr
	Initial  Expression
}

// TypeDecl is a top-level TYPE Name : <typeExpr>; END_TYPE entry.
// Most commonly used for STRUCT UDTs, but the parser allows aliasing any
// type expression so enums/subranges can be added later without grammar churn.
type TypeDecl struct {
	Name string
	Type TypeExpr
}

func (t *TypeDecl) nodeType() string { return "TypeDecl" }

// ─── Type expressions ───────────────────────────────────────────────────────

// TypeExpr describes a variable's type as written in source.
// Concrete kinds: ScalarType, ArrayType, NamedType, StructType.
type TypeExpr interface {
	Node
	typeExprNode()
	// String renders the type in its canonical textual form, suitable for
	// the VarDecl.Datatype shim and LSP hover displays.
	String() string
}

// ScalarType is a builtin IEC elementary type: BOOL, INT, REAL, TIME, ...
type ScalarType struct {
	Name string
}

func (s *ScalarType) nodeType() string { return "ScalarType" }
func (s *ScalarType) typeExprNode()    {}
func (s *ScalarType) String() string   { return s.Name }

// NamedType references a user-defined type by name (UDT or FB type).
// Resolution to StructDef or FBDef happens in the lowering pass; the parser
// has no way to distinguish a UDT from an FB type here.
type NamedType struct {
	Name string
}

func (n *NamedType) nodeType() string { return "NamedType" }
func (n *NamedType) typeExprNode()    {}
func (n *NamedType) String() string   { return n.Name }

// ArrayType describes ARRAY [lo..hi [, lo..hi]*] OF Elem.
type ArrayType struct {
	Dims []ArrayDim
	Elem TypeExpr
}

func (a *ArrayType) nodeType() string { return "ArrayType" }
func (a *ArrayType) typeExprNode()    {}
func (a *ArrayType) String() string {
	out := "ARRAY["
	for i, d := range a.Dims {
		if i > 0 {
			out += ", "
		}
		out += d.String()
	}
	out += "] OF " + a.Elem.String()
	return out
}

// ArrayDim is a single dimension expressed as lo..hi. Bounds must be compile-time
// constants; the parser stores arbitrary expressions and the lowering pass evaluates them.
type ArrayDim struct {
	Lo, Hi Expression
}

func (d ArrayDim) String() string {
	// Best-effort pretty-print: only literal bounds render readably here.
	return exprShort(d.Lo) + ".." + exprShort(d.Hi)
}

// StructType is an inline STRUCT ... END_STRUCT body.
// Wrapped in a TypeDecl for user-defined UDTs; can also appear inline
// inside arrays or other structs if the parser is extended later.
type StructType struct {
	Fields []VarDecl
}

func (s *StructType) nodeType() string { return "StructType" }
func (s *StructType) typeExprNode()    {}
func (s *StructType) String() string   { return "STRUCT" }

// exprShort renders an expression as a compact string for type pretty-printing.
// Only covers the literal shapes that appear in array bounds / initial values.
func exprShort(e Expression) string {
	switch v := e.(type) {
	case *NumberLit:
		return v.Value
	case *IdentExpr:
		return v.Name
	case *UnaryExpr:
		return v.Op + exprShort(v.Operand)
	}
	return "?"
}

// ─── Statements ─────────────────────────────────────────────────────────────

// Statement is the interface for all statements.
type Statement interface {
	Node
	stmtNode()
}

// AssignStmt is a variable assignment: target := expr;
// Target is kept as a string for back-compat with the Starlark codegen path.
// TargetExpr holds the structured LValue (IdentExpr / MemberExpr / IndexExpr)
// and is what the IR lowering pass should consume.
type AssignStmt struct {
	Target     string
	TargetExpr Expression
	Value      Expression
}

func (s *AssignStmt) nodeType() string { return "AssignStmt" }
func (s *AssignStmt) stmtNode()        {}

// IfStmt is IF ... THEN ... ELSIF ... ELSE ... END_IF.
type IfStmt struct {
	Condition Expression
	Then      []Statement
	ElsIfs    []ElsIfClause
	Else      []Statement
}

type ElsIfClause struct {
	Condition Expression
	Body      []Statement
}

func (s *IfStmt) nodeType() string { return "IfStmt" }
func (s *IfStmt) stmtNode()        {}

// ForStmt is FOR i := start TO end [BY step] DO ... END_FOR.
type ForStmt struct {
	Variable string
	Start    Expression
	End      Expression
	Step     Expression // nil means step=1
	Body     []Statement
}

func (s *ForStmt) nodeType() string { return "ForStmt" }
func (s *ForStmt) stmtNode()        {}

// WhileStmt is WHILE cond DO ... END_WHILE.
type WhileStmt struct {
	Condition Expression
	Body      []Statement
}

func (s *WhileStmt) nodeType() string { return "WhileStmt" }
func (s *WhileStmt) stmtNode()        {}

// RepeatStmt is REPEAT ... UNTIL cond END_REPEAT.
type RepeatStmt struct {
	Body      []Statement
	Condition Expression
}

func (s *RepeatStmt) nodeType() string { return "RepeatStmt" }
func (s *RepeatStmt) stmtNode()        {}

// CaseStmt is CASE expr OF ... END_CASE.
type CaseStmt struct {
	Expression Expression
	Cases      []CaseClause
	Else       []Statement
}

type CaseClause struct {
	Values []Expression
	Body   []Statement
}

func (s *CaseStmt) nodeType() string { return "CaseStmt" }
func (s *CaseStmt) stmtNode()        {}

// CallStmt is a standalone function or FB-instance call: name(args);
type CallStmt struct {
	Call *CallExpr
}

func (s *CallStmt) nodeType() string { return "CallStmt" }
func (s *CallStmt) stmtNode()        {}

// ReturnStmt is RETURN;
type ReturnStmt struct{}

func (s *ReturnStmt) nodeType() string { return "ReturnStmt" }
func (s *ReturnStmt) stmtNode()        {}

// ExitStmt is EXIT; (break out of the innermost loop).
type ExitStmt struct{}

func (s *ExitStmt) nodeType() string { return "ExitStmt" }
func (s *ExitStmt) stmtNode()        {}

// ContinueStmt is CONTINUE; (skip to the next loop iteration).
type ContinueStmt struct{}

func (s *ContinueStmt) nodeType() string { return "ContinueStmt" }
func (s *ContinueStmt) stmtNode()        {}

// ─── Expressions ────────────────────────────────────────────────────────────

// Expression is the interface for all expressions.
type Expression interface {
	Node
	exprNode()
}

// NumberLit is a numeric literal. Value is the canonical decimal string —
// based literals (16#FF, 2#1010) are decoded by the lexer.
type NumberLit struct {
	Value string
	Base  int // 10, 16, 2, 8. 10 is the default for conventional literals.
}

func (e *NumberLit) nodeType() string { return "NumberLit" }
func (e *NumberLit) exprNode()        {}

// StringLit is a string literal.
type StringLit struct {
	Value string
}

func (e *StringLit) nodeType() string { return "StringLit" }
func (e *StringLit) exprNode()        {}

// BoolLit is TRUE or FALSE.
type BoolLit struct {
	Value bool
}

func (e *BoolLit) nodeType() string { return "BoolLit" }
func (e *BoolLit) exprNode()        {}

// IdentExpr is a variable reference.
type IdentExpr struct {
	Name string
}

func (e *IdentExpr) nodeType() string { return "IdentExpr" }
func (e *IdentExpr) exprNode()        {}

// BinaryExpr is a binary operation: left op right.
type BinaryExpr struct {
	Left  Expression
	Op    string // "+", "-", "*", "/", "=", "<>", "<", "<=", ">", ">=", "AND", "OR", "XOR", "MOD"
	Right Expression
}

func (e *BinaryExpr) nodeType() string { return "BinaryExpr" }
func (e *BinaryExpr) exprNode()        {}

// UnaryExpr is a unary operation: NOT expr, -expr.
type UnaryExpr struct {
	Op      string // "NOT", "-"
	Operand Expression
}

func (e *UnaryExpr) nodeType() string { return "UnaryExpr" }
func (e *UnaryExpr) exprNode()        {}

// CallExpr is a function call: func(arg1, arg2, ...) or func(IN := x, PT := T#5s).
// Args holds positional arguments in declaration order. NamedArgs holds the
// IEC-style "name := value" form used for FB instance calls. A call may use
// either form exclusively — mixing is not validated at parse time.
type CallExpr struct {
	Name      string
	Args      []Expression
	NamedArgs []NamedArg
}

// NamedArg is a `name := value` argument in an FB/function call.
type NamedArg struct {
	Name  string
	Value Expression
}

func (e *CallExpr) nodeType() string { return "CallExpr" }
func (e *CallExpr) exprNode()        {}

// MemberExpr is a dotted member access: obj.field.
type MemberExpr struct {
	Object Expression
	Member string
}

func (e *MemberExpr) nodeType() string { return "MemberExpr" }
func (e *MemberExpr) exprNode()        {}

// IndexExpr is an array subscript: a[i] or a[i, j].
type IndexExpr struct {
	Array   Expression
	Indices []Expression
}

func (e *IndexExpr) nodeType() string { return "IndexExpr" }
func (e *IndexExpr) exprNode()        {}

// TimeLit is a time literal: T#5s, T#100ms, T#1h30m.
type TimeLit struct {
	Raw string // e.g., "5s", "100ms", "1h30m"
}

func (e *TimeLit) nodeType() string { return "TimeLit" }
func (e *TimeLit) exprNode()        {}

// TypedLit is a type-prefixed literal: INT#42, REAL#3.14, BOOL#TRUE, STRING#'x'.
// TypeName is the prefix as written (uppercased); Inner is the payload literal.
type TypedLit struct {
	TypeName string
	Inner    Expression
}

func (e *TypedLit) nodeType() string { return "TypedLit" }
func (e *TypedLit) exprNode()        {}
