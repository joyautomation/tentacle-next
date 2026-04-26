//go:build plc || all

package lad

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc/ir"
	"github.com/joyautomation/tentacle/internal/plc/st"
)

// Lower converts a parsed LAD diagram into typed IR.
//
// The strategy mirrors st.Lower:
//   - Collect VAR declarations into ir.VarSlot (or VarGlobal symbols).
//   - Lower each rung to a sequence of IR statements:
//       __rungN := <power-flow expression>
//       <coil assignments / FB calls bound to __rungN>
//   - Reuse the IR's BinOp(And/Or) and UnOp(Not) directly — no separate
//     ladder evaluation engine.
//
// userFBs is the engine-supplied registry of user-defined FB types. LAD
// shares this registry with ST so a FUNCTION_BLOCK declared in either
// language is callable from the other.
func Lower(diag *Diagram, userFBs ...map[string]*ir.FBDef) (*ir.Program, error) {
	var resolver map[string]*ir.FBDef
	if len(userFBs) > 0 {
		resolver = userFBs[0]
	}
	l := &lowerer{
		diag:    diag,
		userFBs: resolver,
		irProg:  &ir.Program{Name: diag.Name, SlotIndex: map[string]int{}},
		scope:   map[string]symbol{},
	}
	if err := l.collectVars(); err != nil {
		return nil, err
	}
	if err := l.lowerRungs(); err != nil {
		return nil, err
	}
	return l.irProg, nil
}

type lowerer struct {
	diag    *Diagram
	userFBs map[string]*ir.FBDef
	irProg  *ir.Program
	scope   map[string]symbol
}

type symbol struct {
	slot   int // -1 for globals
	typ    *ir.Type
	kind   ir.VarKind
	global string
}

func (l *lowerer) collectVars() error {
	for _, vd := range l.diag.Variables {
		if _, dup := l.scope[vd.Name]; dup {
			return fmt.Errorf("duplicate declaration %q", vd.Name)
		}
		t, err := l.resolveType(vd.Type)
		if err != nil {
			return fmt.Errorf("VAR %s: %w", vd.Name, err)
		}
		var init ir.Value
		if vd.Init != "" {
			init, err = parseInit(vd.Init, t)
			if err != nil {
				return fmt.Errorf("VAR %s init: %w", vd.Name, err)
			}
		}
		kind := varKindFor(vd.Kind)
		if kind == ir.VarGlobal {
			l.scope[vd.Name] = symbol{slot: -1, typ: t, kind: ir.VarGlobal, global: vd.Name}
			continue
		}
		slot := len(l.irProg.Slots)
		l.irProg.Slots = append(l.irProg.Slots, ir.VarSlot{
			Name:     vd.Name,
			Type:     t,
			Init:     init,
			Retained: vd.Retain,
			Kind:     kind,
		})
		l.irProg.SlotIndex[vd.Name] = slot
		l.scope[vd.Name] = symbol{slot: slot, typ: t, kind: kind}
	}
	return nil
}

func varKindFor(k string) ir.VarKind {
	switch strings.ToLower(k) {
	case "input":
		return ir.VarInput
	case "output":
		return ir.VarOutput
	case "global":
		return ir.VarGlobal
	}
	return ir.VarLocal
}

func (l *lowerer) resolveType(name string) (*ir.Type, error) {
	if t, err := resolveScalar(name); err == nil {
		return t, nil
	}
	if t := ir.LookupFBType(name); t != nil {
		return t, nil
	}
	if l.userFBs != nil {
		if def, ok := l.userFBs[name]; ok && def != nil {
			return &ir.Type{Kind: ir.TypeFB, FB: def}, nil
		}
	}
	return nil, fmt.Errorf("unknown type %q", name)
}

func resolveScalar(name string) (*ir.Type, error) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
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

