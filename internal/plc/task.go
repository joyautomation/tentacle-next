//go:build plc || all

package plc

import (
	"log/slog"
	"runtime"
	"time"
)

// taskRunner executes a named top-level function in a compiled Starlark
// program on a fixed scan interval.
type taskRunner struct {
	name     string
	progRef  string
	entryFn  string
	scanRate time.Duration
	engine   *Engine
	state    *TaskState
	stats    *scanStats
	log      *slog.Logger
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// newTaskRunner creates a task runner for a compiled program. entryFn is the
// top-level function invoked each scan; empty string defaults to "main".
func newTaskRunner(name, progRef, entryFn string, scanRateMs int, engine *Engine, log *slog.Logger) *taskRunner {
	if entryFn == "" {
		entryFn = "main"
	}
	return &taskRunner{
		name:     name,
		progRef:  progRef,
		entryFn:  entryFn,
		scanRate: time.Duration(scanRateMs) * time.Millisecond,
		engine:   engine,
		state:    NewTaskState(),
		stats:    newScanStats(),
		log:      log.With("task", name),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// start begins the scan loop in a goroutine.
func (t *taskRunner) start() {
	go t.run()
}

func (t *taskRunner) run() {
	// Pin this goroutine to its OS thread for the lifetime of the task.
	// Eliminates scheduler migration between threads between ticks, which
	// is a meaningful source of jitter at sub-10ms scan rates. When the
	// goroutine exits the OS thread also exits — acceptable because tasks
	// are long-lived and stopped only on config reload/shutdown.
	runtime.LockOSThread()

	defer close(t.doneCh)
	ticker := time.NewTicker(t.scanRate)
	defer ticker.Stop()

	t.log.Info("task started", "scanRate", t.scanRate)

	for {
		select {
		case <-t.stopCh:
			t.log.Info("task stopped")
			return
		case <-ticker.C:
			start := time.Now()
			err := t.engine.Execute(t.progRef, t.entryFn, t.state)
			t.stats.record(time.Since(start), err)
			if err != nil {
				t.log.Error("task execution error", "error", err)
			}
		}
	}
}

// stop signals the task to stop and waits for it to finish.
func (t *taskRunner) stop() {
	close(t.stopCh)
	<-t.doneCh
}
