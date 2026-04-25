//go:build plc || all

package st

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc/ir"
)

// Lower converts a parsed ST program into typed IR.
//
// It resolves UDTs, builds a slot table for locals, rejects undeclared
// identifiers (declare in VAR_* / VAR_GLOBAL / VAR_EXTERNAL), type-checks
// every expression, and rewrites array indexing to 0-based form using each
// array's declared lower bound.
//
// Phase 3 intentionally does not lower function or function-block calls;
// those arrive in Phase 4 alongside the FB runtime and built-in registry.
func Lower(prog *Program) (*ir.Program, error) {
	l := newLowerer(prog)
	if err := l.collectTypes(); err != nil {
		return nil, err
	}
	if err := l.collectVars(); err != nil {
		return nil, err
	}
	body, err := l.lowerStmts(prog.Statements)
	if err != nil {
		return nil, err
	}
	l.irProg.Body = body
	return l.irProg, nil
}

type lowerer struct {
	prog   *Program
	irProg *ir.Program
	scope  map[string]symbol
	types  map[string]*ir.Type
}

type symbol struct {
	slot   int // -1 for globals
	typ    *ir.Type
	kind   ir.VarKind
	global string
}

func newLowerer(prog *Program) *lowerer {
	return &lowerer{
		prog:   prog,
		irProg: &ir.Program{Name: prog.Name, SlotIndex: map[string]int{}},
		scope:  map[string]symbol{},
		types:  map[string]*ir.Type{},
	}
}

// ─── Type resolution ──────────────────────────────────────────────────────

// collectTypes resolves program-level TypeDecls in two passes so struct
// fields can reference peer UDTs declared in the same TYPE block.
func (l *lowerer) collectTypes() error {
	for _, td := range l.prog.TypeDecls {
		if _, dup := l.types[td.Name]; dup {
			return errAt(td.Pos, fmt.Errorf("duplicate TYPE declaration %q", td.Name))
		}
		l.types[td.Name] = &ir.Type{
			Kind:   ir.TypeStruct,
			Struct: &ir.StructDef{Name: td.Name, FieldIndex: map[string]int{}},
		}
	}
	for _, td := range l.prog.TypeDecls {
		t := l.types[td.Name]
		switch body := td.Type.(type) {
		case *StructType:
			for i, f := range body.Fields {
				ft, err := l.resolveType(f.Type)
				if err != nil {
					return errAt(f.Pos, fmt.Errorf("TYPE %s field %q: %w", td.Name, f.Name, err))
				}
				t.Struct.Fields = append(t.Struct.Fields, ir.StructField{Name: f.Name, Type: ft})
				t.Struct.FieldIndex[f.Name] = i
			}
		default:
			resolved, err := l.resolveType(td.Type)
			if err != nil {
				return errAt(td.Pos, fmt.Errorf("TYPE %s: %w", td.Name, err))
			}
			*t = *resolved
		}
	}
	return nil
}

