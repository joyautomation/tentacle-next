//go:build plc || all

package plc

import (
	"fmt"
	"log/slog"
	"sort"
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

// compiledProgram holds a parsed Starlark program's global namespace.
// Any top-level callable can be used as a task entry function.
type compiledProgram struct {
	name    string
	globals starlark.StringDict
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
// Any top-level callable in the program can serve as a task entry point.
func (e *Engine) Compile(name, source string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	thread := &starlark.Thread{Name: name}
	globals, err := starlark.ExecFile(thread, name+".star", source, e.builtins)
	if err != nil {
		return fmt.Errorf("compile %s: %w", name, err)
	}

	e.programs[name] = &compiledProgram{
		name:    name,
		globals: globals,
	}
	return nil
}

// Remove deletes a compiled program.
func (e *Engine) Remove(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.programs, name)
}

// Execute runs a named top-level function in a compiled program with the given
// TaskState. fnName defaults to "main" when empty for backwards compatibility.
func (e *Engine) Execute(name, fnName string, state *TaskState) error {
	if fnName == "" {
		fnName = "main"
	}
	e.mu.RLock()
	prog, ok := e.programs[name]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("execute: program %q not found", name)
	}

	fnVal, ok := prog.globals[fnName]
	if !ok {
		return fmt.Errorf("execute %s: function %q not found", name, fnName)
	}
	callable, ok := fnVal.(starlark.Callable)
	if !ok {
		return fmt.Errorf("execute %s: %q is not callable", name, fnName)
	}

	if state.thread == nil {
		state.thread = &starlark.Thread{Name: name}
		state.thread.SetLocal("taskState", state)
		state.thread.SetLocal("vars", e.vars)
	}

	_, err := starlark.Call(state.thread, callable, nil, nil)
	if err != nil {
		return fmt.Errorf("execute %s.%s: %w", name, fnName, err)
	}
	return nil
}

// EntryFunctions returns the names of top-level callables in a compiled
// program, sorted alphabetically. Useful for populating task-entry dropdowns.
func (e *Engine) EntryFunctions(name string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	prog, ok := e.programs[name]
	if !ok {
		return nil
	}
	var out []string
	for k, v := range prog.globals {
		if _, ok := v.(starlark.Callable); ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
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
