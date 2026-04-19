//go:build plc || all

// Package lsp implements a minimal in-process Language Server Protocol
// server for tentacle-plc's Starlark and Structured Text dialects.
//
// Scope note: this is intentionally not a full LSP implementation. Only the
// request/notification types we actually serve are modelled here. The type
// names mirror the LSP spec so future additions are mechanical.
package lsp

import "encoding/json"

// JSON-RPC 2.0 message envelope. A single struct covers requests, responses,
// and notifications — fields are optional and filled in based on direction.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// LSP position and range types use UTF-16 code units per spec; for ASCII PLC
// code the UTF-16 / UTF-8 distinction does not matter. Revisit if we ever
// support comments with non-ASCII.
type Position struct {
	Line      int `json:"line"`      // 0-based
	Character int `json:"character"` // 0-based
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// DiagnosticSeverity values per LSP spec.
const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentContentChangeEvent struct {
	// Full-sync variant: Range omitted, Text is the whole document.
	// We only support full sync for now; declared so we can detect the
	// incremental case and reject it.
	Range *Range `json:"range,omitempty"`
	Text  string `json:"text"`
}

type DidOpenParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidCloseParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// InitializeResult announces server capabilities to the client.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// TextDocumentSyncKind.Full = 1 — client sends whole document on every change.
type ServerCapabilities struct {
	TextDocumentSync int `json:"textDocumentSync"`
}
