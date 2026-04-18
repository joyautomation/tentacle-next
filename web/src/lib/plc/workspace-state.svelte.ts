export type SelectionKind = 'variable' | 'task' | 'program';

export type Selection = {
	kind: SelectionKind;
	id: string;
} | null;

export type ProgramTab = {
	name: string;
	language: string;
};

const state = $state<{
	selection: Selection;
	tabs: ProgramTab[];
	activeTab: string | null;
	dirty: Record<string, boolean>;
}>({
	selection: null,
	tabs: [],
	activeTab: null,
	dirty: {}
});

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

export const workspaceTabs = {
	get list() {
		return state.tabs;
	},
	get active() {
		return state.activeTab;
	},
	get dirty() {
		return state.dirty;
	},
	open(tab: ProgramTab) {
		if (!state.tabs.some((t) => t.name === tab.name)) {
			state.tabs = [...state.tabs, tab];
		}
		state.activeTab = tab.name;
	},
	activate(name: string) {
		if (state.tabs.some((t) => t.name === name)) {
			state.activeTab = name;
		}
	},
	close(name: string) {
		const idx = state.tabs.findIndex((t) => t.name === name);
		if (idx === -1) return;
		const next = state.tabs.filter((t) => t.name !== name);
		state.tabs = next;
		delete state.dirty[name];
		if (state.activeTab === name) {
			state.activeTab =
				next[idx]?.name ?? next[idx - 1]?.name ?? next[next.length - 1]?.name ?? null;
		}
	},
	setDirty(name: string, dirty: boolean) {
		if (dirty) {
			state.dirty[name] = true;
		} else {
			delete state.dirty[name];
		}
	},
	clearDirty(name: string) {
		delete state.dirty[name];
	}
};
