<script lang="ts">
  import { invalidateAll } from '$app/navigation';
  import { slide } from 'svelte/transition';
  import { apiPut, apiDelete } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import type { PageData } from './$types';
  import {
    SERVICE_NAMES,
    SERVICE_CATEGORIES,
    REMOTE_CONFIG_STATUS,
    type RemoteConfigStatus,
  } from '$lib/constants/services';
  import type { FleetModule } from '$lib/types/fleet';
  import SystemTopology from '$lib/components/SystemTopology.svelte';

  let { data }: { data: PageData } = $props();

  const fleetBase = $derived(
    `/fleet/nodes/${encodeURIComponent(data.group)}/${encodeURIComponent(data.node)}/services`,
  );

  let busy = $state<Record<string, boolean>>({});
  let confirmRemove = $state<string | null>(null);
  let topologyOpen = $state(true);

  async function toggleService(svc: FleetModule) {
    busy = { ...busy, [svc.id]: true };
    const res = await apiPut<FleetModule>(`${fleetBase}/${encodeURIComponent(svc.id)}`, {
      running: !svc.running,
    });
    busy = { ...busy, [svc.id]: false };
    if (res.error) {
      saltState.addNotification({ message: `Failed to toggle ${svc.id}: ${res.error.error}`, type: 'error' });
      return;
    }
    saltState.addNotification({
      message: `${svc.id} marked ${res.data?.running ? 'running' : 'stopped'} (edge syncs within ~5s)`,
      type: 'success',
    });
    await invalidateAll();
  }

  async function installService(serviceType: string) {
    busy = { ...busy, [serviceType]: true };
    const res = await apiPut<FleetModule>(`${fleetBase}/${encodeURIComponent(serviceType)}`, {
      running: true,
      version: 'latest',
    });
    busy = { ...busy, [serviceType]: false };
    if (res.error) {
      saltState.addNotification({ message: `Failed to add ${serviceType}: ${res.error.error}`, type: 'error' });
      return;
    }
    saltState.addNotification({ message: `Added ${serviceType} (edge syncs within ~5s)`, type: 'success' });
    await invalidateAll();
  }

  async function removeService(name: string) {
    busy = { ...busy, [name]: true };
    const res = await apiDelete<void>(`${fleetBase}/${encodeURIComponent(name)}`);
    busy = { ...busy, [name]: false };
    confirmRemove = null;
    if (res.error) {
      saltState.addNotification({ message: `Failed to remove ${name}: ${res.error.error}`, type: 'error' });
      return;
    }
    saltState.addNotification({ message: `Removed ${name} from desired state`, type: 'success' });
    await invalidateAll();
  }

  const installedById = $derived(new Map(data.services.map((s) => [s.id, s])));

  interface Row {
    serviceType: string;
    name: string;
    installed: FleetModule | null;
    remoteStatus: RemoteConfigStatus;
  }

  const categories = $derived(
    SERVICE_CATEGORIES.map((cat) => ({
      id: cat.id,
      label: cat.label,
      rows: cat.serviceTypes.map<Row>((st) => ({
        serviceType: st,
        name: SERVICE_NAMES[st] ?? st,
        installed: installedById.get(st) ?? null,
        remoteStatus: REMOTE_CONFIG_STATUS[st] ?? 'bus-driven',
      })),
    })),
  );

  const targetSuffix = $derived(
    `?target=${encodeURIComponent(`${data.group}/${data.node}`)}`,
  );

  function configureHref(serviceType: string): string {
    return `/services/${serviceType}${targetSuffix}`;
  }

  // Map FleetModule[] (desired services from gitops) into the Service[] shape
  // SystemTopology expects. Mantle doesn't have live per-module heartbeats
  // for remote nodes yet, so this renders the *desired* topology — what's
  // intended to run on the edge. Live status will fill in via Sparkplug
  // (Phase 3).
  const topologyServices = $derived(
    data.services
      .filter((m) => m.running)
      .map((m) => ({
        serviceType: m.id,
        moduleId: m.id,
        startedAt: data.fleetNode?.firstSeen ?? Date.now(),
        version: m.version ?? null,
        metadata: null as Record<string, unknown> | null,
        enabled: m.running,
      })),
  );

  function formatRelative(ts: number): string {
    if (!ts) return 'never';
    const secs = Math.floor((Date.now() - ts) / 1000);
    if (secs < 5) return 'just now';
    if (secs < 60) return `${secs}s ago`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
    if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
    return `${Math.floor(secs / 86400)}d ago`;
  }
</script>

