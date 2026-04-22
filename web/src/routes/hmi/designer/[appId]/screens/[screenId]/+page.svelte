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

  onMount(refresh);
</script>

<svelte:head>
  <title>{screen?.name ?? screenId} · {app?.name ?? appId}</title>
</svelte:head>

<section class="page">
  <header class="page-header">
    <div>
      <a href="/hmi/designer/{encodeURIComponent(appId)}" class="back">&larr; {app?.name ?? appId}</a>
      <h1>{screen?.name ?? screenId}</h1>
      {#if screen}
        <p class="subtitle">{screen.widgets?.length ?? 0} widgets</p>
      {/if}
    </div>
  </header>

  {#if error}
    <div class="banner error">{error}</div>
  {/if}

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if !screen}
    <p class="muted">Screen not found.</p>
  {:else}
    <ScreenCanvas {screen} />
  {/if}
</section>

<style lang="scss">
  .page { max-width: 80rem; margin: 0 auto; padding: 2rem 1.5rem; }
  .page-header { margin-bottom: 1rem; }
  .back { color: var(--theme-text-muted); text-decoration: none; font-size: 0.875rem; }
  h1 { margin: 0.25rem 0 0; font-family: 'Righteous', sans-serif; color: var(--theme-text); }
  .subtitle { margin: 0.125rem 0 0; color: var(--theme-text-muted); font-size: 0.8125rem; }
  .banner.error {
    margin-bottom: 1rem;
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .muted { color: var(--theme-text-muted); }
</style>
