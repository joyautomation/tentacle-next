/**
 * Rainbow brackets extension for CodeMirror 6.
 *
 * Colors matching pairs of `()`, `[]`, and `{}` in rotating hues so scope is
 * visible at a glance (VSCode-style). The scanner is string- and comment-
 * aware: brackets inside string literals or after `#` are ignored.
 *
 * The extension runs on visible ranges only and rebuilds decorations on
 * viewport/document changes — cheap even for large files.
 */

import { ViewPlugin, Decoration, type DecorationSet, EditorView, type ViewUpdate } from '@codemirror/view';
import { RangeSetBuilder } from '@codemirror/state';

const DEPTH_CLASSES = 6;

const depthMarks = Array.from({ length: DEPTH_CLASSES }, (_, i) =>
	Decoration.mark({ class: `cm-rainbow-bracket cm-rainbow-depth-${i}` })
);
const unmatchedMark = Decoration.mark({ class: 'cm-rainbow-bracket cm-rainbow-unmatched' });

function isOpener(c: string): boolean {
	return c === '(' || c === '[' || c === '{';
}

function isCloser(c: string): boolean {
	return c === ')' || c === ']' || c === '}';
}

function matches(open: string, close: string): boolean {
	return (
		(open === '(' && close === ')') ||
		(open === '[' && close === ']') ||
		(open === '{' && close === '}')
	);
}

/**
 * Build decorations by scanning the entire document, tracking bracket depth
 * with string/comment awareness. We scan the whole doc (not just the
 * viewport) so depth stays consistent across scroll.
 */
function buildDecorations(view: EditorView): DecorationSet {
	const builder = new RangeSetBuilder<Decoration>();
	const text = view.state.doc.toString();
	const stack: { ch: string; pos: number }[] = [];
	// Collect matched pairs so we can emit decorations in document order.
	const marks: { from: number; to: number; depth: number; matched: boolean }[] = [];

	let i = 0;
	while (i < text.length) {
		const c = text[i];

		// Line comment — skip to EOL.
		if (c === '#') {
			while (i < text.length && text[i] !== '\n') i++;
			continue;
		}

		// Triple-quoted string.
		if ((c === '"' || c === "'") && text[i + 1] === c && text[i + 2] === c) {
			const q = c;
			i += 3;
			while (i < text.length) {
				if (text[i] === '\\' && i + 1 < text.length) {
					i += 2;
					continue;
				}
				if (text[i] === q && text[i + 1] === q && text[i + 2] === q) {
					i += 3;
					break;
				}
				i++;
			}
			continue;
		}

		// Single-line string.
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

		if (isOpener(c)) {
			stack.push({ ch: c, pos: i });
		} else if (isCloser(c)) {
			const top = stack[stack.length - 1];
			if (top && matches(top.ch, c)) {
				const depth = (stack.length - 1) % DEPTH_CLASSES;
				marks.push({ from: top.pos, to: top.pos + 1, depth, matched: true });
				marks.push({ from: i, to: i + 1, depth, matched: true });
				stack.pop();
			} else {
				marks.push({ from: i, to: i + 1, depth: 0, matched: false });
			}
		}

		i++;
	}

	// Unmatched openers still on the stack.
	for (const open of stack) {
		marks.push({ from: open.pos, to: open.pos + 1, depth: 0, matched: false });
	}

	marks.sort((a, b) => a.from - b.from);
	for (const m of marks) {
		builder.add(m.from, m.to, m.matched ? depthMarks[m.depth] : unmatchedMark);
	}
	return builder.finish();
}

const rainbowTheme = EditorView.baseTheme({
	'.cm-rainbow-bracket': { fontWeight: 'bold' },
	'.cm-rainbow-depth-0': { color: '#ffd700' }, // gold
	'.cm-rainbow-depth-1': { color: '#da70d6' }, // orchid
	'.cm-rainbow-depth-2': { color: '#179fff' }, // azure
	'.cm-rainbow-depth-3': { color: '#64d88a' }, // green
	'.cm-rainbow-depth-4': { color: '#ff9966' }, // salmon
	'.cm-rainbow-depth-5': { color: '#c792ea' }, // lavender
	'.cm-rainbow-unmatched': {
		color: '#ff3b3b',
		textDecoration: 'underline wavy #ff3b3b'
	}
});

const rainbowPlugin = ViewPlugin.fromClass(
	class {
		decorations: DecorationSet;

		constructor(view: EditorView) {
			this.decorations = buildDecorations(view);
		}

		update(u: ViewUpdate) {
			if (u.docChanged || u.viewportChanged) {
				this.decorations = buildDecorations(u.view);
			}
		}
	},
	{ decorations: (v) => v.decorations }
);

export function rainbowBrackets() {
	return [rainbowPlugin, rainbowTheme];
}
