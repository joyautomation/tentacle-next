<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import {
    getHmiApp,
    createHmiScreen,
    createHmiComponent,
    deleteHmiScreen,
    deleteHmiComponent,
    putHmiScreen,
    listHmiUdts,
  } from '$lib/api/hmi';
  import { api } from '$lib/api/client';
  import type { HmiAppConfig, HmiUdtTemplate, HmiWidget } from '$lib/types/hmi';

  const appId = $derived($page.params.appId as string);
  let app = $state<HmiAppConfig | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let newScreenName = $state('');
  let creating = $state(false);
  let seeding = $state(false);
  let udtTemplates = $state<HmiUdtTemplate[]>([]);
  let newComponentName = $state('');
  let newComponentTemplate = $state('');
  let creatingComponent = $state(false);

  async function refresh() {
    loading = true;
    error = null;
    const [r, ur] = await Promise.all([getHmiApp(appId), listHmiUdts()]);
    if (r.error) error = r.error.error;
    else app = r.data ?? null;
    if (ur.data) udtTemplates = ur.data;
    loading = false;
  }

  async function handleCreateScreen(e: SubmitEvent) {
    e.preventDefault();
    if (!newScreenName.trim()) return;
    creating = true;
    const r = await createHmiScreen(appId, { name: newScreenName.trim() });
    creating = false;
    if (r.error) {
      error = r.error.error;
      return;
    }
    newScreenName = '';
    await refresh();
  }

  async function handleDeleteScreen(screenId: string) {
    if (!confirm(`Delete screen "${screenId}"?`)) return;
    const r = await deleteHmiScreen(appId, screenId);
    if (r.error) {
      error = r.error.error;
      return;
    }
    await refresh();
  }

  async function handleDeleteComponent(componentId: string) {
    if (!confirm(`Delete component "${componentId}"?`)) return;
    const r = await deleteHmiComponent(appId, componentId);
    if (r.error) {
      error = r.error.error;
      return;
    }
    await refresh();
  }

  async function handleCreateComponent(e: SubmitEvent) {
    e.preventDefault();
    if (!newComponentName.trim()) return;
    creatingComponent = true;
    const r = await createHmiComponent(appId, {
      name: newComponentName.trim(),
      udtTemplate: newComponentTemplate || undefined,
    });
    creatingComponent = false;
    if (r.error) {
      error = r.error.error;
      return;
    }
    const created = r.data;
    newComponentName = '';
    newComponentTemplate = '';
    await refresh();
    if (created?.componentId) {
      goto(`/hmi/designer/${encodeURIComponent(appId)}/components/${encodeURIComponent(created.componentId)}`);
    }
  }

  /**
   * Seed a demo screen so users can immediately verify live data flow.
   * Picks the first available gateway variable and binds a Label, NumericDisplay,
   * and Indicator to it.
   */
  async function seedDemo() {
    seeding = true;
    error = null;
    try {
      const r = await api<Array<{ id: string; moduleId?: string; variableId?: string; datatype?: string }>>(
        '/variables'
      );
      const vars = r.data ?? [];
      if (vars.length === 0) {
        error = 'No gateway variables are available yet — configure a gateway first.';
        return;
      }
      const v = vars[0] as any;
      const gateway = v.moduleId ?? v.ModuleID ?? '';
      const variable = v.variableId ?? v.VariableID ?? v.id ?? '';
      if (!gateway || !variable) {
        error = 'Variable did not include moduleId/variableId — aborted.';
        return;
      }
      const screenId = 'demo';
      const widgets: HmiWidget[] = [
        {
          id: 'lbl1',
          type: 'label',
          x: 24, y: 24, w: 480, h: 40,
          props: { text: `Live demo · ${gateway}/${variable}`, size: 'lg', weight: 'bold' },
        },
        {
          id: 'num1',
          type: 'numeric',
          x: 24, y: 88, w: 220, h: 96,
          props: { label: variable, precision: 2 },
          bindings: { value: { kind: 'variable', gateway, variable } },
        },
        {
          id: 'ind1',
          type: 'indicator',
          x: 264, y: 88, w: 220, h: 48,
          props: { label: 'Has value' },
          bindings: { value: { kind: 'variable', gateway, variable } },
        },
        {
          id: 'bar1',
          type: 'bar',
          x: 24, y: 200, w: 460, h: 88,
          props: { label: variable, min: 0, max: 100 },
          bindings: { value: { kind: 'variable', gateway, variable } },
        },
      ];
      const put = await putHmiScreen(appId, screenId, {
        name: 'Demo',
        width: 0,
        height: 360,
        widgets,
      });
      if (put.error) {
        error = put.error.error;
        return;
      }
      await refresh();
      goto(`/hmi/designer/${encodeURIComponent(appId)}/screens/${screenId}`);
    } finally {
      seeding = false;
    }
  }

  onMount(refresh);

  const screens = $derived(app ? Object.values(app.screens ?? {}) : []);
  const components = $derived(app ? Object.values(app.components ?? {}) : []);
