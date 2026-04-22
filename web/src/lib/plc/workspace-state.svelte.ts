export type SelectionKind = 'variable' | 'task' | 'program';

export type Selection = {
	kind: SelectionKind;
	id: string;
} | null;

export type EditorTabKind = 'program' | 'variable' | 'task';

export type EditorTab = {
	id: string; // composite `${kind}:${name}` so tabs of different kinds can share a name
	name: string;
	kind: EditorTabKind;
	language?: string;
	// Untitled tabs created in-editor before their first save. The id is a
	// synthetic `program:__new_<n>` so it stays stable while the user types
	// their def header; once saved, the tab is renamed via renameTab().
	isNew?: boolean;
};

export function tabId(kind: EditorTabKind, name: string): string {
	return `${kind}:${name}`;
}

let newTabCounter = 0;
export function newTabId(kind: EditorTabKind): string {
	newTabCounter += 1;
	return `${kind}:__new_${newTabCounter}`;
}

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

export type ReferenceSite = {
	source: 'program' | 'taskProgramRef';
	program?: string;
	task?: string;
	line?: number;
	startCol?: number;
	endCol?: number;
	lineText?: string;
};

export type ReferencesQuery = {
	name: string;
	kind: 'program' | 'variable';
	sites: ReferenceSite[];
	loading: boolean;
	error?: string;
};

export type OutputTab = 'problems' | 'logs' | 'references';

// EditorGoto is the per-tab "jump to (line, col)" hook the editor exposes
// so References results can navigate into the open source.
export type EditorGoto = (line: number, col: number) => void;

const state = $state<{
	selection: Selection;
	tabs: EditorTab[];
	activeTab: string | null;
	dirty: Record<string, boolean>;
	diagnostics: Record<string, WorkspaceDiagnostic[]>;
	view: { showInlineValues: boolean };
	variableDrafts: Record<string, VariableDraft>;
	references: ReferencesQuery | null;
	outputTab: OutputTab;
}>({
	selection: null,
	tabs: [],
	activeTab: null,
	dirty: {},
	diagnostics: {},
	view: loadViewPrefs(),
	variableDrafts: {},
	references: null,
	outputTab: 'problems'
});

// Goto handlers are kept outside $state because Svelte's deep proxy would
// wrap function values and break CodeMirror's `this` bindings on dispatch.
const editorGotos = new Map<string, EditorGoto>();

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
	open(input: Omit<EditorTab, 'id'>) {
		const id = tabId(input.kind, input.name);
		if (!state.tabs.some((t) => t.id === id)) {
			state.tabs = [...state.tabs, { id, ...input }];
		}
		state.activeTab = id;
	},
	// openNew creates an unsaved tab with a synthetic id. The tab renders
	// the editor in "new" mode — the user types a def, the pending name is
	// derived from the header, and renameTab promotes the tab to its real
	// key on first save.
	openNew(kind: EditorTabKind, language?: string): string {
		const id = newTabId(kind);
		const tab: EditorTab = { id, name: '', kind, language, isNew: true };
		state.tabs = [...state.tabs, tab];
		state.activeTab = id;
		return id;
	},
	// renameTab swaps a tab's id/name (and clears isNew) after a successful
	// save or rename. If a tab with the target id already exists, we drop
	// the source tab to avoid duplicates.
	renameTab(oldId: string, newName: string) {
		const idx = state.tabs.findIndex((t) => t.id === oldId);
		if (idx === -1) return;
		const src = state.tabs[idx];
		const newId = tabId(src.kind, newName);
		const collision = state.tabs.findIndex((t) => t.id === newId);
		if (collision !== -1 && collision !== idx) {
			// Caller is expected to prevent this — but if it happens, close
			// the stale source tab rather than shadowing the existing one.
			this.close(oldId);
			state.activeTab = newId;
			return;
		}
		const next = state.tabs.slice();
		next[idx] = { ...src, id: newId, name: newName, isNew: false };
		state.tabs = next;
		if (state.activeTab === oldId) state.activeTab = newId;
		if (state.dirty[oldId]) {
			state.dirty[newId] = true;
			delete state.dirty[oldId];
		}
	},
	activate(id: string) {
		if (state.tabs.some((t) => t.id === id)) {
			state.activeTab = id;
		}
	},
	close(id: string) {
		const idx = state.tabs.findIndex((t) => t.id === id);
		if (idx === -1) return;
		const next = state.tabs.filter((t) => t.id !== id);
		state.tabs = next;
		delete state.dirty[id];
		if (state.activeTab === id) {
			state.activeTab =
				next[idx]?.id ?? next[idx - 1]?.id ?? next[next.length - 1]?.id ?? null;
		}
	},
	// setTabLabel updates only the display name for a tab. Intended for
	// unsaved ("new") tabs whose name is being derived live from the def
	// header the user is typing — the tab's id stays on its synthetic key.
	setTabLabel(id: string, label: string) {
		const idx = state.tabs.findIndex((t) => t.id === id);
		if (idx === -1) return;
		if (state.tabs[idx].name === label) return;
		const next = state.tabs.slice();
		next[idx] = { ...next[idx], name: label };
		state.tabs = next;
	},
	setDirty(id: string, dirty: boolean) {
		if (dirty) {
			state.dirty[id] = true;
		} else {
			delete state.dirty[id];
		}
	},
	clearDirty(id: string) {
		delete state.dirty[id];
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

// workspaceReferences is the reactive store behind the "References" tab in
// the bottom output panel. Callers (Inspector, future editor context menu)
// kick off a query via setLoading → setResult; the panel reads `current`
// and re-renders. Fetching the endpoint is the caller's job — this module
// stays presentation-free.
export const workspaceReferences = {
	get current() {
		return state.references;
	},
	setLoading(name: string, kind: 'program' | 'variable') {
		state.references = { name, kind, sites: [], loading: true };
	},
	setResult(name: string, kind: 'program' | 'variable', sites: ReferenceSite[]) {
		state.references = { name, kind, sites, loading: false };
	},
	setError(name: string, kind: 'program' | 'variable', message: string) {
		state.references = { name, kind, sites: [], loading: false, error: message };
	},
	clear() {
		state.references = null;
	}
};

export const workspaceOutput = {
	get tab() {
		return state.outputTab;
	},
	setTab(tab: OutputTab) {
		state.outputTab = tab;
	}
};

// workspaceEditorGotos is a non-reactive registry mapping tab ids to the
// editor's `goto(line, col)` handler. Tabs register on mount and clear on
// destroy. References results call `invoke(tabId, line, col)` to jump.
export const workspaceEditorGotos = {
	register(tabId: string, goto: EditorGoto) {
		editorGotos.set(tabId, goto);
	},
	unregister(tabId: string) {
		editorGotos.delete(tabId);
	},
	invoke(tabId: string, line: number, col: number): boolean {
		const goto = editorGotos.get(tabId);
		if (!goto) return false;
		goto(line, col);
		return true;
	}
};
