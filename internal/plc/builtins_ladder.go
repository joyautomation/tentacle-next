//go:build plc || all

package plc

import (
	"fmt"
	"time"

	"go.starlark.net/starlark"
)

// ─── Element Types ──────────────────────────────────────────────────────────

// ladderElementType distinguishes contacts, coils, timers, counters, and structural elements.
type ladderElementType int

const (
	elemContact ladderElementType = iota
	elemCoil
	elemTimer
	elemCounter
	elemReset
	elemBranch
	elemSeries
)

// ladderElement is a Starlark value representing a ladder logic element.
// Its String() method produces canonical Starlark source for round-trip parsing.
type ladderElement struct {
	kind      ladderElementType
	subtype   string // "NO", "NC", "OTE", "OTL", "OTU", "TON", "TOF", "CTU", "CTD", "RES"
	tag       string
	preset    int           // timer ms or counter preset
	children  []*ladderPath // for branch: multiple parallel paths
	elements  []*ladderElement // for series: sequential elements
}

// ladderPath is a single path within a branch (OR logic).
type ladderPath struct {
	elements []*ladderElement
}

// Starlark Value interface implementation.
func (e *ladderElement) String() string {
	switch e.kind {
	case elemContact:
		return fmt.Sprintf("%s(%q)", e.subtype, e.tag)
	case elemCoil:
		return fmt.Sprintf("%s(%q)", e.subtype, e.tag)
	case elemTimer:
		return fmt.Sprintf("%s(%q, %d)", e.subtype, e.tag, e.preset)
	case elemCounter:
		return fmt.Sprintf("%s(%q, %d)", e.subtype, e.tag, e.preset)
	case elemReset:
		return fmt.Sprintf("RES(%q)", e.tag)
	case elemBranch:
		return "branch(...)"
	case elemSeries:
		return "series(...)"
	default:
		return "unknown"
	}
}

func (e *ladderElement) Type() string          { return "LadderElement" }
func (e *ladderElement) Freeze()               {}
func (e *ladderElement) Truth() starlark.Bool   { return true }
func (e *ladderElement) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: LadderElement") }

// isCondition returns true if this element is a contact or structural (branch/series).
func (e *ladderElement) isCondition() bool {
	return e.kind == elemContact || e.kind == elemBranch || e.kind == elemSeries
}

// ─── Element Constructor Built-ins ──────────────────────────────────────────

func builtinNO(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &tag); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemContact, subtype: "NO", tag: tag}, nil
}

func builtinNC(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &tag); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemContact, subtype: "NC", tag: tag}, nil
}

func builtinOTE(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &tag); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemCoil, subtype: "OTE", tag: tag}, nil
}

func builtinOTL(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &tag); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemCoil, subtype: "OTL", tag: tag}, nil
}

func builtinOTU(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &tag); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemCoil, subtype: "OTU", tag: tag}, nil
}

func builtinTON(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	var presetMs int
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &tag, &presetMs); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemTimer, subtype: "TON", tag: tag, preset: presetMs}, nil
}

func builtinTOF(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	var presetMs int
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &tag, &presetMs); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemTimer, subtype: "TOF", tag: tag, preset: presetMs}, nil
}

func builtinCTU(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	var preset int
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &tag, &preset); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemCounter, subtype: "CTU", tag: tag, preset: preset}, nil
}

func builtinCTD(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	var preset int
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &tag, &preset); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemCounter, subtype: "CTD", tag: tag, preset: preset}, nil
}

func builtinRES(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tag string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &tag); err != nil {
		return nil, err
	}
	return &ladderElement{kind: elemReset, subtype: "RES", tag: tag}, nil
}

func builtinBranch(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	elem := &ladderElement{kind: elemBranch, subtype: "branch"}
	for i := 0; i < args.Len(); i++ {
		arg := args.Index(i)
		le, ok := arg.(*ladderElement)
		if !ok {
			return nil, fmt.Errorf("branch: argument %d is not a ladder element", i)
		}
		// Each argument to branch is a single element or a series().
		// Wrap in a path.
		elem.children = append(elem.children, &ladderPath{elements: []*ladderElement{le}})
	}
	return elem, nil
}

