<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { invalidateAll } from '$app/navigation';
  import { apiDelete } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  let pollHandle: ReturnType<typeof setInterval> | null = null;

  let deleteTarget: { groupId: string; nodeId: string; deviceCount: number } | null = $state(null);
  let deleteConfirmInput = $state('');
  let deleting = $state(false);

  const deleteTargetKey = $derived(deleteTarget ? `${deleteTarget.groupId}/${deleteTarget.nodeId}` : '');

  function openDelete(groupId: string, nodeId: string, deviceCount: number) {
    deleteTarget = { groupId, nodeId, deviceCount };
    deleteConfirmInput = '';
  }

  function closeDelete() {
    if (deleting) return;
    deleteTarget = null;
    deleteConfirmInput = '';
  }

  async function confirmDelete() {
    if (!deleteTarget || deleteConfirmInput !== deleteTargetKey) return;
    deleting = true;
    const { groupId, nodeId } = deleteTarget;
    const result = await apiDelete<{ repoError?: string }>(
      `/fleet/nodes/${encodeURIComponent(groupId)}/${encodeURIComponent(nodeId)}`,
    );
    deleting = false;
    if (result.error) {
      saltState.addNotification({ message: `Failed to delete ${groupId}/${nodeId}: ${result.error.error}`, type: 'error' });
      return;
    }
    if (result.data?.repoError) {
      saltState.addNotification({ message: `Evicted ${groupId}/${nodeId}; repo cleanup warning: ${result.data.repoError}`, type: 'warning' });
    } else {
      saltState.addNotification({ message: `Evicted ${groupId}/${nodeId}`, type: 'success' });
    }
    deleteTarget = null;
    deleteConfirmInput = '';
    await invalidateAll();
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

  function nodeHref(groupId: string, nodeId: string): string {
    return `/fleet/${encodeURIComponent(groupId)}/${encodeURIComponent(nodeId)}`;
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
            <th>Modules</th>
            <th class="num">Devices</th>
            <th>Last Seen</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each data.nodes as n (n.groupId + '/' + n.nodeId)}
            <tr class="row" onclick={() => (window.location.href = nodeHref(n.groupId, n.nodeId))}>
              <td class="mono">{n.groupId}</td>
              <td class="mono">{n.nodeId}</td>
              <td>
                <span class="badge" class:online={n.online} class:offline={!n.online}>
                  {n.online ? 'Online' : 'Offline'}
                </span>
              </td>
              <td>
                {#if n.modulesError}
                  <span class="muted module-err" title={n.modulesError}>repo error</span>
                {:else if !n.modules || n.modules.length === 0}
                  <span class="muted module-empty">—</span>
                {:else}
                  <div class="module-chips">
                    {#each n.modules as mod (mod.id)}
                      <span class="module-chip" class:stopped={!mod.running} title={mod.running ? 'running' : 'stopped'}>
                        {mod.id}
                      </span>
                    {/each}
                  </div>
                {/if}
              </td>
              <td class="num mono">{Object.keys(n.devices ?? {}).length}</td>
              <td class="muted">{formatRelative(n.lastSeen)}</td>
              <td class="actions">
                <a class="link-btn" href={nodeHref(n.groupId, n.nodeId)} onclick={(e) => e.stopPropagation()}>
                  Configure →
                </a>
                <button
                  class="delete-btn"
                  title="Evict this node from the fleet"
                  onclick={(e) => { e.stopPropagation(); openDelete(n.groupId, n.nodeId, Object.keys(n.devices ?? {}).length); }}
                >
                  Delete
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

{#if deleteTarget}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onkeydown={(e) => { if (e.key === 'Escape') closeDelete(); }} onclick={closeDelete}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h2>Evict node from fleet</h2>
      <p class="modal-warning">
        This removes <strong>{deleteTarget.groupId}/{deleteTarget.nodeId}</strong> from the inventory and
        deletes its bare gitops repo. The edge's next <code>git pull</code> will fail —
        that's how it learns it's no longer adopted here. If the edge keeps publishing
        NBIRTH it will reappear in inventory until you stop or reconfigure it.
      </p>
      <p class="modal-confirm-label">Type <strong>{deleteTargetKey}</strong> to confirm:</p>
      <input
        class="modal-input"
        bind:value={deleteConfirmInput}
        placeholder={deleteTargetKey}
        autocomplete="off"
        spellcheck="false"
        onkeydown={(e) => { if (e.key === 'Enter' && deleteConfirmInput === deleteTargetKey) confirmDelete(); }}
      />
      <div class="modal-actions">
        <button class="modal-cancel-btn" onclick={closeDelete} disabled={deleting}>Cancel</button>
        <button
          class="modal-delete-btn"
          disabled={deleteConfirmInput !== deleteTargetKey || deleting}
          onclick={confirmDelete}
        >{deleting ? 'Evicting...' : 'Evict node'}</button>
      </div>
    </div>
  </div>
{/if}

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

  .fleet-table tbody tr.row {
    cursor: pointer;
    transition: background 120ms;

    &:hover {
      background: color-mix(in srgb, var(--theme-primary) 6%, transparent);
    }
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

  .module-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }

  .module-chip {
    display: inline-block;
    font-family: var(--font-mono, monospace);
    font-size: 0.7rem;
    padding: 0.125rem 0.4rem;
    border-radius: var(--rounded-sm, 0.25rem);
    background: var(--badge-muted-bg);
    color: var(--badge-muted-text);
    border: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);

    &.stopped {
      opacity: 0.55;
      text-decoration: line-through;
    }
  }

  .module-err {
    font-size: 0.75rem;
    font-style: italic;
  }

  .module-empty {
    font-family: var(--font-mono, monospace);
    font-size: 0.875rem;
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

  .actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    justify-content: flex-end;
  }

  .delete-btn {
    padding: 0.25rem 0.625rem;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-danger, #ef4444);
    background: color-mix(in srgb, var(--theme-danger, #ef4444) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--theme-danger, #ef4444) 35%, transparent);
    border-radius: var(--rounded-md, 0.375rem);
    cursor: pointer;
    transition: background 120ms;

    &:hover {
      background: color-mix(in srgb, var(--theme-danger, #ef4444) 18%, transparent);
    }
  }

  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 1000;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
  }

  .modal {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg, 0.5rem);
    padding: 1.5rem;
    max-width: 520px;
    width: 100%;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 1rem;
    }
  }

  .modal-warning {
    font-size: 0.8125rem;
    color: var(--theme-danger, #ef4444);
    line-height: 1.5;
    margin: 0 0 1rem;

    code {
      font-family: var(--font-mono, monospace);
      background: color-mix(in srgb, var(--theme-danger, #ef4444) 12%, transparent);
      padding: 0 0.25rem;
      border-radius: var(--rounded-sm, 0.25rem);
    }
  }

  .modal-confirm-label {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 0.5rem;
  }

  .modal-input {
    width: 100%;
    padding: 0.375rem 0.5rem;
    font-size: 0.8125rem;
    font-family: var(--font-mono, monospace);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--theme-input-bg, var(--theme-surface));
    color: var(--theme-text);
    box-sizing: border-box;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }

  .modal-cancel-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;

    &:hover:not(:disabled) {
      background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface));
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .modal-delete-btn {
    padding: 0.375rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--theme-danger, #ef4444);
    color: white;
    cursor: pointer;

    &:hover:not(:disabled) {
      opacity: 0.9;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }
</style>
