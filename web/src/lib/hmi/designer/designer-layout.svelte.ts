import { browser } from '$app/environment';

const STORAGE_KEY = 'hmi-designer-layout-v1';

export type DesignerLayout = {
	leftOpen: boolean;
	rightOpen: boolean;
	leftSize: number;
	rightSize: number;
	previewSize: number;
};

const DEFAULT_LAYOUT: DesignerLayout = {
	leftOpen: true,
	rightOpen: true,
	leftSize: 16,
	rightSize: 22,
	previewSize: 55
};

function load(): DesignerLayout {
	if (!browser) return { ...DEFAULT_LAYOUT };
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (!raw) return { ...DEFAULT_LAYOUT };
		const parsed = JSON.parse(raw) as Partial<DesignerLayout>;
		return { ...DEFAULT_LAYOUT, ...parsed };
	} catch {
		return { ...DEFAULT_LAYOUT };
	}
}

export function createDesignerLayout() {
	const state = $state<DesignerLayout>(load());

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
		get previewSize() {
			return state.previewSize;
		},
		set previewSize(v: number) {
			state.previewSize = v;
			save();
		}
	};
}
