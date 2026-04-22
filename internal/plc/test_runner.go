//go:build plc || all

package plc

import (
	"sync"
	"time"

	itypes "github.com/joyautomation/tentacle/internal/types"
	"go.starlark.net/starlark"
)

// testLogBuffer collects log_* output during a unit-test run so it can be
// returned to the caller and displayed in the UI.
type testLogBuffer struct {
	mu    sync.Mutex
	lines []string
}

func (b *testLogBuffer) append(line string) {
	b.mu.Lock()
	b.lines = append(b.lines, line)
	b.mu.Unlock()
}

func (b *testLogBuffer) snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

// RunTest executes a unit-test Starlark source against the live engine.
// Cross-program functions are accessible via the same call-proxy mechanism
// used for regular programs. Assertion failures and runtime errors return
// a PlcTestResult with Status="fail" or "error"; a clean run returns "pass".
//
// The test source executes top-to-bottom; no harness is required. Tests
// may define helper functions and call them, but anything at module scope
// runs on load.
func (e *Engine) RunTest(name, source string) itypes.PlcTestResult {
	startWall := time.Now()
	result := itypes.PlcTestResult{
		Name:      name,
		StartedAt: startWall.UnixMilli(),
	}
	buf := &testLogBuffer{}

	// Snapshot current cross-program exports so the test can call live code.
	e.mu.RLock()
	predeclared := make(starlark.StringDict, len(e.builtins)+16)
	for k, v := range e.builtins {
		predeclared[k] = v
	}
	for k, v := range e.makeAssertBuiltins() {
		predeclared[k] = v
	}
	exportedBy := map[string][]string{}
	for pn := range e.sources {
		src := e.sources[pn]
		stripped, _ := StripAnnotations(src)
		defs, err := extractTopLevelDefs(stripped)
		if err != nil {
			continue
		}
		for _, d := range defs {
			exportedBy[d] = append(exportedBy[d], pn)
		}
	}
	e.mu.RUnlock()

	for defName, owners := range exportedBy {
		if _, isBuiltin := predeclared[defName]; isBuiltin {
			continue
		}
		predeclared[defName] = e.makeCallProxy(defName, append([]string(nil), owners...))
	}

	stripped, _ := StripAnnotations(source)
	thread := &starlark.Thread{Name: "test:" + name}
	thread.SetLocal("test_log_buffer", buf)
	thread.SetLocal("vars", e.vars)

	_, err := starlark.ExecFile(thread, name+".star", stripped, predeclared)
	result.DurationMs = time.Since(startWall).Milliseconds()
	result.Logs = buf.snapshot()
	if err != nil {
		result.Message = err.Error()
		// Distinguish assertion failures from other errors by the "failed:" marker
		// our assert_* builtins use. Everything else is classified as "error".
		if containsAssertionFailure(err.Error()) {
			result.Status = "fail"
		} else {
			result.Status = "error"
		}
		return result
	}
	result.Status = "pass"
	return result
}

// containsAssertionFailure reports whether msg looks like one of our
// assert_* failures rather than an arbitrary runtime error.
func containsAssertionFailure(msg string) bool {
	for _, marker := range []string{"assert_eq failed", "assert_ne failed", "assert_true failed", "assert_false failed", "assert_near failed", "assert_raises failed"} {
		if indexOf(msg, marker) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(haystack, needle string) int {
	n := len(needle)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(haystack); i++ {
		if haystack[i:i+n] == needle {
			return i
		}
	}
	return -1
}
