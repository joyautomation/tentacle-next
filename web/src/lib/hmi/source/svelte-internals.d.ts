// Internal Svelte sub-modules used at runtime to evaluate user-authored
// Svelte source. They have no shipped .d.ts declarations because they
// aren't part of the public API contract — we depend on them anyway so
// the runtime compiler in `svelteRuntime.ts` can satisfy the imports
// emitted by `svelte/compiler`.
declare module 'svelte/internal/client';
declare module 'svelte/internal/disclose-version';
declare module 'svelte/internal/flags/legacy';
