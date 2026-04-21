<script lang="ts">
  import type { PageData } from './$types';
  import type { ModuleInfo, ServiceStatus, DesiredService } from './+page';
  import { getModuleName } from '$lib/constants/services';
  import { goto, invalidateAll } from '$app/navigation';
  import { apiPut, apiDelete } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import { isMonolith } from '$lib/stores/mode';
  import { get } from 'svelte/store';

  let { data }: { data: PageData } = $props();

  const modules = $derived(data.modules as ModuleInfo[]);
  const statuses = $derived(data.statuses as ServiceStatus[]);
  const desired = $derived(data.desired as DesiredService[]);

  const statusMap = $derived(() => {
    const m = new Map<string, ServiceStatus>();
    for (const s of statuses) m.set(s.moduleId, s);
    return m;
  });

  const desiredMap = $derived(() => {
    const m = new Map<string, DesiredService>();
    for (const d of desired) m.set(d.moduleId, d);
    return m;
  });

  // Split modules into installed and available
  const installed = $derived(() => {
    const dm = desiredMap();
    return modules.filter(m => dm.has(m.moduleId));
  });

  const available = $derived(() => {
    const dm = desiredMap();
    return modules.filter(m => !dm.has(m.moduleId));
  });

  let busyModuleId: string | null = $state(null);

  function navigateToModule(moduleId: string) {
    goto(`/modules/${moduleId}`);
  }

  async function uninstallModule(moduleId: string) {
    busyModuleId = moduleId;
    try {
      const result = await apiDelete(`/orchestrator/desired-services/${moduleId}`);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `${getModuleName(moduleId)} ${get(isMonolith) ? 'disabled' : 'uninstalled'}`, type: 'success' });
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Failed', type: 'error' });
    } finally {
      busyModuleId = null;
    }
  }

  async function toggleRunning(moduleId: string, currentRunning: boolean) {
    busyModuleId = moduleId;
    const ds = desiredMap().get(moduleId);
    try {
      const result = await apiPut(`/orchestrator/desired-services/${moduleId}`, {
        version: ds?.version ?? 'latest',
        running: !currentRunning,
      });
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({
          message: `${getModuleName(moduleId)} ${!currentRunning ? 'started' : 'stopped'}`,
          type: 'success',
        });
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Failed', type: 'error' });
    } finally {
      busyModuleId = null;
    }
  }

  function getStateColor(state: string): string {
    if (state === 'active') return 'var(--green-500, #22c55e)';
    if (state === 'failed') return 'var(--red-500, #ef4444)';
    if (state === 'inactive' || state === 'dead') return 'var(--theme-text-muted)';
    return 'var(--amber-500, #f59e0b)';
  }

  function getReconcileBadge(state: string): { color: string; label: string } {
    if (state === 'ok') return { color: 'var(--badge-green-text)', label: 'ok' };
    if (state === 'error') return { color: 'var(--red-500, #ef4444)', label: 'error' };
    if (state === 'needs_config') return { color: 'var(--amber-500, #f59e0b)', label: 'needs config' };
    if (state === 'downloading') return { color: 'var(--amber-500, #f59e0b)', label: 'downloading' };
    return { color: 'var(--theme-text-muted)', label: state };
  }
</script>

