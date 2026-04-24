<script lang="ts">
  import type { HmiWidget, HmiComponentConfig } from '$lib/types/hmi';
  import { useLiveTags } from '../tagStore.svelte';
  import {
    makeWidget,
    appendChild,
    findWidget,
    findParent,
    removeWidget,
    replaceWidget,
    collectIds,
    schemaByType,
  } from '../widgetSchema';
  import DesignerWidget from './DesignerWidget.svelte';
  import { setHmiStyleContext } from '../styles/styleContext';
  import { compileScopedCss } from '../styles/cssScope';

  interface Props {
    widgets: HmiWidget[];
    selectedId: string | null;
    width?: number;
    height?: number;
    onChange: (widgets: HmiWidget[]) => void;
    onSelect: (id: string | null) => void;
    components?: Record<string, HmiComponentConfig>;
    /** App-wide CSS classes — emitted at the canvas root for live preview. */
    appClasses?: Record<string, string>;
    /** When editing a component, its classes are emitted scoped under
     * `cmp-<componentId>` so widgets inside use those rules. */
    componentClasses?: Record<string, string>;
    componentId?: string;
  }

  let {
    widgets,
    selectedId,
    width = 0,
    height = 600,
    onChange,
    onSelect,
    components,
    appClasses,
    componentClasses,
    componentId,
  }: Props = $props();

  $effect(() => {
    const ctx: any = { appClasses };
    if (componentClasses && componentId) {
      ctx.component = { prefix: `cmp-${componentId}`, classes: componentClasses };
    }
    setHmiStyleContext(ctx);
  });

  const designCss = $derived.by(() => {
    const parts: string[] = [];
    const app = compileScopedCss(appClasses, '');
    if (app) parts.push(app);
    if (componentClasses && componentId) {
      const cmp = compileScopedCss(componentClasses, `cmp-${componentId}`);
      if (cmp) parts.push(cmp);
    }
    return parts.join('\n\n');
  });

  useLiveTags();

  const canvasW = $derived(width && width > 0 ? `${width}px` : '100%');
  const canvasH = $derived(`${Math.max(height, 400)}px`);

  let canvasEl: HTMLDivElement | undefined = $state();
  let dropTargetId = $state<string | null>(null);

  function snap(n: number): number {
    return Math.round(n / 8) * 8;
  }

  /** Find the nearest container widget under the pointer by walking the
   * event path for `data-container-id`. Returns null when no container is
   * under the pointer (drop should land on the canvas root). */
  function containerAtEvent(e: DragEvent): string | null {
    const path = e.composedPath() as HTMLElement[];
    for (const el of path) {
      if (el === canvasEl) break;
      const id = (el as HTMLElement).dataset?.containerId;
      if (id) return id;
    }
    return null;
  }

  /** Find the widget id under the pointer (for class chip drops). */
  function widgetAtEvent(e: DragEvent): string | null {
    const path = e.composedPath() as HTMLElement[];
    for (const el of path) {
      if (el === canvasEl) break;
      const id = (el as HTMLElement).dataset?.widgetId;
      if (id) return id;
    }
    return null;
  }

  function onDragOver(e: DragEvent) {
    if (!e.dataTransfer) return;
    const types = Array.from(e.dataTransfer.types);
    const isWidget = types.includes('application/x-hmi-widget');
    const isClass = types.includes('application/x-hmi-class');
    if (!isWidget && !isClass) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
    dropTargetId = isWidget ? containerAtEvent(e) : null;
  }

  function onDragLeave(e: DragEvent) {
    // Only clear when leaving the canvas root itself.
    if (e.target === canvasEl) dropTargetId = null;
  }

  function onDrop(e: DragEvent) {
    if (!e.dataTransfer || !canvasEl) return;
    e.preventDefault();

    // Class chip drop: add the class to the widget under the pointer.
    const classRaw = e.dataTransfer.getData('application/x-hmi-class');
    if (classRaw) {
      const targetId = widgetAtEvent(e);
      if (!targetId) return;
      try {
        const { name } = JSON.parse(classRaw) as { name: string };
        const w = findWidget(widgets, targetId);
        if (!w || !name) return;
        const existing = (w.props?.$classes as string[] | undefined) ?? [];
        if (existing.includes(name)) return;
        const next: HmiWidget = {
          ...w,
          props: { ...(w.props ?? {}), $classes: [...existing, name] },
        };
        onChange(replaceWidget(widgets, next));
        onSelect(targetId);
      } catch {
        // ignore malformed payloads
      }
      return;
    }

    // Palette drop: create a new widget.
    const type = e.dataTransfer.getData('application/x-hmi-widget');
    if (!type) return;
    const parentId = containerAtEvent(e);
    const ids = collectIds(widgets);
    const rect = canvasEl.getBoundingClientRect();
    const x = parentId ? 0 : Math.max(0, snap(e.clientX - rect.left));
    const y = parentId ? 0 : Math.max(0, snap(e.clientY - rect.top));
    const widget = makeWidget(type, x, y, ids);
    const componentId = e.dataTransfer.getData('application/x-hmi-component-id');
    if (componentId && type === 'componentInstance') {
      widget.props = { ...(widget.props ?? {}), componentId };
    }
    onChange(appendChild(widgets, parentId, widget));
    onSelect(widget.id);
    dropTargetId = null;
  }

  type DragMode = { kind: 'move'; id: string; startX: number; startY: number; origX: number; origY: number }
                | { kind: 'resize'; id: string; startX: number; startY: number; origW: number; origH: number };
  let drag = $state<DragMode | null>(null);

  function startMove(e: PointerEvent, w: HmiWidget) {
    if (e.button !== 0) return;
    e.stopPropagation();
    onSelect(w.id);
    drag = { kind: 'move', id: w.id, startX: e.clientX, startY: e.clientY, origX: w.x, origY: w.y };
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
  }
  function startResize(e: PointerEvent, w: HmiWidget) {
    if (e.button !== 0) return;
    e.stopPropagation();
    onSelect(w.id);
    drag = { kind: 'resize', id: w.id, startX: e.clientX, startY: e.clientY, origW: w.w, origH: w.h };
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
  }
  function onPointerMove(e: PointerEvent) {
    if (!drag) return;
    const target = findWidget(widgets, drag.id);
    if (!target) return;
    const dx = e.clientX - drag.startX;
    const dy = e.clientY - drag.startY;
    let updated: HmiWidget;
    if (drag.kind === 'move') {
      updated = { ...target, x: Math.max(0, snap(drag.origX + dx)), y: Math.max(0, snap(drag.origY + dy)) };
    } else {
      updated = { ...target, w: Math.max(40, snap(drag.origW + dx)), h: Math.max(24, snap(drag.origH + dy)) };
    }
    onChange(replaceWidget(widgets, updated));
  }
  function onPointerUp() {
    drag = null;
  }

  function onCanvasClick(e: MouseEvent) {
    if (e.target === canvasEl) onSelect(null);
  }

  function onKeyDown(e: KeyboardEvent) {
    if (!selectedId) return;
    const target = e.target as HTMLElement;
    const tag = target?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;

    if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault();
      onChange(removeWidget(widgets, selectedId));
      onSelect(null);
      return;
    }
    const arrows: Record<string, [number, number]> = {
      ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1],
    };
    if (arrows[e.key]) {
      // Arrow nudge only applies to top-level widgets (children flow in containers).
      const isTopLevel = widgets.some((w) => w.id === selectedId);
      if (!isTopLevel) return;
      e.preventDefault();
      const step = e.shiftKey ? 8 : 1;
      const [dx, dy] = arrows[e.key];
      const w = findWidget(widgets, selectedId)!;
      onChange(replaceWidget(widgets, { ...w, x: Math.max(0, w.x + dx * step), y: Math.max(0, w.y + dy * step) }));
    }
  }
