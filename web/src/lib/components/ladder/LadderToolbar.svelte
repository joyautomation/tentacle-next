<script lang="ts">
  import type { Selection } from './types.js';

  interface Props {
    selection: Selection;
    monitoring: boolean;
    onAddContact: (type: 'NO' | 'NC') => void;
    onAddCoil: (type: 'OTE' | 'OTL' | 'OTU') => void;
    onAddBranch: () => void;
    onAddTimer: (type: 'TON' | 'TOF') => void;
    onAddRung: () => void;
    onDelete: () => void;
  }

  let {
    selection, monitoring,
    onAddContact, onAddCoil, onAddBranch, onAddTimer, onAddRung, onDelete,
  }: Props = $props();

  let showOutputMenu = $state(false);
  let showTimerMenu = $state(false);
</script>

<div class="toolbar">
  <div class="toolbar-group">
    <span class="toolbar-label">Rung</span>
    <button class="toolbar-btn" onclick={onAddRung} title="Add new rung">
      + Rung
    </button>
  </div>

  <div class="toolbar-separator"></div>

  <div class="toolbar-group">
    <span class="toolbar-label">Contacts</span>
    <button class="toolbar-btn" onclick={() => onAddContact('NO')} title="Normally Open contact">
      <svg width="24" height="16" viewBox="0 0 24 16">
        <line x1="0" y1="8" x2="7" y2="8" stroke="currentColor" stroke-width="1.5"/>
        <line x1="17" y1="8" x2="24" y2="8" stroke="currentColor" stroke-width="1.5"/>
        <line x1="7" y1="2" x2="7" y2="14" stroke="currentColor" stroke-width="2"/>
        <line x1="17" y1="2" x2="17" y2="14" stroke="currentColor" stroke-width="2"/>
      </svg>
      NO
    </button>
    <button class="toolbar-btn" onclick={() => onAddContact('NC')} title="Normally Closed contact">
      <svg width="24" height="16" viewBox="0 0 24 16">
        <line x1="0" y1="8" x2="7" y2="8" stroke="currentColor" stroke-width="1.5"/>
        <line x1="17" y1="8" x2="24" y2="8" stroke="currentColor" stroke-width="1.5"/>
        <line x1="7" y1="2" x2="7" y2="14" stroke="currentColor" stroke-width="2"/>
        <line x1="17" y1="2" x2="17" y2="14" stroke="currentColor" stroke-width="2"/>
        <line x1="8" y1="13" x2="16" y2="3" stroke="currentColor" stroke-width="1.5"/>
      </svg>
      NC
    </button>
  </div>

  <div class="toolbar-separator"></div>

  <div class="toolbar-group">
    <span class="toolbar-label">Outputs</span>
    <button class="toolbar-btn" onclick={() => onAddCoil('OTE')} title="Output Energize">
      <svg width="24" height="16" viewBox="0 0 24 16">
        <line x1="0" y1="8" x2="6" y2="8" stroke="currentColor" stroke-width="1.5"/>
        <line x1="18" y1="8" x2="24" y2="8" stroke="currentColor" stroke-width="1.5"/>
        <path d="M 9 2 C 5 2, 5 14, 9 14" fill="none" stroke="currentColor" stroke-width="2"/>
        <path d="M 15 2 C 19 2, 19 14, 15 14" fill="none" stroke="currentColor" stroke-width="2"/>
      </svg>
      OTE
    </button>
    <div class="dropdown-wrapper">
      <button class="toolbar-btn small" onclick={() => showOutputMenu = !showOutputMenu} title="More outputs">
        ...
      </button>
      {#if showOutputMenu}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="dropdown" onclick={() => showOutputMenu = false}>
          <button class="dropdown-item" onclick={() => onAddCoil('OTL')}>OTL (Latch)</button>
          <button class="dropdown-item" onclick={() => onAddCoil('OTU')}>OTU (Unlatch)</button>
        </div>
      {/if}
    </div>
  </div>

  <div class="toolbar-separator"></div>

  <div class="toolbar-group">
    <span class="toolbar-label">Timers</span>
    <button class="toolbar-btn" onclick={() => onAddTimer('TON')} title="Timer On Delay">
      TON
    </button>
    <button class="toolbar-btn" onclick={() => onAddTimer('TOF')} title="Timer Off Delay">
      TOF
    </button>
  </div>

  <div class="toolbar-separator"></div>

  <div class="toolbar-group">
    <span class="toolbar-label">Logic</span>
    <button
      class="toolbar-btn"
      onclick={onAddBranch}
      disabled={!selection || selection.type !== 'condition'}
      title="Add parallel branch (select a contact first)"
    >
      Branch
    </button>
  </div>

  <div class="toolbar-separator"></div>

  <div class="toolbar-group">
    <button
      class="toolbar-btn danger"
      onclick={onDelete}
      disabled={!selection}
      title="Delete selected element (Del)"
    >
      Delete
    </button>
  </div>

  {#if monitoring}
    <div class="toolbar-status">
      <span class="monitoring-badge">MONITORING</span>
    </div>
  {/if}
</div>

<style lang="scss">
  .toolbar {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 10px;
    background: var(--theme-surface, #1a1a1a);
    border-bottom: 1px solid var(--theme-border, #333);
    flex-wrap: wrap;
  }

  .toolbar-group {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .toolbar-label {
    font-size: 10px;
    color: var(--theme-text-muted, #666);
    font-family: var(--theme-font-basic, sans-serif);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-right: 2px;
  }

  .toolbar-separator {
    width: 1px;
    height: 24px;
    background: var(--theme-border, #333);
    margin: 0 4px;
  }

  .toolbar-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px;
    background: var(--theme-neutral-200, #333);
    border: 1px solid var(--theme-border, #444);
    border-radius: 4px;
    color: var(--theme-text, #ccc);
    font-size: 11px;
    font-family: monospace;
    cursor: pointer;
    white-space: nowrap;

    &:hover:not(:disabled) {
      background: var(--theme-neutral-300, #444);
      border-color: var(--theme-primary, #0ea5e9);
    }

    &:disabled {
      opacity: 0.4;
      cursor: not-allowed;
    }

    &.danger:hover:not(:disabled) {
      background: var(--theme-error-200, #4a1515);
      border-color: var(--color-red-500, #ef4444);
      color: var(--color-red-400, #f87171);
    }

    &.small {
      padding: 4px 6px;
    }

    svg {
      display: block;
    }
  }

  .dropdown-wrapper {
    position: relative;
  }

  .dropdown {
    position: absolute;
    top: 100%;
    left: 0;
    z-index: 10;
    background: var(--theme-surface, #1a1a1a);
    border: 1px solid var(--theme-border, #333);
    border-radius: 4px;
    padding: 4px;
    margin-top: 2px;
    min-width: 140px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
  }

  .dropdown-item {
    display: block;
    width: 100%;
    padding: 6px 10px;
    background: none;
    border: none;
    color: var(--theme-text, #ccc);
    font-size: 11px;
    font-family: monospace;
    text-align: left;
    cursor: pointer;
    border-radius: 2px;

    &:hover {
      background: var(--theme-neutral-200, #333);
    }
  }

  .toolbar-status {
    margin-left: auto;
  }

  .monitoring-badge {
    background: var(--badge-green-bg);
    color: var(--badge-green-text);
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 10px;
    font-family: monospace;
    font-weight: bold;
    letter-spacing: 1px;
  }
</style>
