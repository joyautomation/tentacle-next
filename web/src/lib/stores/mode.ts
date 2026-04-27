import { writable, derived } from 'svelte/store';

/** True when the orchestrator is running in monolith mode (all modules embedded in one binary). */
export const isMonolith = writable(false);

/**
 * Explicit identity of the running binary, declared at build time
 * (build-tag-gated init in internal/version/role_mantle.go). "tentacle" is
 * the default for edge nodes; "mantle" indicates the central control plane
 * build. The UI uses this — not the running module set — to choose its
 * branding, so the chrome only flips on a deliberate operator action.
 */
export const role = writable<'tentacle' | 'mantle'>('tentacle');

export const brandName = derived(role, ($role) => ($role === 'mantle' ? 'Mantle' : 'Tentacle'));
