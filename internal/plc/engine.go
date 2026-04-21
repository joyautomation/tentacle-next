//go:build plc || all

package plc

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	itypes "github.com/joyautomation/tentacle/internal/types"
	"go.starlark.net/starlark"
)

// Engine manages Starlark program compilation and execution.
type Engine struct {
	mu        sync.RWMutex
	programs  map[string]*compiledProgram
	builtins  starlark.StringDict
	vars      *VariableStore
	templates map[string]*itypes.PlcTemplate
	log       *slog.Logger
}

// compiledProgram holds a parsed Starlark program and its entry function.
type compiledProgram struct {
	name    string
	globals starlark.StringDict
	mainFn  starlark.Callable
}

// TaskState holds persistent state for a single task across scan cycles.
type TaskState struct {
	Timers   map[string]*TimerState
	Counters map[string]*CounterState
	RungEdge map[string]bool

	// thread is reused across scans to avoid allocating a fresh
	// starlark.Thread per tick. Lazily initialised by Engine.Execute.
	thread *starlark.Thread
}

// TimerState tracks a timer's persistent state.
type TimerState struct {
	Preset   time.Duration
	ACC      time.Duration
	DN       bool
	EN       bool
	TT       bool
	lastTick time.Time
	prevEN   bool
}

// CounterState tracks a counter's persistent state.
type CounterState struct {
	Preset int
	ACC    int
	DN     bool
	prevEN bool
}

// NewTaskState creates a fresh TaskState.
func NewTaskState() *TaskState {
	return &TaskState{
		Timers:   make(map[string]*TimerState),
		Counters: make(map[string]*CounterState),
		RungEdge: make(map[string]bool),
	}
}

// NewEngine creates a new Starlark execution engine.
func NewEngine(vars *VariableStore, log *slog.Logger) *Engine {
	e := &Engine{
		programs:  make(map[string]*compiledProgram),
		vars:      vars,
		templates: make(map[string]*itypes.PlcTemplate),
		log:       log,
	}
	e.builtins = e.makeBuiltins()
	return e
}

// Compile parses and compiles a Starlark source program.
// The program must define a main() function.
func (e *Engine) Compile(name, source string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	thread := &starlark.Thread{Name: name}
	globals, err := starlark.ExecFile(thread, name+".star", source, e.builtins)
	if err != nil {
		return fmt.Errorf("compile %s: %w", name, err)
	}

	mainFn, ok := globals["main"]
	if !ok {
		return fmt.Errorf("compile %s: program must define a main() function", name)
	}
	callable, ok := mainFn.(starlark.Callable)
	if !ok {
		return fmt.Errorf("compile %s: main is not callable", name)
	}

	e.programs[name] = &compiledProgram{
		name:    name,
		globals: globals,
		mainFn:  callable,
	}
	return nil
}

// Remove deletes a compiled program.
func (e *Engine) Remove(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.programs, name)
}

// Execute runs a compiled program's main() function with the given TaskState.
func (e *Engine) Execute(name string, state *TaskState) error {
	e.mu.RLock()
	prog, ok := e.programs[name]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("execute: program %q not found", name)
	}

	if state.thread == nil {
		state.thread = &starlark.Thread{Name: name}
		state.thread.SetLocal("taskState", state)
		state.thread.SetLocal("vars", e.vars)
	}

	_, err := starlark.Call(state.thread, prog.mainFn, nil, nil)
	if err != nil {
		return fmt.Errorf("execute %s: %w", name, err)
	}
	return nil
}

// HasProgram returns true if a program with the given name is compiled.
func (e *Engine) HasProgram(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.programs[name]
	return ok
}

// ProgramCount returns the number of compiled programs.
func (e *Engine) ProgramCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.programs)
}
