<script lang="ts">
  import type { PageData } from './$types';
  import { invalidateAll } from '$app/navigation';
  import { apiPut } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import StoreForwardStatus from '$lib/components/StoreForwardStatus.svelte';
  import SparkplugHostNodes from '$lib/components/SparkplugHostNodes.svelte';
  import {
    getServiceName,
    getServiceTabs,
    getRemoteConfigStatus,
  } from '$lib/constants/services';
  import type { FleetModule } from '$lib/types/fleet';

  let { data }: { data: PageData } = $props();

  const serviceNames: Record<string, string> = {
    nats: 'NATS',
    graphql: 'GraphQL',
    web: 'Web UI',
    ethernetip: 'EtherNet/IP',
    'ethernetip-server': 'EtherNet/IP Server',
    mqtt: 'MQTT',
    plc: 'PLC',
    network: 'Network',
    profinet: 'PROFINET Device',
    profinetcontroller: 'PROFINET Controller',
  };

  const serviceDescriptions: Record<string, string> = {
    nats: 'Central message bus for inter-service communication',
    graphql: 'GraphQL API gateway for the tentacle platform',
    web: 'Web-based management interface',
    ethernetip: 'EtherNet/IP scanner for Allen-Bradley/Rockwell PLCs',
    'ethernetip-server': 'EtherNet/IP CIP server exposing PLC variables to external clients',
    mqtt: 'MQTT Sparkplug B bridge for publishing PLC data',
    plc: 'PLC runtime project',
    network: 'Network interface monitoring and configuration management',
    profinet: 'PROFINET IO Device for field-level communication',
    profinetcontroller: 'PROFINET IO Controller for scanning devices',
  };

  function formatUptime(startedAt: string | number): string {
    const startMs = typeof startedAt === 'number' ? startedAt : Date.parse(startedAt);
    if (!startMs || Number.isNaN(startMs)) return '—';
    const seconds = Math.floor((Date.now() - startMs) / 1000);
    if (seconds < 0) return '—';
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;
    const parts: string[] = [];
    if (days > 0) parts.push(`${days}d`);
    if (hours > 0) parts.push(`${hours}h`);
    if (minutes > 0) parts.push(`${minutes}m`);
    if (parts.length === 0) parts.push(`${secs}s`);
    return parts.join(' ');
  }

  function formatDate(iso: string): string {
    return new Date(iso).toLocaleString();
  }

  // Infrastructure services (nats, web) are always running if we can reach graphql
  const infraServices = new Set(['nats', 'web']);
  const isInfra = $derived(infraServices.has(data.serviceType));
  const isRunning = $derived(isInfra ? data.graphqlConnected : (data.instances?.length ?? 0) > 0);

  const description = $derived(
    serviceDescriptions[data.serviceType] ?? 'Tentacle service'
  );

  // Remote-mode derivations: when ?target=group/node is set, the load
  // function returns `remoteModule` (the gitops desired-state entry for
  // this serviceType on the remote edge) instead of local `instances`.
  const isRemote = $derived(!!data.target);
  const remoteName = $derived(getServiceName(data.serviceType));
  const remoteStatus = $derived(getRemoteConfigStatus(data.serviceType));
  const remoteTargetSuffix = $derived(
    data.target ? `?target=${encodeURIComponent(data.target)}` : '',
  );
  const remoteConfigTabs = $derived(
    getServiceTabs(data.serviceType).filter((t) => t.scope === 'config'),
  );
  const remoteFleetBase = $derived(
    data.target
      ? `/fleet/nodes/${encodeURIComponent(data.remoteGroup)}/${encodeURIComponent(data.remoteNode)}/services`
      : '',
  );

  let remoteToggling = $state(false);
  let remoteOptimisticRunning: boolean | null = $state(null);

  const remoteRunning = $derived.by(() => {
    if (remoteOptimisticRunning !== null) return remoteOptimisticRunning;
    return data.remoteModule?.running ?? false;
  });

  async function toggleRemote() {
    if (!data.remoteModule) return;
    const next = !remoteRunning;
    remoteToggling = true;
    remoteOptimisticRunning = next;
    const res = await apiPut<FleetModule>(
      `${remoteFleetBase}/${encodeURIComponent(data.serviceType)}`,
      { running: next },
    );
    remoteToggling = false;
    if (res.error) {
      remoteOptimisticRunning = !next;
      saltState.addNotification({ message: res.error.error, type: 'error' });
      return;
    }
    saltState.addNotification({
      message: `${data.serviceType} marked ${next ? 'enabled' : 'disabled'} (edge syncs within ~5s)`,
      type: 'success',
    });
    await invalidateAll();
    remoteOptimisticRunning = null;
  }

  let togglingModuleId: string | null = $state(null);
  let optimisticEnabled: Record<string, boolean> = $state({});

  function getEnabled(instance: { moduleId: string; enabled: boolean }): boolean {
    return instance.moduleId in optimisticEnabled
      ? optimisticEnabled[instance.moduleId]
      : instance.enabled;
  }

  async function toggleEnabled(moduleId: string, currentEnabled: boolean) {
    togglingModuleId = moduleId;
    const newEnabled = !currentEnabled;
    optimisticEnabled[moduleId] = newEnabled;
    try {
      const result = await apiPut<{ moduleId: string; enabled: boolean }>(`/services/${moduleId}/enabled`, { enabled: newEnabled });

      if (result.error) {
        optimisticEnabled[moduleId] = currentEnabled;
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        const newState = result.data?.enabled;
        saltState.addNotification({
          message: `${moduleId} ${newState ? 'enabled' : 'disabled'}`,
          type: 'success',
        });
        await invalidateAll();
        delete optimisticEnabled[moduleId];
      }
    } catch (err) {
      optimisticEnabled[moduleId] = currentEnabled;
      saltState.addNotification({
        message: err instanceof Error ? err.message : 'Failed to toggle service',
        type: 'error',
      });
    } finally {
      togglingModuleId = null;
    }
  }
