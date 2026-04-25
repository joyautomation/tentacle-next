<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { invalidateAll } from '$app/navigation';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  let pollHandle: ReturnType<typeof setInterval> | null = null;

  function formatRelative(ts: number): string {
    if (!ts) return '—';
    const secs = Math.floor((Date.now() - ts) / 1000);
    if (secs < 5) return 'just now';
    if (secs < 60) return `${secs}s ago`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
    if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
    return `${Math.floor(secs / 86400)}d ago`;
  }

  function targetParam(groupId: string, nodeId: string): string {
    return encodeURIComponent(`${groupId}/${nodeId}`);
  }

  onMount(() => {
    pollHandle = setInterval(() => {
      invalidateAll();
    }, 5000);
  });

  onDestroy(() => {
    if (pollHandle) clearInterval(pollHandle);
  });
</script>

<div class="page">
  <header class="page-header">
    <div class="header-content">
      <h1>Fleet</h1>
      <p class="subtitle">Edge tentacles known to this mantle (via sparkplug-host inventory). Pick a node to configure it remotely.</p>
    </div>
    <div class="header-actions">
      <button class="btn btn-secondary" onclick={() => invalidateAll()}>Refresh</button>
    </div>
  </header>

  {#if data.error}
    <div class="info-box error">
      <h3>Inventory unavailable</h3>
      <p>{data.error}</p>
      <p class="hint">Make sure the <code>sparkplug-host</code> module is running on this mantle.</p>
    </div>
  {:else if data.nodes.length === 0}
    <div class="info-box muted">
      <h3>No edge nodes observed yet</h3>
      <p>Mantle hasn't received an NBIRTH from any tentacle. Check that edge tentacles are publishing Sparkplug B to this broker.</p>
    </div>
  {:else}
    <div class="content">
      <table class="fleet-table">
        <thead>
          <tr>
            <th>Group</th>
            <th>Node</th>
            <th>Status</th>
            <th class="num">Devices</th>
            <th class="num">Metrics</th>
            <th>Last Seen</th>
            <th>Configure</th>
          </tr>
        </thead>
        <tbody>
          {#each data.nodes as n (n.groupId + '/' + n.nodeId)}
            <tr>
              <td class="mono">{n.groupId}</td>
              <td class="mono">{n.nodeId}</td>
              <td>
                <span class="badge" class:online={n.online} class:offline={!n.online}>
                  {n.online ? 'Online' : 'Offline'}
                </span>
              </td>
              <td class="num mono">{Object.keys(n.devices ?? {}).length}</td>
              <td class="num mono">{n.metricCount}</td>
              <td class="muted">{formatRelative(n.lastSeen)}</td>
              <td>
                <a class="link-btn" href="/services/modbus/tag-config?target={targetParam(n.groupId, n.nodeId)}">
                  Modbus tags
                </a>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style lang="scss">
  .page {
    padding: 1.5rem 2rem;
    max-width: 1400px;
    margin: 0 auto;
  }

  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 1rem;
    padding-bottom: 1.25rem;
    margin-bottom: 1.5rem;
    border-bottom: 1px solid var(--theme-border);
  }

  .header-content h1 {
    margin: 0 0 0.25rem;
    font-size: 1.5rem;
    font-weight: 600;
    color: var(--theme-text);
  }

  .subtitle {
    margin: 0;
    color: var(--theme-text-muted);
    font-size: 0.875rem;
  }

  .info-box {
    padding: 1rem 1.25rem;
    border-radius: var(--rounded-md, 0.5rem);
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);

    h3 {
      margin: 0 0 0.25rem;
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
    }

    p {
      margin: 0.25rem 0 0;
      color: var(--theme-text-muted);
      font-size: 0.875rem;
    }

    .hint {
      font-size: 0.8125rem;
      opacity: 0.85;
    }

    &.error h3 {
      color: var(--theme-danger, #ef4444);
    }
  }

  .content {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.5rem);
    overflow: hidden;
  }

  .fleet-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.875rem;
  }

  .fleet-table th,
  .fleet-table td {
    padding: 0.625rem 0.875rem;
    text-align: left;
    border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
  }

  .fleet-table th {
    font-weight: 500;
    font-size: 0.75rem;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-surface) 80%, var(--theme-border) 20%);
  }

  .fleet-table tbody tr:last-child td {
    border-bottom: none;
  }

  .fleet-table .num {
    text-align: right;
  }

  .mono {
    font-family: var(--font-mono, monospace);
  }

  .muted {
    color: var(--theme-text-muted);
  }

  .badge {
    display: inline-block;
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-full, 999px);
    text-transform: uppercase;
    letter-spacing: 0.05em;

    &.online {
      background: var(--badge-green-bg);
      color: var(--badge-green-text);
    }

    &.offline {
      background: var(--badge-muted-bg);
      color: var(--badge-muted-text);
    }
  }

  .link-btn {
    display: inline-block;
    padding: 0.25rem 0.625rem;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-primary);
    background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--theme-primary) 35%, transparent);
    border-radius: var(--rounded-md, 0.375rem);
    text-decoration: none;
    transition: background 120ms;

    &:hover {
      background: color-mix(in srgb, var(--theme-primary) 18%, transparent);
    }
  }

  .btn {
    padding: 0.5rem 1rem;
    font-size: 0.875rem;
    border-radius: var(--rounded-md, 0.375rem);
    cursor: pointer;
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    color: var(--theme-text);

    &:hover {
      background: color-mix(in srgb, var(--theme-surface) 80%, var(--theme-text) 8%);
    }
  }
</style>