func (l *lowerer) resolveType(te TypeExpr) (*ir.Type, error) {
	switch t := te.(type) {
	case *ScalarType:
		return resolveScalar(t.Name)
	case *NamedType:
		if udt, ok := l.types[t.Name]; ok {
			return udt, nil
		}
		// Built-in FB types (TON, R_TRIG, CTU, …) live in the IR's
		// FB registry, not the program's TYPE block. Every VAR
		// declaration of an FB type gets a fresh *Type wrapping the
		// shared *FBDef so per-instance Zero allocates a private
		// FBInstance with its own slot vector.
		if fbT := ir.LookupFBType(t.Name); fbT != nil {
			return fbT, nil
		}
		return nil, fmt.Errorf("unknown type %q", t.Name)
	case *ArrayType:
		elem, err := l.resolveType(t.Elem)
		if err != nil {
			return nil, err
		}
		// Build nested arrays innermost→outermost. `ARRAY[1..3, 1..4] OF INT`
		// lowers to ARRAY[1..3] OF (ARRAY[1..4] OF INT) so indexing a[i, j]
		// decomposes cleanly into a[i][j].
		current := elem
		for i := len(t.Dims) - 1; i >= 0; i-- {
			lo, err := evalConstInt(t.Dims[i].Lo)
			if err != nil {
				return nil, fmt.Errorf("array dim lo: %w", err)
			}
			hi, err := evalConstInt(t.Dims[i].Hi)
			if err != nil {
				return nil, fmt.Errorf("array dim hi: %w", err)
			}
			if hi < lo {
				return nil, fmt.Errorf("array dim hi (%d) < lo (%d)", hi, lo)
			}
			current = &ir.Type{
				Kind:       ir.TypeArray,
				Elem:       current,
				ArrLen:     int(hi - lo + 1),
				ArrLoBound: int(lo),
			}
		}
		return current, nil
	case *StructType:
		def := &ir.StructDef{FieldIndex: map[string]int{}}
		for i, f := range t.Fields {
			ft, err := l.resolveType(f.Type)
			if err != nil {
				return nil, err
			}
			def.Fields = append(def.Fields, ir.StructField{Name: f.Name, Type: ft})
			def.FieldIndex[f.Name] = i
		}
		return &ir.Type{Kind: ir.TypeStruct, Struct: def}, nil
	}
	return nil, fmt.Errorf("unsupported type %T", te)
}

func resolveScalar(name string) (*ir.Type, error) {
	switch strings.ToUpper(name) {
	case "BOOL":
		return ir.BoolT, nil
	case "BYTE", "SINT", "USINT", "INT", "UINT", "WORD", "DINT", "UDINT", "DWORD", "LINT", "ULINT", "LWORD":
		return ir.IntT, nil
	case "REAL", "LREAL":
		return ir.RealT, nil
	case "TIME", "LTIME":
		return ir.TimeT, nil
	case "STRING", "WSTRING", "CHAR", "WCHAR":
		return ir.StringT, nil
	}
	return nil, fmt.Errorf("unknown scalar type %q", name)
}

// ─── Constant folding for bounds & initial values ──────────────────────────

func evalConstInt(e Expression) (int64, error) {
	switch n := e.(type) {
	case *NumberLit:
		base := n.Base
		if base == 0 {
			base = 10
		}
		return strconv.ParseInt(n.Value, base, 64)
	case *UnaryExpr:
		v, err := evalConstInt(n.Operand)
		if err != nil {
			return 0, err
		}
		if n.Op == "-" {
			return -v, nil
		}
		return v, nil
	case *TypedLit:
		return evalConstInt(n.Inner)
	}
	return 0, fmt.Errorf("not a compile-time integer: %T", e)
}

func evalConstValue(e Expression, t *ir.Type) (ir.Value, error) {
	switch n := e.(type) {
	case *NumberLit:
		base := n.Base
		if base == 0 {
			base = 10
		}
		if t != nil && t.Kind == ir.TypeReal {
			v, err := strconv.ParseFloat(n.Value, 64)
			if err != nil {
				return ir.Value{}, err
			}
			return ir.RealVal(v), nil
		}
		v, err := strconv.ParseInt(n.Value, base, 64)
		if err != nil {
			return ir.Value{}, err
		}
		if t != nil && t.Kind == ir.TypeTime {
			return ir.TimeVal(v), nil
		}
		return ir.IntVal(v), nil
	case *BoolLit:
		return ir.BoolVal(n.Value), nil
	case *StringLit:
		return ir.StringVal(n.Value), nil
	case *TimeLit:
		return ir.TimeVal(int64(parseTimeMs(n.Raw))), nil
	case *TypedLit:
		return evalConstValue(n.Inner, t)
	case *UnaryExpr:
		v, err := evalConstValue(n.Operand, t)
		if err != nil {
			return ir.Value{}, err
		}
		switch n.Op {
		case "-":
			if v.Kind == ir.TypeReal {
				return ir.RealVal(-v.F), nil
			}
			return ir.Value{Kind: v.Kind, I: -v.I}, nil
		case "NOT":
			if v.Kind == ir.TypeBool {
				return ir.BoolVal(!v.B), nil
			}
		}
	}
	return ir.Value{}, fmt.Errorf("initial value must be a literal constant: %T", e)
}

