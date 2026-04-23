//go:build plc || all

package ir

import "fmt"

// Host exposes the surrounding PLC runtime to the IR VM.
// The engine integration (phase 3) adapts the tentacle variable store to this interface.
type Host interface {
	ReadGlobal(name string) (Value, error)
	WriteGlobal(name string, v Value) error
	NowMs() int64
}

// EvalCtx is the per-scan evaluation state. A fresh ctx is used per Run call;
// the Frame (retained state) is stable across scans.
type EvalCtx struct {
	Program *Program
	Frame   *Frame
	Host    Host

	// control-flow sentinels
	returning bool
}

// Run executes one scan of the program body against the frame.
func Run(prog *Program, frame *Frame, host Host) error {
	ctx := &EvalCtx{Program: prog, Frame: frame, Host: host}
	return execBlock(ctx, prog.Body)
}

func execBlock(ctx *EvalCtx, stmts []Stmt) error {
	for _, s := range stmts {
		if err := execStmt(ctx, s); err != nil {
			return err
		}
		if ctx.returning {
			return nil
		}
	}
	return nil
}

func execStmt(ctx *EvalCtx, s Stmt) error {
	switch n := s.(type) {
	case *Assign:
		v, err := evalExpr(ctx, n.Value)
		if err != nil {
			return err
		}
		return writeLValue(ctx, n.Target, v)

	case *If:
		cv, err := evalExpr(ctx, n.Cond)
		if err != nil {
			return err
		}
		if cv.B {
			return execBlock(ctx, n.Then)
		}
		return execBlock(ctx, n.Else)

	case *For:
		startV, err := evalExpr(ctx, n.Start)
		if err != nil {
			return err
		}
		endV, err := evalExpr(ctx, n.End)
		if err != nil {
			return err
		}
		step := int64(1)
		if n.Step != nil {
			sv, err := evalExpr(ctx, n.Step)
			if err != nil {
				return err
			}
			step = sv.I
		}
		if step == 0 {
			return fmt.Errorf("FOR step is zero")
		}
		for i := startV.I; (step > 0 && i <= endV.I) || (step < 0 && i >= endV.I); i += step {
			ctx.Frame.Slots[n.Slot] = IntVal(i)
			if err := execBlock(ctx, n.Body); err != nil {
				return err
			}
			if ctx.returning {
				return nil
			}
		}
		return nil

	case *While:
		const guard = 1_000_000 // bound to prevent scan-hang from runaway loop
		for i := 0; i < guard; i++ {
			cv, err := evalExpr(ctx, n.Cond)
			if err != nil {
				return err
			}
			if !cv.B {
				return nil
			}
			if err := execBlock(ctx, n.Body); err != nil {
				return err
			}
			if ctx.returning {
				return nil
			}
		}
		return fmt.Errorf("WHILE exceeded iteration guard")

	case *Repeat:
		const guard = 1_000_000
		for i := 0; i < guard; i++ {
			if err := execBlock(ctx, n.Body); err != nil {
				return err
			}
			if ctx.returning {
				return nil
			}
			cv, err := evalExpr(ctx, n.Cond)
			if err != nil {
				return err
			}
			if cv.B {
				return nil
			}
		}
		return fmt.Errorf("REPEAT exceeded iteration guard")

	case *Case:
		sv, err := evalExpr(ctx, n.Expr)
		if err != nil {
			return err
		}
		for _, cl := range n.Clauses {
			for _, vExpr := range cl.Values {
				vv, err := evalExpr(ctx, vExpr)
				if err != nil {
					return err
				}
				if valEq(sv, vv) {
					return execBlock(ctx, cl.Body)
				}
			}
		}
		return execBlock(ctx, n.Else)

	case *Return:
		ctx.returning = true
		return nil
	}
	return fmt.Errorf("unknown stmt %T", s)
}

func writeLValue(ctx *EvalCtx, lv LValue, v Value) error {
	switch n := lv.(type) {
	case *SlotRef:
		ctx.Frame.Slots[n.Slot] = v
		return nil
	case *GlobalRef:
		return ctx.Host.WriteGlobal(n.Name, v)
	}
	return fmt.Errorf("unknown lvalue %T", lv)
}

