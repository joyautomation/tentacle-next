<script lang="ts">
  import { onMount } from 'svelte';
  import type { LadderProgram, Rung, Selection, TagValues, LadderCondition, LadderOutput, LadderElement } from './types.js';
  import { layoutProgram, layoutRung } from './layout.js';
  import LadderRung from './LadderRung.svelte';
  import LadderToolbar from './LadderToolbar.svelte';
  import { LAYOUT } from './types.js';

  interface Props {
    program: LadderProgram;
    tagValues?: TagValues;
    monitoring?: boolean;
    onProgramChange?: (program: LadderProgram) => void;
  }

  let { program, tagValues = {}, monitoring = false, onProgramChange }: Props = $props();

  let selection = $state<Selection>(null);
  let containerEl: HTMLDivElement | undefined = $state();
  let containerWidth = $state(800);

  const programLayout = $derived(layoutProgram(program.rungs));

  // Resize observer
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

  function updateProgram(updatedRungs: Rung[]) {
    const updated = { ...program, rungs: updatedRungs };
    onProgramChange?.(updated);
  }

  function handleSelect(rungId: string, path: number[], type: 'condition' | 'output') {
    selection = { rungId, path, type };
  }

  function handleDeselect() {
    selection = null;
  }

  function addRung() {
    const newRung: Rung = {
      id: `rung_${Date.now()}`,
      comment: '',
      conditions: [],
      outputs: [],
    };
    updateProgram([...program.rungs, newRung]);
  }

  function deleteRung(rungId: string) {
    updateProgram(program.rungs.filter(r => r.id !== rungId));
    if (selection?.rungId === rungId) selection = null;
  }

  function addContact(type: 'NO' | 'NC') {
    if (!selection) {
      // No selection — add to last rung, or create one
      if (program.rungs.length === 0) addRung();
      const lastRung = program.rungs[program.rungs.length - 1];
      const updated = program.rungs.map(r =>
        r.id === lastRung.id
          ? { ...r, conditions: [...r.conditions, { type, tag: 'newTag' } as LadderCondition] }
          : r
      );
      updateProgram(updated);
      return;
    }

    const updated = program.rungs.map(r => {
      if (r.id !== selection!.rungId) return r;
      // Insert contact after selection position
      const newConditions = [...r.conditions];
      const insertIdx = selection!.type === 'condition'
        ? Math.min(selection!.path[0] + 1, newConditions.length)
        : newConditions.length;
      newConditions.splice(insertIdx, 0, { type, tag: 'newTag' });
      return { ...r, conditions: newConditions };
    });
    updateProgram(updated);
  }

  function addCoil(type: 'OTE' | 'OTL' | 'OTU') {
    if (!selection) {
      if (program.rungs.length === 0) addRung();
      const lastRung = program.rungs[program.rungs.length - 1];
      const updated = program.rungs.map(r =>
        r.id === lastRung.id
          ? { ...r, outputs: [...r.outputs, { type, tag: 'newTag' } as LadderOutput] }
          : r
      );
      updateProgram(updated);
      return;
    }

    const updated = program.rungs.map(r => {
      if (r.id !== selection!.rungId) return r;
      return { ...r, outputs: [...r.outputs, { type, tag: 'newTag' }] };
    });
    updateProgram(updated);
  }

  function addBranch() {
    if (!selection || selection.type !== 'condition') return;

    const updated = program.rungs.map(r => {
      if (r.id !== selection!.rungId) return r;
      const idx = selection!.path[0];
      if (idx < 0 || idx >= r.conditions.length) return r;

      // Wrap the selected condition in a branch with an empty parallel path
      const selected = r.conditions[idx];
      const branch: LadderCondition = {
        type: 'branch',
        paths: [selected, { type: 'NO', tag: 'newTag' }],
      };
      const newConditions = [...r.conditions];
      newConditions[idx] = branch;
      return { ...r, conditions: newConditions };
    });
    updateProgram(updated);
  }

  function addTimer(type: 'TON' | 'TOF') {
    const target = selection?.rungId ?? program.rungs[program.rungs.length - 1]?.id;
    if (!target) return;

    const updated = program.rungs.map(r => {
      if (r.id !== target) return r;
      return { ...r, outputs: [...r.outputs, { type, tag: 'newTimer', preset: 1000 }] };
    });
    updateProgram(updated);
  }

  function deleteSelected() {
    if (!selection) return;

    const updated = program.rungs.map(r => {
      if (r.id !== selection!.rungId) return r;
      if (selection!.type === 'condition') {
        const newConditions = [...r.conditions];
        newConditions.splice(selection!.path[0], 1);
        return { ...r, conditions: newConditions };
      } else {
        const newOutputs = [...r.outputs];
        newOutputs.splice(selection!.path[0], 1);
        return { ...r, outputs: newOutputs };
      }
    });
    updateProgram(updated);
    selection = null;
  }

  function handleTagEdit(rungId: string, path: number[], type: 'condition' | 'output', newTag: string) {
    const updated = program.rungs.map(r => {
      if (r.id !== rungId) return r;
      if (type === 'condition') {
        const newConditions = structuredClone(r.conditions);
        const el = newConditions[path[0]];
        if (el && (el.type === 'NO' || el.type === 'NC')) {
          el.tag = newTag;
        }
        return { ...r, conditions: newConditions };
      } else {
        const newOutputs = structuredClone(r.outputs);
        const el = newOutputs[path[0]];
        if (el && 'tag' in el) {
          el.tag = newTag;
        }
        return { ...r, outputs: newOutputs };
      }
    });
    updateProgram(updated);
  }

  function handleCommentEdit(rungId: string, comment: string) {
    const updated = program.rungs.map(r =>
      r.id === rungId ? { ...r, comment } : r
    );
    updateProgram(updated);
  }

  // Keyboard shortcuts
  function handleKeydown(e: KeyboardEvent) {
    if (e.target instanceof HTMLInputElement) return;

    switch (e.key) {
      case 'Delete':
      case 'Backspace':
        deleteSelected();
        e.preventDefault();
        break;
      case 'Escape':
        handleDeselect();
        break;
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="ladder-editor" bind:this={containerEl}>
  <LadderToolbar
    {selection}
    {monitoring}
    onAddContact={addContact}
    onAddCoil={addCoil}
    onAddBranch={addBranch}
    onAddTimer={addTimer}
    onAddRung={addRung}
    onDelete={deleteSelected}
  />

  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="ladder-canvas" onclick={handleDeselect}>
    <svg
      width={containerWidth}
      height={Math.max(200, programLayout.totalHeight + 40)}
      viewBox="0 0 {Math.max(containerWidth, programLayout.totalWidth + 40)} {Math.max(200, programLayout.totalHeight + 40)}"
    >
      {#each program.rungs as rung, rungIdx}
        {@const layout = programLayout.layouts[rungIdx]}
        {@const yOffset = programLayout.layouts.slice(0, rungIdx).reduce((sum, l) => sum + l.totalHeight + LAYOUT.RUNG_GAP, 0)}
        <g transform="translate(0, {yOffset})">
          <LadderRung
            {rung}
            {layout}
            {selection}
            {tagValues}
            {monitoring}
            rungNumber={rungIdx}
            onSelect={handleSelect}
            onTagEdit={handleTagEdit}
            onCommentEdit={handleCommentEdit}
            onDeleteRung={() => deleteRung(rung.id)}
          />
        </g>
      {/each}

      {#if program.rungs.length === 0}
        <text
          x={containerWidth / 2}
          y={100}
          text-anchor="middle"
          fill="var(--theme-text-muted, #888)"
          font-size="14"
          font-family="var(--theme-font-basic, sans-serif)"
        >
          Click "Add Rung" to get started
        </text>
      {/if}
    </svg>
  </div>
</div>

<style lang="scss">
  .ladder-editor {
    display: flex;
    flex-direction: column;
    gap: 0;
    background: var(--theme-background, #111);
    border: 1px solid var(--theme-border, #333);
    border-radius: var(--rounded-lg, 8px);
    overflow: hidden;
  }

  .ladder-canvas {
    overflow: auto;
    padding: 8px;
    min-height: 200px;

    svg {
      display: block;
    }
  }
</style>
