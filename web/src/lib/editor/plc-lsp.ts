/**
 * Thin LSP client for tentacle-plc's in-process language server.
 *
 * Scope: enough to drive diagnostics in CodeMirror. Intentionally does not
 * implement the full LSP client surface. When we add completion/hover/
 * code-action in later phases, consider migrating to @codemirror/lsp-client
 * — but only once the tentacle-plc analyzer's feature set justifies it.
 *
 * Transport is a single WebSocket per editor session. The client sends one
 * JSON-RPC message per frame (no Content-Length framing — the server accepts
 * raw JSON-per-frame over the WS).
 */

import { ViewPlugin, type PluginValue, type ViewUpdate, EditorView } from '@codemirror/view';
import { setDiagnostics, type Diagnostic } from '@codemirror/lint';

type PlcLspLanguage = 'starlark' | 'python' | 'st';

export interface PlcLspOptions {
	/** Language of this editor's document. Called on each change so the caller can switch dynamically. */
	language: () => PlcLspLanguage;
	/** Synthetic URI for this editor's document. Must be stable for the editor's lifetime. */
	uri: string;
	/** WebSocket URL. Defaults to /api/plcs/plc/lsp on the current origin. */
	url?: string;
	/** Debounce delay (ms) for sending didChange. Defaults to 250. */
	changeDelay?: number;
}

interface LspPosition {
	line: number;
	character: number;
}
interface LspRange {
	start: LspPosition;
	end: LspPosition;
}
interface LspDiagnostic {
	range: LspRange;
	severity?: number;
	message: string;
	source?: string;
	code?: string;
}
interface PublishDiagnosticsParams {
	uri: string;
	version?: number;
	diagnostics: LspDiagnostic[];
}

function severityToCmSeverity(sev: number | undefined): Diagnostic['severity'] {
	// LSP: 1=Error 2=Warning 3=Info 4=Hint
	if (sev === 2) return 'warning';
	if (sev === 3 || sev === 4) return 'info';
	return 'error';
}

function defaultUrl(): string {
	const loc = globalThis.location;
	const proto = loc.protocol === 'https:' ? 'wss:' : 'ws:';
	return `${proto}//${loc.host}/api/v1/plcs/plc/lsp`;
}

/**
 * Create a CodeMirror extension that opens an LSP session and routes
 * diagnostics back into the editor. One session per extension instance;
 * the plugin owns the WebSocket lifecycle.
 */