// parseInit accepts a small set of literal forms — enough to seed scalar
// VARs without dragging the full ST expression parser into LAD. Anything
// fancier should be expressed in ST.
func parseInit(src string, t *ir.Type) (ir.Value, error) {
	src = strings.TrimSpace(src)
	switch t.Kind {
	case ir.TypeBool:
		switch strings.ToUpper(src) {
		case "TRUE", "1":
			return ir.BoolVal(true), nil
		case "FALSE", "0":
			return ir.BoolVal(false), nil
		}
	case ir.TypeInt:
		v, err := strconv.ParseInt(src, 10, 64)
		if err != nil {
			return ir.Value{}, err
		}
		return ir.IntVal(v), nil
	case ir.TypeReal:
		v, err := strconv.ParseFloat(src, 64)
		if err != nil {
			return ir.Value{}, err
		}
		return ir.RealVal(v), nil
	case ir.TypeTime:
		return parseTimeInit(src)
	case ir.TypeString:
		// Strip surrounding single quotes if present (IEC ST style).
		if len(src) >= 2 && src[0] == '\'' && src[len(src)-1] == '\'' {
			src = src[1 : len(src)-1]
		}
		return ir.StringVal(src), nil
	}
	return ir.Value{}, fmt.Errorf("unsupported init literal %q for %s", src, t)
}

func parseTimeInit(src string) (ir.Value, error) {
	// Accept "T#5s", "TIME#5s", or bare "5s".
	upper := strings.ToUpper(src)
	for _, prefix := range []string{"T#", "TIME#", "LT#", "LTIME#"} {
		if strings.HasPrefix(upper, prefix) {
			src = src[len(prefix):]
			break
		}
	}
	return ir.TimeVal(int64(st.ParseTimeMs(src))), nil
}

// ─── Rung lowering ──────────────────────────────────────────────────────────

func (l *lowerer) lowerRungs() error {
	for i, r := range l.diag.Rungs {
		if err := l.lowerRung(i, r); err != nil {
			return fmt.Errorf("rung %d: %w", i, err)
		}
	}
	return nil
}

func (l *lowerer) lowerRung(idx int, r *Rung) error {
	power, err := l.lowerElement(r.Logic)
	if err != nil {
		return fmt.Errorf("logic: %w", err)
	}
	if power.ExprType().Kind != ir.TypeBool {
		return fmt.Errorf("rung logic must be BOOL, got %s", power.ExprType())
	}
	// Materialise the rung's power flow into a synthetic local slot so each
	// output reads the same boolean without re-evaluating the contact tree.
	rungSlot := len(l.irProg.Slots)
	rungName := fmt.Sprintf("__rung_%d", idx)
	l.irProg.Slots = append(l.irProg.Slots, ir.VarSlot{
		Name: rungName,
		Type: ir.BoolT,
		Kind: ir.VarLocal,
	})
	l.irProg.SlotIndex[rungName] = rungSlot
	rungRef := func() ir.Expr {
		return &ir.SlotRef{Slot: rungSlot, T: ir.BoolT}
	}

	l.irProg.Body = append(l.irProg.Body, &ir.Assign{
		Target: &ir.SlotRef{Slot: rungSlot, T: ir.BoolT},
		Value:  power,
	})
	for i, out := range r.Outputs {
		if err := l.lowerOutput(out, rungRef); err != nil {
			return fmt.Errorf("output %d: %w", i, err)
		}
	}
	return nil
}

func (l *lowerer) lowerElement(e Element) (ir.Expr, error) {
	switch n := e.(type) {
	case *Contact:
		ref, err := l.resolveOperand(n.Operand)
		if err != nil {
			return nil, err
		}
		if ref.ExprType().Kind != ir.TypeBool {
			return nil, fmt.Errorf("contact %q must be BOOL, got %s", n.Operand, ref.ExprType())
		}
		if n.Form == "NC" {
			return &ir.UnOp{Op: ir.OpNot, X: ref, T: ir.BoolT}, nil
		}
		return ref, nil
	case *Series:
		return l.foldBool(n.Items, ir.OpAnd)
	case *Parallel:
		return l.foldBool(n.Items, ir.OpOr)
	}
	return nil, fmt.Errorf("unknown element %T", e)
}

