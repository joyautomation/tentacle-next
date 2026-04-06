<script lang="ts">
  import type { PageData } from './$types';
  import { page } from '$app/stores';
  import { onMount, onDestroy } from 'svelte';
  import { subscribe } from '$lib/api/subscribe';

  let { data }: { data: PageData } = $props();

  const serviceType = $derived($page.params.serviceType ?? '');

  // ═══════════════════════════════════════════════════════════════════════
  // Network status state
  // ═══════════════════════════════════════════════════════════════════════

  interface NetworkAddress {
    family: string;
    address: string;
    prefixlen: number;
    scope: string;
    broadcast: string | null;
  }

  interface NetworkInterfaceStats {
    rxBytes: number;
    txBytes: number;
    rxPackets: number;
    txPackets: number;
    rxErrors: number;
    txErrors: number;
    rxDropped: number;
    txDropped: number;
  }

  interface NetworkInterface {
    name: string;
    operstate: string;
    carrier: boolean;
    speed: number | null;
    duplex: string | null;
    mac: string;
    mtu: number;
    type: number;
    flags: string[];
    addresses: NetworkAddress[];
    statistics: NetworkInterfaceStats;
  }

  interface NetworkState {
    moduleId: string;
    timestamp: string;
    interfaces: NetworkInterface[];
  }

  let interfaces: NetworkInterface[] = $state([]);
  let lastUpdated: Date | null = $state(null);
  let unsubscribe: (() => void) | null = null;

  $effect(() => {
    if (data.networkState) {
      interfaces = data.networkState.interfaces;
      lastUpdated = new Date(data.networkState.timestamp);
    }
  });

  // ═══════════════════════════════════════════════════════════════════════
  // Nftables status state
  // ═══════════════════════════════════════════════════════════════════════

  interface NatRule {
    id: string;
    enabled: boolean;
    protocol: string;
    connectingDevices: string;
    incomingInterface: string;
    outgoingInterface: string;
    natAddr: string;
    originalPort: string;
    translatedPort: string;
    deviceAddr: string;
    deviceName: string;
    doubleNat: boolean;
    doubleNatAddr: string;
    comment: string;
  }

  interface NftablesConfig {
    natRules: NatRule[];
  }

  let natRules: NatRule[] = $state([]);

  $effect(() => {
    if (data.nftablesConfig) {
      natRules = data.nftablesConfig.natRules ?? [];
    }
  });

  // ═══════════════════════════════════════════════════════════════════════
  // Subscriptions
  // ═══════════════════════════════════════════════════════════════════════

  onMount(() => {
    if (serviceType === 'nftables') {
      unsubscribe = subscribe<NftablesConfig>(
        '/nftables/config/stream',
        (result) => {
          natRules = result.natRules ?? [];
        }
      );
    } else {
      unsubscribe = subscribe<NetworkState>(
        '/network/interfaces/stream',
        (result) => {
          interfaces = result.interfaces;
          lastUpdated = new Date(result.timestamp);
        }
      );
    }
  });

  onDestroy(() => {
    unsubscribe?.();
  });

  // ═══════════════════════════════════════════════════════════════════════
  // Network helpers
  // ═══════════════════════════════════════════════════════════════════════

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    const value = bytes / Math.pow(1024, i);
    return `${value.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
  }

  function formatSpeed(speed: number | null): string {
    if (speed === null || speed <= 0) return 'Unknown';
    if (speed >= 1000) return `${speed / 1000} Gbps`;
    return `${speed} Mbps`;
  }

  function formatNumber(n: number): string {
    return n.toLocaleString();
  }

  function formatTime(date: Date): string {
    return date.toLocaleTimeString();
  }

  const physicalInterfaces = $derived(
    interfaces.filter((i) => i.type === 1 || i.addresses.length > 0)
  );

  // ═══════════════════════════════════════════════════════════════════════
  // Nftables helpers
  // ═══════════════════════════════════════════════════════════════════════

  const enabledRules = $derived(natRules.filter((r) => r.enabled));
  const disabledRules = $derived(natRules.filter((r) => !r.enabled));

  function formatPort(port: string): string {
    return port || '*';
  }

  function formatProtocol(proto: string): string {
    return proto.toUpperCase();
  }
</script>

{#if serviceType === 'nftables'}
<!-- ═══════════════════════════════════════════════════════════════════════ -->
<!-- Nftables Status Page                                                   -->
<!-- ═══════════════════════════════════════════════════════════════════════ -->
<div class="status-page">
  {#if data.error}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {/if}

  <div class="status-header">
    <div>
      <h1>NAT Rules Status</h1>
      <p class="subtitle">
        {enabledRules.length} active rule{enabledRules.length !== 1 ? 's' : ''}
        {#if disabledRules.length > 0}
          &middot; {disabledRules.length} disabled
        {/if}
      </p>
    </div>
  </div>

  {#if natRules.length > 0}
    <div class="rules-list">
      {#each natRules as rule (rule.id)}
        <div class="card" class:card-disabled={!rule.enabled}>
          <div class="card-header">
            <div class="rule-header-left">
              <span class="operstate-badge" class:up={rule.enabled} class:down={!rule.enabled}>
                {rule.enabled ? 'Active' : 'Disabled'}
              </span>
              <span class="rule-name">
                {#if rule.deviceName}
                  {rule.deviceName}
                {:else}
                  {rule.deviceAddr}
                {/if}
              </span>
            </div>
            <span class="proto-badge">{formatProtocol(rule.protocol)}</span>
          </div>
          <div class="card-body">
            <!-- Traffic Flow -->
            <div class="section">
              <h3>Traffic Flow</h3>
              <div class="flow-diagram">
                <div class="flow-step">
                  <span class="flow-label">Source</span>
                  <span class="flow-value mono">{rule.connectingDevices || 'any'}</span>
                </div>
                <div class="flow-arrow">&#8594;</div>
                <div class="flow-step">
                  <span class="flow-label">{rule.incomingInterface}</span>
                  <span class="flow-value mono">{rule.natAddr}{rule.originalPort ? `:${rule.originalPort}` : ''}</span>
                </div>
                <div class="flow-arrow">&#8594;</div>
                <div class="flow-step">
                  <span class="flow-label">{rule.outgoingInterface}</span>
                  <span class="flow-value mono">{rule.deviceAddr}{rule.translatedPort ? `:${rule.translatedPort}` : ''}</span>
                </div>
              </div>
            </div>

            <!-- Details -->
            <div class="section">
              <h3>Details</h3>
              <div class="detail-row">
                <span class="label">NAT Address</span>
                <span class="value mono">{rule.natAddr}</span>
              </div>
              <div class="detail-row">
                <span class="label">Device IP</span>
                <span class="value mono">{rule.deviceAddr}</span>
              </div>
              {#if rule.protocol === 'tcp' || rule.protocol === 'udp'}
                <div class="detail-row">
                  <span class="label">Ports</span>
                  <span class="value mono">{formatPort(rule.originalPort)} &#8594; {formatPort(rule.translatedPort)}</span>
                </div>
              {/if}
              <div class="detail-row">
                <span class="label">Interfaces</span>
                <span class="value mono">{rule.incomingInterface} &#8594; {rule.outgoingInterface}</span>
              </div>
              {#if rule.doubleNat}
                <div class="detail-row">
                  <span class="label">Double NAT</span>
                  <span class="value mono">{rule.doubleNatAddr || 'masquerade'}</span>
                </div>
              {/if}
              {#if rule.comment}
                <div class="detail-row">
                  <span class="label">Comment</span>
                  <span class="value">{rule.comment}</span>
                </div>
              {/if}
            </div>
          </div>
        </div>
      {/each}
    </div>
  {:else if !data.error}
    <div class="info-box">
      <p>No NAT rules configured. Is tentacle-nftables running?</p>
    </div>
  {/if}
</div>

{:else}
<!-- ═══════════════════════════════════════════════════════════════════════ -->
<!-- Network Status Page                                                    -->
<!-- ═══════════════════════════════════════════════════════════════════════ -->
<div class="status-page">
  {#if data.error}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {/if}

  <div class="status-header">
    <div>
      <h1>Network Interfaces</h1>
      <p class="subtitle">
        {physicalInterfaces.length} interface{physicalInterfaces.length !== 1 ? 's' : ''}
        {#if lastUpdated}
          &middot; Updated {formatTime(lastUpdated)}
        {/if}
      </p>
    </div>
  </div>

  {#if physicalInterfaces.length > 0}
    <div class="interface-grid">
      {#each physicalInterfaces as iface (iface.name)}
        <div class="card">
          <div class="card-header">
            <span class="iface-name">{iface.name}</span>
            <span class="operstate-badge" class:up={iface.operstate === 'up'} class:down={iface.operstate === 'down'}>
              {iface.operstate}
            </span>
          </div>
          <div class="card-body">
            <!-- Connection -->
            <div class="section">
              <h3>Connection</h3>
              <div class="detail-row">
                <span class="label">Carrier</span>
                <span class="value" class:text-green={iface.carrier} class:text-red={!iface.carrier}>
                  {iface.carrier ? 'Connected' : 'No link'}
                </span>
              </div>
              {#if iface.speed !== null && iface.speed > 0}
                <div class="detail-row">
                  <span class="label">Speed</span>
                  <span class="value">{formatSpeed(iface.speed)}</span>
                </div>
              {/if}
              {#if iface.duplex}
                <div class="detail-row">
                  <span class="label">Duplex</span>
                  <span class="value">{iface.duplex}</span>
                </div>
              {/if}
            </div>

            <!-- Addresses -->
            {#if iface.addresses.length > 0}
              <div class="section">
                <h3>Addresses</h3>
                {#each iface.addresses as addr}
                  <div class="detail-row">
                    <span class="label">{addr.family === 'inet' ? 'IPv4' : 'IPv6'}</span>
                    <span class="value mono">{addr.address}/{addr.prefixlen}</span>
                  </div>
                {/each}
              </div>
            {/if}

            <!-- Identity -->
            <div class="section">
              <h3>Identity</h3>
              <div class="detail-row">
                <span class="label">MAC</span>
                <span class="value mono">{iface.mac}</span>
              </div>
              <div class="detail-row">
                <span class="label">MTU</span>
                <span class="value">{iface.mtu}</span>
              </div>
              {#if iface.flags.length > 0}
                <div class="detail-row">
                  <span class="label">Flags</span>
                  <span class="value flags">{iface.flags.join(', ')}</span>
                </div>
              {/if}
            </div>

            <!-- Statistics -->
            <div class="section">
              <h3>Statistics</h3>
              <div class="stats-grid">
                <div class="stat">
                  <span class="stat-label">RX Bytes</span>
                  <span class="stat-value">{formatBytes(iface.statistics.rxBytes)}</span>
                </div>
                <div class="stat">
                  <span class="stat-label">TX Bytes</span>
                  <span class="stat-value">{formatBytes(iface.statistics.txBytes)}</span>
                </div>
                <div class="stat">
                  <span class="stat-label">RX Packets</span>
                  <span class="stat-value">{formatNumber(iface.statistics.rxPackets)}</span>
                </div>
                <div class="stat">
                  <span class="stat-label">TX Packets</span>
                  <span class="stat-value">{formatNumber(iface.statistics.txPackets)}</span>
                </div>
                {#if iface.statistics.rxErrors > 0 || iface.statistics.txErrors > 0}
                  <div class="stat">
                    <span class="stat-label">RX Errors</span>
                    <span class="stat-value text-red">{formatNumber(iface.statistics.rxErrors)}</span>
                  </div>
                  <div class="stat">
                    <span class="stat-label">TX Errors</span>
                    <span class="stat-value text-red">{formatNumber(iface.statistics.txErrors)}</span>
                  </div>
                {/if}
                {#if iface.statistics.rxDropped > 0 || iface.statistics.txDropped > 0}
                  <div class="stat">
                    <span class="stat-label">RX Dropped</span>
                    <span class="stat-value text-amber">{formatNumber(iface.statistics.rxDropped)}</span>
                  </div>
                  <div class="stat">
                    <span class="stat-label">TX Dropped</span>
                    <span class="stat-value text-amber">{formatNumber(iface.statistics.txDropped)}</span>
                  </div>
                {/if}
              </div>
            </div>
          </div>
        </div>
      {/each}
    </div>
  {:else if !data.error}
    <div class="info-box">
      <p>No network interfaces detected. Is tentacle-network running?</p>
    </div>
  {/if}
</div>
{/if}

<style lang="scss">
  .status-page {
    padding: 2rem;
  }

  .status-header {
    margin-bottom: 1.5rem;

    h1 {
      font-size: 1.5rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }

    .subtitle {
      margin: 0.25rem 0 0;
      color: var(--theme-text-muted);
      font-size: 0.875rem;
    }
  }

  .interface-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(350px, 1fr));
    gap: 1rem;
  }

  .card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--theme-border);
    background: color-mix(in srgb, var(--theme-surface) 50%, var(--theme-background));
  }

  .iface-name {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--theme-text);
  }

  .operstate-badge {
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-full);
    font-size: 0.6875rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    background: var(--badge-muted-bg);
    color: var(--badge-muted-text);

    &.up {
      background: var(--badge-green-bg);
      color: var(--badge-green-text);
    }

    &.down {
      background: var(--badge-red-bg);
      color: var(--badge-red-text);
    }
  }

  .card-body {
    padding: 0.5rem 1rem 0.75rem;
  }

  .section {
    padding: 0.5rem 0;

    &:not(:last-child) {
      border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    }

    h3 {
      font-size: 0.6875rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--theme-text-muted);
      margin: 0 0 0.375rem;
    }
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
    padding: 0.25rem 0;
    min-width: 0;
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

  .mono {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
  }

  .flags {
    font-size: 0.75rem;
    text-align: right;
    max-width: 60%;
    word-break: break-word;
  }

  .text-green {
    color: var(--color-green-500, #22c55e);
  }

  .text-red {
    color: var(--color-red-500, #ef4444);
  }

  .text-amber {
    color: var(--color-amber-500, #f59e0b);
  }

  .stats-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.25rem 1rem;
  }

  .stat {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.125rem 0;
  }

  .stat-label {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
  }

  .stat-value {
    font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text);
  }

  .info-box {
    padding: 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);

    p {
      margin: 0;
      font-size: 0.875rem;
      color: var(--theme-text-muted);
    }

    &.error {
      border-color: var(--color-red-500, #ef4444);
      p { color: var(--color-red-500, #ef4444); }
    }
  }

  /* Nftables status styles */

  .rules-list {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .card-disabled {
    opacity: 0.6;
  }

  .rule-header-left {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .rule-name {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--theme-text);
  }

  .proto-badge {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.6875rem;
    font-weight: 600;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-md, 4px);
    color: var(--badge-blue-text);
    background: var(--badge-blue-bg);
  }

  .flow-diagram {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.375rem 0;
    flex-wrap: wrap;
  }

  .flow-step {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 0.375rem 0.625rem;
    border-radius: var(--rounded-md, 4px);
    background: color-mix(in srgb, var(--theme-border) 30%, transparent);
    min-width: 0;
  }

  .flow-label {
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
  }

  .flow-value {
    font-size: 0.75rem;
    color: var(--theme-text);
    word-break: break-all;
  }

  .flow-arrow {
    font-size: 1rem;
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }
</style>