// ─── Slot / symbol table ───────────────────────────────────────────────────

func (l *lowerer) collectVars() error {
	for _, vb := range l.prog.VarBlocks {
		kind := varKindFor(vb.Kind)
		for _, vd := range vb.Variables {
			if _, dup := l.scope[vd.Name]; dup {
				return errAt(vd.Pos, fmt.Errorf("duplicate declaration %q", vd.Name))
			}
			t, err := l.resolveType(vd.Type)
			if err != nil {
				return errAt(vd.Pos, fmt.Errorf("VAR %s: %w", vd.Name, err))
			}
			var init ir.Value
			if vd.Initial != nil {
				init, err = evalConstValue(vd.Initial, t)
				if err != nil {
					return errAt(vd.Pos, fmt.Errorf("VAR %s initial: %w", vd.Name, err))
				}
			}
			if kind == ir.VarGlobal {
				l.scope[vd.Name] = symbol{slot: -1, typ: t, kind: ir.VarGlobal, global: vd.Name}
				continue
			}
			slot := len(l.irProg.Slots)
			l.irProg.Slots = append(l.irProg.Slots, ir.VarSlot{
				Name:     vd.Name,
				Type:     t,
				Init:     init,
				Retained: vb.Retain,
				Kind:     kind,
			})
			l.irProg.SlotIndex[vd.Name] = slot
			l.scope[vd.Name] = symbol{slot: slot, typ: t, kind: kind}
		}
	}
	return nil
}

func varKindFor(blockKind string) ir.VarKind {
	switch blockKind {
	case "VAR_INPUT":
		return ir.VarInput
	case "VAR_OUTPUT":
		return ir.VarOutput
	case "VAR_GLOBAL", "VAR_EXTERNAL":
		return ir.VarGlobal
	}
	return ir.VarLocal
}

// ─── Statement lowering ───────────────────────────────────────────────────

func (l *lowerer) lowerStmts(stmts []Statement) ([]ir.Stmt, error) {
	out := make([]ir.Stmt, 0, len(stmts))
	for _, s := range stmts {
		ls, err := l.lowerStmt(s)
		if err != nil {
			return nil, errAt(nodePos(s), err)
		}
		if ls != nil {
			out = append(out, ls)
		}
	}
	return out, nil
}