func (l *lowerer) foldBool(items []Element, op ir.BinKind) (ir.Expr, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("empty branch")
	}
	cur, err := l.lowerElement(items[0])
	if err != nil {
		return nil, err
	}
	if cur.ExprType().Kind != ir.TypeBool {
		return nil, fmt.Errorf("branch element must be BOOL, got %s", cur.ExprType())
	}
	for _, it := range items[1:] {
		next, err := l.lowerElement(it)
		if err != nil {
			return nil, err
		}
		if next.ExprType().Kind != ir.TypeBool {
			return nil, fmt.Errorf("branch element must be BOOL, got %s", next.ExprType())
		}
		cur = &ir.BinOp{Op: op, L: cur, R: next, T: ir.BoolT}
	}
	return cur, nil
}

func (l *lowerer) lowerOutput(out Output, power func() ir.Expr) error {
	switch n := out.(type) {
	case *Coil:
		return l.lowerCoil(n, power)
	case *FBCall:
		return l.lowerFBCall(n, power)
	}
	return fmt.Errorf("unknown output %T", out)
}

func (l *lowerer) lowerCoil(c *Coil, power func() ir.Expr) error {
	target, err := l.resolveOperandLValue(c.Operand)
	if err != nil {
		return err
	}
	if target.ExprType().Kind != ir.TypeBool {
		return fmt.Errorf("coil %q must target BOOL, got %s", c.Operand, target.ExprType())
	}
	switch c.Form {
	case "OTE":
		l.irProg.Body = append(l.irProg.Body, &ir.Assign{Target: target, Value: power()})
	case "OTL":
		l.irProg.Body = append(l.irProg.Body, &ir.If{
			Cond: power(),
			Then: []ir.Stmt{&ir.Assign{Target: target, Value: &ir.Lit{V: ir.BoolVal(true), T: ir.BoolT}}},
		})
	case "OTU":
		l.irProg.Body = append(l.irProg.Body, &ir.If{
			Cond: power(),
			Then: []ir.Stmt{&ir.Assign{Target: target, Value: &ir.Lit{V: ir.BoolVal(false), T: ir.BoolT}}},
		})
	default:
		return fmt.Errorf("unknown coil form %q", c.Form)
	}
	return nil
}

func (l *lowerer) lowerFBCall(fc *FBCall, power func() ir.Expr) error {
	sym, ok := l.scope[fc.Instance]
	if !ok {
		return fmt.Errorf("FB call %q: undeclared instance", fc.Instance)
	}
	if sym.kind == ir.VarGlobal {
		return fmt.Errorf("FB call %q: instance must be local, not global", fc.Instance)
	}
	if sym.typ == nil || sym.typ.Kind != ir.TypeFB {
		return fmt.Errorf("FB call %q: declared as %s, not a function block", fc.Instance, sym.typ)
	}
	def := sym.typ.FB
	if len(def.Inputs) == 0 {
		return fmt.Errorf("FB call %q: %s has no inputs to drive from power flow", fc.Instance, def.Name)
	}
	powerInput := fc.PowerInput
	if powerInput == "" {
		powerInput = def.Inputs[0].Name
	}
	powerIdx, ok := def.SlotIndex[powerInput]
	if !ok || powerIdx >= len(def.Inputs) {
		return fmt.Errorf("FB %s has no input named %q", def.Name, powerInput)
	}
	if def.Inputs[powerIdx].Type.Kind != ir.TypeBool {
		return fmt.Errorf("FB %s power input %q must be BOOL, got %s", def.Name, powerInput, def.Inputs[powerIdx].Type)
	}
	bindings := []ir.FBInput{{
		SlotIdx: powerIdx,
		Value:   power(),
	}}
	for name, expr := range fc.Inputs {
		if name == powerInput {
			return fmt.Errorf("FB call %q: input %q already bound to power flow", fc.Instance, name)
		}
		idx, ok := def.SlotIndex[name]
		if !ok {
			return fmt.Errorf("FB %s has no input %q", def.Name, name)
		}
		if idx >= len(def.Inputs) {
			return fmt.Errorf("FB %s field %q is not an input", def.Name, name)
		}
		v, err := l.lowerExpr(expr, def.Inputs[idx].Type)
		if err != nil {
			return fmt.Errorf("FB %s arg %q: %w", def.Name, name, err)
		}
		bindings = append(bindings, ir.FBInput{SlotIdx: idx, Value: v})
	}
	l.irProg.Body = append(l.irProg.Body, &ir.FBCall{
		InstanceSlot: sym.slot,
		Def:          def,
		Inputs:       bindings,
	})
	return nil
}

