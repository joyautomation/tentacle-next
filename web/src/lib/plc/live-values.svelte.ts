/**
 * Global live variable values store.
 *
 * One SSE subscription feeds every editor, inspector, etc. that needs live
 * PLC values. Components call `startLiveValues()` and hold onto the returned
 * stop function until they unmount — when the last consumer leaves, the SSE
 * connection is torn down.
 *
 * A one-shot bootstrap fetch of `/variables` seeds the map so values appear
 * immediately instead of waiting for the first batch tick.
 */

import { subscribe } from '$lib/api/subscribe';
import { api } from '$lib/api/client';
import type { LiveValue, LiveValueMap } from '$lib/editor/inline-values';

type PlcDataPayload = {
	variableId: string;
	value: unknown;
	datatype?: string;
	timestamp?: number;
};

type VariableRecord = {
	variableId: string;
	value: unknown;
	datatype?: string;
	quality?: string;
	lastUpdated?: number;
};

const internal = $state<Record<string, LiveValue>>({});

let subscribers = 0;
let unsubSse: (() => void) | null = null;
let bootstrapped = false;

// The map reference the editor sees changes on every batch so the
// StateEffect with a new reference actually triggers a rebuild.
let snapshotVersion = $state(0);

const snapshotCache = $derived.by<LiveValueMap>(() => {
	void snapshotVersion;
	const m = new Map<string, LiveValue>();
	for (const k in internal) m.set(k, internal[k]);
	return m;
});

function applyOne(msg: PlcDataPayload): void {
	if (!msg || !msg.variableId) return;
	internal[msg.variableId] = {
		value: msg.value,
		datatype: msg.datatype,
		lastUpdated: msg.timestamp,
		quality: 'good'
	};
}

async function bootstrap(): Promise<void> {
	if (bootstrapped) return;
	bootstrapped = true;
	const result = await api<VariableRecord[]>('/variables');
	if (result.error) return;
	for (const v of result.data ?? []) {
		internal[v.variableId] = {
			value: v.value,
			datatype: v.datatype,
			quality: v.quality,
			lastUpdated: v.lastUpdated
		};
	}
	snapshotVersion++;
}

export function startLiveValues(): () => void {
	subscribers++;
	if (subscribers === 1) {
		void bootstrap();
		unsubSse = subscribe<PlcDataPayload[]>('/variables/stream/batch', (batch) => {
			if (!Array.isArray(batch)) return;
			for (const msg of batch) applyOne(msg);
			snapshotVersion++;
		});
	}
	let stopped = false;
	return () => {
		if (stopped) return;
		stopped = true;
		subscribers--;
		if (subscribers === 0) {
			unsubSse?.();
			unsubSse = null;
		}
	};
}

export const liveValues = {
	get snapshot(): LiveValueMap {
		return snapshotCache;
	},
	get version(): number {
		return snapshotVersion;
	},
	get(name: string): LiveValue | undefined {
		return internal[name];
	}
};
