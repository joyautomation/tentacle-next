//go:build plc || all

package plc

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TestSessionManager coordinates RSLogix-style "test edit" sessions.
//
// A session hot-swaps a candidate source into the running engine while
// remembering the pre-test source. If the candidate raises a runtime
// error or the watchdog expires, the pre-test source is restored — the
// PLC keeps scanning the whole time.
//
// Sessions are per-program. Starting a new session on a program with an
// active session is an error (the caller must Stop first).
type TestSessionManager struct {
	engine *Engine
	log    *slog.Logger
	onEvent func(TestEvent)

	removeObserver func()

	mu         sync.Mutex
	sessions   map[string]*testSession // active sessions keyed by program
	lastEvents map[string]TestEvent    // last terminal event per program
}

type testSession struct {
	program       string
	preTestSource string
	startedAt     time.Time
	expiresAt     time.Time
	timer         *time.Timer
}

// TestSessionInfo describes an active session in API responses.
type TestSessionInfo struct {
	Program   string `json:"program"`
	StartedAt int64  `json:"startedAt"`
	ExpiresAt int64  `json:"expiresAt"`
}

// TestEvent is a terminal event — a session started, stopped, or was
// auto-reverted. Emitted through the manager's onEvent callback so the
// module can forward it to subscribers.
type TestEvent struct {
	Program string `json:"program"`
	Reason  string `json:"reason"` // started, stopped, timeout, error
	Error   string `json:"error,omitempty"`
	At      int64  `json:"at"`
}

// NewTestSessionManager wires up an error observer on the engine so
// runtime faults in a tested program trigger an automatic revert.
func NewTestSessionManager(engine *Engine, log *slog.Logger, onEvent func(TestEvent)) *TestSessionManager {
	m := &TestSessionManager{
		engine:     engine,
		log:        log,
		onEvent:    onEvent,
		sessions:   make(map[string]*testSession),
		lastEvents: make(map[string]TestEvent),
	}
	m.removeObserver = engine.AddErrorObserver(m.onExecuteError)
	return m
}

// Close deregisters the manager's error observer and stops pending timers.
// Any active session remains compiled in the engine — callers should drain
// via Stop before Close if they need the pre-test source restored.
func (m *TestSessionManager) Close() {
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
func (m *TestSessionManager) Start(program, candidate string, timeout time.Duration) (TestSessionInfo, error) {
	if program == "" {
		return TestSessionInfo{}, fmt.Errorf("program is required")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	m.mu.Lock()
	if _, ok := m.sessions[program]; ok {
		m.mu.Unlock()
		return TestSessionInfo{}, fmt.Errorf("test session already active for %q", program)
	}
	m.mu.Unlock()

	preTest := m.engine.Source(program)
	if preTest == "" {
		return TestSessionInfo{}, fmt.Errorf("program %q is not loaded in the engine", program)
	}

	// Compile the candidate. Compile mutates sources[program] before attempting
	// the build, so on failure we must restore the pre-test source ourselves.
	if err := m.engine.Compile(program, candidate); err != nil {
		if rerr := m.engine.Compile(program, preTest); rerr != nil {
			m.log.Error("plc: failed to restore pre-test source after compile error",
				"program", program, "error", rerr)
		}
		return TestSessionInfo{}, fmt.Errorf("compile candidate: %w", err)
	}

	now := time.Now()
	sess := &testSession{
		program:       program,
		preTestSource: preTest,
		startedAt:     now,
		expiresAt:     now.Add(timeout),
	}

	m.mu.Lock()
	m.sessions[program] = sess
	m.mu.Unlock()

	sess.timer = time.AfterFunc(timeout, func() {
		m.revert(program, "timeout", "")
	})

	ev := TestEvent{Program: program, Reason: "started", At: now.UnixMilli()}
	if m.onEvent != nil {
		m.onEvent(ev)
	}
	m.log.Info("plc: test session started", "program", program, "timeout", timeout)

	return TestSessionInfo{
		Program:   program,
		StartedAt: now.UnixMilli(),
		ExpiresAt: sess.expiresAt.UnixMilli(),
	}, nil
}

// Stop reverts the program to its pre-test source. Returns nil if no
// session is active (stop on a non-existent session is a no-op).
func (m *TestSessionManager) Stop(program string) error {
	return m.revert(program, "stopped", "")
}

// Active returns info about the currently-active session for a program,
// or nil when no session is running.
func (m *TestSessionManager) Active(program string) *TestSessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[program]
	if !ok {
		return nil
	}
	return &TestSessionInfo{
		Program:   s.program,
		StartedAt: s.startedAt.UnixMilli(),
		ExpiresAt: s.expiresAt.UnixMilli(),
	}
}

// LastEvent returns the most recent terminal event for a program, if any.
func (m *TestSessionManager) LastEvent(program string) *TestEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev, ok := m.lastEvents[program]; ok {
		return &ev
	}
	return nil
}

// onExecuteError is the engine error observer. When a task reports an
// error in a program with an active test session, revert immediately.
func (m *TestSessionManager) onExecuteError(program string, err error) {
	m.mu.Lock()
	_, ok := m.sessions[program]
	m.mu.Unlock()
	if !ok {
		return
	}
	// revert() takes its own locks — call it without holding ours.
	go m.revert(program, "error", err.Error())
}

// revert restores the pre-test source and emits a terminal event. A no-op
// if no session is active for the program.
func (m *TestSessionManager) revert(program, reason, errMsg string) error {
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

	if cerr := m.engine.Compile(program, sess.preTestSource); cerr != nil {
		m.log.Error("plc: failed to restore pre-test source",
			"program", program, "error", cerr)
	}

	ev := TestEvent{
		Program: program,
		Reason:  reason,
		Error:   errMsg,
		At:      time.Now().UnixMilli(),
	}
	m.mu.Lock()
	m.lastEvents[program] = ev
	m.mu.Unlock()

	if m.onEvent != nil {
		m.onEvent(ev)
	}
	m.log.Info("plc: test session reverted",
		"program", program, "reason", reason, "error", errMsg)
	return nil
}
