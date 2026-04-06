<script lang="ts">
  import type { PageData } from './$types';
  import { invalidateAll } from '$app/navigation';
  import { apiPut } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import StoreForwardStatus from '$lib/components/StoreForwardStatus.svelte';

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
  };

  function formatUptime(seconds: number): string {
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

  let togglingModuleId: string | null = $state(null);

  async function toggleEnabled(moduleId: string, currentEnabled: boolean) {
    togglingModuleId = moduleId;
    try {
      const result = await apiPut<{ moduleId: string; enabled: boolean }>(`/services/${moduleId}/enabled`, { enabled: !currentEnabled });

      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        const newState = result.data?.enabled;
        saltState.addNotification({
          message: `${moduleId} ${newState ? 'enabled' : 'disabled'}`,
          type: 'success',
        });
        await invalidateAll();
      }
    } catch (err) {
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
      <div class="enable-row">
        <span class="enable-label">
          Enabled
          {#if !instance.enabled}
            <span class="disabled-badge">Disabled</span>
          {/if}
        </span>
        <label class="toggle" title={instance.enabled ? 'Disable service' : 'Enable service'}>
          <input
            type="checkbox"
            checked={instance.enabled}
            disabled={togglingModuleId === instance.moduleId}
            onchange={() => toggleEnabled(instance.moduleId, instance.enabled)}
          />
          <span class="toggle-slider"></span>
        </label>
      </div>

      <div class="details" class:disabled={!instance.enabled}>
        <div class="detail-row">
          <span class="label">Uptime</span>
          <span class="value">{formatUptime(instance.uptime)}</span>
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
    background: var(--color-green-500, #22c55e);
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
    &.error {
      border-color: var(--color-red-500, #ef4444);
      display: flex;
      align-items: center;
      justify-content: space-between;
      p { color: var(--color-red-500, #ef4444); }
    }
  }
</style>
