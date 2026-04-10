# Web UI Conventions

## Theming

Always use `--theme-*` CSS variables for interactive elements. Never hardcode hex colors or use raw `--color-*` variables directly.

## Transitions

Use Svelte `slide` transitions for conditional elements that affect document flow (e.g. `{#if}` blocks that push content around).

## Button text swap

When a button temporarily changes its label (e.g. "Copy" → "Copied!"), use an opacity overlay to prevent the button from resizing:

1. Keep the original text in a `<span>` — this sizes the button. Fade it to `opacity: 0`.
2. Absolutely position a second `<span>` centered over the button with the temporary text.
3. Fade the overlay in/out with CSS transitions.

Do NOT swap text content directly or use `min-width` hacks. See `GitOpsSetup.svelte` `.copy-btn` for the reference implementation.

## API payloads

When the Go backend struct field is `map[string]T`, send a `Record<string, T>` (object keyed by name/id), NOT an array. The Go JSON decoder fails when it receives `[{name: "foo", ...}]` instead of `{"foo": {...}}`. Always wrap maps in the struct field name (e.g. `{ overrides }` not just `overrides`).

## Clipboard

`navigator.clipboard` requires HTTPS or localhost. Always include a `document.execCommand('copy')` fallback for non-secure contexts (common on dev/LAN access).