func evalExpr(ctx *EvalCtx, e Expr) (Value, error) {
	switch n := e.(type) {
	case *Lit:
		return n.V, nil
	case *SlotRef:
		return ctx.Frame.Slots[n.Slot], nil
	case *GlobalRef:
		return ctx.Host.ReadGlobal(n.Name)
	case *BinOp:
		l, err := evalExpr(ctx, n.L)
		if err != nil {
			return Value{}, err
		}
		r, err := evalExpr(ctx, n.R)
		if err != nil {
			return Value{}, err
		}
		return evalBin(n.Op, l, r, n.T), nil
	case *UnOp:
		x, err := evalExpr(ctx, n.X)
		if err != nil {
			return Value{}, err
		}
		return evalUn(n.Op, x, n.T), nil
	}
	return Value{}, fmt.Errorf("unknown expr %T", e)
}

func evalBin(op BinKind, l, r Value, t *Type) Value {
	switch op {
	case OpAdd:
		if t.Kind == TypeReal {
			return RealVal(l.F + r.F)
		}
		return Value{Kind: t.Kind, I: l.I + r.I}
	case OpSub:
		if t.Kind == TypeReal {
			return RealVal(l.F - r.F)
		}
		return Value{Kind: t.Kind, I: l.I - r.I}
	case OpMul:
		if t.Kind == TypeReal {
			return RealVal(l.F * r.F)
		}
		return Value{Kind: t.Kind, I: l.I * r.I}
	case OpDiv:
		if t.Kind == TypeReal {
			if r.F == 0 {
				return RealVal(0)
			}
			return RealVal(l.F / r.F)
		}
		if r.I == 0 {
			return Value{Kind: t.Kind}
		}
		return Value{Kind: t.Kind, I: l.I / r.I}
	case OpMod:
		if r.I == 0 {
			return Value{Kind: t.Kind}
		}
		return Value{Kind: t.Kind, I: l.I % r.I}
	case OpEq:
		return BoolVal(valEq(l, r))
	case OpNeq:
		return BoolVal(!valEq(l, r))
	case OpLt:
		if l.Kind == TypeReal || r.Kind == TypeReal {
			return BoolVal(asFloat(l) < asFloat(r))
		}
		return BoolVal(l.I < r.I)
	case OpLte:
		if l.Kind == TypeReal || r.Kind == TypeReal {
			return BoolVal(asFloat(l) <= asFloat(r))
		}
		return BoolVal(l.I <= r.I)
	case OpGt:
		if l.Kind == TypeReal || r.Kind == TypeReal {
			return BoolVal(asFloat(l) > asFloat(r))
		}
		return BoolVal(l.I > r.I)
	case OpGte:
		if l.Kind == TypeReal || r.Kind == TypeReal {
			return BoolVal(asFloat(l) >= asFloat(r))
		}
		return BoolVal(l.I >= r.I)
	case OpAnd:
		return BoolVal(l.B && r.B)
	case OpOr:
		return BoolVal(l.B || r.B)
	case OpXor:
		if t.Kind == TypeBool {
			return BoolVal(l.B != r.B)
		}
		return Value{Kind: t.Kind, I: l.I ^ r.I}
	}
	return Value{}
}

func evalUn(op UnKind, x Value, t *Type) Value {
	switch op {
	case OpNeg:
		if t.Kind == TypeReal {
			return RealVal(-x.F)
		}
		return Value{Kind: t.Kind, I: -x.I}
	case OpNot:
		return BoolVal(!x.B)
	}
	return Value{}
}

func valEq(l, r Value) bool {
	if l.Kind == TypeReal || r.Kind == TypeReal {
		return asFloat(l) == asFloat(r)
	}
	if l.Kind != r.Kind {
		return false
	}
	switch l.Kind {
	case TypeBool:
		return l.B == r.B
	case TypeInt, TypeTime:
		return l.I == r.I
	case TypeString:
		return l.S == r.S
	}
	return false
}

func asFloat(v Value) float64 {
	if v.Kind == TypeReal {
		return v.F
	}
	return float64(v.I)
}
