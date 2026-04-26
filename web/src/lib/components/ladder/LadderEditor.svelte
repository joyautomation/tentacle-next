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
  import { printRung } from './printer.js';
  import { apiPost } from '$lib/api/client';

  interface Props {
    diagram: Diagram;
    tagValues?: TagValues;
    monitoring?: boolean;
    /** Names available for operand autocomplete + drag-drop targets. */
    variableNames?: string[];
    /** PLC ID for the parse-rung endpoint. Defaults to "plc" (single-PLC builds). */
    plcId?: string;
    onChange?: (next: Diagram) => void;
  }

  let {
    diagram,
    tagValues = {},
    monitoring = false,
    variableNames = [],
    plcId = 'plc',
    onChange,
  }: Props = $props();

  type ParseRungResp = {
    rung?: Rung;
    diagnostics: { severity: string; message: string; line: number; col: number }[];
  };

  // Per-rung text editor state. When non-null the panel is visible and
  // covers the inspector; the rung index is fixed at open time so the
  // user always edits the rung they double-clicked, even if selection
  // changes underneath.
  let editingRungIdx = $state<number | null>(null);
  let editingText = $state('');
  let editingError = $state<string | null>(null);
  let editingBusy = $state(false);

  // Stable id so the inspector input can reference its own <datalist>
  // even if multiple LadderEditor instances mount on the page.
  const operandListId = `lad-operand-list-${Math.random().toString(36).slice(2, 8)}`;
  let lastBlockMessage = $state<string | null>(null);
  let blockMessageTimer: number | null = null;
  function flashBlocked(msg: string) {
    lastBlockMessage = msg;
    if (blockMessageTimer !== null) window.clearTimeout(blockMessageTimer);
    blockMessageTimer = window.setTimeout(() => (lastBlockMessage = null), 2400);
  }

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
    // Illegal-state guard: a rung with no output is not useful, so block
    // deleting the last output instead of letting the diagram reach that
    // state. The user can always replace it instead.
    if (selection.kind === 'output') {
      const rung = diagram.rungs[selection.rung];
      if (rung && (rung.outputs?.length ?? 0) <= 1) {
        flashBlocked("Can't delete the rung's only output. Add another first, then remove this one.");
        return;
      }
    }
    const next = deleteAtPath(diagram, selection);
    selection = null;
    emit(next);
  }

  function handleVariableDrop(path: EditPath, varName: string) {
    if (!varName) return;
    emit(setOperand(diagram, path, varName));
    selection = path;
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

  function openRungTextEditor() {
    if (!selection) return;
    const idx = selection.rung;
    if (idx < 0 || idx >= diagram.rungs.length) return;
    editingRungIdx = idx;
    editingText = printRung(diagram.rungs[idx]);
    editingError = null;
  }

  function closeRungTextEditor() {
    editingRungIdx = null;
    editingError = null;
    editingBusy = false;
  }

  async function applyRungTextEdit() {
    if (editingRungIdx === null || editingBusy) return;
    editingBusy = true;
    editingError = null;
    const idx = editingRungIdx;
    const result = await apiPost<ParseRungResp>(
      `/plcs/${encodeURIComponent(plcId)}/lad/parse-rung`,
      { source: editingText },
    );
    editingBusy = false;
    if (result.error) {
      editingError = result.error.error;
      return;
    }
    const data = result.data;
    if (!data || !data.rung) {
      const diag = data?.diagnostics?.[0];
      editingError = diag
        ? `line ${diag.line}: ${diag.message}`
        : 'Failed to parse rung';
      return;
    }
    const next: Diagram = {
      ...diagram,
      rungs: diagram.rungs.map((r, i) => (i === idx ? data.rung! : r)),
    };
    closeRungTextEditor();
    emit(next);
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
    onEditRungText={openRungTextEditor}
  />

  <div class="ladder-body">
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="ladder-canvas" onclick={handleDeselect}>
      <svg
        width={Math.max(containerWidth, programLayout.totalWidth + 40)}
        height={Math.max(160, programLayout.totalHeight + 40)}
        viewBox={`0 0 ${Math.max(containerWidth, programLayout.totalWidth + 40)} ${Math.max(160, programLayout.totalHeight + 40)}`}
      >
        <!-- Power rails span the full diagram height. -->
        <line
          class="rail"
          x1={programLayout.rails.leftX}
          y1={programLayout.rails.topY}
          x2={programLayout.rails.leftX}
          y2={programLayout.rails.bottomY}
        />
        <line
          class="rail"
          x1={programLayout.rails.rightX}
          y1={programLayout.rails.topY}
          x2={programLayout.rails.rightX}
          y2={programLayout.rails.bottomY}
        />

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
              onVariableDrop={handleVariableDrop}
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

      {#if lastBlockMessage}
        <div class="block-toast" role="status" aria-live="polite">
          {lastBlockMessage}
        </div>
      {/if}
    </div>

    {#if editingRungIdx !== null}
      <aside class="inspector rung-text-editor">
        <h4>Rung {editingRungIdx + 1} — Text</h4>
        <textarea
          bind:value={editingText}
          spellcheck="false"
          rows={6}
          aria-label="Rung text DSL"
          placeholder="rung NO(start) -> OTE(motor)"
        ></textarea>
        {#if editingError}
          <p class="error">{editingError}</p>
        {/if}
        <div class="rung-text-actions">
          <button type="button" onclick={closeRungTextEditor} disabled={editingBusy}>
            Cancel
          </button>
          <button type="button" class="primary" onclick={applyRungTextEdit} disabled={editingBusy}>
            {editingBusy ? 'Parsing…' : 'Apply'}
          </button>
        </div>
        <p class="muted">
          Use <code>&amp;</code> for series, <code>|</code> for parallel,
          <code>NO(x)</code>/<code>NC(x)</code> for contacts, <code>OTE/OTL/OTU(y)</code> for coils.
        </p>
      </aside>
    {:else if selection && selectedElement}
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
              list={operandListId}
              autocomplete="off"
            />
            {#if variableNames.length > 0}
              <datalist id={operandListId}>
                {#each variableNames as v}
                  <option value={v}></option>
                {/each}
              </datalist>
            {/if}
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
    position: relative;

    svg {
      display: block;
    }

    :global(.rail) {
      stroke: var(--theme-text, #ddd);
      stroke-width: 2.5;
      stroke-linecap: square;
    }
  }

  .block-toast {
    position: absolute;
    bottom: 12px;
    left: 50%;
    transform: translateX(-50%);
    background: var(--theme-surface, #181818);
    color: var(--theme-text, #ddd);
    border: 1px solid var(--theme-warning, #d97706);
    border-radius: 4px;
    padding: 6px 12px;
    font-size: 12px;
    font-family: var(--theme-font-basic, sans-serif);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
    pointer-events: none;
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

  .rung-text-editor {
    width: 320px;

    textarea {
      background: var(--theme-background, #111);
      border: 1px solid var(--theme-border, #333);
      color: var(--theme-text, #ddd);
      padding: 8px;
      border-radius: 4px;
      font-family: var(--theme-font-mono, ui-monospace, monospace);
      font-size: 12px;
      line-height: 1.5;
      resize: vertical;

      &:focus {
        outline: none;
        border-color: var(--theme-primary, #3b82f6);
      }
    }

    .error {
      color: var(--theme-warning, #d97706);
      font-size: 11px;
      margin: 0;
      white-space: pre-wrap;
    }

    .rung-text-actions {
      display: flex;
      justify-content: flex-end;
      gap: 6px;

      button {
        background: var(--theme-background, #111);
        border: 1px solid var(--theme-border, #333);
        color: var(--theme-text, #ddd);
        padding: 4px 12px;
        border-radius: 4px;
        font-size: 12px;
        font-family: var(--theme-font-basic, sans-serif);
        cursor: pointer;

        &:hover:not(:disabled) {
          border-color: var(--theme-primary, #3b82f6);
        }

        &:disabled {
          opacity: 0.5;
          cursor: not-allowed;
        }

        &.primary {
          border-color: var(--theme-primary, #3b82f6);
          color: var(--theme-primary, #3b82f6);
        }
      }
    }

    code {
      font-family: var(--theme-font-mono, ui-monospace, monospace);
      background: var(--theme-background, #111);
      padding: 0 4px;
      border-radius: 2px;
    }
  }
</style>
