<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getHmiApp } from '$lib/api/hmi';
  import ScreenCanvas from '$lib/hmi/ScreenCanvas.svelte';
  import type { HmiAppConfig, HmiScreenConfig } from '$lib/types/hmi';

  const appId = $derived($page.params.appId as string);
  const screenId = $derived($page.params.screenId as string);
  let app = $state<HmiAppConfig | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  async function refresh() {
    loading = true;
    error = null;
    const r = await getHmiApp(appId);
    if (r.error) error = r.error.error;
    else app = r.data ?? null;
    loading = false;
  }

  const screen = $derived<HmiScreenConfig | null>(app?.screens?.[screenId] ?? null);
  const screens = $derived<HmiScreenConfig[]>(app ? Object.values(app.screens ?? {}) : []);

  onMount(refresh);
</script>

<svelte:head>
  <title>{screen?.name ?? screenId} · {app?.name ?? appId}</title>
</svelte:head>

<div class="runtime">
  <header class="runtime-header">
    <div class="title">
      <span class="app-name">{app?.name ?? appId}</span>
      <span class="sep">/</span>
      <span class="screen-name">{screen?.name ?? screenId}</span>
    </div>
    {#if screens.length > 1}
      <nav class="screen-nav">
        {#each screens as scr (scr.screenId)}
          <a
            href="/hmi/{encodeURIComponent(appId)}/screens/{encodeURIComponent(scr.screenId)}"
            class="screen-tab"
            class:active={scr.screenId === screenId}
          >{scr.name}</a>
        {/each}
      </nav>
    {/if}
    <a href="/hmi/designer/{encodeURIComponent(appId)}" class="designer-link" title="Open designer">Edit</a>
  </header>

  {#if error}
    <div class="banner error">{error}</div>
  {:else if loading}
    <p class="muted">Loading…</p>
  {:else if !screen}
    <p class="muted">Screen not found.</p>
  {:else}
    <ScreenCanvas {screen} components={app?.components ?? {}} />
  {/if}
</div>

<style lang="scss">
  .runtime { display: flex; flex-direction: column; min-height: calc(100vh - var(--header-height)); }
  .runtime-header {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.75rem 1.5rem;
    border-bottom: 1px solid var(--theme-border);
    background: var(--theme-surface);
  }
  .title { display: flex; align-items: baseline; gap: 0.5rem; font-family: 'Righteous', sans-serif; }
  .app-name { color: var(--theme-text-muted); font-size: 0.9375rem; }
  .sep { color: var(--theme-text-muted); }
  .screen-name { color: var(--theme-text); font-size: 1.0625rem; }
  .screen-nav { display: flex; gap: 0.25rem; margin-left: 1rem; flex: 1; }
  .screen-tab {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    text-decoration: none;
    border-radius: var(--rounded-sm, 4px);
    &:hover { color: var(--theme-text); background: var(--theme-background); }
    &.active { color: var(--theme-text); background: var(--theme-background); }
  }
  .designer-link {
    margin-left: auto;
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    color: var(--theme-text);
    text-decoration: none;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    &:hover { border-color: var(--theme-text); }
  }
  .banner.error {
    margin: 1rem;
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .muted { color: var(--theme-text-muted); padding: 1rem 1.5rem; }
</style>
