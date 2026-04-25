/**
 * Thin LSP client for tentacle-plc's in-process language server.
 *
 * Scope: enough to drive diagnostics, completion, and hover in CodeMirror.
 * Intentionally does not implement the full LSP client surface — when we
 * add signature help / go-to-definition / code-action, consider migrating
 * to @codemirror/lsp-client, but only once the feature set justifies it.
 *
 * Transport is a single WebSocket per editor session. Frames are one JSON-
 * RPC message each (no Content-Length framing). Requests return Promises
 * keyed by id so callers can await results.
 */

import { ViewPlugin, type PluginValue, type ViewUpdate, EditorView, hoverTooltip, type Tooltip } from '@codemirror/view';
import { setDiagnostics, type Diagnostic } from '@codemirror/lint';
import {
	type CompletionContext,
	type CompletionResult,
	type CompletionSource,
	snippet
} from '@codemirror/autocomplete';

type PlcLspLanguage = 'starlark' | 'starlark-test' | 'python' | 'st';

export interface PlcLspOptions {
	/** Language of this editor's document. Called on each change so the caller can switch dynamically. */
	language: () => PlcLspLanguage;
	/** Synthetic URI for this editor's document. Must be stable for the editor's lifetime. */
	uri: string;
	/** WebSocket URL. Defaults to /api/v1/plcs/plc/lsp on the current origin. */
	url?: string;
	/** Debounce delay (ms) for sending didChange. Defaults to 250. */
	changeDelay?: number;
	/** Side-channel callback: fires on every publishDiagnostics with the raw LSP payload. */
	onDiagnostics?: (uri: string, diagnostics: LspDiagnostic[]) => void;
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

interface LspCompletionItem {
	label: string;
	kind?: number;
	detail?: string;
	documentation?: string;
	insertText?: string;
	insertTextFormat?: number; // 1 plain, 2 snippet
	sortText?: string;
}

interface LspCompletionList {
	isIncomplete: boolean;
	items: LspCompletionItem[];
}

interface LspHover {
	contents: { kind: string; value: string };
	range?: LspRange;
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

// LSP CompletionItemKind → short label shown in CM's completion list.
function completionKindLabel(kind: number | undefined): string {
	switch (kind) {
		case 3:
			return 'function';
		case 6:
			return 'variable';
		case 14:
			return 'keyword';
		case 23:
			return 'ladder';
		default:
			return '';
	}
}

/**
 * Create a CodeMirror extension bundle for the PLC LSP. Returns the view
 * plugin + hover tooltip extension, and a completion source the caller can
 * pass to @codemirror/autocomplete's `override`.
 */
export function plcLsp(opts: PlcLspOptions): {
	extension: readonly unknown[];
	completionSource: CompletionSource;
} {
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
		// Responses we're waiting for, keyed by request id.
		private inflight = new Map<
			number,
			{ resolve: (value: unknown) => void; reject: (err: unknown) => void }
		>();
		// Requests queued while the socket is not yet open; flushed on open.
		private preopenQueue: { id: number; method: string; params: unknown }[] = [];

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
			// Reject any still-in-flight requests so callers don't hang.
			for (const { reject } of this.inflight.values()) {
				reject(new Error('editor destroyed'));
			}
			this.inflight.clear();
			// Drop any diagnostics we published for this URI — the view is gone.
			opts.onDiagnostics?.(opts.uri, []);
		}

