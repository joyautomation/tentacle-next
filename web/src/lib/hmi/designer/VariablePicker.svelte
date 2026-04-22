<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api/client';

  interface Props {
    open: boolean;
    onClose: () => void;
    onPick: (gateway: string, variable: string) => void;
  }

  let { open, onClose, onPick }: Props = $props();

  interface VarRow {
    moduleId?: string;
    ModuleID?: string;
    variableId?: string;
    VariableID?: string;
    id?: string;
    datatype?: string;
    value?: unknown;
  }

  let loading = $state(false);
  let error = $state<string | null>(null);
  let rows = $state<VarRow[]>([]);
  let filter = $state('');
  let loaded = $state(false);

  async function load() {
    loading = true;
    error = null;
    const r = await api<VarRow[]>('/variables');
    if (r.error) error = r.error.error;
    else rows = r.data ?? [];
    loading = false;
    loaded = true;
  }

  $effect(() => {
    if (open && !loaded) load();
  });

  function gatewayOf(v: VarRow): string {
    return (v.moduleId ?? v.ModuleID ?? '') as string;
  }
  function variableOf(v: VarRow): string {
    return (v.variableId ?? v.VariableID ?? v.id ?? '') as string;
  }

  const filtered = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((v) => {
      const g = gatewayOf(v).toLowerCase();
      const id = variableOf(v).toLowerCase();
      return g.includes(q) || id.includes(q);
    });
  });

  // Group by gateway for readability.
  const grouped = $derived.by(() => {
    const map = new Map<string, VarRow[]>();
    for (const v of filtered) {
      const g = gatewayOf(v) || '(no gateway)';
      const arr = map.get(g) ?? [];
      arr.push(v);
      map.set(g, arr);
    }
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
  });

  function pick(v: VarRow) {
    const g = gatewayOf(v);
    const id = variableOf(v);
    if (!g || !id) return;
    onPick(g, id);
  }

  function onBackdropKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose();
  }
</script>

{#if open}
  <div class="backdrop" role="button" tabindex="-1" onclick={onClose} onkeydown={onBackdropKey}>
    <div class="dialog" role="dialog" aria-modal="true" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
      <header class="dialog-header">
        <h2>Pick a variable</h2>
        <button class="close" onclick={onClose} aria-label="Close">×</button>
      </header>
      <input
        class="filter"
        type="text"
        placeholder="Filter by gateway or variable id…"
        bind:value={filter}
      />
      {#if error}
        <div class="banner error">{error}</div>
      {:else if loading}
        <p class="muted">Loading…</p>
      {:else if grouped.length === 0}
        <p class="muted">No variables match. Configure a gateway first.</p>
      {:else}
        <ul class="group-list">
          {#each grouped as [gateway, vars] (gateway)}
            <li class="group">
              <div class="group-name">{gateway}</div>
              <ul class="var-list">
                {#each vars as v}
                  {@const id = variableOf(v)}
                  <li>
                    <button class="var-row" onclick={() => pick(v)}>
                      <span class="var-id">{id}</span>
                      {#if v.datatype}<span class="var-type">{v.datatype}</span>{/if}
                    </button>
                  </li>
                {/each}
              </ul>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
{/if}

<style lang="scss">
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }
  .dialog {
    width: min(40rem, calc(100% - 2rem));
    max-height: calc(100vh - 4rem);
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .dialog-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.875rem 1.125rem;
    border-bottom: 1px solid var(--theme-border);
    h2 { margin: 0; font-size: 1rem; color: var(--theme-text); }
  }
  .close {
    background: transparent;
    border: none;
    color: var(--theme-text-muted);
    font-size: 1.25rem;
    cursor: pointer;
    &:hover { color: var(--theme-text); }
  }
  .filter {
    margin: 0.875rem 1.125rem 0.5rem;
    padding: 0.5rem 0.75rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
    color: var(--theme-text);
    font-family: inherit;
  }
  .group-list, .var-list { list-style: none; margin: 0; padding: 0; }
  .group-list { overflow-y: auto; padding: 0 1.125rem 1.125rem; }
  .group { margin-top: 0.75rem; }
  .group-name {
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
    margin-bottom: 0.25rem;
    font-family: 'IBM Plex Mono', monospace;
  }
  .var-list { display: flex; flex-direction: column; gap: 0.125rem; }
  .var-row {
    width: 100%;
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    padding: 0.375rem 0.5rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    color: var(--theme-text);
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.8125rem;
    cursor: pointer;
    text-align: left;
    &:hover { border-color: var(--theme-text); }
  }
  .var-id { color: var(--theme-text); }
  .var-type { color: var(--theme-text-muted); font-size: 0.75rem; }
  .banner.error {
    margin: 0 1.125rem 1rem;
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .muted { color: var(--theme-text-muted); padding: 1rem 1.125rem; margin: 0; }
</style>
