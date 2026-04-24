<script lang="ts">
  interface Props {
    /** Selected element index (null = nothing selected). */
    idx: number | null;
    /** Currently-active anchors derived from the element's inline style. */
    anchors: { x: 'left' | 'right'; y: 'top' | 'bottom' } | null;
    onAnchorChange: (axis: 'x' | 'y', anchor: 'left' | 'right' | 'top' | 'bottom') => void;
    onClear: () => void;
  }

  let { idx, anchors, onAnchorChange, onClear }: Props = $props();
</script>

<details class="element-editor" open>
  <summary class="hdr"><span class="h3">Element</span></summary>
  <div class="body">
    {#if idx === null}
      <p class="hint">Click an element in the preview to select it.</p>
    {:else}
      <div class="top">
        <button class="clear" onclick={onClear} title="Clear selection">clear</button>
      </div>
      <div class="row">
        <span class="label">x anchor</span>
        <div class="seg">
          <button
            class:active={anchors?.x === 'left'}
            onclick={() => onAnchorChange('x', 'left')}
          >left</button>
          <button
            class:active={anchors?.x === 'right'}
            onclick={() => onAnchorChange('x', 'right')}
          >right</button>
        </div>
      </div>
      <div class="row">
        <span class="label">y anchor</span>
        <div class="seg">
          <button
            class:active={anchors?.y === 'top'}
            onclick={() => onAnchorChange('y', 'top')}
          >top</button>
          <button
            class:active={anchors?.y === 'bottom'}
            onclick={() => onAnchorChange('y', 'bottom')}
          >bottom</button>
        </div>
      </div>
      <p class="hint">Toggling re-pins the element from its current position so it stays put visually.</p>
    {/if}
  </div>
</details>

<style lang="scss">
  .element-editor {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    overflow: hidden;
  }
  .hdr {
    list-style: none;
    cursor: pointer;
    padding: 0.5rem 0.75rem;
    user-select: none;
    display: flex;
    align-items: center;
    gap: 0.375rem;
    &::-webkit-details-marker { display: none; }
    &::before {
      content: '▸';
      font-size: 0.625rem;
      color: var(--theme-text-muted);
      transition: transform 0.12s ease;
    }
    .h3 {
      font-size: 0.6875rem;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--theme-text-muted);
      font-weight: 600;
    }
  }
  .element-editor[open] > .hdr::before { transform: rotate(90deg); }
  .body {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0 0.75rem 0.75rem;
  }
  .top {
    display: flex;
    justify-content: flex-end;
    .clear {
      background: transparent;
      border: 1px solid var(--theme-border);
      color: var(--theme-text-muted);
      cursor: pointer;
      font-size: 0.6875rem;
      padding: 0.125rem 0.5rem;
      border-radius: var(--rounded-sm, 4px);
      &:hover { color: var(--theme-text); border-color: var(--theme-text); }
    }
  }
  .row {
    display: grid;
    grid-template-columns: 5rem 1fr;
    align-items: center;
    gap: 0.5rem;
    .label {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
      color: var(--theme-text-muted);
    }
  }
  .seg {
    display: flex;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    overflow: hidden;
    button {
      flex: 1;
      background: var(--theme-background);
      color: var(--theme-text-muted);
      border: none;
      padding: 0.25rem 0.5rem;
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
      cursor: pointer;
      &:not(:last-child) { border-right: 1px solid var(--theme-border); }
      &:hover { color: var(--theme-text); }
      &.active {
        background: var(--theme-text);
        color: var(--theme-background);
      }
    }
  }
  .hint {
    margin: 0;
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    line-height: 1.4;
  }
</style>
