//go:build plc || all

package plc

import (
	"fmt"
	"math"
	"strings"

	"go.starlark.net/starlark"
)

// makeAssertBuiltins returns the assertion helpers available to unit tests.
// These are added to the predeclared scope only when a test is running.
//
// run_program / reset_program are the canonical IR-program drivers; they
// dispatch to whichever language (ST or LAD) currently owns the named
// program. run_st / reset_st remain as backwards-compatible aliases.
func (e *Engine) makeAssertBuiltins() starlark.StringDict {
	return starlark.StringDict{
		"assert_eq":     starlark.NewBuiltin("assert_eq", builtinAssertEq),
		"assert_ne":     starlark.NewBuiltin("assert_ne", builtinAssertNe),
		"assert_true":   starlark.NewBuiltin("assert_true", builtinAssertTrue),
		"assert_false":  starlark.NewBuiltin("assert_false", builtinAssertFalse),
		"assert_near":   starlark.NewBuiltin("assert_near", builtinAssertNear),
		"assert_raises": starlark.NewBuiltin("assert_raises", builtinAssertRaises),
		"fail":          starlark.NewBuiltin("fail", builtinFail),
		"run_program":   starlark.NewBuiltin("run_program", e.builtinRunProgram),
		"reset_program": starlark.NewBuiltin("reset_program", e.builtinResetProgram),
		"run_st":        starlark.NewBuiltin("run_st", e.builtinRunProgram),
		"reset_st":      starlark.NewBuiltin("reset_st", e.builtinResetProgram),
		"run_lad":       starlark.NewBuiltin("run_lad", e.builtinRunProgram),
		"reset_lad":     starlark.NewBuiltin("reset_lad", e.builtinResetProgram),
	}
}

// builtinRunProgram executes one scan of a named IR-backed program.
// State (retained vars, FB instances) is preserved across calls within a
// single test by keeping the TaskState in a thread-local map keyed by
// program name. Reset state explicitly with reset_program(name).
func (e *Engine) builtinRunProgram(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	if !e.HasProgram(name) {
		return nil, fmt.Errorf("%s: program %q not found", fn.Name(), name)
	}
	states, _ := thread.Local("ir_states").(map[string]*TaskState)
	if states == nil {
		states = map[string]*TaskState{}
		thread.SetLocal("ir_states", states)
	}
	state, ok := states[name]
	if !ok {
		state = NewTaskState()
		states[name] = state
	}
	if err := e.Execute(name, "", state); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// builtinResetProgram clears the cached TaskState for an IR program so
// the next run_program call starts with a fresh frame (useful between
// sub-tests).
func (e *Engine) builtinResetProgram(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	if states, ok := thread.Local("ir_states").(map[string]*TaskState); ok {
		delete(states, name)
	}
	return starlark.None, nil
}

func assertPrefix(msg string) string {
	if msg == "" {
		return ""
	}
	return msg + ": "
}

func builtinAssertEq(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var actual, expected starlark.Value
	msg := ""
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "actual", &actual, "expected", &expected, "msg?", &msg); err != nil {
		return nil, err
	}
	eq, err := starlark.Equal(actual, expected)
	if err != nil {
		return nil, fmt.Errorf("%sassert_eq compare: %w", assertPrefix(msg), err)
	}
	if !eq {
		return nil, fmt.Errorf("%sassert_eq failed: expected %s, got %s", assertPrefix(msg), expected.String(), actual.String())
	}
	return starlark.None, nil
}

func builtinAssertNe(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var actual, expected starlark.Value
	msg := ""
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "actual", &actual, "expected", &expected, "msg?", &msg); err != nil {
		return nil, err
	}
	eq, err := starlark.Equal(actual, expected)
	if err != nil {
		return nil, fmt.Errorf("%sassert_ne compare: %w", assertPrefix(msg), err)
	}
	if eq {
		return nil, fmt.Errorf("%sassert_ne failed: both sides equal %s", assertPrefix(msg), actual.String())
	}
	return starlark.None, nil
}

func builtinAssertTrue(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond starlark.Value
	msg := ""
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "cond", &cond, "msg?", &msg); err != nil {
		return nil, err
	}
	if !bool(cond.Truth()) {
		return nil, fmt.Errorf("%sassert_true failed: value is %s", assertPrefix(msg), cond.String())
	}
	return starlark.None, nil
}

func builtinAssertFalse(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond starlark.Value
	msg := ""
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "cond", &cond, "msg?", &msg); err != nil {
		return nil, err
	}
	if bool(cond.Truth()) {
		return nil, fmt.Errorf("%sassert_false failed: value is %s", assertPrefix(msg), cond.String())
	}
	return starlark.None, nil
}

func builtinAssertNear(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var actual, expected starlark.Float
	tol := starlark.Float(1e-6)
	msg := ""
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "actual", &actual, "expected", &expected, "tolerance?", &tol, "msg?", &msg); err != nil {
		return nil, err
	}
	diff := math.Abs(float64(actual) - float64(expected))
	if diff > float64(tol) {
		return nil, fmt.Errorf("%sassert_near failed: |%v - %v| = %v > %v", assertPrefix(msg), float64(actual), float64(expected), diff, float64(tol))
	}
	return starlark.None, nil
}

// assert_raises(substring, fn, *args) — calls fn(*args) and expects the
// returned error message to contain substring.
func builtinAssertRaises(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("assert_raises: expected (substring, callable, *args)")
	}
	subStr, ok := args[0].(starlark.String)
	if !ok {
		return nil, fmt.Errorf("assert_raises: first argument must be a string")
	}
	callable, ok := args[1].(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("assert_raises: second argument must be callable")
	}
	rest := args[2:]
	_, err := starlark.Call(thread, callable, rest, kwargs)
	if err == nil {
		return nil, fmt.Errorf("assert_raises failed: expected error containing %q, got no error", string(subStr))
	}
	if !strings.Contains(err.Error(), string(subStr)) {
		return nil, fmt.Errorf("assert_raises failed: expected error containing %q, got %q", string(subStr), err.Error())
	}
	return starlark.None, nil
}

func builtinFail(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if s, ok := a.(starlark.String); ok {
			parts = append(parts, string(s))
		} else {
			parts = append(parts, a.String())
		}
	}
	return nil, fmt.Errorf("%s", strings.Join(parts, " "))
}
