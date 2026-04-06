package plc

import (
	"log/slog"
	"time"
)

// TaskConfig defines a cyclic scan task.
type TaskConfig struct {
	Name        string
	Description string
	ScanRate    time.Duration
	Program     ProgramFunc
}

// taskRunner executes a task's program on a fixed interval.
type taskRunner struct {
	cfg    TaskConfig
	vars   *Variables
	update UpdateFunc
	log    *slog.Logger
	stopCh chan struct{}
	doneCh chan struct{}
}

func newTaskRunner(cfg TaskConfig, vars *Variables, update UpdateFunc, log *slog.Logger) *taskRunner {
	return &taskRunner{
		cfg:    cfg,
		vars:   vars,
		update: update,
		log:    log,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (t *taskRunner) start() {
	go t.run()
}

func (t *taskRunner) run() {
	defer close(t.doneCh)
	ticker := time.NewTicker(t.cfg.ScanRate)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.execute()
		}
	}
}

func (t *taskRunner) execute() {
	defer func() {
		if r := recover(); r != nil {
			t.log.Error("task panic recovered", "task", t.cfg.Name, "error", r)
		}
	}()
	t.cfg.Program(t.vars, t.update)
}

func (t *taskRunner) stop() {
	close(t.stopCh)
	<-t.doneCh
}
