<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getHmiApp, putHmiScreen } from '$lib/api/hmi';
  import Palette from '$lib/hmi/designer/Palette.svelte';
  import DesignerCanvas from '$lib/hmi/designer/DesignerCanvas.svelte';
  import Inspector from '$lib/hmi/designer/Inspector.svelte';
  import ClassRail from '$lib/hmi/styles/ClassRail.svelte';
  import { findWidget, findParent, removeWidget, replaceWidget, schemaByType } from '$lib/hmi/widgetSchema';
  import type { HmiAppConfig, HmiScreenConfig, HmiWidget, HmiComponentConfig } from '$lib/types/hmi';

  const appId = $derived($page.params.appId as string);
  const screenId = $derived($page.params.screenId as string);

  let app = $state<HmiAppConfig | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  let widgets = $state<HmiWidget[]>([]);
  let screenName = $state('');
  let screenWidth = $state(0);
  let screenHeight = $state(600);
  let selectedId = $state<string | null>(null);
  let dirty = $state(false);
  let saving = $state(false);
  let saveError = $state<string | null>(null);

  const selectedWidget = $derived<HmiWidget | null>(
    selectedId ? findWidget(widgets, selectedId) ?? null : null
  );
  const selectedParent = $derived<HmiWidget | undefined>(
    selectedId ? findParent(widgets, selectedId) : undefined
  );
  const parentIsContainer = $derived(!!(selectedParent && schemaByType[selectedParent.type]?.isContainer));
  const componentList = $derived<HmiComponentConfig[]>(app ? Object.values(app.components ?? {}) : []);
  const componentMap = $derived<Record<string, HmiComponentConfig>>(app?.components ?? {});

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
      const screen = app?.screens?.[screenId];
      if (screen) {
        widgets = (screen.widgets ?? []).map((w) => ({
          ...w,
          props: { ...(w.props ?? {}) },
          bindings: { ...(w.bindings ?? {}) },
        }));
        screenName = screen.name;
        screenWidth = screen.width ?? 0;
        screenHeight = screen.height ?? 600;
      }
      dirty = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      console.error('designer refresh failed', e);
    } finally {
      loading = false;
    }
  }

  function onWidgetsChange(next: HmiWidget[]) {
    widgets = next;
    dirty = true;
  }

  function onWidgetChange(updated: HmiWidget) {
    widgets = replaceWidget(widgets, updated);
    dirty = true;
  }

  function onWidgetDelete() {
    if (!selectedId) return;
    widgets = removeWidget(widgets, selectedId);
    selectedId = null;
    dirty = true;
  }

  async function save() {
    saving = true;
    saveError = null;
    const payload: Omit<HmiScreenConfig, 'screenId'> = {
      name: screenName,
      width: screenWidth || undefined,
      height: screenHeight || undefined,
      widgets,
    };
    const r = await putHmiScreen(appId, screenId, payload);
    if (r.error) saveError = r.error.error;
    else dirty = false;
    saving = false;
  }

  onMount(refresh);
</script>

<svelte:head>
  <title>{screenName || screenId} · {app?.name ?? appId} · Designer</title>
</svelte:head>

<section class="designer">
  <header class="topbar">
    <div class="left">
      <a href="/hmi/designer/{encodeURIComponent(appId)}" class="back">&larr; {app?.name ?? appId}</a>
      <h1>{screenName || screenId}</h1>
      <span class="meta">{widgets.length} widgets{dirty ? ' · unsaved' : ''}</span>
    </div>
    <div class="right">
      {#if saveError}<span class="save-error">{saveError}</span>{/if}
      <a class="run" href="/hmi/{encodeURIComponent(appId)}/screens/{encodeURIComponent(screenId)}" target="_blank" rel="noopener">▶ Preview</a>
      <button class="save" onclick={save} disabled={!dirty || saving}>
        {saving ? 'Saving…' : dirty ? 'Save' : 'Saved'}
      </button>
    </div>
  </header>

  {#if error}
    <div class="banner error">{error}</div>
  {/if}

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if !app?.screens?.[screenId]}
    <p class="muted">Screen not found.</p>
  {:else}
    <div class="workspace">
      <Palette components={componentList} />
      <DesignerCanvas
        {widgets}
        {selectedId}
        width={screenWidth}
        height={screenHeight}
        onChange={onWidgetsChange}
        onSelect={(id) => (selectedId = id)}
        components={componentMap}
        appClasses={app?.classes}
      />
      <div class="right-rail">
        <ClassRail
          title="App classes"
          classes={app?.classes}
          accent="app"
          editHref="/hmi/designer/{encodeURIComponent(appId)}/styles"
        />
        <Inspector
          widget={selectedWidget}
          onChange={onWidgetChange}
          onDelete={onWidgetDelete}
          components={componentList}
          {parentIsContainer}
        />
      </div>
    </div>
  {/if}
</section>

<style lang="scss">
  .designer {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: var(--theme-background);
  }
  .topbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.625rem 1rem;
    border-bottom: 1px solid var(--theme-border);
    background: var(--theme-surface);
    .left { display: flex; align-items: baseline; gap: 0.75rem; }
    .right { display: flex; align-items: center; gap: 0.5rem; }
  }
  .back { color: var(--theme-text-muted); text-decoration: none; font-size: 0.8125rem; &:hover { color: var(--theme-text); } }
  h1 { margin: 0; font-size: 1rem; color: var(--theme-text); font-family: 'Righteous', sans-serif; }
  .meta { color: var(--theme-text-muted); font-size: 0.75rem; }
  .save-error { color: #ef4444; font-size: 0.75rem; }
  .run {
    color: var(--theme-text);
    text-decoration: none;
    padding: 0.375rem 0.75rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    font-size: 0.8125rem;
    &:hover { border-color: var(--theme-text); }
  }
  .save {
    background: var(--theme-text);
    color: var(--theme-background);
    border: 1px solid var(--theme-text);
    padding: 0.375rem 0.875rem;
    border-radius: var(--rounded-md);
    font-size: 0.8125rem;
    cursor: pointer;
    &:disabled { opacity: 0.5; cursor: default; }
  }
  .workspace {
    flex: 1;
    display: flex;
    min-height: 0;
    overflow: hidden;
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
  .right-rail :global(.inspector) {
    width: 100%;
    border-left: none;
    padding: 0;
    background: transparent;
    overflow: visible;
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
