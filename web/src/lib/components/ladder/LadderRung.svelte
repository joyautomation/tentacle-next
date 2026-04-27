<script lang="ts">
  import type {
    EditPath,
    LayoutNode,
    Rung,
    Selection,
    TagValues,
  } from './types.js';
  import { LAYOUT } from './types.js';
  import { layoutRungInCanvas } from './layout.js';

  interface Props {
    rung: Rung;
    rungIndex: number;
    /** Width available in the rung-list container (drives flex-grow). */
    availableWidth: number;
    selection: Selection;
    tagValues?: TagValues;
    monitoring?: boolean;
    onSelect: (path: EditPath) => void;
    onVariableDrop?: (path: EditPath, varName: string) => void;
  }

  let {
    rung,
    rungIndex,
    availableWidth,
    selection,
    tagValues = {},
    monitoring = false,
    onSelect,
    onVariableDrop,
  }: Props = $props();

  const computed = $derived(layoutRungInCanvas(rung, rungIndex, availableWidth));
  const layout = $derived(computed.layout);
  const rails = $derived(computed.rails);

  // Tracks which node is currently being hovered over with a variable
  // drag — drives the .drop-target style so the user sees where the drop
  // will land.
  let dropTargetKey = $state<string | null>(null);

  function nodeKey(node: LayoutNode): string {
    if (node.path.kind === 'logic') return `L:${node.path.rung}:${node.path.logic.join('.')}`;
    return `O:${node.path.rung}:${node.path.output}`;
  }

  function isContactOrCoil(node: LayoutNode): boolean {
    return node.kind === 'contact' || node.kind === 'coil';
  }

  function handleDragOver(e: DragEvent, node: LayoutNode) {
    if (!onVariableDrop) return;
    if (!isContactOrCoil(node)) return;
    if (!e.dataTransfer?.types.includes('application/x-plc-variable')) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
    dropTargetKey = nodeKey(node);
  }

  function handleDragLeave(node: LayoutNode) {
    if (dropTargetKey === nodeKey(node)) dropTargetKey = null;
  }

  function handleDrop(e: DragEvent, node: LayoutNode) {
    if (!onVariableDrop) return;
    if (!isContactOrCoil(node)) return;
    const raw = e.dataTransfer?.getData('application/x-plc-variable');
    if (!raw) return;
    e.preventDefault();
    e.stopPropagation();
    dropTargetKey = null;
    try {
      const payload = JSON.parse(raw) as { name?: string };
      if (payload?.name) onVariableDrop(node.path, payload.name);
    } catch {
      // Malformed payload — ignore silently.
    }
  }

  // Two paths are equal when they target the same rung+kind+sub-index.
  function pathEq(a: EditPath, b: EditPath): boolean {
    if (a.kind !== b.kind || a.rung !== b.rung) return false;
    if (a.kind === 'output' && b.kind === 'output') return a.output === b.output;
    if (a.kind === 'logic' && b.kind === 'logic') {
      if (a.logic.length !== b.logic.length) return false;
      return a.logic.every((v, i) => v === b.logic[i]);
    }
    return false;
  }

  function isSelected(node: LayoutNode): boolean {
    return selection !== null && pathEq(selection, node.path);
  }

  function isEnergized(operand: string): boolean {
    if (!monitoring) return false;
    const tv = tagValues[operand];
    return tv ? Boolean(tv.value) : false;
  }

  function valueText(operand: string): string | null {
    const tv = tagValues[operand];
    if (!tv) return null;
    if (typeof tv.value === 'boolean') return tv.value ? '1' : '0';
    if (typeof tv.value === 'number') return Number.isInteger(tv.value) ? String(tv.value) : tv.value.toFixed(2);
    return String(tv.value);
  }

  // Tagnames are shown beneath each element, but the visible width is
  // narrow — middle-truncate long names and hand the full string to a
  // <title> tooltip so the user can hover to see the rest.
  function truncateOperand(name: string): string {
    if (!name) return '?';
    const max = LAYOUT.OPERAND_LABEL_CHARS;
    if (name.length <= max) return name;
    const head = Math.ceil((max - 1) / 2);
    const tail = Math.floor((max - 1) / 2);
    return `${name.slice(0, head)}…${name.slice(name.length - tail)}`;
  }

  function handleNodeClick(e: MouseEvent, node: LayoutNode) {
    e.stopPropagation();
    onSelect(node.path);
  }