func (l *lowerer) lowerStmt(s Statement) (ir.Stmt, error) {
	switch n := s.(type) {
	case *AssignStmt:
		return l.lowerAssign(n)

	case *IfStmt:
		cond, err := l.lowerExpr(n.Condition)
		if err != nil {
			return nil, err
		}
		if cond.ExprType().Kind != ir.TypeBool {
			return nil, fmt.Errorf("IF condition must be BOOL, got %s", cond.ExprType())
		}
		thenB, err := l.lowerStmts(n.Then)
		if err != nil {
			return nil, err
		}
		elseB, err := l.lowerStmts(n.Else)
		if err != nil {
			return nil, err
		}
		// Desugar ELSIF into nested IFs, building from the last clause up.
		for i := len(n.ElsIfs) - 1; i >= 0; i-- {
			ec, err := l.lowerExpr(n.ElsIfs[i].Condition)
			if err != nil {
				return nil, err
			}
			if ec.ExprType().Kind != ir.TypeBool {
				return nil, fmt.Errorf("ELSIF condition must be BOOL, got %s", ec.ExprType())
			}
			eb, err := l.lowerStmts(n.ElsIfs[i].Body)
			if err != nil {
				return nil, err
			}
			elseB = []ir.Stmt{&ir.If{Cond: ec, Then: eb, Else: elseB}}
		}
		return &ir.If{Cond: cond, Then: thenB, Else: elseB}, nil

	case *ForStmt:
		sym, ok := l.scope[n.Variable]
		if !ok {
			return nil, fmt.Errorf("FOR: undeclared loop variable %q", n.Variable)
		}
		if sym.kind == ir.VarGlobal {
			return nil, fmt.Errorf("FOR: loop variable %q must be local, not global", n.Variable)
		}
		if sym.typ.Kind != ir.TypeInt {
			return nil, fmt.Errorf("FOR: loop variable %q must be integer, got %s", n.Variable, sym.typ)
		}
		start, err := l.lowerExpr(n.Start)
		if err != nil {
			return nil, err
		}
		end, err := l.lowerExpr(n.End)
		if err != nil {
			return nil, err
		}
		var step ir.Expr
		if n.Step != nil {
			step, err = l.lowerExpr(n.Step)
			if err != nil {
				return nil, err
			}
		}
		body, err := l.lowerStmts(n.Body)
		if err != nil {
			return nil, err
		}
		return &ir.For{Slot: sym.slot, Start: start, End: end, Step: step, Body: body}, nil

	case *WhileStmt:
		cond, err := l.lowerExpr(n.Condition)
		if err != nil {
			return nil, err
		}
		if cond.ExprType().Kind != ir.TypeBool {
			return nil, fmt.Errorf("WHILE condition must be BOOL, got %s", cond.ExprType())
		}
		body, err := l.lowerStmts(n.Body)
		if err != nil {
			return nil, err
		}
		return &ir.While{Cond: cond, Body: body}, nil

	case *RepeatStmt:
		body, err := l.lowerStmts(n.Body)
		if err != nil {
			return nil, err
		}
		cond, err := l.lowerExpr(n.Condition)
		if err != nil {
			return nil, err
		}
		if cond.ExprType().Kind != ir.TypeBool {
			return nil, fmt.Errorf("UNTIL condition must be BOOL, got %s", cond.ExprType())
		}
		return &ir.Repeat{Body: body, Cond: cond}, nil

	case *CaseStmt:
		expr, err := l.lowerExpr(n.Expression)
		if err != nil {
			return nil, err
		}
		var clauses []ir.CaseClause
		for _, c := range n.Cases {
			var vals []ir.Expr
			for _, v := range c.Values {
				lv, err := l.lowerExpr(v)
				if err != nil {
					return nil, err
				}
				vals = append(vals, lv)
			}
			body, err := l.lowerStmts(c.Body)
			if err != nil {
				return nil, err
			}
			clauses = append(clauses, ir.CaseClause{Values: vals, Body: body})
		}
		elseB, err := l.lowerStmts(n.Else)
		if err != nil {
			return nil, err
		}
		return &ir.Case{Expr: expr, Clauses: clauses, Else: elseB}, nil

	case *ReturnStmt:
		return &ir.Return{}, nil
	case *ExitStmt:
		return &ir.Exit{}, nil
	case *ContinueStmt:
		return &ir.Continue{}, nil
	case *CallStmt:
		return l.lowerCallStmt(n)
	}
	return nil, fmt.Errorf("unsupported statement %T", s)
}

