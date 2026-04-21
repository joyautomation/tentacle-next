/**
 * Live PLC variable values store.
 *
 * Consumers subscribe to specific root variables they care about. The
 * store refcounts each watched root and keeps a single SSE connection
 * open to `/variables/stream/watch` with the union of all watched
 * roots. When the watched set changes the connection is reopened with
 * the new filter (EventSource can't update its URL in place — but the
 * set changes only when components mount/unmount, not per value update).
 *
 * `startLiveValues()` remains for consumers that want the legacy
 * firehose (everything). These share one all-variables connection.
 *
 * Bootstrapping: the first watch call fetches `/variables` once so
 * consumers see data immediately without waiting for the first SSE
 * flush tick.
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

// Per-root refcount. 0 means unwatched; positive means one or more
// consumers have declared interest.
const watchedRefs = new Map<string, number>();

// A separate refcount for the "give me everything" subscribers that
// use startLiveValues() (legacy API, plus pages like the variable
// tree that truly want all tags).
let wildcardRefs = 0;

let currentSse: (() => void) | null = null;
let currentKey = '';
let bootstrapped = false;
let reconcileScheduled = false;

function ingestBatch(batch: unknown) {
	if (!Array.isArray(batch)) return;
	let changed = false;
	for (const m of batch as PlcDataPayload[]) {
		if (!m?.variableId) continue;
		raw.set(m.variableId, {
			value: m.value,
			datatype: m.datatype,
			lastUpdated: m.timestamp,
			quality: 'good'
		});
		changed = true;
	}
	if (changed) version++;
}

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

function buildFilterKey(): string {
	if (wildcardRefs > 0) return '*';
	const names = Array.from(watchedRefs.keys()).filter((n) => (watchedRefs.get(n) ?? 0) > 0);
	names.sort();
	return names.join(',');
}

function reconcileSubscription() {
	const key = buildFilterKey();
	if (key === currentKey) return;
	currentKey = key;

	if (currentSse) {
		currentSse();
		currentSse = null;
	}

	if (key === '') return; // no subscribers

	const path = key === '*' ? '/variables/stream/watch' : `/variables/stream/watch?vars=${encodeURIComponent(key)}`;
	currentSse = subscribe<PlcDataPayload[]>(path, ingestBatch);
}

function scheduleReconcile() {
	if (reconcileScheduled) return;
	reconcileScheduled = true;
	// Defer to microtask so a component that watches N variables in
	// rapid succession only triggers one SSE reopen.
	queueMicrotask(() => {
		reconcileScheduled = false;
		reconcileSubscription();
	});
}

/**
 * Watch a single root variable. Returns a stop function that
 * decrements the refcount and, if zero, drops it from the
 * subscription filter.
 */
export function watchVariable(name: string): () => void {
	if (!name) return () => {};
	void bootstrap();
	watchedRefs.set(name, (watchedRefs.get(name) ?? 0) + 1);
	scheduleReconcile();
	let stopped = false;
	return () => {
		if (stopped) return;
		stopped = true;
		const n = (watchedRefs.get(name) ?? 0) - 1;
		if (n <= 0) watchedRefs.delete(name);
		else watchedRefs.set(name, n);
		scheduleReconcile();
	};
}

/**
 * Watch a set of root variables. Convenience for pages that render a
 * list and want to swap the watched set atomically as the list changes.
 */
export function watchVariables(names: Iterable<string>): () => void {
	const stops: Array<() => void> = [];
	for (const n of names) stops.push(watchVariable(n));
	let stopped = false;
	return () => {
		if (stopped) return;
		stopped = true;
		for (const s of stops) s();
	};
}

/**
 * Legacy API: subscribe to *all* variables via the wildcard filter.
 * Used by pages that render a full variable tree. Prefer
 * `watchVariable` when you know the specific roots you need.
 */
export function startLiveValues(): () => void {
	wildcardRefs++;
	void bootstrap();
	scheduleReconcile();
	let stopped = false;
	return () => {
		if (stopped) return;
		stopped = true;
		wildcardRefs--;
		if (wildcardRefs < 0) wildcardRefs = 0;
		scheduleReconcile();
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
