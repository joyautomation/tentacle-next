export type SelectionKind = 'variable' | 'task' | 'program';

export type Selection = {
	kind: SelectionKind;
	id: string;
} | null;

const state = $state<{ selection: Selection }>({ selection: null });

export const workspaceSelection = {
	get current() {
		return state.selection;
	},
	select(kind: SelectionKind, id: string) {
		state.selection = { kind, id };
	},
	clear() {
		state.selection = null;
	},
	isSelected(kind: SelectionKind, id: string) {
		return state.selection?.kind === kind && state.selection.id === id;
	}
};
