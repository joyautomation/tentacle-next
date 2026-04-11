//go:build plc || all

package plc

import (
	"fmt"
	"math"
	"time"

	"go.starlark.net/starlark"
)

// makeBuiltins creates the predeclared Starlark functions available to all PLC programs.
func (e *Engine) makeBuiltins() starlark.StringDict {
	return starlark.StringDict{
		// Variable access
		"get_var":  starlark.NewBuiltin("get_var", e.builtinGetVar),
		"get_num":  starlark.NewBuiltin("get_num", e.builtinGetNum),
		"get_bool": starlark.NewBuiltin("get_bool", e.builtinGetBool),
		"get_str":  starlark.NewBuiltin("get_str", e.builtinGetStr),
		"set_var":  starlark.NewBuiltin("set_var", e.builtinSetVar),

		// Math
		"abs":   starlark.NewBuiltin("abs", builtinAbs),
		"clamp": starlark.NewBuiltin("clamp", builtinClamp),
		"sqrt":  starlark.NewBuiltin("sqrt", builtinSqrt),
		"pow":   starlark.NewBuiltin("pow", builtinPow),

		// Ladder logic (Phase 4 will add more)
		"rung":   starlark.NewBuiltin("rung", e.builtinRung),
		"NO":     starlark.NewBuiltin("NO", builtinNO),
		"NC":     starlark.NewBuiltin("NC", builtinNC),
		"OTE":    starlark.NewBuiltin("OTE", builtinOTE),
		"OTL":    starlark.NewBuiltin("OTL", builtinOTL),
		"OTU":    starlark.NewBuiltin("OTU", builtinOTU),
		"TON":    starlark.NewBuiltin("TON", builtinTON),
		"TOF":    starlark.NewBuiltin("TOF", builtinTOF),
		"CTU":    starlark.NewBuiltin("CTU", builtinCTU),
		"CTD":    starlark.NewBuiltin("CTD", builtinCTD),
		"RES":    starlark.NewBuiltin("RES", builtinRES),
		"branch": starlark.NewBuiltin("branch", builtinBranch),
		"series": starlark.NewBuiltin("series", builtinSeries),
	}
}

// ─── Variable Access Built-ins ──────────────────────────────────────────────

func (e *Engine) builtinGetVar(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	val := e.vars.Get(name)
	return goToStarlark(val), nil
}

func (e *Engine) builtinGetNum(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	return starlark.Float(e.vars.GetNumber(name)), nil
}

func (e *Engine) builtinGetBool(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	return starlark.Bool(e.vars.GetBool(name)), nil
}

func (e *Engine) builtinGetStr(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	return starlark.String(e.vars.GetString(name)), nil
}

func (e *Engine) builtinSetVar(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var val starlark.Value
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &name, &val); err != nil {
		return nil, err
	}
	goVal := starlarkToGo(val)
	now := time.Now().UnixMilli()
	if !e.vars.Set(name, goVal, now) {
		return nil, fmt.Errorf("set_var: unknown variable %q", name)
	}
	return starlark.None, nil
}

// ─── Math Built-ins ─────────────────────────────────────────────────────────

func builtinAbs(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Float
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	return starlark.Float(math.Abs(float64(x))), nil
}

func builtinClamp(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val, lo, hi starlark.Float
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 3, &val, &lo, &hi); err != nil {
		return nil, err
	}
	v := float64(val)
	if v < float64(lo) {
		v = float64(lo)
	}
	if v > float64(hi) {
		v = float64(hi)
	}
	return starlark.Float(v), nil
}

func builtinSqrt(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Float
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &x); err != nil {
		return nil, err
	}
	return starlark.Float(math.Sqrt(float64(x))), nil
}

func builtinPow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var base, exp starlark.Float
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &base, &exp); err != nil {
		return nil, err
	}
	return starlark.Float(math.Pow(float64(base), float64(exp))), nil
}

// ─── Type Conversion Helpers ────────────────────────────────────────────────

// goToStarlark converts a Go value to a Starlark value.
func goToStarlark(v interface{}) starlark.Value {
	if v == nil {
		return starlark.None
	}
	switch val := v.(type) {
	case bool:
		return starlark.Bool(val)
	case float64:
		return starlark.Float(val)
	case float32:
		return starlark.Float(float64(val))
	case int:
		return starlark.MakeInt(val)
	case int64:
		return starlark.MakeInt64(val)
	case string:
		return starlark.String(val)
	default:
		return starlark.String(fmt.Sprintf("%v", val))
	}
}

// starlarkToGo converts a Starlark value to a Go value.
func starlarkToGo(v starlark.Value) interface{} {
	switch val := v.(type) {
	case starlark.Bool:
		return bool(val)
	case starlark.Float:
		return float64(val)
	case starlark.Int:
		if i, ok := val.Int64(); ok {
			return float64(i) // normalize to float64 for consistency
		}
		return float64(0)
	case starlark.String:
		return string(val)
	case *starlark.List:
		result := make([]interface{}, val.Len())
		for i := 0; i < val.Len(); i++ {
			result[i] = starlarkToGo(val.Index(i))
		}
		return result
	case *starlark.Dict:
		result := make(map[string]interface{})
		for _, item := range val.Items() {
			key := starlarkToGo(item[0])
			if k, ok := key.(string); ok {
				result[k] = starlarkToGo(item[1])
			}
		}
		return result
	default:
		return nil
	}
}
