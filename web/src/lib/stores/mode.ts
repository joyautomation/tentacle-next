import { writable } from 'svelte/store';

/** True when the orchestrator is running in monolith mode (all modules embedded in one binary). */
export const isMonolith = writable(false);
