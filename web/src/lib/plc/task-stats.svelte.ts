/**
 * Per-task scan-time stats, polled from /plcs/plc/tasks/stats.
 *
 * Refcounted so every consumer that calls startTaskStats() shares a
 * single 1 Hz poll. The shape mirrors the Go-side TaskStatsSnapshot —
 * see internal/plc/task_stats.go.
 */

import { api } from '$lib/api/client';

export type TaskStats = {
	samples: number;
	totalRuns: number;
	totalErrors: number;
	p50Us: number;
	p95Us: number;
	p99Us: number;
	minUs: number;
	maxUs: number;
	meanUs: number;
	lastUs: number;
	lastRunMs: number;
	lastError?: string;
	effectiveHz: number;
	scanRateMs: number;
};

export type TaskStatsMap = Record<string, TaskStats>;

let current = $state<TaskStatsMap>({});
let version = $state(0);
let subscribers = 0;
let timer: ReturnType<typeof setInterval> | null = null;

async function fetchOnce(): Promise<void> {
	const result = await api<TaskStatsMap>('/plcs/plc/tasks/stats');
	if (result.error || !result.data) return;
	current = result.data;
	version++;
}

export function startTaskStats(): () => void {
	subscribers++;
	if (subscribers === 1) {
		void fetchOnce();
		timer = setInterval(fetchOnce, 1000);
	}
	let stopped = false;
	return () => {
		if (stopped) return;
		stopped = true;
		subscribers--;
		if (subscribers === 0 && timer) {
			clearInterval(timer);
			timer = null;
		}
	};
}

export function taskStatsVersion(): number {
	return version;
}

export function getTaskStats(taskId: string): TaskStats | undefined {
	return current[taskId];
}
