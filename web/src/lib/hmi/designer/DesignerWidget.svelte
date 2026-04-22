<script lang="ts">
  import type { HmiWidget, HmiComponentConfig } from '$lib/types/hmi';
  import { schemaByType } from '../widgetSchema';
  import WidgetView from '../WidgetView.svelte';
  import Self from './DesignerWidget.svelte';

  interface Props {
    widget: HmiWidget;
    selectedId: string | null;
    onSelect: (id: string) => void;
    onMoveStart: (e: PointerEvent, w: HmiWidget) => void;
    onResizeStart: (e: PointerEvent, w: HmiWidget) => void;
    /** True when this widget is rendered at the canvas root (absolute positioning). */
    topLevel: boolean;
    components?: Record<string, HmiComponentConfig>;
    /** True while dragging onto this container widget (highlight drop target). */
    dropTargetId: string | null;
  }

  let {
    widget,
    selectedId,
    onSelect,
    onMoveStart,
    onResizeStart,
    topLevel,
    components,
    dropTargetId,
  }: Props = $props();

  const schema = $derived(schemaByType[widget.type]);
  const isContainer = $derived(!!schema?.isContainer);
  const isSelected = $derived(widget.id === selectedId);
  const isDropTarget = $derived(widget.id === dropTargetId);

  const p = $derived(widget.props ?? {});

  // Flex layout for stack containers (mirrors runtime Stack.svelte).
  const stackStyle = $derived.by(() => {
    if (widget.type !== 'stack') return '';
    const dir = (p.direction as string) ?? 'column';
    const gap = `${(p.gap as number) ?? 8}px`;
    const padding = `${(p.padding as number) ?? 8}px`;
    const align = mapAlign((p.align as string) ?? 'stretch');
    const justify = mapJustify((p.justify as string) ?? 'start');
    const wrap = p.wrap === 'yes' ? 'wrap' : 'nowrap';
    return `display:flex;flex-direction:${dir};gap:${gap};padding:${padding};align-items:${align};justify-content:${justify};flex-wrap:${wrap};width:100%;height:100%;box-sizing:border-box;`;
  });

  function mapAlign(v: string): string {
    return v === 'start' || v === 'end' ? `flex-${v}` : v;
  }
  function mapJustify(v: string): string {
    return v === 'start' || v === 'end' ? `flex-${v}` : v;
  }

  function childWrapperStyle(c: HmiWidget, parent: HmiWidget): string {
    const cp = c.props ?? {};
    const dir = (parent.props?.direction as string) ?? 'column';
    const parts: string[] = [];
    const grow = cp.$grow;
    if (typeof grow === 'number' && grow > 0) parts.push(`flex-grow:${grow}`);
    const basis = cp.$basis as string | undefined;
    if (basis) parts.push(`flex-basis:${basis}`);
    const alignSelf = cp.$alignSelf as string | undefined;
    if (alignSelf) parts.push(`align-self:${mapAlign(alignSelf)}`);
    if (!basis) {
      if (dir === 'row' && c.w) parts.push(`width:${c.w}px`);
      if (dir === 'column' && c.h) parts.push(`height:${c.h}px`);
    }
    parts.push('min-width:0', 'min-height:0', 'position:relative');
    return parts.join(';');
  }

  function onClick(e: MouseEvent) {
    e.stopPropagation();
    onSelect(widget.id);
  }
  function onPointerDownTopLevel(e: PointerEvent) {
    if (!topLevel) return;
    onMoveStart(e, widget);
  }
</script>

{#if topLevel}
  <div
    class="slot top"
    class:selected={isSelected}
    class:drop-target={isDropTarget}
    style:left="{widget.x}px"
    style:top="{widget.y}px"
    style:width="{widget.w}px"
    style:height="{widget.h}px"
    data-widget-id={widget.id}
    data-container-id={isContainer ? widget.id : null}
    onpointerdown={onPointerDownTopLevel}
    onclick={onClick}
    role="button"
    tabindex="-1"
  >
    {#if widget.type === 'stack'}
      <div class="stack-canvas" style={stackStyle}>
        {#if (widget.children ?? []).length === 0}
          <div class="empty-stack">stack · drop widgets here</div>
        {/if}
        {#each widget.children ?? [] as child (child.id)}
          <div class="child" style={childWrapperStyle(child, widget)}>
            <Self
              widget={child}
              {selectedId}
              {onSelect}
              {onMoveStart}
              {onResizeStart}
              topLevel={false}
              {components}
              {dropTargetId}
            />
          </div>
        {/each}
      </div>
    {:else}
      <div class="widget-content">
        <WidgetView {widget} {components} />
      </div>
    {/if}
    <div class="hit-overlay"></div>
    {#if isSelected}
      <div class="resize-handle" onpointerdown={(e) => onResizeStart(e, widget)} role="presentation"></div>
    {/if}
  </div>
{:else}
  <div
    class="slot child"
    class:selected={isSelected}
    class:drop-target={isDropTarget}
    data-widget-id={widget.id}
    data-container-id={isContainer ? widget.id : null}
    onclick={onClick}
    role="button"
    tabindex="-1"
  >
    {#if widget.type === 'stack'}
      <div class="stack-canvas" style={stackStyle}>
        {#if (widget.children ?? []).length === 0}
          <div class="empty-stack">stack · drop widgets here</div>
        {/if}
        {#each widget.children ?? [] as child (child.id)}
          <div class="child" style={childWrapperStyle(child, widget)}>
            <Self
              widget={child}
              {selectedId}
              {onSelect}
              {onMoveStart}
              {onResizeStart}
              topLevel={false}
              {components}
              {dropTargetId}
            />
          </div>
        {/each}
      </div>
    {:else}
      <div class="widget-content">
        <WidgetView {widget} {components} />
      </div>
    {/if}
  </div>
{/if}

<style lang="scss">
  .slot {
    position: relative;
  }
  .slot.top {
    position: absolute;
    cursor: move;
  }
  .slot.child {
    width: 100%;
    height: 100%;
  }
  .widget-content { width: 100%; height: 100%; pointer-events: none; }
  .hit-overlay { position: absolute; inset: 0; background: transparent; }
  .stack-canvas {
    pointer-events: auto;
    border: 1px dashed color-mix(in srgb, var(--theme-border) 80%, transparent);
    border-radius: var(--rounded-sm, 4px);
    background: color-mix(in srgb, var(--theme-surface) 50%, transparent);
  }
  .empty-stack {
    width: 100%;
    text-align: center;
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace;
    padding: 0.5rem;
  }
  .slot.selected {
    outline: 2px solid var(--theme-text);
    outline-offset: 2px;
    z-index: 1;
  }
  .slot.drop-target > .stack-canvas {
    border-color: var(--theme-text);
    background: color-mix(in srgb, var(--theme-text) 8%, var(--theme-surface));
  }
  .resize-handle {
    position: absolute;
    right: -6px;
    bottom: -6px;
    width: 12px;
    height: 12px;
    background: var(--theme-text);
    border-radius: 2px;
    cursor: nwse-resize;
    z-index: 2;
  }
</style>
