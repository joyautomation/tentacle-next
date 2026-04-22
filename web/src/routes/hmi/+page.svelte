<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { listHmiApps, createHmiApp, deleteHmiApp } from '$lib/api/hmi';
  import type { HmiAppConfig } from '$lib/types/hmi';

  let apps = $state<HmiAppConfig[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let creating = $state(false);
  let newName = $state('');

  async function refresh() {
    loading = true;
    error = null;
    const r = await listHmiApps();
    if (r.error) error = r.error.error;
    else apps = r.data ?? [];
    loading = false;
  }

  async function handleCreate(e: SubmitEvent) {
    e.preventDefault();
    if (!newName.trim()) return;
    creating = true;
    const r = await createHmiApp({ name: newName.trim() });
    creating = false;
    if (r.error) {
      error = r.error.error;
      return;
    }
    newName = '';
    await refresh();
    if (r.data) goto(`/hmi/${encodeURIComponent(r.data.appId)}`);
  }

  async function handleDelete(appId: string) {
    if (!confirm(`Delete HMI app "${appId}"? This cannot be undone.`)) return;
    const r = await deleteHmiApp(appId);
    if (r.error) {
      error = r.error.error;
      return;
    }
    await refresh();
  }

  onMount(refresh);

  function screenCount(app: HmiAppConfig): number {
    return Object.keys(app.screens ?? {}).length;
  }
  function componentCount(app: HmiAppConfig): number {
    return Object.keys(app.components ?? {}).length;
  }
</script>

<svelte:head>
  <title>HMI Apps · Tentacle</title>
</svelte:head>

<section class="page">
  <header class="page-header">
    <div>
      <h1>HMI Apps</h1>
      <p class="subtitle">Build screens against gateway tags and UDTs.</p>
    </div>
    <a href="/hmi/udts" class="link-btn">Browse UDTs &rarr;</a>
  </header>

  {#if error}
    <div class="banner error">{error}</div>
  {/if}

  <form class="create-form" onsubmit={handleCreate}>
    <input
      type="text"
      placeholder="New app name (e.g. Plant Floor)"
      bind:value={newName}
      disabled={creating}
    />
    <button type="submit" disabled={creating || !newName.trim()}>
      {creating ? 'Creating…' : 'Create app'}
    </button>
  </form>

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if apps.length === 0}
    <div class="empty">
      <p>No HMI apps yet. Create one above to get started.</p>
    </div>
  {:else}
    <ul class="app-list">
      {#each apps as app (app.appId)}
        <li class="app-card">
          <a href="/hmi/{encodeURIComponent(app.appId)}" class="app-link">
            <div class="app-name">{app.name}</div>
            <div class="app-id">{app.appId}</div>
            {#if app.description}
              <div class="app-desc">{app.description}</div>
            {/if}
            <div class="app-counts">
              {screenCount(app)} screens · {componentCount(app)} components
            </div>
          </a>
          <button class="delete-btn" onclick={() => handleDelete(app.appId)} title="Delete">
            ×
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style lang="scss">
  .page {
    max-width: 64rem;
    margin: 0 auto;
    padding: 2rem 1.5rem;
  }
  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    margin-bottom: 1.5rem;
    h1 { margin: 0; font-family: 'Righteous', sans-serif; color: var(--theme-text); }
    .subtitle { margin: 0.25rem 0 0; color: var(--theme-text-muted); }
  }
  .link-btn {
    text-decoration: none;
    color: var(--theme-text);
    padding: 0.5rem 0.875rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
    transition: background 0.15s;
    &:hover { background: var(--theme-surface-hover, var(--theme-border)); }
  }
  .banner.error {
    margin-bottom: 1rem;
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .create-form {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 2rem;
    input {
      flex: 1;
      padding: 0.5rem 0.75rem;
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-md);
      background: var(--theme-background);
      color: var(--theme-text);
      font-family: inherit;
    }
    button {
      padding: 0.5rem 1rem;
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-md);
      background: var(--theme-text);
      color: var(--theme-background);
      cursor: pointer;
      font-weight: 600;
      &:disabled { opacity: 0.5; cursor: not-allowed; }
    }
  }
  .empty {
    padding: 3rem 1rem;
    text-align: center;
    border: 1px dashed var(--theme-border);
    border-radius: var(--rounded-lg);
    color: var(--theme-text-muted);
  }
  .app-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(18rem, 1fr));
    gap: 1rem;
  }
  .app-card {
    position: relative;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    transition: border-color 0.15s;
    &:hover { border-color: var(--theme-text); }
  }
  .app-link {
    display: block;
    padding: 1rem 1.125rem;
    text-decoration: none;
    color: var(--theme-text);
  }
  .app-name {
    font-weight: 600;
    font-size: 1.125rem;
  }
  .app-id {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    margin-top: 0.125rem;
  }
  .app-desc {
    margin-top: 0.5rem;
    font-size: 0.875rem;
    color: var(--theme-text-muted);
  }
  .app-counts {
    margin-top: 0.75rem;
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
  }
  .delete-btn {
    position: absolute;
    top: 0.5rem;
    right: 0.5rem;
    width: 1.75rem;
    height: 1.75rem;
    line-height: 1;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 50%;
    color: var(--theme-text-muted);
    cursor: pointer;
    font-size: 1.25rem;
    &:hover {
      background: rgba(239, 68, 68, 0.1);
      border-color: rgba(239, 68, 68, 0.4);
      color: #ef4444;
    }
  }
  .muted { color: var(--theme-text-muted); }
</style>
