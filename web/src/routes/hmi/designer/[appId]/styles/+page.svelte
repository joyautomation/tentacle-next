<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getHmiApp, putHmiApp } from '$lib/api/hmi';
  import ClassEditor from '$lib/hmi/styles/ClassEditor.svelte';
  import { compileScopedCss } from '$lib/hmi/styles/cssScope';
  import type { HmiAppConfig } from '$lib/types/hmi';

  const appId = $derived($page.params.appId as string);
  let app = $state<HmiAppConfig | null>(null);
  let classes = $state<Record<string, string>>({});
  let loading = $state(true);
  let error = $state<string | null>(null);
  let dirty = $state(false);
  let saving = $state(false);
  let saveError = $state<string | null>(null);

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
      classes = { ...(app?.classes ?? {}) };
      dirty = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function onClassesChange(next: Record<string, string>) {
    classes = next;
    dirty = true;
  }

  async function save() {
    if (!app) return;
    saving = true;
    saveError = null;
    const next: HmiAppConfig = { ...app, classes };
    const r = await putHmiApp(appId, next);
    if (r.error) saveError = r.error.error;
    else {
      app = r.data ?? next;
      dirty = false;
    }
    saving = false;
  }

  // Live preview block — emits the same scoped CSS the runtime will produce
  // for app-level classes.
  const previewCss = $derived(compileScopedCss(classes, ''));

  onMount(refresh);
</script>

<svelte:head>
  <title>App styles · {app?.name ?? appId}</title>
</svelte:head>

<section class="page">
  <header class="page-header">
    <div>
      <a href="/hmi/designer/{encodeURIComponent(appId)}" class="back">&larr; {app?.name ?? appId}</a>
      <h1>App styles</h1>
      <p class="subtitle">CSS classes available to every screen and component in this app.</p>
    </div>
    <div class="actions">
      {#if saveError}<span class="save-error">{saveError}</span>{/if}
      <button class="save" onclick={save} disabled={!dirty || saving}>
        {saving ? 'Saving…' : dirty ? 'Save' : 'Saved'}
      </button>
    </div>
  </header>

  {#if error}<div class="banner error">{error}</div>{/if}

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if !app}
    <p class="muted">App not found.</p>
  {:else}
    <div class="grid">
      <div class="left">
        <ClassEditor {classes} onChange={onClassesChange} title="App classes" accent="app" />
      </div>
      <div class="right">
        <h3>Compiled CSS</h3>
        <pre class="preview">{previewCss || '/* (no classes yet) */'}</pre>
        <p class="muted small">
          Drop a class chip onto a widget on any screen or component to apply it.
          App classes are emitted once at the runtime root.
        </p>
      </div>
    </div>
  {/if}
</section>

<style lang="scss">
  .page { max-width: 64rem; margin: 0 auto; padding: 2rem 1.5rem; display: flex; flex-direction: column; gap: 1.5rem; }
  .page-header {
    display: flex; justify-content: space-between; align-items: flex-end; gap: 1rem;
    .back { color: var(--theme-text-muted); text-decoration: none; font-size: 0.875rem; }
    h1 { margin: 0.25rem 0 0; font-family: 'Righteous', sans-serif; color: var(--theme-text); }
    .subtitle { margin: 0.125rem 0 0; color: var(--theme-text-muted); font-size: 0.875rem; }
  }
  .actions { display: flex; gap: 0.5rem; align-items: center; }
  .save-error { color: #ef4444; font-size: 0.75rem; }
  .save {
    background: var(--theme-text); color: var(--theme-background);
    border: 1px solid var(--theme-text);
    padding: 0.5rem 1rem;
    border-radius: var(--rounded-md);
    font-family: inherit;
    cursor: pointer;
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }
  .banner.error {
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .grid {
    display: grid;
    grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
    gap: 1.25rem;
    align-items: start;
    @media (max-width: 56rem) { grid-template-columns: 1fr; }
  }
  .right h3 {
    margin: 0 0 0.5rem;
    font-size: 0.6875rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
  }
  .preview {
    margin: 0;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text);
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    padding: 0.75rem;
    overflow: auto;
    max-height: 24rem;
    line-height: 1.5;
  }
  .muted { color: var(--theme-text-muted); &.small { font-size: 0.75rem; margin-top: 0.5rem; } }
</style>
