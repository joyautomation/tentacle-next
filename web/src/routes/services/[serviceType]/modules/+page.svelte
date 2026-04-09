<script lang="ts">
  import type { PageData } from './$types';
  import { getModuleName } from '$lib/constants/services';

  let { data }: { data: PageData } = $props();

  type Module = {
    moduleId: string;
    repo: string;
    description: string;
    category: string;
    runtime: string;
  };

  type Status = {
    moduleId: string;
    installedVersions: string[];
    activeVersion: string;
    systemdState: string;
    reconcileState: string;
    runtime: string;
    category: string;
    repo: string;
    updatedAt: number;
  };

  const modules = $derived(data.modules as Module[]);
  const statuses = $derived(data.statuses as Status[]);

  const statusMap = $derived(() => {
    const m = new Map<string, Status>();
    for (const s of statuses) m.set(s.moduleId, s);
    return m;
  });

  function getStateColor(state: string): string {
    if (state === 'active') return 'var(--color-green-500, #22c55e)';
    if (state === 'failed') return 'var(--color-red-500, #ef4444)';
    if (state === 'inactive' || state === 'dead') return 'var(--theme-text-muted)';
    return 'var(--color-amber-500, #f59e0b)';
  }

  function getReconcileColor(state: string): string {
    if (state === 'ok') return 'var(--color-green-500, #22c55e)';
    if (state === 'error') return 'var(--color-red-500, #ef4444)';
    return 'var(--color-amber-500, #f59e0b)';
  }
</script>

<div class="modules-page">
  {#if data.error}
    <div class="error-box"><p>{data.error}</p></div>
  {/if}

  <div class="modules-header">
    <h1>Modules</h1>
    <span class="count-badge">{modules.length} available</span>
    <span class="count-badge">{statuses.length} running</span>
  </div>

  {#if modules.length > 0}
    <div class="modules-list">
      {#each modules as mod}
        {@const status = statusMap().get(mod.moduleId)}
        <div class="module-card" class:active={status?.systemdState === 'active'}>
          <div class="module-header">
            <span class="module-name">{getModuleName(mod.moduleId)}</span>
            <span class="module-id">{mod.moduleId}</span>
            {#if status}
              <span class="state-dot" style="background: {getStateColor(status.systemdState)}" title="systemd: {status.systemdState}"></span>
              <span class="state-label" style="color: {getStateColor(status.systemdState)}">{status.systemdState}</span>
            {:else}
              <span class="state-dot" style="background: var(--theme-text-muted)" title="not running"></span>
              <span class="state-label" style="color: var(--theme-text-muted)">not running</span>
            {/if}
          </div>
          <p class="module-desc">{mod.description}</p>
          <div class="module-meta">
            <span class="meta-badge">{mod.category}</span>
            <span class="meta-badge">{mod.runtime}</span>
            {#if status}
              <span class="meta-badge" style="color: {getReconcileColor(status.reconcileState)}">
                reconcile: {status.reconcileState}
              </span>
              <span class="meta-badge">v{status.activeVersion}</span>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {:else if !data.error}
    <div class="empty-state">
      <p>No modules registered with the orchestrator.</p>
    </div>
  {/if}
</div>

<style lang="scss">
  .modules-page { padding: 2rem; max-width: 900px; }

  .modules-header {
    display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1.5rem;
    h1 { font-size: 1.5rem; font-weight: 600; color: var(--theme-text); margin: 0; }
  }

  .count-badge {
    padding: 0.2rem 0.5rem; border-radius: var(--rounded-md); font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace; background: var(--badge-teal-bg); color: var(--badge-teal-text);
  }

  .modules-list {
    display: flex; flex-direction: column; gap: 0.5rem;
  }

  .module-card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1rem 1.25rem;

    &.active {
      border-color: color-mix(in srgb, var(--color-green-500, #22c55e) 30%, var(--theme-border));
    }
  }

  .module-header {
    display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.375rem;
  }

  .module-name {
    font-weight: 600; font-size: 0.9375rem; color: var(--theme-text);
  }

  .module-id {
    font-family: 'IBM Plex Mono', monospace; font-size: 0.75rem;
    color: var(--theme-text-muted); padding: 0.1rem 0.35rem;
    border-radius: var(--rounded-sm); background: var(--badge-muted-bg);
  }

  .state-dot {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; margin-left: auto;
  }

  .state-label {
    font-size: 0.75rem; font-weight: 500; text-transform: uppercase; letter-spacing: 0.03em;
  }

  .module-desc {
    margin: 0 0 0.5rem; font-size: 0.8125rem; color: var(--theme-text-muted);
  }

  .module-meta {
    display: flex; flex-wrap: wrap; gap: 0.375rem;
  }

  .meta-badge {
    font-size: 0.6875rem; font-family: 'IBM Plex Mono', monospace;
    color: var(--badge-muted-text); padding: 0.1rem 0.35rem;
    border-radius: var(--rounded-sm); background: var(--badge-muted-bg);
  }

  .error-box {
    padding: 1rem; border-radius: var(--rounded-lg); background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444); margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .empty-state {
    padding: 3rem 2rem; text-align: center;
    p { color: var(--theme-text-muted); font-size: 0.875rem; }
  }
</style>