		private connect() {
			if (this.destroyed) return;
			try {
				this.ws = new WebSocket(url);
			} catch {
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
			this.sendRequest('initialize', {
				processId: null,
				rootUri: null,
				capabilities: {}
			});
			this.initialized = true;
			this.sendDidOpen();
			// Flush any completion/hover requests that arrived before the socket opened.
			for (const q of this.preopenQueue) {
				this.writeFrame({ jsonrpc: '2.0', id: q.id, method: q.method, params: q.params });
			}
			this.preopenQueue = [];
		}

		private onClose() {
			this.initialized = false;
			this.openedLang = null;
			this.ws = null;
			// Reject in-flight requests; they'll be retried on the next request.
			for (const { reject } of this.inflight.values()) reject(new Error('socket closed'));
			this.inflight.clear();
			if (this.destroyed) return;
			setTimeout(() => this.connect(), 1000);
		}

		private onMessage(e: MessageEvent) {
			let msg: {
				method?: string;
				params?: unknown;
				id?: number;
				result?: unknown;
				error?: { code: number; message: string };
			};
			try {
				msg = JSON.parse(typeof e.data === 'string' ? e.data : '');
			} catch {
				return;
			}
			// Response to one of our requests.
			if (typeof msg.id === 'number' && (msg.result !== undefined || msg.error !== undefined)) {
				const waiter = this.inflight.get(msg.id);
				if (waiter) {
					this.inflight.delete(msg.id);
					if (msg.error) waiter.reject(msg.error);
					else waiter.resolve(msg.result);
				}
				return;
			}
			// Server-initiated notification.
			if (msg.method === 'textDocument/publishDiagnostics') {
				this.applyDiagnostics(msg.params as PublishDiagnosticsParams);
			}
		}

		private applyDiagnostics(params: PublishDiagnosticsParams) {
			if (params.uri !== opts.uri) return;
			opts.onDiagnostics?.(params.uri, params.diagnostics);
			const doc = this.view.state.doc;
			const diagnostics: Diagnostic[] = params.diagnostics.map((d) => {
				const from = this.posFromLsp(d.range.start);
				let to = this.posFromLsp(d.range.end);
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

		private lspFromPos(offset: number): LspPosition {
			const line = this.view.state.doc.lineAt(offset);
			return { line: line.number - 1, character: offset - line.from };
		}

		private scheduleDidChange() {
			if (this.pending !== null) clearTimeout(this.pending);
			this.pending = setTimeout(() => {
				this.pending = null;
				this.sendDidChange();
			}, changeDelay) as unknown as number;
		}

		// flushDidChange sends any pending didChange immediately. Completion
		// and hover requests must call this first — otherwise a request
		// fired right after a keystroke races the 250ms debounce and the
		// server answers against stale document text (no `.`, short line,
		// out-of-range character offsets).
		private flushDidChange() {
			if (this.pending === null) return;
			clearTimeout(this.pending);
			this.pending = null;
			this.sendDidChange();
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

		private sendRequest(method: string, params: unknown): Promise<unknown> {
			const id = this.nextId++;
			const p = new Promise<unknown>((resolve, reject) => {
				this.inflight.set(id, { resolve, reject });
			});
			if (this.ws?.readyState === WebSocket.OPEN) {
				this.writeFrame({ jsonrpc: '2.0', id, method, params });
			} else {
				this.preopenQueue.push({ id, method, params });
			}
			return p;
		}

		private sendNotification(method: string, params: unknown) {
			if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
			this.writeFrame({ jsonrpc: '2.0', method, params });
		}

		private writeFrame(obj: unknown) {
			this.ws?.send(JSON.stringify(obj));
		}

		// ------- public surface consumed by completion/hover sources --------

		async requestCompletion(ctx: CompletionContext): Promise<CompletionResult | null> {
			// Flush pending didChange so the server sees the text the user
			// is actually looking at. Without this, typing `.` and
			// immediately asking for completion answers against the
			// pre-dot document.
			this.flushDidChange();
			// CM gives us the cursor offset; find the word prefix that triggered
			// completion. If there's no word boundary the user just typed a
			// trigger char; we still ask the server (it returns the full list).
			const word = ctx.matchBefore(/[A-Za-z_][A-Za-z0-9_]*/);
			const from = word ? word.from : ctx.pos;
			const pos = this.lspFromPos(ctx.pos);

			let result: LspCompletionList;
			try {
				const raw = (await this.sendRequest('textDocument/completion', {
					textDocument: { uri: opts.uri },
					position: pos
				})) as LspCompletionList | LspCompletionItem[] | null;
				if (!raw) return null;
				result = Array.isArray(raw) ? { isIncomplete: false, items: raw } : raw;
			} catch {
				return null;
			}
			if (!result.items?.length) return null;

			const options = result.items.map((it) => {
				const insertIsSnippet = it.insertTextFormat === 2;
				const insert = it.insertText ?? it.label;
				const detail = it.detail ?? completionKindLabel(it.kind);
				return {
					label: it.label,
					detail,
					info: it.documentation ?? undefined,
					type: lspKindToCmType(it.kind),
					apply: insertIsSnippet ? snippet(insert) : insert,
					boost: it.sortText ? -sortTextBoost(it.sortText) : 0
				};
			});

			return {
				from,
				options,
				// If the server said its list is complete, let CM filter/reuse it
				// until the identifier boundary is left.
				validFor: result.isIncomplete ? undefined : /^[A-Za-z_][A-Za-z0-9_]*$/
			};
		}

		async requestHover(pos: number): Promise<Tooltip | null> {
			this.flushDidChange();
			const lspPos = this.lspFromPos(pos);
			let hov: LspHover | null;
			try {
				hov = (await this.sendRequest('textDocument/hover', {
					textDocument: { uri: opts.uri },
					position: lspPos
				})) as LspHover | null;
			} catch {
				return null;
			}
			if (!hov || !hov.contents) return null;
			const from = hov.range ? this.posFromLsp(hov.range.start) : pos;
			const to = hov.range ? this.posFromLsp(hov.range.end) : pos;
			const value = unwrapMarkdownFence(hov.contents.value);
			return {
				pos: from,
				end: to,
				above: true,
				create: () => {
					const dom = document.createElement('div');
					dom.className = 'cm-plc-hover';
					dom.textContent = value;
					return { dom };
				}
			};
		}
	}

	const PluginDef = ViewPlugin.define((view) => new Session(view));

	const hoverExt = hoverTooltip(async (view, pos) => {
		const s = view.plugin(PluginDef);
		if (!s) return null;
		return s.requestHover(pos);
	});

	const completionSource: CompletionSource = async (ctx) => {
		if (!ctx.view) return null;
		const s = ctx.view.plugin(PluginDef);
		if (!s) return null;
		return s.requestCompletion(ctx);
	};

	const hoverTheme = EditorView.baseTheme({
		'.cm-plc-hover': {
			padding: '0.5rem 0.75rem',
			fontFamily: "'IBM Plex Mono', monospace",
			fontSize: '0.8125rem',
			whiteSpace: 'pre-wrap',
			maxWidth: '32rem'
		}
	});

	return {
		extension: [PluginDef, hoverExt, hoverTheme],
		completionSource
	};
}

// Hover content arrives as LSP MarkupContent (kind: "markdown"). Our server
// wraps the single type-signature line in a ```lang ... ``` fence for future
// markdown renderers; the current tooltip is a plain <div>, so unwrap the
// fence and show the inner text.
function unwrapMarkdownFence(value: string): string {
	const m = /^\s*```[a-zA-Z_-]*\n([\s\S]*?)\n```\s*$/.exec(value);
	return m ? m[1] : value;
}

function lspKindToCmType(kind: number | undefined): string | undefined {
	// Map LSP kinds to CM's named icon slots (string type is rendered as a class).
	switch (kind) {
		case 3:
			return 'function';
		case 6:
			return 'variable';
		case 14:
			return 'keyword';
		case 23:
			return 'class';
		default:
			return undefined;
	}
}

// Convert the server's sortText (we use "0name" / "1name" / "2name") into a
// numeric boost CodeMirror can rank on. Lower prefix → higher priority.
function sortTextBoost(s: string): number {
	const first = s.charCodeAt(0);
	return first * 1000;
}
