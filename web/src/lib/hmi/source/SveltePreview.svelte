<script lang="ts">
  import SvelteHost from './SvelteHost.svelte';
  import { setHmiStyleContext } from '../styles/styleContext';
  import { compileScopedCss } from '../styles/cssScope';

  interface Props {
    source: string;
    /** Auto-injected `<script>` body. */
    scriptHeader?: string;
    /** Props passed into the compiled component (e.g. `{ udt: {...} }`). */
    props?: Record<string, unknown>;
    /** App-wide CSS classes (selectors stay bare). */
    appClasses?: Record<string, string>;
    /** Component-private CSS classes (scoped under `prefix`). */
    componentClasses?: Record<string, string>;
    /** Selector prefix for component-scoped classes, e.g. `cmp-pump`. */
    prefix?: string;
    /** Well-known container declarations (position, display, …). */
    containerProps?: Record<string, string>;
    /** Freeform CSS appended to the wrapper inline style. */
    containerCss?: string;
    /** Snap dragged-element coordinates to this grid, in pixels. 0 = no snap. */
    snapGrid?: number;
    /** Debounce ms for recompiling on source changes. */
    debounceMs?: number;
    /** Called when a class chip is dropped on the preview surface. The host
     * is expected to splice the class onto the source's Nth element. */
    onClassDrop?: (idx: number, className: string) => void;
    /** Called when an element is dragged to a new position in absolute mode.
     * `offsets` carries only the anchors that were in use (left vs right,
     * top vs bottom) — detected from the element's existing inline style. */
    onElementMove?: (
      idx: number,
      offsets: { left?: number; right?: number; top?: number; bottom?: number },
    ) => void;
    /** Currently-selected element index (or null). The host owns selection
     * state; preview just renders the visual outline. */
    selectedIdx?: number | null;
    /** Called when the user clicks on a tagged element (selection) or on
     * empty surface (clears, idx=null). Click ≠ drag — fires only when no
     * drag commit happened. */
    onElementSelect?: (idx: number | null) => void;
  }

  let {
    source,
    scriptHeader,
    props = {},
    appClasses,
    componentClasses,
    prefix = '',
    containerProps,
    containerCss,
    snapGrid = 0,
    debounceMs = 300,
    onClassDrop,
    onElementMove,
    selectedIdx = null,
    onElementSelect,
  }: Props = $props();

  let compiling = $state(false);
  let error = $state<string | null>(null);
  let surfaceEl: HTMLDivElement | undefined = $state();
  let dropTargetIdx = $state<number | null>(null);

  const css = $derived.by(() => {
    const parts: string[] = [];
    const a = compileScopedCss(appClasses, '');
    if (a) parts.push(a);
    if (prefix) {
      const c = compileScopedCss(componentClasses, prefix, 'descendant');
      if (c) parts.push(c);
    }
    return parts.join('\n\n');
  });

  const surfaceStyle = $derived.by(() => {
    const decls: string[] = [];
    if (containerProps) {
      for (const [k, v] of Object.entries(containerProps)) {
        if (v) decls.push(`${k}: ${v}`);
      }
    }
    if (containerCss?.trim()) decls.push(containerCss.trim().replace(/;$/, ''));
    return decls.join('; ');
  });

  const dragMode = $derived(
    !!onElementMove && (containerProps?.position === 'relative' || containerProps?.position === 'absolute'),
  );

  // Make widget classes (if the user's source uses HMI widgets) resolve.
  $effect(() => {
    const ctx: any = { appClasses };
    if (prefix && componentClasses) {
      ctx.component = { prefix, classes: componentClasses };
    }
    setHmiStyleContext(ctx);
  });

  /** Walk up from a target node to find the nearest element carrying a
   * `data-hmi-el` marker. Returns the element + index, or null if none. */
  function elementAt(target: EventTarget | null): { el: HTMLElement; idx: number } | null {
    let el: HTMLElement | null = target instanceof HTMLElement ? target : null;
    while (el && el !== surfaceEl) {
      const v = el.dataset?.hmiEl;
      if (v !== undefined && v !== '') {
        const n = Number(v);
        if (Number.isFinite(n)) return { el, idx: n };
      }
      el = el.parentElement;
    }
    return null;
  }

  function onDragOver(e: DragEvent) {
    if (!e.dataTransfer) return;
    if (!Array.from(e.dataTransfer.types).includes('application/x-hmi-class')) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
    dropTargetIdx = elementAt(e.target)?.idx ?? null;
  }

  function onDragLeave(e: DragEvent) {
    if (e.target === surfaceEl) dropTargetIdx = null;
  }

  function onDrop(e: DragEvent) {
    if (!e.dataTransfer) return;
    const raw = e.dataTransfer.getData('application/x-hmi-class');
    if (!raw) return;
    e.preventDefault();
    const idx = elementAt(e.target)?.idx ?? null;
    dropTargetIdx = null;
    if (idx === null) return;
    try {
      const { name } = JSON.parse(raw) as { name: string };
      if (!name) return;
      onClassDrop?.(idx, name);
    } catch {
      // ignore malformed
    }
  }

  // Highlight the hovered drop target by toggling a CSS attribute on the
  // surface — the actual element is found by selector.
  $effect(() => {
    if (!surfaceEl) return;
    surfaceEl
      .querySelectorAll<HTMLElement>('[data-hmi-el].hmi-drop-target')
      .forEach((el) => el.classList.remove('hmi-drop-target'));
    if (dropTargetIdx !== null) {
      const el = surfaceEl.querySelector<HTMLElement>(`[data-hmi-el="${dropTargetIdx}"]`);
      el?.classList.add('hmi-drop-target');
    }
  });

  // --- Element drag-to-position (only when container is positioned) ---
  // We track every pointerdown over a tagged element as a *candidate* and
  // only commit to a drag once the pointer moves more than DRAG_THRESHOLD
  // pixels. That keeps casual clicks (e.g. on a <button>) working while
  // still letting the user reposition any element by drag.

  const DRAG_THRESHOLD = 4;

  // A pointerdown registers a pending selection — committed on pointerup
  // unless the user dragged. `pendingSelectActive` distinguishes "ready to
  // commit a select" from "no pointer interaction in flight".
  let pendingSelect: number | null = null;
  let pendingSelectActive = false;

  let drag: {
    idx: number;
    el: HTMLElement;
    pointerId: number;
    startClientX: number;
    startClientY: number;
    anchorX: 'left' | 'right';
    anchorY: 'top' | 'bottom';
    startOffsetX: number;
    startOffsetY: number;
    committed: boolean;
  } | null = null;

  function snap(v: number): number {
    if (!snapGrid || snapGrid <= 0) return Math.round(v);
    return Math.round(v / snapGrid) * snapGrid;
  }

  // Pick the anchor the user has already chosen by virtue of which inline
  // offset they set. Default to left/top so fresh elements still drag.
  function pickAnchorX(el: HTMLElement): 'left' | 'right' {
    if (el.style.left) return 'left';
    if (el.style.right) return 'right';
    return 'left';
  }
  function pickAnchorY(el: HTMLElement): 'top' | 'bottom' {
    if (el.style.top) return 'top';
    if (el.style.bottom) return 'bottom';
    return 'top';
  }

  function currentOffset(
    elRect: DOMRect,
    surfaceRect: DOMRect,
    anchor: 'left' | 'right' | 'top' | 'bottom',
  ): number {
    switch (anchor) {
      case 'left': return elRect.left - surfaceRect.left;
      case 'right': return surfaceRect.right - elRect.right;
      case 'top': return elRect.top - surfaceRect.top;
      case 'bottom': return surfaceRect.bottom - elRect.bottom;
    }
  }

  function onPointerDown(e: PointerEvent) {
    if (!surfaceEl) return;
    if (e.button !== 0) return;
    const hit = elementAt(e.target);
    // Selection-only mode: when drag isn't enabled, still capture the click
    // for selection bookkeeping (handled in pointerup).
    if (!dragMode) {
      pendingSelect = hit ? hit.idx : null;
      pendingSelectActive = true;
      return;
    }
    if (!hit) {
      pendingSelect = null;
      pendingSelectActive = true;
      return;
    }
    pendingSelect = hit.idx;
    pendingSelectActive = true;
    const surfaceRect = surfaceEl.getBoundingClientRect();
    const elRect = hit.el.getBoundingClientRect();
    const anchorX = pickAnchorX(hit.el);
    const anchorY = pickAnchorY(hit.el);
    drag = {
      idx: hit.idx,
      el: hit.el,
      pointerId: e.pointerId,
      startClientX: e.clientX,
      startClientY: e.clientY,
      anchorX,
      anchorY,
      startOffsetX: currentOffset(elRect, surfaceRect, anchorX),
      startOffsetY: currentOffset(elRect, surfaceRect, anchorY),
      committed: false,
    };
  }

  function liveOffsets(d: NonNullable<typeof drag>, e: PointerEvent): { x: number; y: number } {
    const dx = e.clientX - d.startClientX;
    const dy = e.clientY - d.startClientY;
    return {
      x: snap(d.startOffsetX + (d.anchorX === 'left' ? dx : -dx)),
      y: snap(d.startOffsetY + (d.anchorY === 'top' ? dy : -dy)),
    };
  }

  function commitDrag() {
    if (!drag || !surfaceEl) return;
    drag.committed = true;
    // Force position:absolute on the *live* element so the chosen anchors
    // visually move during drag. NOT persisted — onElementMove only writes
    // the offsets, so the next recompile drops this transient style.
    drag.el.style.position = 'absolute';
    drag.el.style[drag.anchorX] = `${drag.startOffsetX}px`;
    drag.el.style[drag.anchorY] = `${drag.startOffsetY}px`;
    drag.el.classList.add('hmi-dragging');
    surfaceEl.setPointerCapture(drag.pointerId);
  }

  function onPointerMove(e: PointerEvent) {
    if (!drag || e.pointerId !== drag.pointerId) return;
    const dx = e.clientX - drag.startClientX;
    const dy = e.clientY - drag.startClientY;
    if (!drag.committed) {
      if (Math.abs(dx) < DRAG_THRESHOLD && Math.abs(dy) < DRAG_THRESHOLD) return;
      commitDrag();
      // A drag suppresses the would-be select for this gesture.
      pendingSelectActive = false;
    }
    const { x, y } = liveOffsets(drag, e);
    drag.el.style[drag.anchorX] = `${x}px`;
    drag.el.style[drag.anchorY] = `${y}px`;
    e.preventDefault();
  }

  function onPointerUp(e: PointerEvent) {
    // Commit a pending select if no drag took over this gesture.
    if (pendingSelectActive) {
      pendingSelectActive = false;
      onElementSelect?.(pendingSelect);
    }
    if (!drag || e.pointerId !== drag.pointerId) return;
    const wasCommitted = drag.committed;
    if (wasCommitted) {
      const { x, y } = liveOffsets(drag, e);
      drag.el.classList.remove('hmi-dragging');
      surfaceEl?.releasePointerCapture(e.pointerId);
      const offsets: { left?: number; right?: number; top?: number; bottom?: number } = {};
      offsets[drag.anchorX] = x;
      offsets[drag.anchorY] = y;
      const idx = drag.idx;
      drag = null;
      onElementMove?.(idx, offsets);
    } else {
      drag = null;
    }
  }

  // Toggle a `.hmi-selected` class on the currently-selected element so
  // the visual outline tracks selection without re-rendering markup.
  $effect(() => {
    if (!surfaceEl) return;
    surfaceEl
      .querySelectorAll<HTMLElement>('[data-hmi-el].hmi-selected')
      .forEach((el) => el.classList.remove('hmi-selected'));
    if (selectedIdx !== null && selectedIdx !== undefined) {
      const el = surfaceEl.querySelector<HTMLElement>(`[data-hmi-el="${selectedIdx}"]`);
      el?.classList.add('hmi-selected');
    }
  });
