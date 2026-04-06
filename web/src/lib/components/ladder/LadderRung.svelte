<script lang="ts">
  import type { Rung, RungLayout, Selection, TagValues, LayoutElement } from './types.js';
  import { LAYOUT } from './types.js';

  interface Props {
    rung: Rung;
    layout: RungLayout;
    selection: Selection;
    tagValues: TagValues;
    monitoring: boolean;
    rungNumber: number;
    onSelect: (rungId: string, path: number[], type: 'condition' | 'output') => void;
    onTagEdit: (rungId: string, path: number[], type: 'condition' | 'output', newTag: string) => void;
    onCommentEdit: (rungId: string, comment: string) => void;
    onDeleteRung: () => void;
  }

  let {
    rung, layout, selection, tagValues, monitoring, rungNumber,
    onSelect, onTagEdit, onCommentEdit, onDeleteRung,
  }: Props = $props();

  let editingTag = $state<string | null>(null);
  let editingComment = $state(false);
  let editValue = $state('');

  function isSelected(el: LayoutElement, idx: number): boolean {
    if (!selection || selection.rungId !== rung.id) return false;
    return selection.path[0] === idx;
  }

  function getElementType(el: LayoutElement): 'condition' | 'output' {
    const t = el.element.type;
    if (t === 'NO' || t === 'NC' || t === 'series' || t === 'branch') return 'condition';
    return 'output';
  }

  function isEnergized(tag: string): boolean {
    if (!monitoring) return false;
    const tv = tagValues[tag];
    return tv ? Boolean(tv.value) : false;
  }

  function getTagValue(tag: string): string {
    const tv = tagValues[tag];
    if (!tv) return '';
    if (typeof tv.value === 'boolean') return tv.value ? '1' : '0';
    if (typeof tv.value === 'number') return tv.value.toFixed(1);
    return String(tv.value);
  }

  function wireColor(energized: boolean): string {
    return energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text-muted, #666)';
  }

  function handleElementClick(e: MouseEvent, elId: string, idx: number, type: 'condition' | 'output') {
    e.stopPropagation();
    onSelect(rung.id, [idx], type);
  }

  function startTagEdit(e: MouseEvent, elId: string, currentTag: string) {
    e.stopPropagation();
    editingTag = elId;
    editValue = currentTag;
  }

  function commitTagEdit(elId: string, idx: number, type: 'condition' | 'output') {
    if (editValue.trim()) {
      onTagEdit(rung.id, [idx], type, editValue.trim());
    }
    editingTag = null;
  }

  function startCommentEdit(e: MouseEvent) {
    e.stopPropagation();
    editingComment = true;
    editValue = rung.comment;
  }

  function commitCommentEdit() {
    onCommentEdit(rung.id, editValue);
    editingComment = false;
  }

  // Find the condition/output index for an element
  function findElementIndex(el: LayoutElement): { idx: number; type: 'condition' | 'output' } {
    const t = el.element.type;
    if (t === 'NO' || t === 'NC' || t === 'series' || t === 'branch') {
      const idx = rung.conditions.indexOf(el.element as any);
      return { idx: Math.max(0, idx), type: 'condition' };
    }
    const idx = rung.outputs.indexOf(el.element as any);
    return { idx: Math.max(0, idx), type: 'output' };
  }
</script>

