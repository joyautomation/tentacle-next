<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { getHmiApp } from '$lib/api/hmi';
  import type { HmiAppConfig, HmiScreenConfig } from '$lib/types/hmi';

  const appId = $derived($page.params.appId as string);
  let app = $state<HmiAppConfig | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  async function load() {
    loading = true;
    error = null;
    const r = await getHmiApp(appId);
    if (r.error) error = r.error.error;
    else app = r.data ?? null;
    loading = false;
    const screens = app ? Object.values(app.screens ?? {}) : [];
    if (screens.length > 0) {
      goto(`/hmi/${encodeURIComponent(appId)}/screens/${encodeURIComponent(screens[0].screenId)}`, { replaceState: true });
    }
  }

  onMount(load);

  const screens = $derived<HmiScreenConfig[]>(app ? Object.values(app.screens ?? {}) : []);
</script>

<svelte:head>
  <title>{app?.name ?? appId} · HMI</title>
</svelte:head>

<section class="page">
  {#if error}
    <div class="banner error">{error}</div>
  {:else if loading}
    <p class="muted">Loading…</p>
  {:else if !app}
    <p class="muted">App not found.</p>
  {:else if screens.length === 0}
    <div class="empty">
      <p>No screens defined for <strong>{app.name}</strong>.</p>
      <a href="/hmi/designer/{encodeURIComponent(appId)}" class="link-btn">Open designer &rarr;</a>
    </div>
  {/if}
</section>

<style lang="scss">
  .page { max-width: 64rem; margin: 0 auto; padding: 2rem 1.5rem; }
  .banner.error {
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .empty {
    padding: 3rem 1rem;
    text-align: center;
    border: 1px dashed var(--theme-border);
    border-radius: var(--rounded-lg);
    color: var(--theme-text-muted);
  }
  .link-btn {
    display: inline-block;
    margin-top: 1rem;
    padding: 0.5rem 0.875rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
    color: var(--theme-text);
    text-decoration: none;
  }
  .muted { color: var(--theme-text-muted); }
</style>
