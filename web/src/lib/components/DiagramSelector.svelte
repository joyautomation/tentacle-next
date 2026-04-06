<script lang="ts">
  import { browser } from "$app/environment";

  export type VizMode = "tree" | "sunburst";

  const STORAGE_KEY = "tentacle-viz-mode";
  const validModes: VizMode[] = ["tree", "sunburst"];

  let { mode = $bindable<VizMode>("tree") }: { mode: VizMode } = $props();

  // Initialize from localStorage on mount
  if (browser) {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (validModes.includes(stored as VizMode)) {
      mode = stored as VizMode;
    }
  }

  function setMode(m: VizMode) {
    mode = m;
    if (browser) localStorage.setItem(STORAGE_KEY, m);
  }
</script>

<div class="view-toggle">
  <button class:active={mode === "tree"} onclick={() => setMode("tree")}
    >Tree</button
  >
  <button class:active={mode === "sunburst"} onclick={() => setMode("sunburst")}
    >Sunburst</button
  >
</div>

<style lang="scss">
  .view-toggle {
    display: flex;
    margin-left: auto;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    overflow: hidden;

    button {
      padding: 0.2rem 0.6rem;
      font-size: 0.75rem;
      font-family: inherit;
      border: none;
      border-right: 1px solid var(--theme-border);
      background: none;
      color: var(--theme-text-muted);
      cursor: pointer;
      transition:
        background 0.15s ease,
        color 0.15s ease;

      &:first-child {
        border-top-right-radius: 0 !important;
        border-bottom-right-radius: 0 !important;
      }
      &:last-child {
        border-top-left-radius: 0 !important;
        border-bottom-left-radius: 0 !important;
        border-right: none;
      }

      &.active {
        background: var(--badge-teal-bg);
        color: var(--badge-teal-text);
      }

      &:hover:not(.active) {
        background: color-mix(in srgb, var(--theme-text) 5%, transparent);
      }
    }
  }
</style>