<g class="rung">
  <!-- Rung number -->
  <text
    x="4"
    y="10"
    fill="var(--theme-text-muted, #666)"
    font-size="10"
    font-family="var(--theme-font-basic, sans-serif)"
  >
    {rungNumber}
  </text>

  <!-- Comment -->
  {#if rung.comment || editingComment}
    {#if editingComment}
      <foreignObject x={LAYOUT.RAIL_LEFT} y="-2" width="400" height="18">
        <input
          type="text"
          class="comment-input"
          bind:value={editValue}
          onblur={() => commitCommentEdit()}
          onkeydown={(e) => { if (e.key === 'Enter') commitCommentEdit(); }}
          autofocus
        />
      </foreignObject>
    {:else}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <text
        x={LAYOUT.RAIL_LEFT}
        y="8"
        fill="var(--color-green-400, #4ade80)"
        font-size="10"
        font-family="monospace"
        font-style="italic"
        class="comment-text"
        ondblclick={startCommentEdit}
      >
        // {rung.comment}
      </text>
    {/if}
  {/if}

  <!-- Power rails -->
  <line
    x1="0" y1="0"
    x2="0" y2={layout.totalHeight}
    stroke="var(--theme-text, #ccc)"
    stroke-width="2"
  />
  <line
    x1={layout.totalWidth} y1="0"
    x2={layout.totalWidth} y2={layout.totalHeight}
    stroke="var(--theme-text, #ccc)"
    stroke-width="2"
  />

  <!-- Wires -->
  {#each layout.wires as wire}
    <line
      x1={wire.x1} y1={wire.y1}
      x2={wire.x2} y2={wire.y2}
      stroke="var(--theme-text-muted, #666)"
      stroke-width="1.5"
    />
  {/each}

  <!-- Branch lines -->
  {#each layout.branchLines as bl}
    <line
      x1={bl.x} y1={bl.y1}
      x2={bl.x} y2={bl.y2}
      stroke="var(--theme-text-muted, #666)"
      stroke-width="1.5"
    />
  {/each}

  <!-- Elements -->
  {#each layout.elements as layoutEl, i}
    {@const el = layoutEl.element}
    {@const elType = getElementType(layoutEl)}
    {@const selected = selection?.rungId === rung.id && selection?.path[0] === i && selection?.type === elType}
    {@const tag = 'tag' in el ? el.tag : ''}
    {@const energized = monitoring && tag ? isEnergized(tag) : false}

    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <g
      class="ladder-element"
      class:selected
      class:energized
      transform="translate({layoutEl.x}, {layoutEl.y})"
      onclick={(e) => handleElementClick(e, layoutEl.id, i, elType)}
    >
      {#if el.type === 'NO' || el.type === 'NC'}
        <!-- Contact symbol -->
        <rect
          x="0" y="0"
          width={layoutEl.width} height={layoutEl.height}
          fill="transparent"
          stroke={selected ? 'var(--theme-primary, #0ea5e9)' : 'transparent'}
          stroke-width="1"
          rx="2"
        />
        <!-- Contact lines -->
        <line x1="0" y1={layoutEl.height / 2} x2={layoutEl.width * 0.3} y2={layoutEl.height / 2}
          stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
          stroke-width="1.5" />
        <line x1={layoutEl.width * 0.7} y1={layoutEl.height / 2} x2={layoutEl.width} y2={layoutEl.height / 2}
          stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
          stroke-width="1.5" />
        <!-- Vertical bars -->
        <line x1={layoutEl.width * 0.3} y1={layoutEl.height * 0.2} x2={layoutEl.width * 0.3} y2={layoutEl.height * 0.8}
          stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
          stroke-width="2" />
        <line x1={layoutEl.width * 0.7} y1={layoutEl.height * 0.2} x2={layoutEl.width * 0.7} y2={layoutEl.height * 0.8}
          stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
          stroke-width="2" />
        {#if el.type === 'NC'}
          <!-- Diagonal slash for NC -->
          <line x1={layoutEl.width * 0.35} y1={layoutEl.height * 0.75} x2={layoutEl.width * 0.65} y2={layoutEl.height * 0.25}
            stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
            stroke-width="1.5" />
        {/if}

        <!-- Tag name -->
        {#if editingTag === layoutEl.id}
          <foreignObject x="5" y={layoutEl.height - 4} width={layoutEl.width - 10} height="18">
            <input
              type="text"
              class="tag-input"
              bind:value={editValue}
              onblur={() => commitTagEdit(layoutEl.id, i, elType)}
              onkeydown={(e) => { if (e.key === 'Enter') commitTagEdit(layoutEl.id, i, elType); }}
              autofocus
            />
          </foreignObject>
        {:else}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <text
            x={layoutEl.width / 2}
            y={-4}
            text-anchor="middle"
            fill="var(--theme-text, #ccc)"
            font-size={LAYOUT.TAG_FONT_SIZE}
            font-family="monospace"
            class="tag-label"
            ondblclick={(e) => startTagEdit(e, layoutEl.id, tag)}
          >
            {tag}
          </text>
        {/if}

        <!-- Type label -->
        <text
          x={layoutEl.width / 2}
          y={layoutEl.height + 12}
          text-anchor="middle"
          fill="var(--theme-text-muted, #666)"
          font-size={LAYOUT.LABEL_FONT_SIZE}
          font-family="monospace"
        >
          {el.type}
        </text>

        <!-- Monitoring value -->
        {#if monitoring && tag}
          <text
            x={layoutEl.width / 2}
            y={layoutEl.height + 22}
            text-anchor="middle"
            fill={energized ? 'var(--color-green-400, #4ade80)' : 'var(--color-red-400, #f87171)'}
            font-size="9"
            font-family="monospace"
            font-weight="bold"
          >
            {getTagValue(tag)}
          </text>
        {/if}

      {:else if el.type === 'OTE' || el.type === 'OTL' || el.type === 'OTU'}
        <!-- Coil symbol -->
        <rect
          x="0" y="0"
          width={layoutEl.width} height={layoutEl.height}
          fill="transparent"
          stroke={selected ? 'var(--theme-primary, #0ea5e9)' : 'transparent'}
          stroke-width="1"
          rx="2"
        />
        <!-- Coil wires -->
        <line x1="0" y1={layoutEl.height / 2} x2={layoutEl.width * 0.25} y2={layoutEl.height / 2}
          stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
          stroke-width="1.5" />
        <line x1={layoutEl.width * 0.75} y1={layoutEl.height / 2} x2={layoutEl.width} y2={layoutEl.height / 2}
          stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
          stroke-width="1.5" />
        <!-- Coil parentheses (arcs) -->
        <path
          d="M {layoutEl.width * 0.35} {layoutEl.height * 0.2} C {layoutEl.width * 0.2} {layoutEl.height * 0.2}, {layoutEl.width * 0.2} {layoutEl.height * 0.8}, {layoutEl.width * 0.35} {layoutEl.height * 0.8}"
          fill="none"
          stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
          stroke-width="2"
        />
        <path
          d="M {layoutEl.width * 0.65} {layoutEl.height * 0.2} C {layoutEl.width * 0.8} {layoutEl.height * 0.2}, {layoutEl.width * 0.8} {layoutEl.height * 0.8}, {layoutEl.width * 0.65} {layoutEl.height * 0.8}"
          fill="none"
          stroke={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
          stroke-width="2"
        />
        {#if el.type === 'OTL'}
          <!-- L for latch -->
          <text x={layoutEl.width / 2} y={layoutEl.height / 2 + 4} text-anchor="middle"
            fill={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
            font-size="12" font-family="monospace" font-weight="bold">L</text>
        {:else if el.type === 'OTU'}
          <!-- U for unlatch -->
          <text x={layoutEl.width / 2} y={layoutEl.height / 2 + 4} text-anchor="middle"
            fill={energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text, #ccc)'}
            font-size="12" font-family="monospace" font-weight="bold">U</text>
        {/if}

        <!-- Tag name -->
        {#if editingTag === layoutEl.id}
          <foreignObject x="5" y={layoutEl.height - 4} width={layoutEl.width - 10} height="18">
            <input
              type="text"
              class="tag-input"
              bind:value={editValue}
              onblur={() => commitTagEdit(layoutEl.id, i, elType)}
              onkeydown={(e) => { if (e.key === 'Enter') commitTagEdit(layoutEl.id, i, elType); }}
              autofocus
            />
          </foreignObject>
        {:else}
          <text
            x={layoutEl.width / 2}
            y={-4}
            text-anchor="middle"
            fill="var(--theme-text, #ccc)"
            font-size={LAYOUT.TAG_FONT_SIZE}
            font-family="monospace"
            class="tag-label"
            ondblclick={(e) => startTagEdit(e, layoutEl.id, tag)}
          >
            {tag}
          </text>
        {/if}

        <!-- Type label -->
        <text
          x={layoutEl.width / 2}
          y={layoutEl.height + 12}
          text-anchor="middle"
          fill="var(--theme-text-muted, #666)"
          font-size={LAYOUT.LABEL_FONT_SIZE}
          font-family="monospace"
        >
          {el.type}
        </text>

        <!-- Monitoring value -->
        {#if monitoring && tag}
          <text
            x={layoutEl.width / 2}
            y={layoutEl.height + 22}
            text-anchor="middle"
            fill={energized ? 'var(--color-green-400, #4ade80)' : 'var(--color-red-400, #f87171)'}
            font-size="9"
            font-family="monospace"
            font-weight="bold"
          >
            {getTagValue(tag)}
          </text>
        {/if}

      {:else if el.type === 'TON' || el.type === 'TOF'}
        <!-- Timer block -->
        <rect
          x="0" y="0"
          width={layoutEl.width} height={layoutEl.height}
          fill="var(--theme-surface, #1a1a1a)"
          stroke={selected ? 'var(--theme-primary, #0ea5e9)' : energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text-muted, #666)'}
          stroke-width={selected ? 2 : 1.5}
          rx="3"
        />
        <!-- Wires in/out -->
        <line x1={-LAYOUT.WIRE_GAP} y1={layoutEl.height / 2} x2="0" y2={layoutEl.height / 2}
          stroke="var(--theme-text-muted, #666)" stroke-width="1.5" />
        <line x1={layoutEl.width} y1={layoutEl.height / 2} x2={layoutEl.width + LAYOUT.WIRE_GAP} y2={layoutEl.height / 2}
          stroke="var(--theme-text-muted, #666)" stroke-width="1.5" />
        <!-- Timer type -->
        <text x={layoutEl.width / 2} y="16" text-anchor="middle"
          fill="var(--theme-text, #ccc)" font-size="12" font-family="monospace" font-weight="bold">
          {el.type}
        </text>
        <!-- Tag -->
        <text x={layoutEl.width / 2} y="32" text-anchor="middle"
          fill="var(--theme-text, #ccc)" font-size={LAYOUT.TAG_FONT_SIZE} font-family="monospace">
          {el.tag}
        </text>
        <!-- Preset -->
        <text x={layoutEl.width / 2} y="48" text-anchor="middle"
          fill="var(--theme-text-muted, #666)" font-size="9" font-family="monospace">
          PRE: {el.preset}ms
        </text>
        <!-- Monitoring: ACC and DN -->
        {#if monitoring}
          <text x={layoutEl.width / 2} y="62" text-anchor="middle"
            fill="var(--color-cyan-400, #22d3ee)" font-size="9" font-family="monospace">
            ACC: {getTagValue(`${el.tag}.ACC`)} | DN: {getTagValue(`${el.tag}.DN`)}
          </text>
        {/if}

      {:else if el.type === 'CTU' || el.type === 'CTD'}
        <!-- Counter block -->
        <rect
          x="0" y="0"
          width={layoutEl.width} height={layoutEl.height}
          fill="var(--theme-surface, #1a1a1a)"
          stroke={selected ? 'var(--theme-primary, #0ea5e9)' : energized ? 'var(--color-green-500, #22c55e)' : 'var(--theme-text-muted, #666)'}
          stroke-width={selected ? 2 : 1.5}
          rx="3"
        />
        <line x1={-LAYOUT.WIRE_GAP} y1={layoutEl.height / 2} x2="0" y2={layoutEl.height / 2}
          stroke="var(--theme-text-muted, #666)" stroke-width="1.5" />
        <line x1={layoutEl.width} y1={layoutEl.height / 2} x2={layoutEl.width + LAYOUT.WIRE_GAP} y2={layoutEl.height / 2}
          stroke="var(--theme-text-muted, #666)" stroke-width="1.5" />
        <text x={layoutEl.width / 2} y="16" text-anchor="middle"
          fill="var(--theme-text, #ccc)" font-size="12" font-family="monospace" font-weight="bold">
          {el.type}
        </text>
        <text x={layoutEl.width / 2} y="32" text-anchor="middle"
          fill="var(--theme-text, #ccc)" font-size={LAYOUT.TAG_FONT_SIZE} font-family="monospace">
          {el.tag}
        </text>
        <text x={layoutEl.width / 2} y="48" text-anchor="middle"
          fill="var(--theme-text-muted, #666)" font-size="9" font-family="monospace">
          PRE: {el.preset}
        </text>
        {#if monitoring}
          <text x={layoutEl.width / 2} y="62" text-anchor="middle"
            fill="var(--color-cyan-400, #22d3ee)" font-size="9" font-family="monospace">
            ACC: {getTagValue(`${el.tag}.ACC`)} | DN: {getTagValue(`${el.tag}.DN`)}
          </text>
        {/if}
      {/if}
    </g>
  {/each}
</g>

<style lang="scss">
  .ladder-element {
    cursor: pointer;

    &:hover {
      opacity: 0.85;
    }

    &.selected {
      filter: drop-shadow(0 0 4px var(--theme-primary, #0ea5e9));
    }

    &.energized line,
    &.energized path {
      filter: drop-shadow(0 0 3px var(--color-green-500, #22c55e));
    }
  }

  .tag-label {
    cursor: text;
    user-select: none;

    &:hover {
      fill: var(--theme-primary, #0ea5e9);
    }
  }

  .comment-text {
    cursor: text;
    user-select: none;

    &:hover {
      text-decoration: underline;
    }
  }

  :global(.tag-input),
  :global(.comment-input) {
    background: var(--theme-input-bg, #333);
    border: 1px solid var(--theme-primary, #0ea5e9);
    color: var(--theme-text, #ccc);
    font-family: monospace;
    font-size: 11px;
    padding: 1px 4px;
    width: 100%;
    height: 100%;
    box-sizing: border-box;
    outline: none;
    border-radius: 2px;
  }
</style>