// lowerCallStmt resolves a `name(...)` statement. Today every callable
// statement is an FB invocation: stateless built-in functions are
// expression-only, so a CallStmt whose name doesn't resolve to an FB
// instance is a programming error.
func (l *lowerer) lowerCallStmt(n *CallStmt) (ir.Stmt, error) {
	sym, ok := l.scope[n.Call.Name]
	if !ok {
		return nil, fmt.Errorf("call to undeclared name %q", n.Call.Name)
	}
	if sym.typ == nil || sym.typ.Kind != ir.TypeFB {
		return nil, fmt.Errorf("%q is not a function-block instance (declare e.g. `t1 : TON;`)", n.Call.Name)
	}
	if sym.kind == ir.VarGlobal {
		return nil, fmt.Errorf("FB instance %q must be a local variable, not VAR_GLOBAL", n.Call.Name)
	}
	def := sym.typ.FB
	if len(n.Call.Args) > 0 {
		return nil, fmt.Errorf("FB call %q must use named args (IN := …, PT := …)", n.Call.Name)
	}
	bindings := make([]ir.FBInput, 0, len(n.Call.NamedArgs))
	for _, na := range n.Call.NamedArgs {
		idx, ok := def.SlotIndex[na.Name]
		if !ok {
			return nil, fmt.Errorf("FB %s has no input %q", def.Name, na.Name)
		}
		if idx >= len(def.Inputs) {
			return nil, fmt.Errorf("FB %s field %q is not an input", def.Name, na.Name)
		}
		v, err := l.lowerExpr(na.Value)
		if err != nil {
			return nil, fmt.Errorf("FB %s arg %q: %w", def.Name, na.Name, err)
		}
		v = coerce(v, def.Inputs[idx].Type)
		bindings = append(bindings, ir.FBInput{SlotIdx: idx, Value: v})
	}
	return &ir.FBCall{InstanceSlot: sym.slot, Def: def, Inputs: bindings}, nil
}

func (l *lowerer) lowerAssign(a *AssignStmt) (ir.Stmt, error) {
	if a.TargetExpr == nil {
		return nil, fmt.Errorf("assignment has no structured target (parser bug)")
	}
	target, err := l.lowerLValue(a.TargetExpr)
	if err != nil {
		return nil, err
	}
	value, err := l.lowerExpr(a.Value)
	if err != nil {
		return nil, err
	}
	value = coerce(value, target.ExprType())
	if !assignable(target.ExprType(), value.ExprType()) {
		return nil, fmt.Errorf("cannot assign %s to %s", value.ExprType(), target.ExprType())
	}
	return &ir.Assign{Target: target, Value: value}, nil
}

// ─── Expression lowering ──────────────────────────────────────────────────

func (l *lowerer) lowerExpr(e Expression) (ir.Expr, error) {
	switch n := e.(type) {
	case *NumberLit:
		return lowerNumberLit(n)
	case *BoolLit:
		return &ir.Lit{V: ir.BoolVal(n.Value), T: ir.BoolT}, nil
	case *StringLit:
		return &ir.Lit{V: ir.StringVal(n.Value), T: ir.StringT}, nil
	case *TimeLit:
		return &ir.Lit{V: ir.TimeVal(int64(parseTimeMs(n.Raw))), T: ir.TimeT}, nil
	case *TypedLit:
		return l.lowerExpr(n.Inner)
	case *IdentExpr:
		return l.lowerIdent(n.Name)
	case *MemberExpr:
		return l.lowerMember(n)
	case *IndexExpr:
		return l.lowerIndex(n)
	case *BinaryExpr:
		return l.lowerBinary(n)
	case *UnaryExpr:
		return l.lowerUnary(n)
	case *CallExpr:
		return l.lowerCallExpr(n)
	}
	return nil, fmt.Errorf("unsupported expression %T", e)
}

