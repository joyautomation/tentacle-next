<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getHmiApp, putHmiComponent, listHmiUdts } from '$lib/api/hmi';
  import ClassRail from '$lib/hmi/styles/ClassRail.svelte';
  import ClassEditor from '$lib/hmi/styles/ClassEditor.svelte';
  import HtmlPalette from '$lib/hmi/source/HtmlPalette.svelte';
  import CodeEditor from '$lib/hmi/source/CodeEditor.svelte';
  import SveltePreview from '$lib/hmi/source/SveltePreview.svelte';
  import ContainerEditor from '$lib/hmi/source/ContainerEditor.svelte';
  import ElementEditor from '$lib/hmi/source/ElementEditor.svelte';
  import {
    stripScriptBlock,
    addClassToElement,
    setInlineStyleProps,
    getInlineStyleProps,
    deleteElementAtIndex,
  } from '$lib/hmi/source/markupTools';
  import type { HmiAppConfig, HmiComponentConfig, HmiUdtTemplate, HmiUdtMember } from '$lib/types/hmi';

  const appId = $derived($page.params.appId as string);
  const componentId = $derived($page.params.componentId as string);

  let app = $state<HmiAppConfig | null>(null);
  let template = $state<HmiUdtTemplate | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  let source = $state('');
  let componentName = $state('');
  let udtTemplateName = $state('');
  let classes = $state<Record<string, string>>({});
  let containerProps = $state<Record<string, string>>({});
  let containerCss = $state<string>('');
  let dirty = $state(false);
  let saving = $state(false);
  let saveError = $state<string | null>(null);

  // Preview-pane size (visual only — not persisted with the component).
  let previewWidth = $state<number | null>(null);
  let previewHeight = $state<number | null>(null);
  let snapEnabled = $state(false);
  const SNAP = 16;

  let selectedIdx = $state<number | null>(null);

  // Anchors derived from the selected element's source-side inline style.
  const selectedAnchors = $derived.by<{ x: 'left' | 'right'; y: 'top' | 'bottom' } | null>(() => {
    if (selectedIdx === null) return null;
    const style = getInlineStyleProps(source, selectedIdx);
    if (!style) return null;
    return {
      x: style.right && !style.left ? 'right' : 'left',
      y: style.bottom && !style.top ? 'bottom' : 'top',
    };
  });

  let editorRef: any = $state();

  const members = $derived<HmiUdtMember[]>(template?.members ?? []);
  const inUdtMode = $derived(!!udtTemplateName);
  const prefix = $derived(`cmp-${componentId}`);

  const mockUdt = $derived.by(() => {
    const obj: Record<string, unknown> = {};
    for (const m of members) obj[m.name] = mockValueFor(m.datatype);
    return obj;
  });

  function mockValueFor(datatype: string): unknown {
    const dt = (datatype || '').toLowerCase();
    if (dt.includes('bool')) return false;
    if (dt.includes('string')) return '';
    if (dt.includes('real') || dt.includes('float') || dt.includes('double')) return 0;
    if (dt.includes('int') || dt.includes('word') || dt.includes('byte')) return 0;
    return null;
  }

  const previewProps = $derived<Record<string, unknown>>(inUdtMode ? { udt: mockUdt } : {});
  const scriptHeader = $derived(inUdtMode ? 'let { udt } = $props();' : '');

  async function refresh() {
    loading = true;
    error = null;
    try {
      const r = await getHmiApp(appId);
      if (r.error) {
        error = r.error.error;
        return;
      }
      app = r.data ?? null;
      const c = app?.components?.[componentId];
      if (!c) {
        error = `Component "${componentId}" not found.`;
        return;
      }
      const raw = c.source ?? defaultSource();
      source = stripScriptBlock(raw).markup.trimStart();
      componentName = c.name;
      udtTemplateName = c.udtTemplate ?? '';
      classes = { ...(c.classes ?? {}) };
      containerProps = { ...(c.containerProps ?? {}) };
      containerCss = c.containerCss ?? '';
      // position:fixed traps the editor — strip it on load so the user can
      // recover from a previously-saved value. Marks dirty so the next save
      // persists the cleaned state.
      let migrated = false;
      if (containerProps.position === 'fixed') {
        delete containerProps.position;
        migrated = true;
      }
      if (udtTemplateName) {
        const tr = await listHmiUdts();
        if (!tr.error) {
          template = (tr.data ?? []).find((t) => t.name === udtTemplateName) ?? null;
        }
      }
      dirty = migrated;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      console.error('component refresh failed', e);
    } finally {
      loading = false;
    }
  }

  function defaultSource(): string {
    return '<div>\n  \n</div>\n';
  }

  function onClassDrop(idx: number, name: string) {
    const next = addClassToElement(source, idx, name);
    if (next === null || next === source) return;
    source = next;
    dirty = true;
  }

  function onElementSelect(idx: number | null) {
    selectedIdx = idx;
  }

  function onAnchorChange(
    axis: 'x' | 'y',
    anchor: 'left' | 'right' | 'top' | 'bottom',
  ) {
    if (selectedIdx === null || !previewFrameEl) return;
    const el = previewFrameEl.querySelector<HTMLElement>(`[data-hmi-el="${selectedIdx}"]`);
    const surfaceEl = previewFrameEl.querySelector<HTMLElement>('.surface');
    if (!el || !surfaceEl) return;
    const elRect = el.getBoundingClientRect();
    const surfRect = surfaceEl.getBoundingClientRect();
    const props: Record<string, string | undefined> = {};
    if (axis === 'x') {
      if (anchor === 'left') {
        props.left = `${Math.round(elRect.left - surfRect.left)}px`;
        props.right = undefined;
      } else {
        props.right = `${Math.round(surfRect.right - elRect.right)}px`;
        props.left = undefined;
      }
    } else {
      if (anchor === 'top') {
        props.top = `${Math.round(elRect.top - surfRect.top)}px`;
        props.bottom = undefined;
      } else {
        props.bottom = `${Math.round(surfRect.bottom - elRect.bottom)}px`;
        props.top = undefined;
      }
    }
    const next = setInlineStyleProps(source, selectedIdx, props);
    if (next === null || next === source) return;
    source = next;
    dirty = true;
  }

  function onElementMove(
    idx: number,
    offsets: { left?: number; right?: number; top?: number; bottom?: number },
  ) {
    const props: Record<string, string> = {};
    for (const [k, v] of Object.entries(offsets)) {
      if (v !== undefined) props[k] = `${v}px`;
    }
    const next = setInlineStyleProps(source, idx, props);
    if (next === null || next === source) return;
    source = next;
    dirty = true;
  }

  function onSourceChange(next: string) {
    source = next;
    dirty = true;
  }

  function onClassesChange(next: Record<string, string>) {
    classes = next;
    dirty = true;
  }

  function onContainerChange(next: { props: Record<string, string>; css: string }) {
    containerProps = next.props;
    containerCss = next.css;
    dirty = true;
  }

  function onInsert(snippet: string) {
    editorRef?.insertAtCursor(snippet);
  }

  async function save() {
    saving = true;
    saveError = null;
    const fullSource = scriptHeader
      ? `<script>\n  ${scriptHeader}\n</` + `script>\n\n${source}`
      : source;
    const payload: HmiComponentConfig = {
      componentId,
      name: componentName,
      udtTemplate: udtTemplateName || undefined,
      source: fullSource,
      classes,
      containerProps: Object.keys(containerProps).length ? containerProps : undefined,
      containerCss: containerCss.trim() ? containerCss : undefined,
    };
    const r = await putHmiComponent(appId, componentId, payload);
    if (r.error) saveError = r.error.error;
    else dirty = false;
    saving = false;
  }

  // --- Preview pane resize handles ---
  let previewFrameEl: HTMLDivElement | undefined = $state();

  function startResize(axis: 'x' | 'y' | 'both') {
    return (e: PointerEvent) => {
      if (!previewFrameEl) return;
      const target = e.currentTarget as HTMLElement;
      target.setPointerCapture(e.pointerId);
      const rect = previewFrameEl.getBoundingClientRect();
      const startW = rect.width;
      const startH = rect.height;
      const startX = e.clientX;
      const startY = e.clientY;
      const onMove = (ev: PointerEvent) => {
        if (axis !== 'y') previewWidth = Math.max(120, startW + (ev.clientX - startX));
        if (axis !== 'x') previewHeight = Math.max(80, startH + (ev.clientY - startY));
      };
      const onUp = (ev: PointerEvent) => {
        target.releasePointerCapture(ev.pointerId);
        target.removeEventListener('pointermove', onMove);
        target.removeEventListener('pointerup', onUp);
      };
      target.addEventListener('pointermove', onMove);
      target.addEventListener('pointerup', onUp);
    };
  }

  function resetSize() {
    previewWidth = null;
    previewHeight = null;
  }

  onMount(refresh);

  function deleteSelected() {
    if (selectedIdx === null) return;
    const next = deleteElementAtIndex(source, selectedIdx);
    if (next === null || next === source) return;
    source = next;
    selectedIdx = null;
    dirty = true;
  }

  function isEditableTarget(t: EventTarget | null): boolean {
    if (!(t instanceof HTMLElement)) return false;
    const tag = t.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
    if (t.isContentEditable) return true;
    // CodeMirror's editable surface is a contenteditable; the wrapper isn't,
    // so also bail if we're anywhere inside the code editor.
    if (t.closest('.cm-editor')) return true;
    return false;
  }

  function onKeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      e.preventDefault();
      if (dirty && !saving) save();
      return;
    }
    if (
      (e.key === 'Delete' || e.key === 'Backspace') &&
      selectedIdx !== null &&
      !isEditableTarget(e.target)
    ) {
      e.preventDefault();
      deleteSelected();
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

<svelte:head>
  <title>{componentName || componentId} · {app?.name ?? appId} · Component</title>
</svelte:head>

<section class="designer">
  <header class="topbar">
    <div class="left">
      <a href="/hmi/designer/{encodeURIComponent(appId)}" class="back">&larr; {app?.name ?? appId}</a>
      <h1>{componentName || componentId}</h1>
      <span class="meta">
        {#if udtTemplateName}bound to <code>{udtTemplateName}</code> · {/if}
        {dirty ? 'unsaved' : 'saved'}
      </span>
    </div>
    <div class="right">
      {#if saveError}<span class="save-error">{saveError}</span>{/if}
      <button class="save" onclick={save} disabled={!dirty || saving}>
        {saving ? 'Saving…' : dirty ? 'Save' : 'Saved'}
      </button>
    </div>
  </header>

  {#if error}<div class="banner error">{error}</div>{/if}

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if !app?.components?.[componentId]}
    <p class="muted">Component not found.</p>
  {:else}
    <div class="workspace">
      <HtmlPalette {onInsert} />
      <div class="center">
        <div class="preview-pane">
          <div class="preview-toolbar">
            <label class="snap">
              <input type="checkbox" bind:checked={snapEnabled} />
              snap to grid ({SNAP}px)
            </label>
            <span class="size-readout">
              {previewWidth ? `${Math.round(previewWidth)}px` : 'fluid'}
              ×
              {previewHeight ? `${Math.round(previewHeight)}px` : 'fluid'}
            </span>
            <button class="reset" onclick={resetSize} disabled={previewWidth === null && previewHeight === null}>
              reset
            </button>
          </div>
          <div class="preview-frame-wrap">
            <div
              class="preview-frame"
              bind:this={previewFrameEl}
              style:width={previewWidth ? `${previewWidth}px` : '100%'}
              style:height={previewHeight ? `${previewHeight}px` : '100%'}
            >
              <SveltePreview
                {source}
                {scriptHeader}
                props={previewProps}
                appClasses={app?.classes}
                componentClasses={classes}
                {prefix}
                {containerProps}
                {containerCss}
                snapGrid={snapEnabled ? SNAP : 0}
                {selectedIdx}
                {onClassDrop}
                {onElementMove}
                {onElementSelect}
              />
              <div
                class="resize-handle x"
                onpointerdown={startResize('x')}
                role="separator"
                aria-orientation="vertical"
              ></div>
              <div
                class="resize-handle y"
                onpointerdown={startResize('y')}
                role="separator"
                aria-orientation="horizontal"
              ></div>
              <div
                class="resize-handle both"
                onpointerdown={startResize('both')}
                role="separator"
              ></div>
            </div>
          </div>
        </div>
        <div class="editor-pane">
          <CodeEditor
            bind:this={editorRef}
            value={source}
            onChange={onSourceChange}
            placeholder={inUdtMode
              ? '<div>\n  <span>{udt.member}</span>\n</div>'
              : '<div>...</div>'}
          />
        </div>
      </div>
      <div class="right-rail">
        {#if inUdtMode}
          <details class="udt-info" open>
            <summary>UDT members</summary>
            <p class="hint">Use <code>udt.&lt;member&gt;</code> in your markup.</p>
            <ul>
              {#each members as m (m.name)}
                <li><code>udt.{m.name}</code> <span class="dt">{m.datatype}</span></li>
              {/each}
            </ul>
          </details>
        {/if}
        <ContainerEditor
          props={containerProps}
          css={containerCss}
          onChange={onContainerChange}
        />
        <ElementEditor
          idx={selectedIdx}
          anchors={selectedAnchors}
          {onAnchorChange}
          onClear={() => (selectedIdx = null)}
        />
        <ClassRail
          title="App classes"
          classes={app?.classes}
          accent="app"
          editHref="/hmi/designer/{encodeURIComponent(appId)}/styles"
        />
        <ClassEditor
          {classes}
          onChange={onClassesChange}
          title="Component classes"
          accent="component"
        />
      </div>
    </div>
  {/if}
</section>

<style lang="scss">
  .designer { display: flex; flex-direction: column; height: 100vh; background: var(--theme-background); }
  .topbar {
    display: flex; justify-content: space-between; align-items: center;
    padding: 0.625rem 1rem;
    border-bottom: 1px solid var(--theme-border);
    background: var(--theme-surface);
    .left { display: flex; align-items: baseline; gap: 0.75rem; }
    .right { display: flex; align-items: center; gap: 0.5rem; }
  }
  .back { color: var(--theme-text-muted); text-decoration: none; font-size: 0.8125rem; &:hover { color: var(--theme-text); } }
  h1 { margin: 0; font-size: 1rem; color: var(--theme-text); font-family: 'Righteous', sans-serif; }
  .meta { color: var(--theme-text-muted); font-size: 0.75rem; code { font-family: 'IBM Plex Mono', monospace; } }
  .save-error { color: #ef4444; font-size: 0.75rem; }
  .save {
    background: var(--theme-text); color: var(--theme-background);
    border: 1px solid var(--theme-text);
    padding: 0.375rem 0.875rem;
    border-radius: var(--rounded-md);
    font-size: 0.8125rem;
    cursor: pointer;
    &:disabled { opacity: 0.5; cursor: default; }
  }
  .workspace { flex: 1; display: flex; min-height: 0; overflow: hidden; }
  .center {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    padding: 0.5rem;
    gap: 0.5rem;
  }
  .preview-pane {
    flex: 1 1 50%;
    min-height: 8rem;
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    min-height: 0;
  }
  .preview-toolbar {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0 0.125rem;
    .snap {
      display: inline-flex;
      align-items: center;
      gap: 0.375rem;
      font-size: 0.75rem;
      color: var(--theme-text-muted);
      cursor: pointer;
      input { accent-color: var(--theme-text); }
    }
    .size-readout {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.6875rem;
      color: var(--theme-text-muted);
    }
    .reset {
      margin-left: auto;
      background: transparent;
      border: 1px solid var(--theme-border);
      color: var(--theme-text-muted);
      padding: 0.125rem 0.5rem;
      border-radius: var(--rounded-sm, 4px);
      font-size: 0.6875rem;
      cursor: pointer;
      &:disabled { opacity: 0.4; cursor: default; }
      &:not(:disabled):hover { color: var(--theme-text); border-color: var(--theme-text); }
    }
  }
  .preview-frame-wrap {
    flex: 1;
    min-height: 0;
    overflow: auto;
    position: relative;
  }
  .preview-frame {
    position: relative;
    max-width: 100%;
    max-height: 100%;
  }
  .resize-handle {
    position: absolute;
    background: transparent;
    z-index: 5;
    &:hover { background: color-mix(in srgb, var(--theme-text) 20%, transparent); }
    &.x { top: 0; right: 0; bottom: 0; width: 6px; cursor: ew-resize; }
    &.y { left: 0; right: 0; bottom: 0; height: 6px; cursor: ns-resize; }
    &.both {
      right: 0; bottom: 0; width: 12px; height: 12px;
      cursor: nwse-resize;
      background: var(--theme-text);
      opacity: 0.4;
      border-radius: 2px;
      &:hover { opacity: 1; }
    }
  }
  .editor-pane {
    flex: 1 1 50%;
    min-height: 10rem;
  }
  .right-rail {
    width: 18rem;
    flex-shrink: 0;
    border-left: 1px solid var(--theme-border);
    background: var(--theme-surface);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.5rem;
  }
  .udt-info {
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    padding: 0.5rem 0.625rem;
    font-size: 0.75rem;
    summary {
      cursor: pointer;
      color: var(--theme-text-muted);
      text-transform: uppercase;
      letter-spacing: 0.06em;
      font-size: 0.6875rem;
    }
    .hint {
      margin: 0.5rem 0 0.25rem;
      color: var(--theme-text-muted);
      code { font-family: 'IBM Plex Mono', monospace; }
    }
    ul { margin: 0; padding: 0 0 0 0.75rem; }
    li {
      list-style: none;
      padding: 0.125rem 0;
      color: var(--theme-text);
      code { font-family: 'IBM Plex Mono', monospace; }
      .dt { color: var(--theme-text-muted); margin-left: 0.5rem; font-size: 0.6875rem; }
    }
  }
  .banner.error {
    margin: 0.75rem 1rem;
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .muted { color: var(--theme-text-muted); padding: 1.5rem; }
</style>
