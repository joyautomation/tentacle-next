//go:build plc || all

package st

// Node is the interface for all AST nodes.
type Node interface {
	nodeType() string
}

// ─── Top-level ──────────────────────────────────────────────────────────────

// Program is the top-level PROGRAM ... END_PROGRAM block.
type Program struct {
	Name       string
	VarBlocks  []VarBlock
	Statements []Statement
}

func (p *Program) nodeType() string { return "Program" }

// VarBlock is a VAR / VAR_INPUT / VAR_OUTPUT ... END_VAR block.
type VarBlock struct {
	Kind      string // "VAR", "VAR_INPUT", "VAR_OUTPUT"
	Variables []VarDecl
}

// VarDecl is a single variable declaration.
type VarDecl struct {
	Name     string
	Datatype string // "INT", "REAL", "BOOL", "STRING", "DINT", "LREAL"
	Initial  Expression
}

// ─── Statements ─────────────────────────────────────────────────────────────

// Statement is the interface for all statements.
type Statement interface {
	Node
	stmtNode()
}

// AssignStmt is a variable assignment: x := expr;
type AssignStmt struct {
	Target string
	Value  Expression
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

// CallStmt is a standalone function call: func(args);
type CallStmt struct {
	Call *CallExpr
}

func (s *CallStmt) nodeType() string { return "CallStmt" }
func (s *CallStmt) stmtNode()        {}

// ReturnStmt is RETURN;
type ReturnStmt struct{}

func (s *ReturnStmt) nodeType() string { return "ReturnStmt" }
func (s *ReturnStmt) stmtNode()        {}

// ─── Expressions ────────────────────────────────────────────────────────────

// Expression is the interface for all expressions.
type Expression interface {
	Node
	exprNode()
}

// NumberLit is a numeric literal.
type NumberLit struct {
	Value string
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

// CallExpr is a function call: func(arg1, arg2, ...).
type CallExpr struct {
	Name string
	Args []Expression
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

// TimeLit is a time literal: T#5s, T#100ms.
type TimeLit struct {
	Raw string // e.g., "5s", "100ms"
}

func (e *TimeLit) nodeType() string { return "TimeLit" }
func (e *TimeLit) exprNode()        {}