func builtinSeries(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	elem := &ladderElement{kind: elemSeries, subtype: "series"}
	for i := 0; i < args.Len(); i++ {
		arg := args.Index(i)
		le, ok := arg.(*ladderElement)
		if !ok {
			return nil, fmt.Errorf("series: argument %d is not a ladder element", i)
		}
		elem.elements = append(elem.elements, le)
	}
	return elem, nil
}

// ─── Rung Evaluation ────────────────────────────────────────────────────────

// builtinRung evaluates a complete ladder rung.
// Arguments are a mix of conditions (contacts, branch, series) and outputs (coils, timers, counters).
// Conditions are evaluated in AND to produce the rung state.
// Outputs are then executed with that rung state.
func (e *Engine) builtinRung(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	state := getTaskState(thread)
	if state == nil {
		return nil, fmt.Errorf("rung: no task state (must be called within a task)")
	}

	// Separate conditions and outputs.
	var conditions []*ladderElement
	var outputs []*ladderElement
	for i := 0; i < args.Len(); i++ {
		le, ok := args.Index(i).(*ladderElement)
		if !ok {
			return nil, fmt.Errorf("rung: argument %d is not a ladder element", i)
		}
		if le.isCondition() {
			conditions = append(conditions, le)
		} else {
			outputs = append(outputs, le)
		}
	}

	// Evaluate conditions in AND (rung starts energized).
	rungState := true
	for _, cond := range conditions {
		rungState = e.evaluateElement(cond, rungState, state)
	}

	// Execute outputs with rung result.
	for _, out := range outputs {
		rungState = e.evaluateElement(out, rungState, state)
	}

	return starlark.None, nil
}

// evaluateElement evaluates a single ladder element with the current rung state.
func (e *Engine) evaluateElement(elem *ladderElement, rungIn bool, state *TaskState) bool {
	switch elem.kind {
	case elemContact:
		return e.evaluateContact(elem, rungIn)
	case elemCoil:
		return e.evaluateCoil(elem, rungIn)
	case elemTimer:
		return e.evaluateTimer(elem, rungIn, state)
	case elemCounter:
		return e.evaluateCounter(elem, rungIn, state)
	case elemReset:
		return e.evaluateReset(elem, rungIn, state)
	case elemBranch:
		return e.evaluateBranch(elem, rungIn, state)
	case elemSeries:
		return e.evaluateSeries(elem, rungIn, state)
	default:
		return rungIn
	}
}

// ─── Contact Evaluation ─────────────────────────────────────────────────────

func (e *Engine) evaluateContact(elem *ladderElement, rungIn bool) bool {
	tagVal := e.vars.GetBool(elem.tag)
	switch elem.subtype {
	case "NO":
		return rungIn && tagVal
	case "NC":
		return rungIn && !tagVal
	default:
		return rungIn
	}
}

// ─── Coil Evaluation ────────────────────────────────────────────────────────

func (e *Engine) evaluateCoil(elem *ladderElement, rungIn bool) bool {
	now := time.Now().UnixMilli()
	switch elem.subtype {
	case "OTE":
		e.vars.Set(elem.tag, rungIn, now)
	case "OTL":
		if rungIn {
			e.vars.Set(elem.tag, true, now)
		}
	case "OTU":
		if rungIn {
			e.vars.Set(elem.tag, false, now)
		}
	}
	return rungIn
}

// ─── Timer Evaluation ───────────────────────────────────────────────────────

