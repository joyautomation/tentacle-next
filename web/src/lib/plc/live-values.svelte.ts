/**
 * Global live variable values store.
 *
 * One SSE subscription shared by every consumer (editor overlays, the
 * Inspector panel, anything else that needs live PLC values). Consumers
 * call `startLiveValues()` at mount and hold onto the returned stop
 * function until unmount. When refcount returns to zero the SSE
 * connection is torn down.
 *
 * A one-shot bootstrap fetch of `/variables` seeds the map so values
 * show up immediately instead of waiting for the first batch tick.
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

const raw = new Map<string, LiveValue>();
let version = $state(0);
let subscribers = 0;
let unsubSse: (() => void) | null = null;
let bootstrapped = false;

async function bootstrap(): Promise<void> {
	if (bootstrapped) return;
	bootstrapped = true;
	const result = await api<VariableRecord[]>('/variables');
	if (result.error) return;
	for (const v of result.data ?? []) {
		if (!v.variableId) continue;
		raw.set(v.variableId, {
			value: v.value,
			datatype: v.datatype,
			quality: v.quality,
			lastUpdated: v.lastUpdated
		});
	}
	version++;
}

export function startLiveValues(): () => void {
	subscribers++;
	if (subscribers === 1) {
		void bootstrap();
		unsubSse = subscribe<PlcDataPayload[]>('/variables/stream/batch', (batch) => {
			if (!Array.isArray(batch)) return;
			for (const msg of batch) {
				if (!msg?.variableId) continue;
				raw.set(msg.variableId, {
					value: msg.value,
					datatype: msg.datatype,
					lastUpdated: msg.timestamp,
					quality: 'good'
				});
			}
			version++;
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

/** Read the reactive version counter. Consumers wrap this in `$derived.by`
 * to subscribe to updates. */
export function liveValuesVersion(): number {
	return version;
}

/** Return a fresh Map snapshot of all current values. */
export function liveValuesSnapshot(): LiveValueMap {
	return new Map(raw);
}

/** Look up a single variable's live value. Non-reactive — wrap a call in
 * `$derived.by(() => { void liveValuesVersion(); return getLiveValue(name); })`
 * when you need reactivity. */
export function getLiveValue(name: string): LiveValue | undefined {
	return raw.get(name);
}
