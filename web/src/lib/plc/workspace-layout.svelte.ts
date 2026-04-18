import { browser } from '$app/environment';

const STORAGE_KEY = 'plc-workspace-layout-v1';

export type WorkspaceLayout = {
	leftOpen: boolean;
	rightOpen: boolean;
	bottomOpen: boolean;
	leftSize: number;
	rightSize: number;
	bottomSize: number;
};

const DEFAULT_LAYOUT: WorkspaceLayout = {
	leftOpen: true,
	rightOpen: true,
	bottomOpen: true,
	leftSize: 20,
	rightSize: 22,
	bottomSize: 25
};

function load(): WorkspaceLayout {
	if (!browser) return { ...DEFAULT_LAYOUT };
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (!raw) return { ...DEFAULT_LAYOUT };
		const parsed = JSON.parse(raw) as Partial<WorkspaceLayout>;
		return { ...DEFAULT_LAYOUT, ...parsed };
	} catch {
		return { ...DEFAULT_LAYOUT };
	}
}

export function createWorkspaceLayout() {
	const state = $state<WorkspaceLayout>(load());

	function save() {
		if (!browser) return;
		try {
			localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
		} catch {
			// ignore quota errors
		}
	}

	return {
		get leftOpen() {
			return state.leftOpen;
		},
		set leftOpen(v: boolean) {
			state.leftOpen = v;
			save();
		},
		get rightOpen() {
			return state.rightOpen;
		},
		set rightOpen(v: boolean) {
			state.rightOpen = v;
			save();
		},
		get bottomOpen() {
			return state.bottomOpen;
		},
		set bottomOpen(v: boolean) {
			state.bottomOpen = v;
			save();
		},
		get leftSize() {
			return state.leftSize;
		},
		set leftSize(v: number) {
			state.leftSize = v;
			save();
		},
		get rightSize() {
			return state.rightSize;
		},
		set rightSize(v: number) {
			state.rightSize = v;
			save();
		},
		get bottomSize() {
			return state.bottomSize;
		},
		set bottomSize(v: number) {
			state.bottomSize = v;
			save();
		}
	};
}