func (e *Engine) evaluateTimer(elem *ladderElement, rungIn bool, state *TaskState) bool {
	t, ok := state.Timers[elem.tag]
	if !ok {
		t = &TimerState{Preset: time.Duration(elem.preset) * time.Millisecond}
		state.Timers[elem.tag] = t
	}
	now := time.Now()

	switch elem.subtype {
	case "TON":
		if rungIn {
			t.EN = true
			if !t.DN {
				if !t.TT {
					t.TT = true
					t.lastTick = now
				}
				t.ACC += now.Sub(t.lastTick)
				t.lastTick = now
				if t.ACC >= t.Preset {
					t.DN = true
					t.TT = false
				}
			} else {
				t.lastTick = now
			}
		} else {
			t.ACC = 0
			t.DN = false
			t.EN = false
			t.TT = false
		}

	case "TOF":
		if rungIn {
			t.DN = true
			t.EN = true
			t.TT = false
			t.ACC = 0
			t.lastTick = now
		} else {
			if t.prevEN && !rungIn {
				t.TT = true
				t.lastTick = now
			}
			if t.TT {
				t.ACC += now.Sub(t.lastTick)
				t.lastTick = now
				if t.ACC >= t.Preset {
					t.DN = false
					t.TT = false
					t.EN = false
				}
			}
		}
		t.prevEN = rungIn
	}

	// Publish timer status variables: {tag}.DN, {tag}.ACC
	nowMs := now.UnixMilli()
	e.vars.Set(elem.tag+".DN", t.DN, nowMs)
	e.vars.Set(elem.tag+".ACC", float64(t.ACC.Milliseconds()), nowMs)

	return t.DN
}

// ─── Counter Evaluation ─────────────────────────────────────────────────────

func (e *Engine) evaluateCounter(elem *ladderElement, rungIn bool, state *TaskState) bool {
	c, ok := state.Counters[elem.tag]
	if !ok {
		c = &CounterState{Preset: elem.preset}
		state.Counters[elem.tag] = c
	}

	switch elem.subtype {
	case "CTU":
		if rungIn && !c.prevEN {
			c.ACC++
		}
		if c.ACC >= c.Preset {
			c.DN = true
		}
	case "CTD":
		if rungIn && !c.prevEN {
			c.ACC--
		}
		if c.ACC <= 0 {
			c.DN = true
		}
	}
	c.prevEN = rungIn

	// Publish counter status variables.
	nowMs := time.Now().UnixMilli()
	e.vars.Set(elem.tag+".DN", c.DN, nowMs)
	e.vars.Set(elem.tag+".ACC", float64(c.ACC), nowMs)

	return c.DN
}

// ─── Reset Evaluation ───────────────────────────────────────────────────────

func (e *Engine) evaluateReset(elem *ladderElement, rungIn bool, state *TaskState) bool {
	if rungIn {
		nowMs := time.Now().UnixMilli()
		if c, ok := state.Counters[elem.tag]; ok {
			c.ACC = 0
			c.DN = false
			e.vars.Set(elem.tag+".DN", false, nowMs)
			e.vars.Set(elem.tag+".ACC", float64(0), nowMs)
		}
		if t, ok := state.Timers[elem.tag]; ok {
			t.ACC = 0
			t.DN = false
			t.EN = false
			t.TT = false
			e.vars.Set(elem.tag+".DN", false, nowMs)
			e.vars.Set(elem.tag+".ACC", float64(0), nowMs)
		}
	}
	return rungIn
}

// ─── Structural Evaluation ──────────────────────────────────────────────────

func (e *Engine) evaluateBranch(elem *ladderElement, rungIn bool, state *TaskState) bool {
	result := false
	for _, path := range elem.children {
		pathResult := rungIn
		for _, child := range path.elements {
			pathResult = e.evaluateElement(child, pathResult, state)
		}
		if pathResult {
			result = true
		}
	}
	return result
}

func (e *Engine) evaluateSeries(elem *ladderElement, rungIn bool, state *TaskState) bool {
	result := rungIn
	for _, child := range elem.elements {
		result = e.evaluateElement(child, result, state)
	}
	return result
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func getTaskState(thread *starlark.Thread) *TaskState {
	if v := thread.Local("taskState"); v != nil {
		if ts, ok := v.(*TaskState); ok {
			return ts
		}
	}
	return nil
}
