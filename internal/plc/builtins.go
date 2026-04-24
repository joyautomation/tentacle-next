//go:build plc || all

package plc

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
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

		// Direct device tag read by (deviceId, tagPath). Returns None if
		// the PLC hasn't yet observed a value for that tag on the bus.
		"read_tag": starlark.NewBuiltin("read_tag", e.builtinReadTag),

		// Logging — all output flows to slog, visible on the Logs tab
		"log":       starlark.NewBuiltin("log", e.makeLogBuiltin(slog.LevelInfo)),
		"log_debug": starlark.NewBuiltin("log_debug", e.makeLogBuiltin(slog.LevelDebug)),
		"log_info":  starlark.NewBuiltin("log_info", e.makeLogBuiltin(slog.LevelInfo)),
		"log_warn":  starlark.NewBuiltin("log_warn", e.makeLogBuiltin(slog.LevelWarn)),
		"log_error": starlark.NewBuiltin("log_error", e.makeLogBuiltin(slog.LevelError)),

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

func (e *Engine) builtinReadTag(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var deviceID, tagPath string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &deviceID, &tagPath); err != nil {
		return nil, err
	}
	if e.deviceTags == nil {
		return starlark.None, nil
	}
	// Aggregate first: if this path has any children (i.e. names a
	// template instance), return the whole struct as a dict. Some
	// scanners also publish the struct root as a stringified value, so
	// a naive leaf-first lookup would hide the fields.
	if children, ok := e.deviceTags.GetAggregate(deviceID, tagPath); ok {
		return goMapToStarlark(children), nil
	}
	if v, ok := e.deviceTags.Get(deviceID, tagPath); ok {
		return goToStarlark(v), nil
	}
	return starlark.None, nil
}

// goMapToStarlark wraps a Go map of scalar values as a Starlark dict.
// Values are converted via goToStarlark; keys are strings. Used by
// read_tag when aggregating a template instance's fields.
func goMapToStarlark(m map[string]interface{}) starlark.Value {
	d := starlark.NewDict(len(m))
	for k, v := range m {
		_ = d.SetKey(starlark.String(k), goToStarlark(v))
	}
	return d
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

// ─── Logging Built-ins ──────────────────────────────────────────────────────

// makeLogBuiltin returns a Starlark function that emits to slog at the given level.
// Args are stringified and joined with spaces (Python print-style). If the calling
// thread carries a *testLogBuffer local, the formatted line is also appended to
// it so unit-test runs can surface their output in the UI.
func (e *Engine) makeLogBuiltin(level slog.Level) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		parts := make([]string, 0, len(args))
		for _, a := range args {
			if s, ok := a.(starlark.String); ok {
				parts = append(parts, string(s))
			} else {
				parts = append(parts, a.String())
			}
		}
		msg := strings.Join(parts, " ")
		if buf, ok := thread.Local("test_log_buffer").(*testLogBuffer); ok && buf != nil {
			buf.append(level.String() + " " + msg)
		}
		if e.log != nil {
			e.log.Log(context.Background(), level, msg, "program", thread.Name)
		}
		return starlark.None, nil
	}
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
// Values that are already Starlark (e.g. *StructValue for template-typed
// variables) pass through unchanged.
func goToStarlark(v interface{}) starlark.Value {
	if v == nil {
		return starlark.None
	}
	if sv, ok := v.(starlark.Value); ok {
		return sv
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
// Template-typed struct values are returned as-is so the variable store
// keeps referring to the live instance (no copy, no serialization loss).
func starlarkToGo(v starlark.Value) interface{} {
	if sv, ok := v.(*StructValue); ok {
		return sv
	}
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