</script>

{#if css}
  {@html `<style data-hmi-preview-classes>${css}</style>`}
{/if}

<div class="preview-shell" class:dragmode={dragMode}>
  <div
    class="surface {prefix}"
    style={surfaceStyle}
    bind:this={surfaceEl}
    ondragover={onDragOver}
    ondragleave={onDragLeave}
    ondrop={onDrop}
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    oncontextmenu={(e) => { if (drag) e.preventDefault(); }}
    role="region"
  >
    <SvelteHost
      {source}
      {scriptHeader}
      markElements={true}
      componentProps={props}
      {debounceMs}
      onStatus={(s) => {
        compiling = s.compiling;
        error = s.error;
      }}
    />
  </div>
  {#if compiling}
    <div class="badge compiling">compiling…</div>
  {/if}
  {#if error}
    <div class="error">
      <strong>compile error</strong>
      <pre>{error}</pre>
    </div>
  {/if}
</div>

<style lang="scss">
  .preview-shell {
    position: relative;
    width: 100%;
    height: 100%;
    box-sizing: border-box;
    background:
      linear-gradient(to right, color-mix(in srgb, var(--theme-border) 40%, transparent) 1px, transparent 1px) 0 0 / 16px 16px,
      linear-gradient(to bottom, color-mix(in srgb, var(--theme-border) 40%, transparent) 1px, transparent 1px) 0 0 / 16px 16px,
      var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    overflow: auto;
  }
  .surface {
    width: 100%;
    height: 100%;
    box-sizing: border-box;
    :global(.hmi-drop-target) {
      outline: 2px dashed var(--theme-text);
      outline-offset: 2px;
    }
    :global(.hmi-dragging) {
      outline: 2px solid var(--theme-text);
      outline-offset: 2px;
      cursor: grabbing !important;
    }
    :global(.hmi-selected) {
      outline: 2px solid color-mix(in srgb, var(--theme-text) 80%, transparent);
      outline-offset: 1px;
    }
  }
  .preview-shell.dragmode .surface {
    :global([data-hmi-el]) { cursor: grab; }
  }
  .badge {
    position: absolute;
    top: 0.5rem;
    right: 0.5rem;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-sm, 4px);
    font-size: 0.6875rem;
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text-muted);
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
  }
  .error {
    position: absolute;
    inset: auto 0.5rem 0.5rem 0.5rem;
    padding: 0.5rem 0.75rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    max-height: 50%;
    overflow: auto;
    strong { display: block; margin-bottom: 0.25rem; font-size: 0.6875rem; text-transform: uppercase; letter-spacing: 0.04em; }
    pre { margin: 0; white-space: pre-wrap; }
  }
</style>