func (l *lowerer) lowerCallExpr(n *CallExpr) (ir.Expr, error) {
	sig, ok := ir.Builtins[strings.ToUpper(n.Name)]
	if !ok {
		// FB instance "calls" inside expressions are illegal — outputs
		// are read via member access (t1.Q), and bare `t1(...)` produces
		// no value. Surface a clearer message when this is the case.
		if sym, defined := l.scope[n.Name]; defined && sym.typ != nil && sym.typ.Kind == ir.TypeFB {
			return nil, fmt.Errorf("FB instance %q can't be used as an expression — invoke it as a statement and read outputs (e.g. %s.Q)", n.Name, n.Name)
		}
		return nil, fmt.Errorf("unknown function %q", n.Name)
	}
	if len(n.NamedArgs) > 0 {
		return nil, fmt.Errorf("function %s does not accept named args", sig.Name)
	}
	args := make([]ir.Expr, 0, len(n.Args))
	argTypes := make([]*ir.Type, 0, len(n.Args))
	for _, a := range n.Args {
		la, err := l.lowerExpr(a)
		if err != nil {
			return nil, fmt.Errorf("function %s arg: %w", sig.Name, err)
		}
		args = append(args, la)
		argTypes = append(argTypes, la.ExprType())
	}
	if !sig.Variadic && len(args) != len(sig.Params) {
		return nil, fmt.Errorf("function %s expects %d argument(s), got %d", sig.Name, len(sig.Params), len(args))
	}
	if sig.Variadic && len(args) < len(sig.Params) {
		return nil, fmt.Errorf("function %s expects at least %d argument(s), got %d", sig.Name, len(sig.Params), len(args))
	}
	resultT := sig.Result
	if sig.Coerce != nil {
		t, err := sig.Coerce(argTypes)
		if err != nil {
			return nil, err
		}
		resultT = t
	}
	for i, p := range sig.Params {
		if p == nil || i >= len(args) {
			continue
		}
		args[i] = coerce(args[i], p)
		if !assignable(p, args[i].ExprType()) {
			return nil, fmt.Errorf("function %s arg %d: cannot pass %s as %s", sig.Name, i+1, args[i].ExprType(), p)
		}
	}
	return &ir.Call{Name: sig.Name, Args: args, Fn: sig.Fn, T: resultT}, nil
}

func lowerNumberLit(n *NumberLit) (ir.Expr, error) {
	base := n.Base
	if base == 0 {
		base = 10
	}
	if base == 10 && strings.ContainsAny(n.Value, ".eE") {
		v, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return nil, err
		}
		return &ir.Lit{V: ir.RealVal(v), T: ir.RealT}, nil
	}
	v, err := strconv.ParseInt(n.Value, base, 64)
	if err != nil {
		return nil, err
	}
	return &ir.Lit{V: ir.IntVal(v), T: ir.IntT}, nil
}

func (l *lowerer) lowerIdent(name string) (ir.Expr, error) {
	sym, ok := l.scope[name]
	if !ok {
		return nil, fmt.Errorf("undeclared identifier %q (declare in VAR_* or VAR_GLOBAL block)", name)
	}
	if sym.kind == ir.VarGlobal {
		return &ir.GlobalRef{Name: sym.global, T: sym.typ}, nil
	}
	return &ir.SlotRef{Slot: sym.slot, T: sym.typ}, nil
}

func (l *lowerer) lowerMember(m *MemberExpr) (ir.Expr, error) {
	obj, err := l.lowerExpr(m.Object)
	if err != nil {
		return nil, err
	}
	ot := obj.ExprType()
	switch ot.Kind {
	case ir.TypeStruct:
		idx, ok := ot.Struct.FieldIndex[m.Member]
		if !ok {
			label := ot.Struct.Name
			if label == "" {
				label = "STRUCT"
			}
			return nil, fmt.Errorf("field %q not found on %s", m.Member, label)
		}
		return &ir.MemberRef{Object: obj, FieldIdx: idx, T: ot.Struct.Fields[idx].Type}, nil
	case ir.TypeFB:
		idx, ok := ot.FB.SlotIndex[m.Member]
		if !ok {
			return nil, fmt.Errorf("FB %s has no field %q", ot.FB.Name, m.Member)
		}
		all := ot.FB.AllSlots()
		return &ir.MemberRef{Object: obj, FieldIdx: idx, T: all[idx].Type}, nil
	}
	return nil, fmt.Errorf("member access on non-struct type %s", ot)
}

