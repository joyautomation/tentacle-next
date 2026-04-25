<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api/client';

  type Device = {
    deviceId: string;
    online: boolean;
    lastSeen: number;
    metricCount: number;
  };
  type Node = {
    groupId: string;
    nodeId: string;
    online: boolean;
    lastSeen: number;
    firstSeen: number;
    bdSeq: number;
    devices: Record<string, Device> | null;
    nbirthTime?: number;
    ndeathTime?: number;
    metricCount: number;
  };

  let nodes: Node[] = $state([]);
  let loaded = $state(false);
  let errMsg = $state<string | null>(null);

  async function poll() {
    const result = await api<Node[] | null>('/fleet/nodes');
    if (result.error) {
      errMsg = result.error.error;
      return;
    }
    nodes = (result.data ?? []).slice().sort((a, b) => {
      const k = `${a.groupId}/${a.nodeId}`.localeCompare(`${b.groupId}/${b.nodeId}`);
      return k;
    });
    errMsg = null;
    loaded = true;
  }

  function formatRelative(ts: number): string {
    if (!ts) return '—';
    const secs = Math.floor((Date.now() - ts) / 1000);
    if (secs < 5) return 'just now';
    if (secs < 60) return `${secs}s ago`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
    if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
    return `${Math.floor(secs / 86400)}d ago`;
  }

  onMount(() => {
    poll();
    const interval = setInterval(poll, 2500);
    return () => clearInterval(interval);
  });
</script>

<div class="fleet-nodes">
  <h2>Edge Nodes</h2>
  {#if errMsg}
    <div class="error">{errMsg}</div>
  {:else if !loaded}
    <div class="muted">Loading…</div>
  {:else if nodes.length === 0}
    <div class="muted">No nodes observed yet. Waiting for NBIRTH frames.</div>
  {:else}
    <table>
      <thead>
        <tr>
          <th>Group</th>
          <th>Node</th>
          <th>Status</th>
          <th>bdSeq</th>
          <th>Devices</th>
          <th>Metrics</th>
          <th>Last Seen</th>
        </tr>
      </thead>
      <tbody>
        {#each nodes as n (n.groupId + '/' + n.nodeId)}
          <tr>
            <td>{n.groupId}</td>
            <td>{n.nodeId}</td>
            <td>
              <span class="badge" class:online={n.online} class:offline={!n.online}>
                {n.online ? 'Online' : 'Offline'}
              </span>
            </td>
            <td class="mono">{n.bdSeq}</td>
            <td class="mono">{Object.keys(n.devices ?? {}).length}</td>
            <td class="mono">{n.metricCount}</td>
            <td class="muted">{formatRelative(n.lastSeen)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style lang="scss">
  .fleet-nodes {
    margin-top: 1.5rem;
  }
  h2 {
    font-size: 1rem;
    font-weight: 600;
    margin: 0 0 0.75rem;
    color: var(--theme-text);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.8125rem;
  }
  th, td {
    text-align: left;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
  }
  th {
    font-weight: 500;
    color: var(--theme-text-muted);
    background: var(--theme-surface);
  }
  .muted {
    color: var(--theme-text-muted);
    font-size: 0.8125rem;
  }
  .error {
    color: var(--red-500, #ef4444);
    font-size: 0.8125rem;
  }
  .mono {
    font-family: var(--font-mono, monospace);
  }
  .badge {
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-full);
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
</style>