</script>

<div class="service-overview">
{#if isRemote}
  <div class="status-header">
    <div class="status-info">
      <h1>{remoteName}</h1>
      <p class="description">{description}</p>
    </div>
    {#if data.remoteModule}
      <span class="status-badge" class:running={remoteRunning} class:stopped={!remoteRunning}>
        {remoteRunning ? 'Enabled' : 'Disabled'}
      </span>
    {:else}
      <span class="status-badge stopped">Not added</span>
    {/if}
  </div>

  {#if data.error}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {/if}

  {#if data.remoteModule}
    <div class="enable-row">
      <span class="enable-label">
        Enabled
        {#if !remoteRunning}
          <span class="disabled-badge">Disabled</span>
        {/if}
      </span>
      <label class="toggle" title={remoteRunning ? 'Disable on edge (keeps config)' : 'Enable on edge'}>
        <input
          type="checkbox"
          checked={remoteRunning}
          disabled={remoteToggling}
          onchange={toggleRemote}
        />
        <span class="toggle-slider"></span>
      </label>
    </div>

    <div class="details" class:disabled={!remoteRunning}>
      {#if data.remoteModule.version}
        <div class="detail-row">
          <span class="label">Version</span>
          <span class="value">{data.remoteModule.version}</span>
        </div>
      {/if}
      <div class="detail-row">
        <span class="label">Source</span>
        <span class="value">gitops desired state</span>
      </div>
    </div>

    {#if remoteStatus === 'configurable' && remoteConfigTabs.length > 0}
      <div class="info-box">
        <p>
          Configure this module on the remote edge:
          {#each remoteConfigTabs as tab, i (tab.path)}
            {#if i > 0}<span class="sep"> · </span>{/if}
            <a href="/services/{data.serviceType}/{tab.path}{remoteTargetSuffix}">{tab.label} →</a>
          {/each}
        </p>
      </div>
    {:else if remoteStatus === 'coming-soon'}
      <div class="info-box">
        <p>
          Standalone configuration UI for <strong>{remoteName}</strong> is coming soon. The module owns its own settings on the edge, but mantle doesn't yet have target-aware endpoints to drive them.
        </p>
      </div>
    {:else if remoteStatus === 'bus-driven'}
      <div class="info-box">
        <p>
          <strong>{remoteName}</strong> has no standalone configuration — its behavior is driven by other modules over the bus. Configure this edge tentacle's <a href="/services/gateway{remoteTargetSuffix}">Gateway</a> instead.
        </p>
      </div>
    {/if}
  {:else if !data.error}
    <div class="info-box">
      <p>This module is not in the desired state for <strong>{data.target}</strong>. Add it from the <a href="/fleet/{encodeURIComponent(data.remoteGroup)}/{encodeURIComponent(data.remoteNode)}">fleet page</a>.</p>
    </div>
  {/if}
{:else}
  <div class="status-header">
    <div class="status-info">
      <h1>{serviceNames[data.serviceType] || data.serviceType}</h1>
      <p class="description">{description}</p>
    </div>
    <span class="status-badge" class:running={isRunning} class:stopped={!isRunning}>
      {isRunning ? 'Running' : 'Stopped'}
    </span>
  </div>


  {#if data.error}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {/if}

  {#if isInfra}
    <!-- Infrastructure services don't need instances/enable-disable -->
  {:else if (data.instances?.length ?? 0) > 0}
    {#each data.instances as instance}
      {@const enabled = getEnabled(instance)}
      <div class="enable-row">
        <span class="enable-label">
          Enabled
          {#if !enabled}
            <span class="disabled-badge">Disabled</span>
          {/if}
        </span>
        <label class="toggle" title={enabled ? 'Disable service' : 'Enable service'}>
          <input
            type="checkbox"
            checked={enabled}
            disabled={togglingModuleId === instance.moduleId}
            onchange={() => toggleEnabled(instance.moduleId, enabled)}
          />
          <span class="toggle-slider"></span>
        </label>
      </div>

      <div class="details" class:disabled={!enabled}>
        <div class="detail-row">
          <span class="label">Uptime</span>
          <span class="value">{formatUptime(instance.startedAt)}</span>
        </div>
        <div class="detail-row">
          <span class="label">Started</span>
          <span class="value">{formatDate(instance.startedAt)}</span>
        </div>
        {#if instance.version}
          <div class="detail-row">
            <span class="label">Version</span>
            <span class="value">{instance.version}</span>
          </div>
        {/if}
        {#if instance.metadata}
          {#each Object.entries(instance.metadata).filter(([key]) => key !== 'enabled') as [key, value]}
            <div class="detail-row">
              <span class="label">{key === 'publishRate' ? 'Throughput' : key}</span>
              <span class="value">{key === 'publishRate' ? `${value} metrics/s` : (typeof value === 'object' ? JSON.stringify(value) : String(value))}</span>
            </div>
          {/each}
        {/if}
      </div>
    {/each}
  {:else if !data.error && !isInfra}
    <div class="info-box">
      <p>No active instances of this service found.</p>
    </div>
  {/if}

  {#if data.serviceType === 'mqtt'}
    <StoreForwardStatus initialStatus={data.storeForwardStatus} />
  {/if}

  {#if data.serviceType === 'sparkplug-host'}
    <SparkplugHostNodes />
  {/if}
{/if}
</div>

<style lang="scss">
  .service-overview {
    padding: 2rem;
    max-width: 800px;
  }

  .status-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 2rem;
  }

  .status-info {
    h1 { font-size: 1.5rem; font-weight: 600; color: var(--theme-text); margin: 0; }
    .description { margin: 0.25rem 0 0; color: var(--theme-text-muted); font-size: 0.875rem; }
  }

  .status-badge {
    padding: 0.25rem 0.75rem;
    border-radius: var(--rounded-full);
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    &.running {
      background: var(--badge-green-bg);
      color: var(--badge-green-text);
    }
    &.stopped {
      background: var(--badge-muted-bg);
      color: var(--badge-muted-text);
    }
  }

  .enable-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1.5rem;
  }

  .enable-label {
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-text);
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .disabled-badge {
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.125rem 0.375rem;
    border-radius: var(--rounded-full);
    background: var(--badge-muted-bg);
    color: var(--badge-muted-text);
  }

  .details {
    &.disabled {
      opacity: 0.5;
    }
  }

  .toggle {
    position: relative;
    display: inline-block;
    width: 36px;
    height: 20px;
    cursor: pointer;
    flex-shrink: 0;

    input {
      opacity: 0;
      width: 0;
      height: 0;
    }
  }

  .toggle-slider {
    position: absolute;
    inset: 0;
    background: var(--theme-border);
    border-radius: 20px;
    transition: background 0.2s;

    &::before {
      content: '';
      position: absolute;
      width: 14px;
      height: 14px;
      left: 3px;
      bottom: 3px;
      background: var(--theme-text);
      border-radius: 50%;
      transition: transform 0.2s;
    }
  }

  .toggle input:checked + .toggle-slider {
    background: var(--green-500, #22c55e);
  }

  .toggle input:checked + .toggle-slider::before {
    transform: translateX(16px);
  }

  .toggle input:disabled + .toggle-slider {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
    padding: 0.375rem 0;
    min-width: 0;
    &:not(:last-child) {
      border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    }
  }

  .label {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .value {
    font-size: 0.8125rem;
    color: var(--theme-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .info-box {
    padding: 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--theme-text-muted); }
    a { color: var(--theme-primary); text-decoration: none; &:hover { text-decoration: underline; } }
    .sep { color: var(--theme-border); margin: 0 0.25rem; }
    &.error {
      border-color: var(--red-500, #ef4444);
      display: flex;
      align-items: center;
      justify-content: space-between;
      p { color: var(--red-500, #ef4444); }
    }
    &.warning {
      background: rgba(245, 158, 11, 0.08);
      border-color: rgba(245, 158, 11, 0.25);
      display: flex;
      align-items: flex-start;
      gap: 0.75rem;
      svg { flex-shrink: 0; margin-top: 0.0625rem; color: var(--amber-500, #f59e0b); }
      p { color: var(--theme-text-muted); }
    }
  }
</style>
