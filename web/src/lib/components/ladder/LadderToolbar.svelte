<script lang="ts">
  import type { Selection } from './types.js';

  interface Props {
    selection: Selection;
    onAddRung: () => void;
    onAddContact: (form: 'NO' | 'NC') => void;
    onAddCoil: (form: 'OTE' | 'OTL' | 'OTU') => void;
    onWrapParallel: () => void;
    onDelete: () => void;
  }

  let {
    selection,
    onAddRung,
    onAddContact,
    onAddCoil,
    onWrapParallel,
    onDelete,
  }: Props = $props();

  const canWrapParallel = $derived(selection !== null && selection.kind === 'logic');
  const canDelete = $derived(selection !== null);
</script>

<div class="toolbar">
  <button type="button" onclick={onAddRung} title="Add a new rung at the bottom">
    + Rung
  </button>

  <span class="sep" aria-hidden="true"></span>

  <button type="button" onclick={() => onAddContact('NO')} title="Add NO contact in series">
    NO
  </button>
  <button type="button" onclick={() => onAddContact('NC')} title="Add NC contact in series">
    NC
  </button>
  <button
    type="button"
    onclick={onWrapParallel}
    disabled={!canWrapParallel}
    title="Wrap selection in a parallel branch (OR)"
  >
    | branch
  </button>

  <span class="sep" aria-hidden="true"></span>

  <button type="button" onclick={() => onAddCoil('OTE')} title="Add output coil (non-retentive)">
    OTE
  </button>
  <button type="button" onclick={() => onAddCoil('OTL')} title="Add latch coil (set on rising power)">
    OTL
  </button>
  <button type="button" onclick={() => onAddCoil('OTU')} title="Add unlatch coil (clear on rising power)">
    OTU
  </button>

  <span class="spacer"></span>

  <button
    type="button"
    class="danger"
    onclick={onDelete}
    disabled={!canDelete}
    title="Delete selected (Del / Backspace)"
  >
    Delete
  </button>
</div>

<style lang="scss">
  .toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    background: var(--theme-surface, #181818);
    border-bottom: 1px solid var(--theme-border, #333);
  }

  button {
    background: var(--theme-background, #111);
    border: 1px solid var(--theme-border, #333);
    color: var(--theme-text, #ddd);
    padding: 4px 10px;
    border-radius: 4px;
    font-family: var(--theme-font-basic, ui-monospace, monospace);
    font-size: 12px;
    cursor: pointer;

    &:hover:not(:disabled) {
      border-color: var(--theme-primary, #3b82f6);
    }

    &:disabled {
      opacity: 0.4;
      cursor: not-allowed;
    }

    &.danger:hover:not(:disabled) {
      border-color: var(--red-500, #ef4444);
      color: var(--red-500, #ef4444);
    }
  }

  .sep {
    width: 1px;
    height: 18px;
    background: var(--theme-border, #333);
    margin: 0 2px;
  }

  .spacer {
    flex: 1;
  }
</style>
