//go:build plc || all

package lsp

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"
)

// memTransport is a two-way in-memory Transport for tests. One pair of
// channels carries bytes in each direction; the test drives the "client"
// side and the server drives the "server" side.
type memTransport struct {
	read  chan []byte
	write chan []byte
	mu    sync.Mutex
	done  bool
}

func newMemPair() (server, client Transport) {
	ab := make(chan []byte, 8)
	ba := make(chan []byte, 8)
	return &memTransport{read: ab, write: ba}, &memTransport{read: ba, write: ab}
}

func (t *memTransport) ReadMessage() ([]byte, error) {
	msg, ok := <-t.read
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

func (t *memTransport) WriteMessage(b []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return io.ErrClosedPipe
	}
	t.write <- b
	return nil
}

func (t *memTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return nil
	}
	t.done = true
	close(t.write)
	return nil
}

// expectMessage reads with a timeout so a broken server doesn't hang the test.
func expectMessage(t *testing.T, tr Transport) rpcMessage {
	t.Helper()
	type result struct {
		raw []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := tr.ReadMessage()
		ch <- result{b, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read: %v", r.err)
		}
		var msg rpcMessage
		if err := json.Unmarshal(r.raw, &msg); err != nil {
			t.Fatalf("unmarshal: %v (raw=%s)", err, r.raw)
		}
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server message")
		return rpcMessage{}
	}
}

func sendJSON(t *testing.T, tr Transport, method string, id any, params any) {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteMessage(raw); err != nil {
		t.Fatal(err)
	}
}

func TestServerDiagnosticsForParseError(t *testing.T) {
	serverSide, clientSide := newMemPair()
	server := NewServer(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, serverSide) }()

	// initialize → expect response
	sendJSON(t, clientSide, "initialize", 1, map[string]any{})
	if reply := expectMessage(t, clientSide); string(reply.ID) != "1" {
		t.Fatalf("expected initialize reply id=1, got %s", reply.ID)
	}

	// didOpen with a deliberate syntax error
	sendJSON(t, clientSide, "textDocument/didOpen", nil, map[string]any{
		"textDocument": map[string]any{
			"uri":        "test://a.star",
			"languageId": "starlark",
			"version":    1,
			"text":       "def main(\n    pass", // missing closing paren
		},
	})

	// Expect publishDiagnostics with at least one error
	got := expectMessage(t, clientSide)
	if got.Method != "textDocument/publishDiagnostics" {
		t.Fatalf("expected publishDiagnostics, got %s", got.Method)
	}
	var params PublishDiagnosticsParams
	if err := json.Unmarshal(got.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.URI != "test://a.star" {
		t.Fatalf("unexpected uri %q", params.URI)
	}
	if len(params.Diagnostics) == 0 {
		t.Fatal("expected at least one diagnostic for broken syntax")
	}
	if params.Diagnostics[0].Severity != SeverityError {
		t.Errorf("expected error severity, got %d", params.Diagnostics[0].Severity)
	}
}

func TestServerDiagnosticsClearOnValidCode(t *testing.T) {
	serverSide, clientSide := newMemPair()
	server := NewServer(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, serverSide) }()

	sendJSON(t, clientSide, "initialize", 1, map[string]any{})
	_ = expectMessage(t, clientSide)

	sendJSON(t, clientSide, "textDocument/didOpen", nil, map[string]any{
		"textDocument": map[string]any{
			"uri":        "test://ok.star",
			"languageId": "starlark",
			"version":    1,
			"text":       "def main():\n    pass\n",
		},
	})
	got := expectMessage(t, clientSide)
	if got.Method != "textDocument/publishDiagnostics" {
		t.Fatalf("expected publishDiagnostics, got %s", got.Method)
	}
	var params PublishDiagnosticsParams
	if err := json.Unmarshal(got.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for valid code, got %d: %+v", len(params.Diagnostics), params.Diagnostics)
	}
}

func TestServerDidChangeUpdatesDiagnostics(t *testing.T) {
	serverSide, clientSide := newMemPair()
	server := NewServer(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, serverSide) }()

	sendJSON(t, clientSide, "initialize", 1, map[string]any{})
	_ = expectMessage(t, clientSide)

	// Open with broken code
	sendJSON(t, clientSide, "textDocument/didOpen", nil, map[string]any{
		"textDocument": map[string]any{
			"uri": "test://b.star", "languageId": "starlark", "version": 1,
			"text": "def main(",
		},
	})
	msg := expectMessage(t, clientSide)
	var p1 PublishDiagnosticsParams
	_ = json.Unmarshal(msg.Params, &p1)
	if len(p1.Diagnostics) == 0 {
		t.Fatal("expected diagnostic on open with broken code")
	}

	// Fix it
	sendJSON(t, clientSide, "textDocument/didChange", nil, map[string]any{
		"textDocument":   map[string]any{"uri": "test://b.star", "version": 2},
		"contentChanges": []any{map[string]any{"text": "def main():\n    pass\n"}},
	})
	msg = expectMessage(t, clientSide)
	var p2 PublishDiagnosticsParams
	_ = json.Unmarshal(msg.Params, &p2)
	if len(p2.Diagnostics) != 0 {
		t.Fatalf("expected diagnostics cleared after fix, got %+v", p2.Diagnostics)
	}
	if p2.Version != 2 {
		t.Errorf("expected version 2 echoed back, got %d", p2.Version)
	}
}