</script>

<svg
  class="rung-svg"
  width={layout.totalWidth}
  height={layout.totalHeight}
  viewBox={`0 0 ${layout.totalWidth} ${layout.totalHeight}`}
>
  <!-- Per-rung rails. When rungs stack with no gap they butt up so the
       rails read as one continuous bus. -->
  <line
    class="rail"
    x1={rails.leftX}
    y1={rails.topY}
    x2={rails.leftX}
    y2={rails.bottomY}
  />
  <line
    class="rail"
    x1={rails.rightX}
    y1={rails.topY}
    x2={rails.rightX}
    y2={rails.bottomY}
  />

  <g class="rung" data-rung={rungIndex}>
  <!-- Rung number gutter -->
  <text
    class="rung-number"
    x="4"
    y={layout.wireY + 4}
  >
    {rungIndex}
  </text>

  {#if rung.comment}
    <text
      class="rung-comment"
      x={LAYOUT.RAIL_LEFT}
      y={Math.max(LAYOUT.RUNG_PADDING_Y - 8, 12)}
    >
      {rung.comment}
    </text>
  {/if}

  <!-- Wires -->
  {#each layout.wires as wire}
    <line
      x1={wire.x1}
      y1={wire.y1}
      x2={wire.x2}
      y2={wire.y2}
      class="wire"
    />
  {/each}

  <!-- Branch rails (vertical lines of Parallel groups) -->
  {#each layout.branchLines as bl}
    <line
      x1={bl.x}
      y1={bl.y1}
      x2={bl.x}
      y2={bl.y2}
      class="wire branch"
    />
  {/each}

  <!-- Nodes -->
  {#each layout.nodes as node}
    {#if node.kind === 'contact'}
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <g
        class="node contact"
        class:selected={isSelected(node)}
        class:energized={isEnergized(node.element.operand)}
        class:drop-target={dropTargetKey === nodeKey(node)}
        transform={`translate(${node.x}, ${node.y})`}
        onclick={(e) => handleNodeClick(e, node)}
        ondragover={(e) => handleDragOver(e, node)}
        ondragleave={() => handleDragLeave(node)}
        ondrop={(e) => handleDrop(e, node)}
      >
        <!-- Hit/highlight rect spans the symbol + tag area below. -->
        <rect
          class="hit"
          x="0"
          y={-LAYOUT.LABEL_TOP_SPACE}
          width={node.width}
          height={node.height + LAYOUT.LABEL_TOP_SPACE + LAYOUT.LABEL_BOTTOM_SPACE}
          rx="2"
        />
        <title>{node.element.operand || '(no operand)'}</title>
        <!-- Internal wire passes through the symbol (no gap with the rung). -->
        <line x1="0" y1={node.height / 2} x2={node.width} y2={node.height / 2} class="wire-through" />
        <!-- Contact bars: two vertical posts straddling the centre. The
             slot width is generous so the tag below has room; the bars
             themselves stay compact and centred. -->
        {#each [node.width / 2 - 5, node.width / 2 + 5] as bx}
          <line x1={bx} y1="2" x2={bx} y2={node.height - 2} class="post" />
        {/each}
        <!-- NC mark: diagonal slash between the two posts. -->
        {#if node.element.form === 'NC'}
          <line
            x1={node.width / 2 - 5}
            y1={node.height - 2}
            x2={node.width / 2 + 5}
            y2="2"
            class="nc-slash"
          />
        {/if}
        <text
          class="form-label"
          x={node.width / 2}
          y={-2}
          text-anchor="middle"
        >
          {node.element.form}
        </text>
        <text
          class="operand"
          x={node.width / 2}
          y={node.height + 12}
          text-anchor="middle"
        >
          {truncateOperand(node.element.operand)}
        </text>
        {#if monitoring}
          {@const v = valueText(node.element.operand)}
          {#if v !== null}
            <text class="live-value" x={node.width / 2} y={node.height + 24} text-anchor="middle">
              {v}
            </text>
          {/if}
        {/if}
      </g>
    {:else if node.kind === 'coil'}
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <g
        class="node coil"
        class:selected={isSelected(node)}
        class:energized={isEnergized(node.element.operand)}
        class:drop-target={dropTargetKey === nodeKey(node)}
        transform={`translate(${node.x}, ${node.y})`}
        onclick={(e) => handleNodeClick(e, node)}
        ondragover={(e) => handleDragOver(e, node)}
        ondragleave={() => handleDragLeave(node)}
        ondrop={(e) => handleDrop(e, node)}
      >
        <rect
          class="hit"
          x="0"
          y={-LAYOUT.LABEL_TOP_SPACE}
          width={node.width}
          height={node.height + LAYOUT.LABEL_TOP_SPACE + LAYOUT.LABEL_BOTTOM_SPACE}
          rx="2"
        />
        <title>{node.element.operand || '(no operand)'}</title>
        <!-- Connecting wire stubs reach the arc endpoints from the rung. -->
        <line x1="0" y1={node.height / 2} x2={node.width / 2 - 8} y2={node.height / 2} class="wire-through" />
        <line x1={node.width / 2 + 8} y1={node.height / 2} x2={node.width} y2={node.height / 2} class="wire-through" />
        <!-- Coil body: two arcs facing each other forming a (). -->
        <path
          class="coil-arc"
          d={`M ${node.width / 2 - 8} 2 Q ${node.width / 2 - 16} ${node.height / 2} ${node.width / 2 - 8} ${node.height - 2}`}
          fill="none"
        />
        <path
          class="coil-arc"
          d={`M ${node.width / 2 + 8} 2 Q ${node.width / 2 + 16} ${node.height / 2} ${node.width / 2 + 8} ${node.height - 2}`}
          fill="none"
        />
        <text class="form-label" x={node.width / 2} y={-2} text-anchor="middle">
          {node.element.form}
        </text>
        <text class="operand" x={node.width / 2} y={node.height + 12} text-anchor="middle">
          {truncateOperand(node.element.operand)}
        </text>
      </g>
    {:else}
      <!-- FB call: header + pin rows. -->
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <g
        class="node fb"
        class:selected={isSelected(node)}
        transform={`translate(${node.x}, ${node.y})`}
        onclick={(e) => handleNodeClick(e, node)}
      >
        <rect class="fb-body" x="0" y="0" width={node.width} height={node.height} rx="3" />
        <line
          x1="0"
          y1={LAYOUT.FB_HEADER_HEIGHT}
          x2={node.width}
          y2={LAYOUT.FB_HEADER_HEIGHT}
          class="fb-header-rule"
        />
        <text
          class="fb-instance"
          x={node.width / 2}
          y={LAYOUT.FB_HEADER_HEIGHT - 8}
          text-anchor="middle"
        >
          {node.element.instance}
        </text>
        {#each node.pins as pin}
          <text
            class="fb-pin-name"
            class:power={pin.isPower}
            x={LAYOUT.FB_HORIZONTAL_PADDING}
            y={pin.y + 4}
          >
            {pin.name}
          </text>
          {#if pin.valueText !== undefined}
            <text
              class="fb-pin-value"
              x={node.width - LAYOUT.FB_HORIZONTAL_PADDING}
              y={pin.y + 4}
              text-anchor="end"
            >
              {pin.valueText}
            </text>
          {/if}
          <!-- Power-flow tick on the left edge for the power pin. -->
          {#if pin.isPower}
            <line x1="-6" y1={pin.y} x2="0" y2={pin.y} class="wire" />
          {/if}
        {/each}
      </g>
    {/if}
  {/each}
  </g>
</svg>

<style lang="scss">
  .rung-svg {
    display: block;
  }

  .rail {
    stroke: var(--theme-text, #ddd);
    stroke-width: 2.5;
    stroke-linecap: square;
  }

  .rung-number {
    fill: var(--theme-text-muted, #888);
    font-size: 10px;
    font-family: var(--theme-font-basic, ui-monospace, monospace);
  }

  .rung-comment {
    fill: var(--theme-text-muted, #888);
    font-size: 11px;
    font-style: italic;
    font-family: var(--theme-font-basic, sans-serif);
  }

  .wire {
    stroke: var(--theme-text-muted, #888);
    stroke-width: 1.5;
  }

  .wire.branch {
    stroke-width: 1.5;
  }

  .wire-through {
    stroke: var(--theme-text-muted, #888);
    stroke-width: 1.5;
  }

  .node {
    cursor: pointer;
  }

  .node .hit {
    fill: transparent;
    stroke: transparent;
  }

  .node.selected .hit {
    fill: var(--theme-primary, #3b82f6);
    fill-opacity: 0.08;
    stroke: var(--theme-primary, #3b82f6);
    stroke-width: 1;
  }

  .node.drop-target .hit {
    fill: var(--green-500, #22c55e);
    fill-opacity: 0.18;
    stroke: var(--green-500, #22c55e);
    stroke-width: 1.5;
    stroke-dasharray: 3 2;
  }

  .post {
    stroke: var(--theme-text, #ddd);
    stroke-width: 2;
    stroke-linecap: round;
  }

  .nc-slash {
    stroke: var(--theme-text, #ddd);
    stroke-width: 1.5;
    stroke-linecap: round;
  }

  .coil-arc {
    stroke: var(--theme-text, #ddd);
    stroke-width: 2;
    stroke-linecap: round;
  }

  .operand {
    fill: var(--theme-text, #ddd);
    font-size: 12px;
    font-family: var(--theme-font-basic, ui-monospace, monospace);
  }

  .form-label {
    fill: var(--theme-text-muted, #888);
    font-size: 9px;
    font-family: var(--theme-font-basic, ui-monospace, monospace);
    letter-spacing: 0.5px;
  }

  .live-value {
    fill: var(--theme-primary, #3b82f6);
    font-size: 10px;
    font-family: var(--theme-font-basic, ui-monospace, monospace);
  }

  .energized .post,
  .energized .nc-slash,
  .energized .coil-arc,
  .energized .wire-through {
    stroke: var(--green-500, #22c55e);
  }

  .fb-body {
    fill: var(--theme-surface, #1f2937);
    stroke: var(--theme-text, #ddd);
    stroke-width: 1.2;
  }

  .fb-header-rule {
    stroke: var(--theme-text-muted, #888);
    stroke-width: 0.8;
  }

  .fb-instance {
    fill: var(--theme-text, #ddd);
    font-size: 12px;
    font-weight: 600;
    font-family: var(--theme-font-basic, ui-monospace, monospace);
  }

  .fb-pin-name {
    fill: var(--theme-text-muted, #aaa);
    font-size: 11px;
    font-family: var(--theme-font-basic, ui-monospace, monospace);
  }

  .fb-pin-name.power {
    fill: var(--theme-text, #ddd);
    font-weight: 600;
  }

  .fb-pin-value {
    fill: var(--theme-text, #ddd);
    font-size: 11px;
    font-family: var(--theme-font-basic, ui-monospace, monospace);
  }

  .fb.selected .fb-body {
    stroke: var(--theme-primary, #3b82f6);
    stroke-width: 1.8;
  }
</style>
