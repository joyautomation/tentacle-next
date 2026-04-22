<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getHmiApp, putHmiComponent, listHmiUdts } from '$lib/api/hmi';
  import Palette from '$lib/hmi/designer/Palette.svelte';
  import DesignerCanvas from '$lib/hmi/designer/DesignerCanvas.svelte';
  import Inspector from '$lib/hmi/designer/Inspector.svelte';
  import type { HmiAppConfig, HmiComponentConfig, HmiUdtTemplate, HmiUdtMember, HmiWidget } from '$lib/types/hmi';

  const appId = $derived($page.params.appId as string);
  const componentId = $derived($page.params.componentId as string);

  let app = $state<HmiAppConfig | null>(null);
  let template = $state<HmiUdtTemplate | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  let widgets = $state<HmiWidget[]>([]);
  let componentName = $state('');
  let udtTemplateName = $state('');
  let width = $state(0);
  let height = $state(400);
  let selectedId = $state<string | null>(null);
  let dirty = $state(false);
  let saving = $state(false);
  let saveError = $state<string | null>(null);

  const selectedWidget = $derived<HmiWidget | null>(
    widgets.find((w) => w.id === selectedId) ?? null
  );

  const members = $derived<HmiUdtMember[]>(template?.members ?? []);
  const inUdtMode = $derived(!!udtTemplateName);

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
      widgets = (c.widgets ?? []).map((w) => ({
        ...w,
        props: { ...(w.props ?? {}) },
        bindings: { ...(w.bindings ?? {}) },
      }));
      componentName = c.name;
      udtTemplateName = c.udtTemplate ?? '';
      width = c.width ?? 0;
      height = c.height ?? 400;
      if (udtTemplateName) {
        const tr = await listHmiUdts();
        if (!tr.error) {
          template = (tr.data ?? []).find((t) => t.name === udtTemplateName) ?? null;
        }
      }
      dirty = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      console.error('component refresh failed', e);
    } finally {
      loading = false;
    }
  }

  function onWidgetsChange(next: HmiWidget[]) { widgets = next; dirty = true; }
  function onWidgetChange(updated: HmiWidget) {
    widgets = widgets.map((w) => (w.id === updated.id ? updated : w));
    dirty = true;
  }
  function onWidgetDelete() {
    if (!selectedId) return;
    widgets = widgets.filter((w) => w.id !== selectedId);
    selectedId = null;
    dirty = true;
  }

  async function save() {
    saving = true;
    saveError = null;
    const payload: HmiComponentConfig = {
      componentId,
      name: componentName,
      udtTemplate: udtTemplateName || undefined,
      width: width || undefined,
      height: height || undefined,
      widgets,
    };
    const r = await putHmiComponent(appId, componentId, payload);
    if (r.error) saveError = r.error.error;
    else dirty = false;
    saving = false;
  }

  onMount(refresh);
</script>

<svelte:head>
  <title>{componentName || componentId} · {app?.name ?? appId} · Component</title>
</svelte:head>

<section class="designer">
  <header class="topbar">
    <div class="left">
      <a href="/hmi/designer/{encodeURIComponent(appId)}" class="back">&larr; {app?.name ?? appId}</a>
      <h1>{componentName || componentId}</h1>
      <span class="meta">
        {widgets.length} widgets
        {#if udtTemplateName}· bound to <code>{udtTemplateName}</code>{/if}
        {dirty ? ' · unsaved' : ''}
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
      <Palette excludeTypes={['componentInstance']} />
      <DesignerCanvas
        {widgets}
        {selectedId}
        {width}
        {height}
        onChange={onWidgetsChange}
        onSelect={(id) => (selectedId = id)}
      />
      <Inspector
        widget={selectedWidget}
        onChange={onWidgetChange}
        onDelete={onWidgetDelete}
        udtMembers={inUdtMode ? members : undefined}
      />
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