<div class="modules-page">
  {#if data.error}
    <div class="error-box"><p>{data.error}</p></div>
  {/if}

  <div class="modules-header">
    <h1>Modules</h1>
    <span class="count-badge">{installed().length} {$isMonolith ? 'enabled' : 'installed'}</span>
    <span class="count-badge secondary">{available().length} available</span>
  </div>

  <!-- Installed Modules -->
  {#if installed().length > 0}
    <section class="section">
      <h2>{$isMonolith ? 'Enabled' : 'Installed'}</h2>
      <div class="modules-list">
        {#each installed() as mod}
          {@const status = statusMap().get(mod.moduleId)}
          {@const ds = desiredMap().get(mod.moduleId)}
          <a href="/modules/{mod.moduleId}" class="module-card clickable" class:active={status?.systemdState === 'active'}>
            <div class="module-header">
              <span class="module-name">{getModuleName(mod.moduleId)}</span>
              <span class="module-id">{mod.moduleId}</span>
              {#if status}
                <span class="state-dot" style="background: {getStateColor(status.systemdState)}"></span>
                <span class="state-label" style="color: {getStateColor(status.systemdState)}">{status.systemdState}</span>
              {/if}
            </div>
            <p class="module-desc">{mod.description}</p>
            <div class="module-meta">
              <span class="meta-badge">{mod.category}</span>
              {#if !$isMonolith}
                <span class="meta-badge">{mod.runtime}</span>
              {/if}
              {#if status}
                {@const badge = getReconcileBadge(status.reconcileState)}
                <span class="meta-badge" style="color: {badge.color}">{badge.label}</span>
              {/if}
              {#if ds && !$isMonolith}
                <span class="meta-badge">v{ds.version}</span>
              {/if}
            </div>
            {#if status?.lastError}
              <p class="module-error">{status.lastError}</p>
            {/if}
            <div class="module-actions">
              {#if ds}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <label class="toggle" title={ds.running ? 'Stop module' : 'Start module'} onclick={(e) => e.preventDefault()}>
                  <input
                    type="checkbox"
                    checked={ds.running}
                    disabled={busyModuleId === mod.moduleId}
                    onchange={() => toggleRunning(mod.moduleId, ds.running)}
                  />
                  <span class="toggle-slider"></span>
                </label>
              {/if}
              <button
                class="action-btn danger"
                disabled={busyModuleId === mod.moduleId}
                onclick={(e) => { e.preventDefault(); uninstallModule(mod.moduleId); }}
              >{$isMonolith ? 'Disable' : 'Uninstall'}</button>
            </div>
          </a>
        {/each}
      </div>
    </section>
  {/if}

  <!-- Available (not installed) Modules -->
  {#if available().length > 0}
    <section class="section">
      <h2>Available</h2>
      <div class="modules-list">
        {#each available() as mod}
          <a href="/modules/{mod.moduleId}" class="module-card available clickable">
            <div class="module-header">
              <span class="module-name">{getModuleName(mod.moduleId)}</span>
              <span class="module-id">{mod.moduleId}</span>
            </div>
            <p class="module-desc">{mod.description}</p>
            <div class="module-meta">
              <span class="meta-badge">{mod.category}</span>
              {#if !$isMonolith}
                <span class="meta-badge">{mod.runtime}</span>
              {/if}
            </div>
            <div class="module-actions">
              <button
                class="action-btn primary"
                onclick={(e) => { e.preventDefault(); navigateToModule(mod.moduleId); }}
              >{$isMonolith ? 'Enable' : 'Install'}</button>
            </div>
          </a>
        {/each}
      </div>
    </section>
  {/if}

  {#if modules.length === 0 && !data.error}
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
    &.secondary { background: var(--badge-muted-bg); color: var(--badge-muted-text); }
  }

  .section {
    margin-bottom: 2rem;
    h2 {
      font-size: 0.8125rem; font-weight: 600; text-transform: uppercase;
      letter-spacing: 0.05em; color: var(--theme-text-muted); margin: 0 0 0.75rem;
    }
  }

  .modules-list { display: flex; flex-direction: column; gap: 0.5rem; }

  .module-card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1rem 1.25rem;

    &.clickable {
      display: block;
      text-decoration: none;
      color: inherit;
      transition: border-color 0.15s;

      &:hover {
        border-color: var(--theme-primary);
      }
    }

    &.active {
      border-color: color-mix(in srgb, var(--green-500, #22c55e) 30%, var(--theme-border));
    }
    &.available {
      opacity: 0.75;
      &:hover { opacity: 1; }
    }
  }

  .module-header {
    display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.375rem;
  }

  .module-name { font-weight: 600; font-size: 0.9375rem; color: var(--theme-text); }

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

  .module-desc { margin: 0 0 0.5rem; font-size: 0.8125rem; color: var(--theme-text-muted); }

  .module-error {
    margin: 0.25rem 0 0.5rem; font-size: 0.75rem; color: var(--red-500, #ef4444);
    font-family: 'IBM Plex Mono', monospace;
  }

  .module-meta { display: flex; flex-wrap: wrap; gap: 0.375rem; margin-bottom: 0.75rem; }

  .meta-badge {
    font-size: 0.6875rem; font-family: 'IBM Plex Mono', monospace;
    color: var(--badge-muted-text); padding: 0.1rem 0.35rem;
    border-radius: var(--rounded-sm); background: var(--badge-muted-bg);
  }

  .module-actions {
    display: flex; align-items: center; gap: 0.75rem;
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    padding-top: 0.75rem;
  }

  .action-btn {
    padding: 0.3rem 0.75rem; font-size: 0.75rem; font-weight: 500;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: var(--theme-surface); color: var(--theme-text);
    cursor: pointer; font-family: inherit;
    &:hover { background: color-mix(in srgb, var(--theme-text) 8%, transparent); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }

    &.primary {
      background: var(--theme-primary); color: white; border-color: var(--theme-primary);
      &:hover { filter: brightness(1.1); }
    }
    &.danger {
      color: var(--red-500, #ef4444); border-color: color-mix(in srgb, var(--red-500, #ef4444) 30%, var(--theme-border));
      margin-left: auto;
      &:hover { background: color-mix(in srgb, var(--red-500, #ef4444) 10%, transparent); }
    }
  }

  .toggle {
    position: relative; display: inline-block; width: 36px; height: 20px; cursor: pointer; flex-shrink: 0;
    input { opacity: 0; width: 0; height: 0; }
  }

  .toggle-slider {
    position: absolute; inset: 0; background: var(--theme-border); border-radius: 20px; transition: background 0.2s;
    &::before {
      content: ''; position: absolute; width: 14px; height: 14px; left: 3px; bottom: 3px;
      background: var(--theme-text); border-radius: 50%; transition: transform 0.2s;
    }
  }

  .toggle input:checked + .toggle-slider { background: var(--green-500, #22c55e); }
  .toggle input:checked + .toggle-slider::before { transform: translateX(16px); }
  .toggle input:disabled + .toggle-slider { opacity: 0.5; cursor: not-allowed; }

  .error-box {
    padding: 1rem; border-radius: var(--rounded-lg); background: var(--theme-surface);
    border: 1px solid var(--red-500, #ef4444); margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--red-500, #ef4444); }
  }

  .empty-state {
    padding: 3rem 2rem; text-align: center;
    p { color: var(--theme-text-muted); font-size: 0.875rem; }
  }
</style>
