//go:build plc || all

package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// Transport is a minimal bidirectional framed-JSON channel. Read blocks until
// the next message arrives or the connection closes; Write serialises a
// message. Implementations are provided by the API layer (WebSocket) and by
// tests (in-memory pipe).
//
// Framing is up to the implementation. The LSP standard uses
// `Content-Length`-prefixed framing over stdio; we use one JSON message per
// WebSocket frame for the browser case. Either way, Transport hides that.
type Transport interface {
	ReadMessage() ([]byte, error)
	WriteMessage([]byte) error
	Close() error
}

// Server is one LSP session. Create one per connection; goroutines that
// invoke its methods must not escape the session lifetime.
type Server struct {
	log *slog.Logger

	mu   sync.Mutex
	docs map[string]*document // uri → doc
}

type document struct {
	uri        string
	languageID string
	version    int
	text       string
}

// NewServer creates an LSP session. The caller runs Serve(ctx, transport) in
// its own goroutine.
func NewServer(log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:  log.With("component", "plc-lsp"),
		docs: make(map[string]*document),
	}
}

// Serve drives the message loop until the transport errors or ctx is
// cancelled. Returns nil on clean EOF, error otherwise.
func (s *Server) Serve(ctx context.Context, tr Transport) error {
	defer tr.Close()
	// ctx cancellation races the blocking ReadMessage; close the transport
	// from a watcher goroutine so the Read unblocks with an error.
	go func() {
		<-ctx.Done()
		_ = tr.Close()
	}()

	for {
		raw, err := tr.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("lsp read: %w", err)
		}
		var msg rpcMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.log.Warn("lsp: malformed message", "err", err)
			continue
		}
		s.dispatch(tr, &msg)
	}
}

func (s *Server) dispatch(tr Transport, msg *rpcMessage) {
	switch msg.Method {
	case "initialize":
		s.handleInitialize(tr, msg)
	case "initialized":
		// no-op notification; client tells server it's ready
	case "shutdown":
		s.reply(tr, msg.ID, json.RawMessage(`null`))
	case "exit":
		// Transport will be closed by the API layer when the WS closes.
	case "textDocument/didOpen":
		s.handleDidOpen(tr, msg)
	case "textDocument/didChange":
		s.handleDidChange(tr, msg)
	case "textDocument/didClose":
		s.handleDidClose(msg)
	default:
		// Respond with MethodNotFound for requests (those with an ID);
		// notifications are silently ignored per LSP spec.
		if len(msg.ID) > 0 {
			s.replyError(tr, msg.ID, -32601, "method not found: "+msg.Method)
		}
	}
}

func (s *Server) handleInitialize(tr Transport, msg *rpcMessage) {
	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: 1, // Full
		},
		ServerInfo: ServerInfo{
			Name:    "tentacle-plc-lsp",
			Version: "0.1.0",
		},
	}
	body, _ := json.Marshal(result)
	s.reply(tr, msg.ID, body)
}

func (s *Server) handleDidOpen(tr Transport, msg *rpcMessage) {
	var params DidOpenParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.log.Warn("didOpen: bad params", "err", err)
		return
	}
	s.mu.Lock()
	s.docs[params.TextDocument.URI] = &document{
		uri:        params.TextDocument.URI,
		languageID: params.TextDocument.LanguageID,
		version:    params.TextDocument.Version,
		text:       params.TextDocument.Text,
	}
	s.mu.Unlock()
	s.publishFor(tr, params.TextDocument.URI)
}

func (s *Server) handleDidChange(tr Transport, msg *rpcMessage) {
	var params DidChangeParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.log.Warn("didChange: bad params", "err", err)
		return
	}
	if len(params.ContentChanges) == 0 {
		return
	}
	// Full-sync only; if a range is provided, ignore the change and log so
	// we notice if the client ever negotiates incremental mode.
	last := params.ContentChanges[len(params.ContentChanges)-1]
	if last.Range != nil {
		s.log.Warn("didChange: incremental sync not supported", "uri", params.TextDocument.URI)
		return
	}
	s.mu.Lock()
	doc, ok := s.docs[params.TextDocument.URI]
	if ok {
		doc.version = params.TextDocument.Version
		doc.text = last.Text
	}
	s.mu.Unlock()
	if ok {
		s.publishFor(tr, params.TextDocument.URI)
	}
}

func (s *Server) handleDidClose(msg *rpcMessage) {
	var params DidCloseParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}
	s.mu.Lock()
	delete(s.docs, params.TextDocument.URI)
	s.mu.Unlock()
}

// publishFor runs the analyzer against the current text for the given uri and
// sends a publishDiagnostics notification. Safe to call with the mutex
// unheld; we snapshot the document under the lock.
func (s *Server) publishFor(tr Transport, uri string) {
	s.mu.Lock()
	doc, ok := s.docs[uri]
	if !ok {
		s.mu.Unlock()
		return
	}
	source, lang, version := doc.text, doc.languageID, doc.version
	s.mu.Unlock()

	diags := Analyze(source, lang)
	if diags == nil {
		diags = []Diagnostic{}
	}
	params := PublishDiagnosticsParams{
		URI:         uri,
		Version:     version,
		Diagnostics: diags,
	}
	body, err := json.Marshal(params)
	if err != nil {
		return
	}
	s.notify(tr, "textDocument/publishDiagnostics", body)
}

func (s *Server) reply(tr Transport, id json.RawMessage, result json.RawMessage) {
	msg := rpcMessage{JSONRPC: "2.0", ID: id, Result: result}
	raw, _ := json.Marshal(msg)
	if err := tr.WriteMessage(raw); err != nil {
		s.log.Warn("lsp write reply", "err", err)
	}
}

func (s *Server) replyError(tr Transport, id json.RawMessage, code int, message string) {
	msg := rpcMessage{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
	raw, _ := json.Marshal(msg)
	_ = tr.WriteMessage(raw)
}

func (s *Server) notify(tr Transport, method string, params json.RawMessage) {
	msg := rpcMessage{JSONRPC: "2.0", Method: method, Params: params}
	raw, _ := json.Marshal(msg)
	if err := tr.WriteMessage(raw); err != nil {
		s.log.Warn("lsp write notify", "err", err, "method", method)
	}
}