func (l *lowerer) lowerIndex(n *IndexExpr) (ir.Expr, error) {
	arr, err := l.lowerExpr(n.Array)
	if err != nil {
		return nil, err
	}
	cur := arr
	curT := arr.ExprType()
	for _, idxExpr := range n.Indices {
		if curT.Kind != ir.TypeArray {
			return nil, fmt.Errorf("indexing non-array type %s", curT)
		}
		idx, err := l.lowerExpr(idxExpr)
		if err != nil {
			return nil, err
		}
		if idx.ExprType().Kind != ir.TypeInt {
			return nil, fmt.Errorf("array index must be integer, got %s", idx.ExprType())
		}
		zero := ir.Expr(idx)
		if curT.ArrLoBound != 0 {
			zero = &ir.BinOp{
				Op: ir.OpSub,
				L:  idx,
				R:  &ir.Lit{V: ir.IntVal(int64(curT.ArrLoBound)), T: ir.IntT},
				T:  ir.IntT,
			}
		}
		cur = &ir.IndexRef{Array: cur, Index: zero, T: curT.Elem}
		curT = curT.Elem
	}
	return cur, nil
}

func (l *lowerer) lowerLValue(e Expression) (ir.LValue, error) {
	lowered, err := l.lowerExpr(e)
	if err != nil {
		return nil, err
	}
	lv, ok := lowered.(ir.LValue)
	if !ok {
		return nil, fmt.Errorf("expression is not assignable: %T", e)
	}
	return lv, nil
}

func (l *lowerer) lowerBinary(b *BinaryExpr) (ir.Expr, error) {
	left, err := l.lowerExpr(b.Left)
	if err != nil {
		return nil, err
	}
	right, err := l.lowerExpr(b.Right)
	if err != nil {
		return nil, err
	}
	op, err := mapBinOp(b.Op)
	if err != nil {
		return nil, err
	}
	resultT, err := resolveBinType(op, left.ExprType(), right.ExprType())
	if err != nil {
		return nil, fmt.Errorf("operator %s on %s and %s: %w", b.Op, left.ExprType(), right.ExprType(), err)
	}
	if resultT.Kind == ir.TypeReal {
		if left.ExprType().Kind == ir.TypeInt {
			left = intToReal(left)
		}
		if right.ExprType().Kind == ir.TypeInt {
			right = intToReal(right)
		}
	}
	return &ir.BinOp{Op: op, L: left, R: right, T: resultT}, nil
}

func (l *lowerer) lowerUnary(u *UnaryExpr) (ir.Expr, error) {
	x, err := l.lowerExpr(u.Operand)
	if err != nil {
		return nil, err
	}
	switch u.Op {
	case "-":
		if !x.ExprType().IsNumeric() {
			return nil, fmt.Errorf("unary - on non-numeric %s", x.ExprType())
		}
		return &ir.UnOp{Op: ir.OpNeg, X: x, T: x.ExprType()}, nil
	case "NOT":
		if x.ExprType().Kind != ir.TypeBool {
			return nil, fmt.Errorf("NOT on non-BOOL %s", x.ExprType())
		}
		return &ir.UnOp{Op: ir.OpNot, X: x, T: ir.BoolT}, nil
	}
	return nil, fmt.Errorf("unknown unary operator %q", u.Op)
}

// ─── Type checking helpers ────────────────────────────────────────────────

func mapBinOp(op string) (ir.BinKind, error) {
	switch op {
	case "+":
		return ir.OpAdd, nil
	case "-":
		return ir.OpSub, nil
	case "*":
		return ir.OpMul, nil
	case "/":
		return ir.OpDiv, nil
	case "MOD":
		return ir.OpMod, nil
	case "=":
		return ir.OpEq, nil
	case "<>":
		return ir.OpNeq, nil
	case "<":
		return ir.OpLt, nil
	case "<=":
		return ir.OpLte, nil
	case ">":
		return ir.OpGt, nil
	case ">=":
		return ir.OpGte, nil
	case "AND":
		return ir.OpAnd, nil
	case "OR":
		return ir.OpOr, nil
	case "XOR":
		return ir.OpXor, nil
	}
	return 0, fmt.Errorf("unknown operator %q", op)
}