</script>

<svelte:window onpointermove={onPointerMove} onpointerup={onPointerUp} onkeydown={onKeyDown} />

{#if designCss}
  {@html `<style data-hmi-design-classes>${designCss}</style>`}
{/if}

<div class="canvas-wrap">
  <div
    class="canvas"
    bind:this={canvasEl}
    style:width={canvasW}
    style:height={canvasH}
    ondragover={onDragOver}
    ondragleave={onDragLeave}
    ondrop={onDrop}
    onclick={onCanvasClick}
    role="application"
    tabindex="0"
  >
    {#each widgets as widget (widget.id)}
      <DesignerWidget
        {widget}
        {selectedId}
        {onSelect}
        onMoveStart={startMove}
        onResizeStart={startResize}
        topLevel={true}
        {components}
        {dropTargetId}
      />
    {/each}
    {#if widgets.length === 0}
      <div class="empty">Drag a widget from the palette to begin.</div>
    {/if}
  </div>
</div>

<style lang="scss">
  .canvas-wrap {
    flex: 1;
    overflow: auto;
    padding: 1.5rem;
    background: var(--theme-background);
  }
  .canvas {
    position: relative;
    background:
      linear-gradient(to right, color-mix(in srgb, var(--theme-border) 40%, transparent) 1px, transparent 1px) 0 0 / 32px 32px,
      linear-gradient(to bottom, color-mix(in srgb, var(--theme-border) 40%, transparent) 1px, transparent 1px) 0 0 / 32px 32px,
      var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    outline: none;
  }
  .empty {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--theme-text-muted);
    font-size: 0.875rem;
    pointer-events: none;
  }
</style>
