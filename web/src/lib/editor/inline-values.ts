/**
 * Inline variable values for the PLC code editor.
 *
 * Scans the document for PLC variable access builtins —
 * `get_var("name")`, `get_num("name")`, `get_bool("name")`,
 * `get_str("name")`, `set_var("name", ...)` — and renders the current
 * runtime value as an inline widget after the closing paren.
 *
 * The values live in an externally owned `Map<string, LiveValue>` pushed in
 * via the `setInlineValuesEffect` StateEffect. The extension rebuilds
 * decorations when the doc changes, the viewport scrolls, or the value map
 * reference changes — so dispatching with a new map reference is enough to
 * trigger a refresh.
 */

import {
	ViewPlugin,
	Decoration,
	type DecorationSet,
	EditorView,
	WidgetType,
	type ViewUpdate
} from '@codemirror/view';
import { RangeSetBuilder, StateEffect, StateField } from '@codemirror/state';

export type LiveValue = {
	value: unknown;
	datatype?: string;
	quality?: string;
	lastUpdated?: number;
};

export type LiveValueMap = ReadonlyMap<string, LiveValue>;

export const setInlineValuesEffect = StateEffect.define<LiveValueMap>();

const inlineValuesField = StateField.define<LiveValueMap>({
	create: () => new Map<string, LiveValue>(),
	update(current, tr) {
		for (const e of tr.effects) {
			if (e.is(setInlineValuesEffect)) return e.value;
		}
		return current;
	}
});