func (l *lowerer) lowerExpr(e Expr, want *ir.Type) (ir.Expr, error) {
	switch n := e.(type) {
	case *Ref:
		ref, err := l.resolveOperand(n.Name)
		if err != nil {
			return nil, err
		}
		return ref, nil
	case *IntLit:
		if want != nil && want.Kind == ir.TypeReal {
			return &ir.Lit{V: ir.RealVal(float64(n.V)), T: ir.RealT}, nil
		}
		if want != nil && want.Kind == ir.TypeTime {
			return &ir.Lit{V: ir.TimeVal(n.V), T: ir.TimeT}, nil
		}
		return &ir.Lit{V: ir.IntVal(n.V), T: ir.IntT}, nil
	case *RealLit:
		return &ir.Lit{V: ir.RealVal(n.V), T: ir.RealT}, nil
	case *BoolLit:
		return &ir.Lit{V: ir.BoolVal(n.V), T: ir.BoolT}, nil
	case *TimeLit:
		ms := n.Ms
		if ms == 0 && n.Raw != "" {
			ms = int64(st.ParseTimeMs(n.Raw))
		}
		return &ir.Lit{V: ir.TimeVal(ms), T: ir.TimeT}, nil
	case *StringLit:
		return &ir.Lit{V: ir.StringVal(n.V), T: ir.StringT}, nil
	}
	return nil, fmt.Errorf("unsupported expression %T", e)
}

// resolveOperand turns an operand string ("name" or "name.field") into
// an IR expression. Member access supports FB outputs and UDT fields.
func (l *lowerer) resolveOperand(name string) (ir.Expr, error) {
	parts := strings.Split(name, ".")
	root := parts[0]
	sym, ok := l.scope[root]
	if !ok {
		return nil, fmt.Errorf("undeclared identifier %q (declare in variables)", root)
	}
	var cur ir.Expr
	if sym.kind == ir.VarGlobal {
		cur = &ir.GlobalRef{Name: sym.global, T: sym.typ}
	} else {
		cur = &ir.SlotRef{Slot: sym.slot, T: sym.typ}
	}
	curT := sym.typ
	for _, member := range parts[1:] {
		switch curT.Kind {
		case ir.TypeFB:
			idx, ok := curT.FB.SlotIndex[member]
			if !ok {
				return nil, fmt.Errorf("FB %s has no field %q", curT.FB.Name, member)
			}
			all := curT.FB.AllSlots()
			cur = &ir.MemberRef{Object: cur, FieldIdx: idx, T: all[idx].Type}
			curT = all[idx].Type
		case ir.TypeStruct:
			idx, ok := curT.Struct.FieldIndex[member]
			if !ok {
				return nil, fmt.Errorf("struct %s has no field %q", curT.Struct.Name, member)
			}
			cur = &ir.MemberRef{Object: cur, FieldIdx: idx, T: curT.Struct.Fields[idx].Type}
			curT = curT.Struct.Fields[idx].Type
		default:
			return nil, fmt.Errorf("cannot access %q on non-struct type %s", member, curT)
		}
	}
	return cur, nil
}

func (l *lowerer) resolveOperandLValue(name string) (ir.LValue, error) {
	e, err := l.resolveOperand(name)
	if err != nil {
		return nil, err
	}
	lv, ok := e.(ir.LValue)
	if !ok {
		return nil, fmt.Errorf("operand %q is not assignable", name)
	}
	return lv, nil
}
