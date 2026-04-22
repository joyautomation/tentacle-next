<script lang="ts">
  import type { HmiWidget } from '$lib/types/hmi';
  import WidgetView from '../WidgetView.svelte';
  import { useLiveTags } from '../tagStore.svelte';
  import { makeWidget } from '../widgetSchema';

  interface Props {
    widgets: HmiWidget[];
    selectedId: string | null;
    width?: number;
    height?: number;
    onChange: (widgets: HmiWidget[]) => void;
    onSelect: (id: string | null) => void;
  }

  let { widgets, selectedId, width = 0, height = 600, onChange, onSelect }: Props = $props();

  useLiveTags();

  const canvasW = $derived(width && width > 0 ? `${width}px` : '100%');
  const canvasH = $derived(`${Math.max(height, 400)}px`);

  let canvasEl: HTMLDivElement | undefined = $state();

  function snap(n: number): number {
    return Math.round(n / 8) * 8;
  }

  function onDragOver(e: DragEvent) {
    if (!e.dataTransfer) return;
    if (Array.from(e.dataTransfer.types).includes('application/x-hmi-widget')) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
    }
  }

  function onDrop(e: DragEvent) {
    if (!e.dataTransfer || !canvasEl) return;
    const type = e.dataTransfer.getData('application/x-hmi-widget');
    if (!type) return;
    e.preventDefault();
    const rect = canvasEl.getBoundingClientRect();
    const x = snap(e.clientX - rect.left);
    const y = snap(e.clientY - rect.top);
    const ids = widgets.map((w) => w.id);
    const widget = makeWidget(type, Math.max(0, x), Math.max(0, y), ids);
    onChange([...widgets, widget]);
    onSelect(widget.id);
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
    const dx = e.clientX - drag.startX;
    const dy = e.clientY - drag.startY;
    const next = widgets.map((w) => {
      if (w.id !== drag!.id) return w;
      if (drag!.kind === 'move') {
        return { ...w, x: Math.max(0, snap(drag!.origX + dx)), y: Math.max(0, snap(drag!.origY + dy)) };
      }
      return { ...w, w: Math.max(40, snap(drag!.origW + dx)), h: Math.max(24, snap(drag!.origH + dy)) };
    });
    onChange(next);
  }
  function onPointerUp() {
    drag = null;
  }

  function onCanvasClick() {
    onSelect(null);
  }

  function onKeyDown(e: KeyboardEvent) {
    if (!selectedId) return;
    if (e.key === 'Delete' || e.key === 'Backspace') {
      const target = e.target as HTMLElement;
      const tag = target?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
      e.preventDefault();
      onChange(widgets.filter((w) => w.id !== selectedId));
      onSelect(null);
      return;
    }
    const arrows: Record<string, [number, number]> = {
      ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1],
    };
    if (arrows[e.key]) {
      e.preventDefault();
      const step = e.shiftKey ? 8 : 1;
      const [dx, dy] = arrows[e.key];
      onChange(widgets.map((w) =>
        w.id === selectedId ? { ...w, x: Math.max(0, w.x + dx * step), y: Math.max(0, w.y + dy * step) } : w
      ));
    }
  }
</script>

<svelte:window onpointermove={onPointerMove} onpointerup={onPointerUp} onkeydown={onKeyDown} />

<div class="canvas-wrap">
  <div
    class="canvas"
    bind:this={canvasEl}
    style:width={canvasW}
    style:height={canvasH}
    ondragover={onDragOver}
    ondrop={onDrop}
    onclick={onCanvasClick}
    role="application"
    tabindex="0"
  >
    {#each widgets as widget (widget.id)}
      {@const isSelected = widget.id === selectedId}
      <div
        class="widget-slot"
        class:selected={isSelected}
        style:left="{widget.x}px"
        style:top="{widget.y}px"
        style:width="{widget.w}px"
        style:height="{widget.h}px"
        onpointerdown={(e) => startMove(e, widget)}
        role="button"
        tabindex="-1"
      >
        <div class="widget-content">
          <WidgetView {widget} />
        </div>
        <div class="hit-overlay"></div>
        {#if isSelected}
          <div class="resize-handle" onpointerdown={(e) => startResize(e, widget)} role="presentation"></div>
        {/if}
      </div>
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
  .widget-slot {
    position: absolute;
    cursor: move;
  }
  .widget-content { width: 100%; height: 100%; pointer-events: none; }
  .hit-overlay {
    position: absolute;
    inset: 0;
    background: transparent;
  }
  .widget-slot.selected {
    outline: 2px solid var(--theme-text);
    outline-offset: 2px;
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
