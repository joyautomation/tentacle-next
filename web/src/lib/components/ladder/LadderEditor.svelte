<script lang="ts">
  import { onMount } from 'svelte';
  import LadderRung from './LadderRung.svelte';
  import LadderToolbar from './LadderToolbar.svelte';
  import {
    type Diagram,
    type EditPath,
    type Element,
    type Rung,
    type Selection,
    type TagValues,
    LAYOUT,
    newCoil,
    newContact,
    newRung,
  } from './types.js';
  import { layoutDiagram } from './layout.js';
  import {
    deleteAtPath,
    getElementAt,
    getOutputAt,
    setOperand,
    setForm,
    wrapInParallel,
    appendContactInSeries,
  } from './mutations.js';

  interface Props {
    diagram: Diagram;
    tagValues?: TagValues;
    monitoring?: boolean;
    onChange?: (next: Diagram) => void;
  }

  let { diagram, tagValues = {}, monitoring = false, onChange }: Props = $props();

  let selection = $state<Selection>(null);
  let containerEl: HTMLDivElement | undefined = $state();
  let containerWidth = $state(800);

  const programLayout = $derived(layoutDiagram(diagram));

  onMount(() => {
    if (!containerEl) return;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        containerWidth = entry.contentRect.width;
      }
    });
    observer.observe(containerEl);
    return () => observer.disconnect();
  });

  function emit(next: Diagram) {
    onChange?.(next);
  }

  function handleSelect(path: EditPath) {
    selection = path;
  }

  function handleDeselect() {
    selection = null;
  }

  function addRung() {
    const next: Diagram = {
      ...diagram,
      rungs: [...diagram.rungs, newRung()],
    };
    selection = { kind: 'logic', rung: next.rungs.length - 1, logic: [] };
    emit(next);
  }

  function deleteSelected() {
    if (!selection) return;
    const next = deleteAtPath(diagram, selection);
    selection = null;
    emit(next);
  }

  function addContactInSeries(form: 'NO' | 'NC') {
    const target = selection ?? defaultSelection();
    if (!target || target.kind !== 'logic') return;
    const next = appendContactInSeries(diagram, target, newContact(form, ''));
    emit(next);
  }

  function addCoil(form: 'OTE' | 'OTL' | 'OTU') {
    const rungIdx = selection?.rung ?? (diagram.rungs.length - 1);
    if (rungIdx < 0 || rungIdx >= diagram.rungs.length) return;
    const rungs = diagram.rungs.map((r, i) =>
      i === rungIdx
        ? { ...r, outputs: [...(r.outputs ?? []), newCoil(form, '')] }
        : r,
    );
    const next: Diagram = { ...diagram, rungs };
    selection = {
      kind: 'output',
      rung: rungIdx,
      output: (next.rungs[rungIdx].outputs?.length ?? 1) - 1,
    };
    emit(next);
  }

  function wrapSelectionInParallel() {
    if (!selection || selection.kind !== 'logic') return;
    const next = wrapInParallel(diagram, selection);
    emit(next);
  }

  function defaultSelection(): EditPath | null {
    if (diagram.rungs.length === 0) return null;
    return { kind: 'logic', rung: diagram.rungs.length - 1, logic: [] };
  }

  // Keyboard shortcuts: Delete/Backspace removes the selected node;
  // Escape clears selection. Inputs are excluded so typing in the
  // operand field doesn't fire shortcuts.
  function handleKeydown(e: KeyboardEvent) {
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) {
      return;
    }
    if (e.key === 'Delete' || e.key === 'Backspace') {
      if (selection) {
        deleteSelected();
        e.preventDefault();
      }
    } else if (e.key === 'Escape') {
      handleDeselect();
    }
  }

  // Inspector-style operand editor for the currently selected node.
  // Reads the live element from the diagram so external updates stay
  // in sync.
  const selectedElement = $derived.by(() => {
    if (!selection) return null;
    if (selection.kind === 'logic') {
      const el = getElementAt(diagram, selection);
      return el && el.kind === 'contact' ? el : null;
    }
    const out = getOutputAt(diagram, selection);
    return out;
  });

  function commitOperand(value: string) {
    if (!selection) return;
    emit(setOperand(diagram, selection, value));
  }

  function commitForm(value: string) {
    if (!selection) return;
    emit(setForm(diagram, selection, value));
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="ladder-editor" bind:this={containerEl}>
  <LadderToolbar
    {selection}
    onAddRung={addRung}
    onAddContact={addContactInSeries}
    onAddCoil={addCoil}
    onWrapParallel={wrapSelectionInParallel}
    onDelete={deleteSelected}
  />

  <div class="ladder-body">
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="ladder-canvas" onclick={handleDeselect}>
      <svg
        width={Math.max(containerWidth, programLayout.totalWidth + 40)}
        height={Math.max(160, programLayout.totalHeight + 40)}
        viewBox={`0 0 ${Math.max(containerWidth, programLayout.totalWidth + 40)} ${Math.max(160, programLayout.totalHeight + 40)}`}
      >
        {#each programLayout.rungs as r, idx}
          <g transform={`translate(0, ${r.yOffset})`}>
            <LadderRung
              rung={diagram.rungs[idx]}
              rungIndex={idx}
              layout={r.layout}
              {selection}
              {tagValues}
              {monitoring}
              onSelect={handleSelect}
            />
          </g>
        {/each}

        {#if diagram.rungs.length === 0}
          <text
            x={containerWidth / 2}
            y={80}
            text-anchor="middle"
            class="empty-hint"
          >
            Click "Add Rung" to start
          </text>
        {/if}
      </svg>
    </div>

    {#if selection && selectedElement}
      <aside class="inspector">
        <h4>Selection</h4>
        {#if selectedElement.kind === 'contact' || selectedElement.kind === 'coil'}
          <label>
            <span>Form</span>
            <select
              value={selectedElement.form}
              onchange={(e) => commitForm((e.target as HTMLSelectElement).value)}
            >
              {#if selectedElement.kind === 'contact'}
                <option value="NO">NO</option>
                <option value="NC">NC</option>
              {:else}
                <option value="OTE">OTE</option>
                <option value="OTL">OTL</option>
                <option value="OTU">OTU</option>
              {/if}
            </select>
          </label>
          <label>
            <span>Operand</span>
            <input
              type="text"
              value={selectedElement.operand}
              oninput={(e) => commitOperand((e.target as HTMLInputElement).value)}
              placeholder="variable name"
            />
          </label>
        {:else if selectedElement.kind === 'fb'}
          <label>
            <span>Instance</span>
            <input type="text" value={selectedElement.instance} disabled />
          </label>
          <p class="muted">FB inputs aren't editable in v1 — edit the source directly for now.</p>
        {/if}
      </aside>
    {/if}
  </div>
</div>

<style lang="scss">
  .ladder-editor {
    display: flex;
    flex-direction: column;
    background: var(--theme-background, #111);
    border: 1px solid var(--theme-border, #333);
    border-radius: var(--rounded-lg, 8px);
    overflow: hidden;
    height: 100%;
    min-height: 240px;
  }

  .ladder-body {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .ladder-canvas {
    flex: 1;
    overflow: auto;
    padding: 8px;

    svg {
      display: block;
    }
  }

  .empty-hint {
    fill: var(--theme-text-muted, #888);
    font-size: 13px;
    font-family: var(--theme-font-basic, sans-serif);
  }

  .inspector {
    width: 240px;
    border-left: 1px solid var(--theme-border, #333);
    padding: 12px;
    background: var(--theme-surface, #181818);
    display: flex;
    flex-direction: column;
    gap: 12px;

    h4 {
      margin: 0;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      color: var(--theme-text-muted, #888);
    }

    label {
      display: flex;
      flex-direction: column;
      gap: 4px;
      font-size: 12px;
      color: var(--theme-text, #ddd);

      span {
        color: var(--theme-text-muted, #888);
      }
    }

    input,
    select {
      background: var(--theme-background, #111);
      border: 1px solid var(--theme-border, #333);
      color: var(--theme-text, #ddd);
      padding: 6px 8px;
      border-radius: 4px;
      font-family: var(--theme-font-basic, ui-monospace, monospace);
      font-size: 12px;

      &:focus {
        outline: none;
        border-color: var(--theme-primary, #3b82f6);
      }
    }

    .muted {
      color: var(--theme-text-muted, #888);
      font-size: 11px;
      margin: 0;
    }
  }
</style>
