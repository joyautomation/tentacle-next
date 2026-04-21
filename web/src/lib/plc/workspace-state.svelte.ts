export type SelectionKind = 'variable' | 'task' | 'program';

export type Selection = {
	kind: SelectionKind;
	id: string;
} | null;

export type EditorTabKind = 'program' | 'variable' | 'task';

export type EditorTab = {
	name: string;
	kind: EditorTabKind;
	language?: string;
};

// Kept as alias for incremental migration; prefer EditorTab.
export type ProgramTab = EditorTab;

export type DiagnosticSeverity = 'error' | 'warning' | 'info' | 'hint';

export type WorkspaceDiagnostic = {
	severity: DiagnosticSeverity;
	message: string;
	startLine: number;
	startCol: number;
	endLine: number;
	endCol: number;
	source?: string;
};

const VIEW_STORAGE_KEY = 'tentacle-plc-workspace-view';

function loadViewPrefs(): { showInlineValues: boolean } {
	if (typeof localStorage === 'undefined') return { showInlineValues: false };
	try {
		const raw = localStorage.getItem(VIEW_STORAGE_KEY);
		if (!raw) return { showInlineValues: false };
		const parsed = JSON.parse(raw) as { showInlineValues?: boolean };
		return { showInlineValues: !!parsed.showInlineValues };
	} catch {
		return { showInlineValues: false };
	}
}

export type VariableDraft = {
	datatype: string;
	direction: string;
	description?: string;
	default?: unknown;
};

const state = $state<{
	selection: Selection;
	tabs: EditorTab[];
	activeTab: string | null;
	dirty: Record<string, boolean>;
	diagnostics: Record<string, WorkspaceDiagnostic[]>;
	view: { showInlineValues: boolean };
	variableDrafts: Record<string, VariableDraft>;
}>({
	selection: null,
	tabs: [],
	activeTab: null,
	dirty: {},
	diagnostics: {},
	view: loadViewPrefs(),
	variableDrafts: {}
});

function persistView() {
	if (typeof localStorage === 'undefined') return;
	try {
		localStorage.setItem(VIEW_STORAGE_KEY, JSON.stringify(state.view));
	} catch {
		/* storage may be blocked — non-fatal */
	}
}

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
	open(tab: EditorTab) {
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

export const workspaceView = {
	get showInlineValues() {
		return state.view.showInlineValues;
	},
	setShowInlineValues(on: boolean) {
		state.view.showInlineValues = on;
		persistView();
	},
	toggleInlineValues() {
		state.view.showInlineValues = !state.view.showInlineValues;
		persistView();
	}
};

export const workspaceVariableDrafts = {
	get map() {
		return state.variableDrafts;
	},
	get(name: string): VariableDraft | null {
		return state.variableDrafts[name] ?? null;
	},
	set(name: string, draft: VariableDraft) {
		state.variableDrafts[name] = draft;
	},
	clear(name: string) {
		delete state.variableDrafts[name];
	}
};

export const workspaceDiagnostics = {
	get byUri() {
		return state.diagnostics;
	},
	get total() {
		let n = 0;
		for (const uri in state.diagnostics) n += state.diagnostics[uri].length;
		return n;
	},
	get errorCount() {
		let n = 0;
		for (const uri in state.diagnostics) {
			for (const d of state.diagnostics[uri]) if (d.severity === 'error') n++;
		}
		return n;
	},
	set(uri: string, diags: WorkspaceDiagnostic[]) {
		if (diags.length === 0) {
			delete state.diagnostics[uri];
		} else {
			state.diagnostics[uri] = diags;
		}
	},
	clear(uri: string) {
		delete state.diagnostics[uri];
	}
};