<div class="page">
  <nav class="breadcrumb">
    <a href="/fleet">Fleet</a>
    <span class="separator">/</span>
    <span class="current mono">{data.group}/{data.node}</span>
  </nav>

  <header class="page-header">
    <div class="header-content">
      <h1>{data.group} <span class="muted">/</span> {data.node}</h1>
      {#if data.fleetNode}
        <p class="subtitle">
          <span class="badge" class:online={data.fleetNode.online} class:offline={!data.fleetNode.online}>
            {data.fleetNode.online ? 'Online' : 'Offline'}
          </span>
          <span class="dot">·</span>
          {Object.keys(data.fleetNode.devices ?? {}).length} devices
          <span class="dot">·</span>
          {data.fleetNode.metricCount} metrics
          <span class="dot">·</span>
          last seen {formatRelative(data.fleetNode.lastSeen)}
        </p>
      {:else}
        <p class="subtitle warn">
          Node not yet observed via Sparkplug. You can still pre-author config — it'll be picked up on next gitops sync.
        </p>
      {/if}
    </div>
  </header>

  {#if data.error}
    <div class="info-box error">
      <h3>Inventory unavailable</h3>
      <p>{data.error}</p>
    </div>
  {/if}

  <section class="section">
    <button class="collapsible-head" onclick={() => (topologyOpen = !topologyOpen)}>
      <span class="caret" class:open={topologyOpen}>▶</span>
      <span class="section-title">Topology</span>
      <span class="section-hint inline">
        {topologyServices.length} module{topologyServices.length === 1 ? '' : 's'} running
      </span>
    </button>
    {#if topologyOpen}
      <div class="topology-wrap" transition:slide={{ duration: 180 }}>
        {#if topologyServices.length === 0}
          <div class="info-box muted">
            <p>No running modules to visualise yet. Add modules below.</p>
          </div>
        {:else}
          <SystemTopology
            services={topologyServices}
            apiConnected={data.fleetNode?.online ?? false}
            monolith={true}
          />
        {/if}
      </div>
    {/if}
  </section>

  <section class="section">
    <h2 class="section-title">Modules</h2>
    <p class="section-hint">
      Add a module to mark it desired in this node's gitops repo. The edge applies changes within ~5s.
    </p>

    {#if data.servicesError}
      <div class="info-box error">
        <h3>Couldn't load services</h3>
        <p>{data.servicesError}</p>
      </div>
    {:else}
      {#each categories as cat (cat.id)}
        <div class="category">
          <h3 class="category-title">{cat.label}</h3>
          <div class="module-list">
            {#each cat.rows as row (row.serviceType)}
              {@const inst = row.installed}
              {@const isBusy = busy[row.serviceType]}
              {@const isConfigurable = row.remoteStatus === 'configurable'}
              {@const isComingSoon = row.remoteStatus === 'coming-soon'}
              <div class="module-row" class:installed={!!inst}>
                <div class="module-name">
                  <span class="name-label">{row.name}</span>
                  <span class="name-id mono">{row.serviceType}</span>
                </div>

                <div class="module-status">
                  {#if inst}
                    <label class="toggle" title="Toggle running state">
                      <input
                        type="checkbox"
                        checked={inst.running}
                        disabled={isBusy}
                        onchange={() => toggleService(inst)}
                      />
                      <span class="toggle-label" class:running={inst.running}>
                        {inst.running ? 'Running' : 'Stopped'}
                      </span>
                    </label>
                  {:else}
                    <span class="status-text muted">Not installed</span>
                  {/if}
                </div>

                <div class="module-actions">
                  {#if inst && isConfigurable}
                    <a class="ghost-btn" href={configureHref(row.serviceType)}>Configure</a>
                  {:else if inst && isComingSoon}
                    <span class="soon-tag" title="Configurator UI coming soon">Configure (soon)</span>
                  {/if}

                  {#if inst}
                    {#if confirmRemove === row.serviceType}
                      <button
                        class="danger-btn"
                        disabled={isBusy}
                        onclick={() => removeService(row.serviceType)}
                      >
                        {isBusy ? 'Removing…' : 'Confirm'}
                      </button>
                      <button class="ghost-btn" onclick={() => (confirmRemove = null)}>Cancel</button>
                    {:else}
                      <button
                        class="ghost-btn danger-ghost"
                        onclick={() => (confirmRemove = row.serviceType)}
                      >Remove</button>
                    {/if}
                  {:else}
                    <button
                      class="primary-btn"
                      disabled={isBusy}
                      onclick={() => installService(row.serviceType)}
                    >
                      {isBusy ? 'Adding…' : '+ Add'}
                    </button>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        </div>
      {/each}
    {/if}
  </section>
</div>

<style lang="scss">
  .page {
    padding: 1.5rem 2rem;
    max-width: 1400px;
    margin: 0 auto;
  }

  .breadcrumb {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.875rem;
    margin-bottom: 1rem;

    a {
      color: var(--theme-text-muted);
      text-decoration: none;

      &:hover { color: var(--theme-primary); }
    }

    .separator { color: var(--theme-border); }
    .current { color: var(--theme-text); font-weight: 500; }
  }

  .page-header {
    padding-bottom: 1.25rem;
    margin-bottom: 1.5rem;
    border-bottom: 1px solid var(--theme-border);
  }

  .header-content h1 {
    margin: 0 0 0.5rem;
    font-size: 1.5rem;
    font-weight: 600;
    color: var(--theme-text);
    font-family: var(--font-mono, monospace);
  }

  .subtitle {
    margin: 0;
    color: var(--theme-text-muted);
    font-size: 0.875rem;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;

    &.warn { color: var(--theme-warning, #d97706); }
  }

  .dot { opacity: 0.5; }

  .info-box {
    padding: 1rem 1.25rem;
    border-radius: var(--rounded-md, 0.5rem);
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    margin-bottom: 1rem;

    h3 { margin: 0 0 0.25rem; font-size: 1rem; font-weight: 600; color: var(--theme-text); }
    p { margin: 0; color: var(--theme-text-muted); font-size: 0.875rem; }
    &.error h3 { color: var(--theme-danger, #ef4444); }
  }

  .section { margin-bottom: 2rem; }

  .section-title {
    margin: 0 0 0.25rem;
    font-size: 1rem;
    font-weight: 600;
    color: var(--theme-text);
  }

  .section-hint {
    margin: 0 0 0.875rem;
    color: var(--theme-text-muted);
    font-size: 0.8125rem;

    &.inline { margin: 0 0 0 0.5rem; }
  }

  .collapsible-head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.5rem 0;
    background: transparent;
    border: none;
    color: var(--theme-text);
    cursor: pointer;
    text-align: left;

    &:hover .section-title { color: var(--theme-primary); }
  }

  .caret {
    display: inline-block;
    font-size: 0.7rem;
    transition: transform 120ms;
    color: var(--theme-text-muted);

    &.open { transform: rotate(90deg); }
  }

  .topology-wrap {
    margin-top: 0.5rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.5rem);
    background: var(--theme-surface);
    overflow: hidden;
    min-height: 360px;
  }

  .badge {
    display: inline-block;
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-full, 999px);
    text-transform: uppercase;
    letter-spacing: 0.05em;

    &.online { background: var(--badge-green-bg); color: var(--badge-green-text); }
    &.offline { background: var(--badge-muted-bg); color: var(--badge-muted-text); }
  }

  .muted { color: var(--theme-text-muted); }
  .mono { font-family: var(--font-mono, monospace); }

  .category {
    margin-bottom: 1.25rem;
  }

  .category-title {
    margin: 0 0 0.5rem;
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--theme-text-muted);
  }

  .module-list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.5rem);
    background: var(--theme-surface);
    overflow: hidden;
  }

  .module-row {
    display: grid;
    grid-template-columns: 1fr auto auto;
    align-items: center;
    gap: 1rem;
    padding: 0.625rem 0.875rem;
    border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    font-size: 0.875rem;
    opacity: 0.7;
    transition: opacity 120ms;

    &:last-child { border-bottom: none; }
    &.installed { opacity: 1; }
    &:hover { background: color-mix(in srgb, var(--theme-text) 3%, transparent); }
  }

  .module-name {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    min-width: 0;
  }

  .name-label {
    font-weight: 500;
    color: var(--theme-text);
  }

  .name-id {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
  }

  .module-status {
    min-width: 7rem;
    text-align: right;
  }

  .status-text {
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-weight: 600;
  }

  .module-actions {
    display: flex;
    gap: 0.375rem;
    justify-content: flex-end;
  }

  .toggle {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    user-select: none;

    input { cursor: pointer; }
  }

  .toggle-label {
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);

    &.running { color: var(--badge-green-text, #16a34a); }
  }

  .soon-tag {
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.25rem 0.625rem;
    border-radius: var(--rounded-md, 0.375rem);
    border: 1px dashed var(--theme-border);
    color: var(--theme-text-muted);
    cursor: not-allowed;
  }

  .ghost-btn {
    padding: 0.25rem 0.625rem;
    font-size: 0.8125rem;
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.375rem);
    color: var(--theme-text);
    cursor: pointer;
    text-decoration: none;
    display: inline-flex;
    align-items: center;

    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface)); }

    &.danger-ghost:hover {
      border-color: var(--theme-danger, #ef4444);
      color: var(--theme-danger, #ef4444);
    }
  }

  .primary-btn {
    padding: 0.25rem 0.75rem;
    font-size: 0.8125rem;
    font-weight: 500;
    background: var(--theme-primary);
    color: white;
    border: none;
    border-radius: var(--rounded-md, 0.375rem);
    cursor: pointer;

    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .danger-btn {
    padding: 0.25rem 0.625rem;
    font-size: 0.8125rem;
    background: var(--theme-danger, #ef4444);
    color: white;
    border: none;
    border-radius: var(--rounded-md, 0.375rem);
    cursor: pointer;

    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }
</style>