</script>

<svelte:head>
  <title>{app?.name ?? appId} · HMI</title>
</svelte:head>

<section class="page">
  <header class="page-header">
    <div>
      <a href="/services/hmi/apps" class="back">&larr; HMI Apps</a>
      {#if app}
        <h1>{app.name}</h1>
        <p class="subtitle">{app.appId}{app.description ? ` · ${app.description}` : ''}</p>
      {:else}
        <h1>{appId}</h1>
      {/if}
    </div>
    <div class="header-actions">
      <a href="/hmi/{encodeURIComponent(appId)}" class="link-btn">▶ Run app</a>
      <a href="/hmi/designer/{encodeURIComponent(appId)}/styles" class="link-btn">App styles</a>
      <a href="/services/hmi/udts" class="link-btn">UDT Browser &rarr;</a>
      <button class="primary" onclick={seedDemo} disabled={seeding}>
        {seeding ? 'Seeding…' : 'Seed demo screen'}
      </button>
    </div>
  </header>

  {#if error}
    <div class="banner error">{error}</div>
  {/if}

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if !app}
    <p class="muted">App not found.</p>
  {:else}
    <section class="block">
      <header class="block-header">
        <h2>Screens</h2>
      </header>
      <form class="inline-form" onsubmit={handleCreateScreen}>
        <input
          type="text"
          placeholder="New screen name"
          bind:value={newScreenName}
          disabled={creating}
        />
        <button type="submit" disabled={creating || !newScreenName.trim()}>
          {creating ? 'Creating…' : 'Add screen'}
        </button>
      </form>
      {#if screens.length === 0}
        <p class="muted">No screens yet. Add one above, or click "Seed demo screen" to create one bound to a real variable.</p>
      {:else}
        <ul class="card-list">
          {#each screens as scr (scr.screenId)}
            <li class="card">
              <a href="/hmi/designer/{encodeURIComponent(appId)}/screens/{encodeURIComponent(scr.screenId)}">
                <div class="card-name">{scr.name}</div>
                <div class="card-id">{scr.screenId}</div>
                <div class="card-meta">{scr.widgets?.length ?? 0} widgets</div>
              </a>
              <button class="del" onclick={() => handleDeleteScreen(scr.screenId)} title="Delete">×</button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="block">
      <header class="block-header">
        <h2>Components</h2>
      </header>
      <form class="inline-form" onsubmit={handleCreateComponent}>
        <input
          type="text"
          placeholder="New component name"
          bind:value={newComponentName}
          disabled={creatingComponent}
        />
        <select bind:value={newComponentTemplate} disabled={creatingComponent}>
          <option value="">freeform (no UDT)</option>
          {#each udtTemplates as t (t.name)}
            <option value={t.name}>bind to UDT: {t.name}</option>
          {/each}
        </select>
        <button type="submit" disabled={creatingComponent || !newComponentName.trim()}>
          {creatingComponent ? 'Creating…' : 'Add component'}
        </button>
      </form>
      {#if components.length === 0}
        <p class="muted">No components yet. Add one above — pick a UDT to make member bindings reusable across instances.</p>
      {:else}
        <ul class="card-list">
          {#each components as c (c.componentId)}
            <li class="card">
              <a href="/hmi/designer/{encodeURIComponent(appId)}/components/{encodeURIComponent(c.componentId)}">
                <div class="card-name">{c.name}</div>
                <div class="card-id">{c.componentId}</div>
                <div class="card-meta">
                  {#if c.udtTemplate}
                    bound to <code>{c.udtTemplate}</code>
                  {:else}
                    freeform
                  {/if}
                  · {c.widgets?.length ?? 0} widgets
                </div>
              </a>
              <button class="del" onclick={() => handleDeleteComponent(c.componentId)} title="Delete">×</button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
</section>

<style lang="scss">
  .page { max-width: 64rem; margin: 0 auto; padding: 2rem 1.5rem; display: flex; flex-direction: column; gap: 2rem; }
  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    gap: 1rem;
    .back { color: var(--theme-text-muted); text-decoration: none; font-size: 0.875rem; }
    h1 { margin: 0.25rem 0 0; font-family: 'Righteous', sans-serif; color: var(--theme-text); }
    .subtitle { margin: 0.125rem 0 0; color: var(--theme-text-muted); font-family: 'IBM Plex Mono', monospace; font-size: 0.8125rem; }
  }
  .header-actions { display: flex; gap: 0.5rem; align-items: center; }
  .link-btn {
    text-decoration: none;
    color: var(--theme-text);
    padding: 0.5rem 0.875rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
    &.small { padding: 0.25rem 0.625rem; font-size: 0.8125rem; }
  }
  button.primary {
    padding: 0.5rem 1rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-text);
    color: var(--theme-background);
    cursor: pointer;
    font-family: inherit;
    font-weight: 600;
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }
  .banner.error {
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .block { display: flex; flex-direction: column; gap: 0.75rem; }
  .block-header {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    h2 { margin: 0; font-size: 1.125rem; font-weight: 600; color: var(--theme-text); }
  }
  .inline-form {
    display: flex; gap: 0.5rem;
    input, select {
      padding: 0.5rem 0.75rem;
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-md);
      background: var(--theme-background);
      color: var(--theme-text);
      font-family: inherit;
    }
    input { flex: 1; }
    button {
      padding: 0.5rem 1rem;
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-md);
      background: var(--theme-surface);
      color: var(--theme-text);
      cursor: pointer;
      font-family: inherit;
      &:disabled { opacity: 0.5; cursor: not-allowed; }
    }
  }
  .card-list {
    list-style: none; margin: 0; padding: 0;
    display: grid; grid-template-columns: repeat(auto-fill, minmax(16rem, 1fr));
    gap: 0.75rem;
  }
  .card {
    position: relative;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    transition: border-color 0.15s;
    &:hover { border-color: var(--theme-text); }
    a, .card-body {
      display: block;
      padding: 0.875rem 1rem;
      text-decoration: none;
      color: var(--theme-text);
    }
  }
  .card-name { font-weight: 600; }
  .card-id { font-family: 'IBM Plex Mono', monospace; font-size: 0.75rem; color: var(--theme-text-muted); margin-top: 0.125rem; }
  .card-meta { margin-top: 0.5rem; font-size: 0.8125rem; color: var(--theme-text-muted); code { font-family: 'IBM Plex Mono', monospace; } }
  .del {
    position: absolute;
    top: 0.375rem;
    right: 0.375rem;
    width: 1.5rem;
    height: 1.5rem;
    line-height: 1;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 50%;
    color: var(--theme-text-muted);
    cursor: pointer;
    &:hover { background: rgba(239, 68, 68, 0.1); border-color: rgba(239, 68, 68, 0.4); color: #ef4444; }
  }
  .muted { color: var(--theme-text-muted); }
</style>