export function plcLsp(opts: PlcLspOptions) {
	const url = opts.url ?? defaultUrl();
	const changeDelay = opts.changeDelay ?? 250;

	class Session implements PluginValue {
		private view: EditorView;
		private ws: WebSocket | null = null;
		private initialized = false;
		private openedLang: PlcLspLanguage | null = null;
		private version = 0;
		private pending: number | null = null;
		private nextId = 1;
		private destroyed = false;

		constructor(view: EditorView) {
			this.view = view;
			this.connect();
		}

		update(u: ViewUpdate) {
			if (u.docChanged) this.scheduleDidChange();
		}

		destroy() {
			this.destroyed = true;
			if (this.pending !== null) clearTimeout(this.pending);
			if (this.ws && this.ws.readyState === WebSocket.OPEN) {
				this.sendDidClose();
				this.ws.close();
			} else if (this.ws) {
				this.ws.close();
			}
		}

		private connect() {
			if (this.destroyed) return;
			try {
				this.ws = new WebSocket(url);
			} catch {
				// Malformed URL or environment without WebSocket — bail silently;
				// editor still works, just no live diagnostics.
				return;
			}
			this.ws.addEventListener('open', () => this.onOpen());
			this.ws.addEventListener('message', (e) => this.onMessage(e));
			this.ws.addEventListener('close', () => this.onClose());
			this.ws.addEventListener('error', () => {
				// Let 'close' handle reconnect logic; browsers fire error before close.
			});
		}

		private onOpen() {
			// Send initialize immediately; server is stateless about capabilities.
			this.sendRequest('initialize', {
				processId: null,
				rootUri: null,
				capabilities: {}
			});
			// Server returns result; we don't wait for it before didOpen because the
			// server accepts didOpen unconditionally. This keeps the first paint of
			// diagnostics as fast as possible.
			this.initialized = true;
			this.sendDidOpen();
		}

		private onClose() {
			this.initialized = false;
			this.openedLang = null;
			this.ws = null;
			if (this.destroyed) return;
			// Simple backoff retry; 1s is fine for a local dev server. Revisit if
			// we ever point the client at a remote LSP endpoint.
			setTimeout(() => this.connect(), 1000);
		}

		private onMessage(e: MessageEvent) {
			let msg: { method?: string; params?: unknown; id?: unknown };
			try {
				msg = JSON.parse(typeof e.data === 'string' ? e.data : '');
			} catch {
				return;
			}
			if (msg.method === 'textDocument/publishDiagnostics') {
				this.applyDiagnostics(msg.params as PublishDiagnosticsParams);
			}
			// Responses to initialize/shutdown are ignored; we don't need the results.
		}

		private applyDiagnostics(params: PublishDiagnosticsParams) {
			if (params.uri !== opts.uri) return;
			const doc = this.view.state.doc;
			const diagnostics: Diagnostic[] = params.diagnostics.map((d) => {
				const from = this.posFromLsp(d.range.start);
				let to = this.posFromLsp(d.range.end);
				// Parsers often emit a caret-only range (start == end). Widen it to
				// the end of the line so the squiggle is visible.
				if (to <= from) {
					const lineNo = Math.max(1, Math.min(d.range.start.line + 1, doc.lines));
					to = Math.min(doc.line(lineNo).to, from + 1);
					if (to === from) to = Math.min(doc.length, from + 1);
				}
				return {
					from,
					to,
					severity: severityToCmSeverity(d.severity),
					message: d.message,
					source: d.source
				};
			});
			this.view.dispatch(setDiagnostics(this.view.state, diagnostics));
		}

		private posFromLsp(p: LspPosition): number {
			const doc = this.view.state.doc;
			const lineNo = Math.max(1, Math.min(p.line + 1, doc.lines));
			const line = doc.line(lineNo);
			const character = Math.max(0, Math.min(p.character, line.length));
			return line.from + character;
		}

		private scheduleDidChange() {
			if (this.pending !== null) clearTimeout(this.pending);
			this.pending = setTimeout(() => {
				this.pending = null;
				this.sendDidChange();
			}, changeDelay) as unknown as number;
		}

		private sendDidOpen() {
			if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
			this.version = 1;
			this.openedLang = opts.language();
			this.sendNotification('textDocument/didOpen', {
				textDocument: {
					uri: opts.uri,
					languageId: this.openedLang,
					version: this.version,
					text: this.view.state.doc.toString()
				}
			});
		}

		private sendDidChange() {
			if (!this.ws || this.ws.readyState !== WebSocket.OPEN || !this.initialized) return;
			// Language changes mid-session aren't supported by our server; close and
			// reopen the document so diagnostics reflect the new language.
			const lang = opts.language();
			if (lang !== this.openedLang) {
				this.sendDidClose();
				this.sendDidOpen();
				return;
			}
			this.version += 1;
			this.sendNotification('textDocument/didChange', {
				textDocument: { uri: opts.uri, version: this.version },
				contentChanges: [{ text: this.view.state.doc.toString() }]
			});
		}

		private sendDidClose() {
			if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
			this.sendNotification('textDocument/didClose', {
				textDocument: { uri: opts.uri }
			});
		}

		private sendRequest(method: string, params: unknown) {
			if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
			this.ws.send(JSON.stringify({ jsonrpc: '2.0', id: this.nextId++, method, params }));
		}

		private sendNotification(method: string, params: unknown) {
			if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
			this.ws.send(JSON.stringify({ jsonrpc: '2.0', method, params }));
		}
	}

	return ViewPlugin.define((view) => new Session(view));
}
