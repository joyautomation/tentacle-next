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
    /** Called when an element is dragged to a new position in absolute mode. */
    onElementMove?: (idx: number, left: number, top: number) => void;
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

  let drag: {
    idx: number;
    el: HTMLElement;
    pointerId: number;
    startClientX: number;
    startClientY: number;
    startLeft: number;
    startTop: number;
  } | null = null;

  function snap(v: number): number {
    if (!snapGrid || snapGrid <= 0) return Math.round(v);
    return Math.round(v / snapGrid) * snapGrid;
  }

  function onPointerDown(e: PointerEvent) {
    if (!dragMode || !surfaceEl) return;
    if (e.button !== 0) return;
    const hit = elementAt(e.target);
    if (!hit) return;
    // Don't hijack inputs/buttons/links etc — only divs/spans-style containers.
    const tag = hit.el.tagName.toLowerCase();
    if (['input', 'select', 'textarea', 'button', 'a', 'label'].includes(tag)) return;
    const surfaceRect = surfaceEl.getBoundingClientRect();
    const elRect = hit.el.getBoundingClientRect();
    drag = {
      idx: hit.idx,
      el: hit.el,
      pointerId: e.pointerId,
      startClientX: e.clientX,
      startClientY: e.clientY,
      startLeft: elRect.left - surfaceRect.left,
      startTop: elRect.top - surfaceRect.top,
    };
    hit.el.style.position = 'absolute';
    hit.el.style.left = `${drag.startLeft}px`;
    hit.el.style.top = `${drag.startTop}px`;
    hit.el.classList.add('hmi-dragging');
    surfaceEl.setPointerCapture(e.pointerId);
    e.preventDefault();
  }

  function onPointerMove(e: PointerEvent) {
    if (!drag || e.pointerId !== drag.pointerId) return;
    const left = snap(drag.startLeft + (e.clientX - drag.startClientX));
    const top = snap(drag.startTop + (e.clientY - drag.startClientY));
    drag.el.style.left = `${left}px`;
    drag.el.style.top = `${top}px`;
  }

  function onPointerUp(e: PointerEvent) {
    if (!drag || e.pointerId !== drag.pointerId) return;
    const left = snap(drag.startLeft + (e.clientX - drag.startClientX));
    const top = snap(drag.startTop + (e.clientY - drag.startClientY));
    drag.el.classList.remove('hmi-dragging');
    surfaceEl?.releasePointerCapture(e.pointerId);
    const idx = drag.idx;
    drag = null;
    onElementMove?.(idx, left, top);
  }
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
