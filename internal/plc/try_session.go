//go:build plc || all

package plc

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TrySessionManager coordinates RSLogix-style "try edit" sessions.
//
// A session hot-swaps a candidate source into the running engine while
// remembering the pre-try source. If the candidate raises a runtime
// error or the watchdog expires, the pre-try source is restored — the
// PLC keeps scanning the whole time.
//
// Sessions are per-program. Starting a new session on a program with an
// active session is an error (the caller must Stop first).
//
// "Try" (not "Test") so the word stays free for the unit-test framework.
type TrySessionManager struct {
	engine  *Engine
	log     *slog.Logger
	onEvent func(TryEvent)

	removeObserver func()

	mu         sync.Mutex
	sessions   map[string]*trySession // active sessions keyed by program
	lastEvents map[string]TryEvent    // last terminal event per program
}

type trySession struct {
	program      string
	preTrySource string
	// varSnapshot captures every variable value at Start so revert can roll
	// back mutations the candidate made. Without this, a botched candidate
	// that flipped outputs leaves them flipped after the code reverts.
	varSnapshot  map[string]interface{}
	startedAt    time.Time
	expiresAt    time.Time
	timer        *time.Timer
}

// TrySessionInfo describes an active session in API responses.
type TrySessionInfo struct {
	Program   string `json:"program"`
	StartedAt int64  `json:"startedAt"`
	ExpiresAt int64  `json:"expiresAt"`
}

// TryEvent is a terminal event — a session started, stopped, or was
// auto-reverted. Emitted through the manager's onEvent callback so the
// module can forward it to subscribers.
type TryEvent struct {
	Program string `json:"program"`
	Reason  string `json:"reason"` // started, stopped, timeout, error
	Error   string `json:"error,omitempty"`
	// VariablesRestored reports how many variable values were rolled back
	// from the pre-try snapshot. 0 on "started" and on reverts where nothing
	// changed. Surfaced so the UI can tell the user "10 values restored".
	VariablesRestored int   `json:"variablesRestored,omitempty"`
	At                int64 `json:"at"`
}

// NewTrySessionManager wires up an error observer on the engine so
// runtime faults in a tried program trigger an automatic revert.
func NewTrySessionManager(engine *Engine, log *slog.Logger, onEvent func(TryEvent)) *TrySessionManager {
	m := &TrySessionManager{
		engine:     engine,
		log:        log,
		onEvent:    onEvent,
		sessions:   make(map[string]*trySession),
		lastEvents: make(map[string]TryEvent),
	}
	m.removeObserver = engine.AddErrorObserver(m.onExecuteError)
	return m
}

// Close deregisters the manager's error observer and stops pending timers.
// Any active session remains compiled in the engine — callers should drain
// via Stop before Close if they need the pre-try source restored.
func (m *TrySessionManager) Close() {
	if m.removeObserver != nil {
		m.removeObserver()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.timer != nil {
			s.timer.Stop()
		}
	}
	m.sessions = nil
}

// Start hot-swaps candidate into the engine after capturing the current
// live source as the revert target. A watchdog timer expires after
// timeout and triggers an automatic revert.
func (m *TrySessionManager) Start(program, candidate string, timeout time.Duration) (TrySessionInfo, error) {
	if program == "" {
		return TrySessionInfo{}, fmt.Errorf("program is required")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	m.mu.Lock()
	if _, ok := m.sessions[program]; ok {
		m.mu.Unlock()
		return TrySessionInfo{}, fmt.Errorf("try session already active for %q", program)
	}
	m.mu.Unlock()

	preTry := m.engine.Source(program)
	if preTry == "" {
		return TrySessionInfo{}, fmt.Errorf("program %q is not loaded in the engine", program)
	}

	// Compile the candidate. Compile mutates sources[program] before attempting
	// the build, so on failure we must restore the pre-try source ourselves.
	if err := m.engine.Compile(program, candidate); err != nil {
		if rerr := m.engine.Compile(program, preTry); rerr != nil {
			m.log.Error("plc: failed to restore pre-try source after compile error",
				"program", program, "error", rerr)
		}
		return TrySessionInfo{}, fmt.Errorf("compile candidate: %w", err)
	}

	now := time.Now()
	sess := &trySession{
		program:      program,
		preTrySource: preTry,
		varSnapshot:  m.engine.vars.Snapshot(),
		startedAt:    now,
		expiresAt:    now.Add(timeout),
	}

	m.mu.Lock()
	m.sessions[program] = sess
	m.mu.Unlock()

	sess.timer = time.AfterFunc(timeout, func() {
		m.revert(program, "timeout", "")
	})

	ev := TryEvent{Program: program, Reason: "started", At: now.UnixMilli()}
	if m.onEvent != nil {
		m.onEvent(ev)
	}
	m.log.Info("plc: try session started", "program", program, "timeout", timeout)

	return TrySessionInfo{
		Program:   program,
		StartedAt: now.UnixMilli(),
		ExpiresAt: sess.expiresAt.UnixMilli(),
	}, nil
}

// Stop reverts the program to its pre-try source. Returns nil if no
// session is active (stop on a non-existent session is a no-op).
func (m *TrySessionManager) Stop(program string) error {
	return m.revert(program, "stopped", "")
}

// Active returns info about the currently-active session for a program,
// or nil when no session is running.
func (m *TrySessionManager) Active(program string) *TrySessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[program]
	if !ok {
		return nil
	}
	return &TrySessionInfo{
		Program:   s.program,
		StartedAt: s.startedAt.UnixMilli(),
		ExpiresAt: s.expiresAt.UnixMilli(),
	}
}

// LastEvent returns the most recent terminal event for a program, if any.
func (m *TrySessionManager) LastEvent(program string) *TryEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev, ok := m.lastEvents[program]; ok {
		return &ev
	}
	return nil
}

// onExecuteError is the engine error observer. When a task reports an
// error in a program with an active try session, revert immediately.
func (m *TrySessionManager) onExecuteError(program string, err error) {
	m.mu.Lock()
	_, ok := m.sessions[program]
	m.mu.Unlock()
	if !ok {
		return
	}
	// revert() takes its own locks — call it without holding ours.
	go m.revert(program, "error", err.Error())
}

// revert restores the pre-try source and emits a terminal event. A no-op
// if no session is active for the program.
func (m *TrySessionManager) revert(program, reason, errMsg string) error {
	m.mu.Lock()
	sess, ok := m.sessions[program]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.sessions, program)
	if sess.timer != nil {
		sess.timer.Stop()
	}
	m.mu.Unlock()

	if cerr := m.engine.Compile(program, sess.preTrySource); cerr != nil {
		m.log.Error("plc: failed to restore pre-try source",
			"program", program, "error", cerr)
	}

	restored := 0
	if sess.varSnapshot != nil {
		restored = m.engine.vars.Restore(sess.varSnapshot, time.Now().UnixMilli())
	}

	ev := TryEvent{
		Program:          program,
		Reason:           reason,
		Error:            errMsg,
		VariablesRestored: restored,
		At:               time.Now().UnixMilli(),
	}
	m.mu.Lock()
	m.lastEvents[program] = ev
	m.mu.Unlock()

	if m.onEvent != nil {
		m.onEvent(ev)
	}
	m.log.Info("plc: try session reverted",
		"program", program, "reason", reason, "error", errMsg,
		"variablesRestored", restored)
	return nil
}
