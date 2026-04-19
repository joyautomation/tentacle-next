/**
 * Multicursor helpers for VSCode-parity keybindings.
 *
 * CodeMirror 6 already ships Alt+Click (add cursor), Alt+drag (column
 * selection), and Ctrl/Cmd+D (select next occurrence, via searchKeymap).
 * What's missing is VSCode's "add cursor above/below" — this module adds
 * those two commands plus the keymap entries.
 */

import { EditorView, keymap, type Command } from '@codemirror/view';
import { EditorSelection, Prec } from '@codemirror/state';

function addCursorVertical(view: EditorView, dir: -1 | 1): boolean {
	const state = view.state;
	const additions = [] as ReturnType<typeof EditorSelection.cursor>[];
	for (const r of state.selection.ranges) {
		const line = state.doc.lineAt(r.head);
		const targetLineNo = line.number + dir;
		if (targetLineNo < 1 || targetLineNo > state.doc.lines) continue;
		const targetLine = state.doc.line(targetLineNo);
		const col = r.head - line.from;
		const pos = Math.min(targetLine.from + col, targetLine.to);
		additions.push(EditorSelection.cursor(pos));
	}
	if (additions.length === 0) return false;
	const allRanges = [...state.selection.ranges, ...additions];
	view.dispatch({
		selection: EditorSelection.create(allRanges, allRanges.length - 1),
		scrollIntoView: true
	});
	return true;
}

const addCursorAbove: Command = (view) => addCursorVertical(view, -1);
const addCursorBelow: Command = (view) => addCursorVertical(view, 1);

/**
 * Make Ctrl/Cmd+Click add a cursor (VSCode's default). CM6's default
 * is Alt+Click; we keep that working by accepting either modifier.
 */
const clickAddsSelection = EditorView.clickAddsSelectionRange.of(
	(event: MouseEvent) => event.altKey || event.ctrlKey || event.metaKey
);

export function multicursor() {
	return [
		clickAddsSelection,
		// High precedence so we shadow the default Mod-Shift-ArrowUp/Down
		// binding (selectDocStart/End) with add-cursor, matching VSCode.
		Prec.high(
			keymap.of([
				{ key: 'Mod-Shift-ArrowUp', run: addCursorAbove, preventDefault: true },
				{ key: 'Mod-Shift-ArrowDown', run: addCursorBelow, preventDefault: true }
			])
		)
	];
}