const BUILTIN_RE = /\b(get_var|get_num|get_bool|get_str|set_var)\s*\(\s*["']([^"'\\\n]*)["']/g;

type CallSite = { name: string; endPos: number };

/**
 * Find the matching ')' for an opening '(' at `openIdx`, tracking nested
 * parens and string literals so unbalanced parens inside strings don't throw
 * off depth.
 */
function findCallEnd(text: string, openIdx: number): number {
	let depth = 1;
	let i = openIdx + 1;
	while (i < text.length && depth > 0) {
		const c = text[i];
		if (c === '"' || c === "'") {
			const q = c;
			i++;
			while (i < text.length && text[i] !== q && text[i] !== '\n') {
				if (text[i] === '\\' && i + 1 < text.length) {
					i += 2;
					continue;
				}
				i++;
			}
			if (i < text.length && text[i] === q) i++;
			continue;
		}
		if (c === '#') {
			while (i < text.length && text[i] !== '\n') i++;
			continue;
		}
		if (c === '(') depth++;
		else if (c === ')') depth--;
		i++;
	}
	return depth === 0 ? i : -1;
}

function scanCallSites(text: string): CallSite[] {
	const out: CallSite[] = [];
	BUILTIN_RE.lastIndex = 0;
	let m: RegExpExecArray | null;
	while ((m = BUILTIN_RE.exec(text)) !== null) {
		const parenOpen = text.indexOf('(', m.index + m[1].length);
		if (parenOpen === -1) continue;
		const endPos = findCallEnd(text, parenOpen);
		if (endPos === -1) continue;
		out.push({ name: m[2], endPos });
	}
	return out;
}

function formatValue(v: unknown): string {
	if (v === null || v === undefined) return '—';
	if (typeof v === 'number') {
		if (Number.isInteger(v)) return String(v);
		const abs = Math.abs(v);
		if (abs !== 0 && (abs < 1e-3 || abs >= 1e6)) return v.toExponential(2);
		return v.toFixed(3);
	}
	if (typeof v === 'boolean') return v ? 'true' : 'false';
	if (typeof v === 'string') {
		const s = v.length > 32 ? v.slice(0, 29) + '…' : v;
		return JSON.stringify(s);
	}
	try {
		const s = JSON.stringify(v);
		return s.length > 32 ? s.slice(0, 29) + '…' : s;
	} catch {
		return String(v);
	}
}

type WidgetKind = 'good' | 'bad' | 'stale' | 'unknown' | 'missing';

function kindFor(lv: LiveValue | undefined): WidgetKind {
	if (!lv) return 'missing';
	const q = lv.quality?.toLowerCase();
	if (q === 'bad') return 'bad';
	if (q === 'uncertain') return 'stale';
	if (q === 'good' || q === undefined || q === '') return 'good';
	return 'unknown';
}

class InlineValueWidget extends WidgetType {
	constructor(
		private readonly label: string,
		private readonly kind: WidgetKind
	) {
		super();
	}

	override toDOM(): HTMLElement {
		const span = document.createElement('span');
		span.className = `cm-inline-value cm-inline-${this.kind}`;
		span.textContent = this.label;
		return span;
	}

	override eq(other: WidgetType): boolean {
		return (
			other instanceof InlineValueWidget &&
			other.label === this.label &&
			other.kind === this.kind
		);
	}

	override ignoreEvent(): boolean {
		return false;
	}
}

function buildDecorations(view: EditorView): DecorationSet {
	const builder = new RangeSetBuilder<Decoration>();
	const values = view.state.field(inlineValuesField, false) ?? new Map();
	const text = view.state.doc.toString();
	const calls = scanCallSites(text);
	// RangeSetBuilder requires ascending `from` positions. Nested calls
	// (e.g. `set_var("a", get_num("b"))`) are emitted outer-first by the
	// regex scanner, which would violate that invariant and silently drop
	// all decorations. Sort by end-of-call position before feeding them in.
	calls.sort((a, b) => a.endPos - b.endPos);
	for (const call of calls) {
		const lv = values.get(call.name);
		const kind = kindFor(lv);
		const label = lv ? formatValue(lv.value) : 'no value';
		const deco = Decoration.widget({
			widget: new InlineValueWidget(label, kind),
			side: 1
		});
		builder.add(call.endPos, call.endPos, deco);
	}
	return builder.finish();
}

const inlineValuesPlugin = ViewPlugin.fromClass(
	class {
		decorations: DecorationSet;

		constructor(view: EditorView) {
			this.decorations = buildDecorations(view);
		}

		update(u: ViewUpdate) {
			const prev = u.startState.field(inlineValuesField, false);
			const curr = u.state.field(inlineValuesField, false);
			if (u.docChanged || u.viewportChanged || prev !== curr) {
				this.decorations = buildDecorations(u.view);
			}
		}
	},
	{ decorations: (v) => v.decorations }
);

const inlineValuesTheme = EditorView.baseTheme({
	'.cm-inline-value': {
		marginLeft: '0.5rem',
		padding: '0.05rem 0.4rem',
		borderRadius: '999px',
		border: '1px solid',
		fontFamily: 'ui-sans-serif, system-ui, sans-serif',
		fontSize: '0.75em',
		fontWeight: '600',
		lineHeight: '1.2',
		letterSpacing: '0.01em',
		verticalAlign: 'middle',
		cursor: 'default',
		pointerEvents: 'none',
		userSelect: 'none'
	},
	'.cm-inline-good': {
		color: '#64d88a',
		borderColor: 'rgba(100, 216, 138, 0.45)',
		background: 'rgba(100, 216, 138, 0.12)'
	},
	'.cm-inline-stale': {
		color: '#e0b050',
		borderColor: 'rgba(224, 176, 80, 0.45)',
		background: 'rgba(224, 176, 80, 0.12)'
	},
	'.cm-inline-bad': {
		color: '#ff6b6b',
		borderColor: 'rgba(255, 107, 107, 0.45)',
		background: 'rgba(255, 107, 107, 0.12)'
	},
	'.cm-inline-unknown': {
		color: '#aaa',
		borderColor: 'rgba(150, 150, 150, 0.4)',
		background: 'rgba(150, 150, 150, 0.12)'
	},
	'.cm-inline-missing': {
		color: '#888',
		borderColor: 'rgba(150, 150, 150, 0.3)',
		background: 'rgba(150, 150, 150, 0.06)',
		fontStyle: 'italic'
	}
});

export function inlineValues() {
	return [inlineValuesField, inlineValuesPlugin, inlineValuesTheme];
}
