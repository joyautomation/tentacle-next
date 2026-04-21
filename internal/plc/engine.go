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
	"go.starlark.net/syntax"
)

// Engine manages Starlark program compilation and execution.
type Engine struct {
	mu        sync.RWMutex
	programs  map[string]*compiledProgram
	sources   map[string]string
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
		sources:   make(map[string]string),
		vars:      vars,
		templates: make(map[string]*itypes.PlcTemplate),
		log:       log,
	}
	e.builtins = e.makeBuiltins()
	return e
}

// Compile parses and compiles a Starlark source program.
// Any top-level callable in the program can serve as a task entry point.
// All currently-registered programs are recompiled so their top-level
// definitions are visible to each other (a program may call another
// program's top-level functions by name).
func (e *Engine) Compile(name, source string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sources[name] = source
	return e.compileAllLocked(name)
}

// Remove deletes a compiled program and recompiles the rest so any
// cross-program references to its top-level defs now fail cleanly.
func (e *Engine) Remove(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.sources, name)
	delete(e.programs, name)
	_ = e.compileAllLocked("")
}

// compileAllLocked rebuilds every program in e.sources with a predeclared
// scope that exposes each other program's top-level def names as proxy
// callables. The proxies resolve the real function at call time, so forward
// and mutual references between programs compile without ordering tricks.
//
// Returns the first compile error encountered. Programs that fail compile
// are dropped from e.programs (caller sees the error via the return value).
//
// focus (optional): if set, the returned error prioritises that program's
// compile error over others — useful when Compile was called for a specific
// program and we want to surface its error to the user.
//
// Caller must hold e.mu.
func (e *Engine) compileAllLocked(focus string) error {
	// Index top-level def names across all programs so we know what each
	// program exports. Programs that fail to parse contribute no exports
	// but won't block cross-linking for the rest.
	ownDefs := map[string]map[string]bool{}
	exportedBy := map[string][]string{}
	for pn, src := range e.sources {
		defs, err := extractTopLevelDefs(src)
		if err != nil {
			continue
		}
		owned := make(map[string]bool, len(defs))
		for _, d := range defs {
			owned[d] = true
			exportedBy[d] = append(exportedBy[d], pn)
		}
		ownDefs[pn] = owned
	}

	newPrograms := make(map[string]*compiledProgram, len(e.sources))
	var firstErr, focusErr error
	for pn, src := range e.sources {
		predeclared := make(starlark.StringDict, len(e.builtins)+len(exportedBy))
		for k, v := range e.builtins {
			predeclared[k] = v
		}
		owned := ownDefs[pn]
		for defName, owners := range exportedBy {
			if owned[defName] {
				continue // file-scope def shadows predeclared proxy
			}
			if _, isBuiltin := e.builtins[defName]; isBuiltin {
				continue // don't shadow builtins
			}
			predeclared[defName] = e.makeCallProxy(defName, append([]string(nil), owners...))
		}

		thread := &starlark.Thread{Name: pn}
		globals, err := starlark.ExecFile(thread, pn+".star", src, predeclared)
		if err != nil {
			wrapped := fmt.Errorf("compile %s: %w", pn, err)
			if pn == focus {
				focusErr = wrapped
			} else if firstErr == nil {
				firstErr = wrapped
			}
			continue
		}
		newPrograms[pn] = &compiledProgram{name: pn, globals: globals}
	}

	e.programs = newPrograms
	if focusErr != nil {
		return focusErr
	}
	return firstErr
}

// makeCallProxy returns a Starlark callable that looks up `name` in one of
// `owners` (other compiled programs) at call time and invokes it. If more
// than one program defines the same name, the proxy refuses to guess.
func (e *Engine) makeCallProxy(name string, owners []string) starlark.Value {
	return starlark.NewBuiltin(name, func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(owners) > 1 {
			return nil, fmt.Errorf("ambiguous call to %q: defined in programs %v", name, owners)
		}
		src := owners[0]
		e.mu.RLock()
		prog, ok := e.programs[src]
		e.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("program %q not compiled", src)
		}
		fnVal, ok := prog.globals[name]
		if !ok {
			return nil, fmt.Errorf("program %q: no function %q", src, name)
		}
		callable, ok := fnVal.(starlark.Callable)
		if !ok {
			return nil, fmt.Errorf("program %q: %q is not callable", src, name)
		}
		return starlark.Call(thread, callable, args, kwargs)
	})
}

// extractTopLevelDefs parses a Starlark source file and returns the names
// of its top-level def statements. Used to index cross-program exports
// without requiring a full compile.
func extractTopLevelDefs(source string) ([]string, error) {
	f, err := syntax.Parse("x.star", source, 0)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, stmt := range f.Stmts {
		if def, ok := stmt.(*syntax.DefStmt); ok {
			names = append(names, def.Name.Name)
		}
	}
	return names, nil
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