func resolveBinType(op ir.BinKind, lt, rt *ir.Type) (*ir.Type, error) {
	switch op {
	case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod:
		if !lt.IsNumeric() || !rt.IsNumeric() {
			return nil, fmt.Errorf("arithmetic requires numeric operands")
		}
		if lt.Kind == ir.TypeReal || rt.Kind == ir.TypeReal {
			return ir.RealT, nil
		}
		if lt.Kind == ir.TypeTime && rt.Kind == ir.TypeTime {
			return ir.TimeT, nil
		}
		if lt.Kind == ir.TypeTime || rt.Kind == ir.TypeTime {
			return nil, fmt.Errorf("TIME may only be combined with TIME")
		}
		return ir.IntT, nil
	case ir.OpEq, ir.OpNeq, ir.OpLt, ir.OpLte, ir.OpGt, ir.OpGte:
		if lt.Kind == ir.TypeBool && rt.Kind == ir.TypeBool && (op == ir.OpEq || op == ir.OpNeq) {
			return ir.BoolT, nil
		}
		if lt.Kind == ir.TypeString && rt.Kind == ir.TypeString && (op == ir.OpEq || op == ir.OpNeq) {
			return ir.BoolT, nil
		}
		if !lt.IsNumeric() || !rt.IsNumeric() {
			return nil, fmt.Errorf("comparison requires numeric operands (or matching BOOL/STRING for =/<>)")
		}
		return ir.BoolT, nil
	case ir.OpAnd, ir.OpOr:
		if lt.Kind == ir.TypeBool && rt.Kind == ir.TypeBool {
			return ir.BoolT, nil
		}
		if lt.Kind == ir.TypeInt && rt.Kind == ir.TypeInt {
			return ir.IntT, nil
		}
		return nil, fmt.Errorf("logical op requires BOOL or INT operands")
	case ir.OpXor:
		if lt.Kind == ir.TypeBool && rt.Kind == ir.TypeBool {
			return ir.BoolT, nil
		}
		if lt.Kind == ir.TypeInt && rt.Kind == ir.TypeInt {
			return ir.IntT, nil
		}
		return nil, fmt.Errorf("XOR requires BOOL or INT operands")
	}
	return nil, fmt.Errorf("internal: unhandled operator")
}

// intToReal promotes an INT expression to REAL. Literals fold directly;
// non-literal ints ride through the VM's asFloat() helper because BinOp
// already coerces on mixed kinds. A dedicated convert node will arrive
// with the conversion-builtins work in Phase 5.
func intToReal(e ir.Expr) ir.Expr {
	if lit, ok := e.(*ir.Lit); ok && lit.V.Kind == ir.TypeInt {
		return &ir.Lit{V: ir.RealVal(float64(lit.V.I)), T: ir.RealT}
	}
	return &ir.BinOp{Op: ir.OpAdd, L: e, R: &ir.Lit{V: ir.RealVal(0), T: ir.RealT}, T: ir.RealT}
}

func coerce(e ir.Expr, want *ir.Type) ir.Expr {
	if want == nil || e.ExprType().Equal(want) {
		return e
	}
	if want.Kind == ir.TypeReal && e.ExprType().Kind == ir.TypeInt {
		return intToReal(e)
	}
	return e
}

func assignable(lhs, rhs *ir.Type) bool {
	if lhs.Equal(rhs) {
		return true
	}
	if lhs.Kind == ir.TypeReal && rhs.Kind == ir.TypeInt {
		return true
	}
	return false
}
