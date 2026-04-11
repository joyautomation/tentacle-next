//go:build plc || all

package plc

import (
	"log/slog"
	"time"
)

// taskRunner executes a compiled Starlark program on a fixed scan interval.
type taskRunner struct {
	name     string
	progRef  string
	scanRate time.Duration
	engine   *Engine
	state    *TaskState
	log      *slog.Logger
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// newTaskRunner creates a task runner for a compiled program.
func newTaskRunner(name, progRef string, scanRateMs int, engine *Engine, log *slog.Logger) *taskRunner {
	return &taskRunner{
		name:     name,
		progRef:  progRef,
		scanRate: time.Duration(scanRateMs) * time.Millisecond,
		engine:   engine,
		state:    NewTaskState(),
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
			if err := t.engine.Execute(t.progRef, t.state); err != nil {
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
