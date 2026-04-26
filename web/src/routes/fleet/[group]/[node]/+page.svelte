<script lang="ts">
  import { invalidateAll } from '$app/navigation';
  import { apiPut, apiDelete } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import type { PageData } from './$types';
  import {
    SERVICE_NAMES,
    REMOTE_CONFIG_STATUS,
    type RemoteConfigStatus,
  } from '$lib/constants/services';
  import type { FleetModule } from '$lib/types/fleet';

  let { data }: { data: PageData } = $props();

  const fleetBase = $derived(
    `/fleet/nodes/${encodeURIComponent(data.group)}/${encodeURIComponent(data.node)}/services`,
  );

  let busy = $state<Record<string, boolean>>({});

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

  let editingVersion = $state<string | null>(null);
  let versionDraft = $state('');

  function startEditVersion(svc: FleetModule) {
    editingVersion = svc.id;
    versionDraft = svc.version ?? 'latest';
  }

  async function commitVersion(svc: FleetModule) {
    if (!editingVersion) return;
    if (versionDraft === (svc.version ?? '')) {
      editingVersion = null;
      return;
    }
    busy = { ...busy, [svc.id]: true };
    const res = await apiPut<FleetModule>(`${fleetBase}/${encodeURIComponent(svc.id)}`, {
      version: versionDraft,
    });
    busy = { ...busy, [svc.id]: false };
    if (res.error) {
      saltState.addNotification({ message: `Failed to set version: ${res.error.error}`, type: 'error' });
      return;
    }
    editingVersion = null;
    await invalidateAll();
  }

  let confirmRemove = $state<string | null>(null);

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

  let addOpen = $state(false);
  let addName = $state('');
  let addVersion = $state('latest');
  let addRunning = $state(true);
  let adding = $state(false);

  const existingNames = $derived(new Set(data.services.map((s) => s.id)));
  const addNameValid = $derived(/^[a-z0-9_-]{1,63}$/.test(addName));
  const addNameTaken = $derived(existingNames.has(addName));

  function openAdd() {
    addOpen = true;
    addName = '';
    addVersion = 'latest';
    addRunning = true;
  }

  function closeAdd() {
    if (adding) return;
    addOpen = false;
  }

  async function submitAdd() {
    if (!addNameValid || addNameTaken) return;
    adding = true;
    const res = await apiPut<FleetModule>(`${fleetBase}/${encodeURIComponent(addName)}`, {
      running: addRunning,
      version: addVersion,
    });
    adding = false;
    if (res.error) {
      saltState.addNotification({ message: `Failed to add ${addName}: ${res.error.error}`, type: 'error' });
      return;
    }
    saltState.addNotification({ message: `Added ${addName} (edge syncs within ~5s)`, type: 'success' });
    addOpen = false;
    await invalidateAll();
  }

  const serviceCatalog = Object.entries(SERVICE_NAMES)
    .map(([id, label]) => ({ id, label }))
    .sort((a, b) => a.label.localeCompare(b.label));

  // We render one tile per known service type so the operator sees the full
  // catalogue of remote-configurable modules. Once edge tentacles publish a
  // module list (Phase 3 SP-B verbs), we'll filter to "what this node has".
  interface Tile {
    serviceType: string;
    name: string;
    status: RemoteConfigStatus;
  }

  const tiles: Tile[] = Object.entries(SERVICE_NAMES)
    .map(([serviceType, name]) => ({
      serviceType,
      name,
      status: REMOTE_CONFIG_STATUS[serviceType] ?? 'bus-driven',
    }))
    .sort((a, b) => {
      // configurable first, coming-soon next, bus-driven last
      const order = { configurable: 0, 'coming-soon': 1, 'bus-driven': 2 };
      return (order[a.status] - order[b.status]) || a.name.localeCompare(b.name);
    });

  const targetSuffix = $derived(
    `?target=${encodeURIComponent(`${data.group}/${data.node}`)}`,
  );

  function tileHref(serviceType: string): string {
    return `/services/${serviceType}${targetSuffix}`;
  }

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
    <div class="section-head">
      <div>
        <h2 class="section-title">Modules</h2>
        <p class="section-hint">
          Desired services from this node's gitops repo. Toggling commits to <code class="mono">main</code>;
          the edge picks it up on its next sync (≤ poll interval).
        </p>
      </div>
      <button class="btn-primary" onclick={openAdd}>+ Add module</button>
    </div>

    {#if data.servicesError}
      <div class="info-box error">
        <h3>Couldn't load services</h3>
        <p>{data.servicesError}</p>
      </div>
    {:else if data.services.length === 0}
      <div class="info-box muted">
        <p>No services defined yet for this node. Click <strong>Add module</strong> to create one.</p>
      </div>
    {:else}
      <table class="modules-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Version</th>
            <th>Desired</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each data.services as svc (svc.id)}
            <tr>
              <td class="mono">{svc.id}</td>
              <td>
                {#if editingVersion === svc.id}
                  <input
                    class="version-input mono"
                    bind:value={versionDraft}
                    onblur={() => commitVersion(svc)}
                    onkeydown={(e) => {
                      if (e.key === 'Enter') commitVersion(svc);
                      if (e.key === 'Escape') editingVersion = null;
                    }}
                    autofocus
                  />
                {:else}
                  <button class="version-btn mono" onclick={() => startEditVersion(svc)} title="Click to edit">
                    {svc.version || 'latest'}
                  </button>
                {/if}
              </td>
              <td>
                <label class="toggle">
                  <input
                    type="checkbox"
                    checked={svc.running}
                    disabled={busy[svc.id]}
                    onchange={() => toggleService(svc)}
                  />
                  <span class="toggle-label" class:running={svc.running}>
                    {svc.running ? 'Running' : 'Stopped'}
                  </span>
                </label>
              </td>
              <td class="row-actions">
                {#if confirmRemove === svc.id}
                  <button class="danger-btn" disabled={busy[svc.id]} onclick={() => removeService(svc.id)}>
                    {busy[svc.id] ? 'Removing...' : 'Confirm remove'}
                  </button>
                  <button class="ghost-btn" onclick={() => (confirmRemove = null)}>Cancel</button>
                {:else}
                  <button class="ghost-btn" onclick={() => (confirmRemove = svc.id)}>Remove</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  <section class="section">
    <h2 class="section-title">Configurable modules</h2>
    <p class="section-hint">
      These modules own their own gitops-managed config. Clicking opens the configurator in remote mode — changes commit to mantle's git repo for this edge node.
    </p>
    <div class="grid">
      {#each tiles.filter((t) => t.status === 'configurable') as tile}
        <a class="tile configurable" href={tileHref(tile.serviceType)}>
          <span class="tile-name">{tile.name}</span>
          <span class="tile-status">Ready</span>
        </a>
      {/each}
    </div>
  </section>

  <section class="section">
    <h2 class="section-title">Coming soon</h2>
    <p class="section-hint">
      These modules have their own settings/config, but mantle doesn't yet have target-aware endpoints for them. They'll light up as backend support lands.
    </p>
    <div class="grid">
      {#each tiles.filter((t) => t.status === 'coming-soon') as tile}
        <div class="tile coming-soon">
          <span class="tile-name">{tile.name}</span>
          <span class="tile-status">Soon</span>
        </div>
      {/each}
    </div>
  </section>

  <section class="section">
    <h2 class="section-title">Bus-driven (no remote config)</h2>
    <p class="section-hint">
      These modules have no standalone configuration — their behavior is driven by other modules over the bus (e.g. EtherNet/IP and PROFINET scanners are configured via Gateway sources).
    </p>
    <div class="grid">
      {#each tiles.filter((t) => t.status === 'bus-driven') as tile}
        <div class="tile bus-driven">
          <span class="tile-name">{tile.name}</span>
          <span class="tile-status">N/A</span>
        </div>
      {/each}
    </div>
  </section>
</div>

{#if addOpen}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onkeydown={(e) => { if (e.key === 'Escape') closeAdd(); }} onclick={closeAdd}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h2>Add module to {data.group}/{data.node}</h2>
      <p class="modal-hint">
        Writes <code class="mono">config/services/{addName || 'NAME'}.yaml</code> on
        <code class="mono">main</code>. The edge orchestrator will start the module on its next sync.
      </p>

      <label class="form-label">Module name</label>
      <input
        class="form-input mono"
        bind:value={addName}
        placeholder="e.g. modbus-server"
        list="module-catalog"
        autocomplete="off"
        spellcheck="false"
      />
      <datalist id="module-catalog">
        {#each serviceCatalog as svc}
          <option value={svc.id}>{svc.label}</option>
        {/each}
      </datalist>
      {#if addName && !addNameValid}
        <p class="form-error">Lowercase letters, digits, dash and underscore only.</p>
      {:else if addNameTaken}
        <p class="form-error">A module named <strong>{addName}</strong> already exists.</p>
      {/if}

      <label class="form-label">Version</label>
      <input class="form-input mono" bind:value={addVersion} placeholder="latest" autocomplete="off" />

      <label class="form-check">
        <input type="checkbox" bind:checked={addRunning} />
        Start running immediately (<code class="mono">spec.running: true</code>)
      </label>

      <div class="modal-actions">
        <button class="modal-cancel-btn" onclick={closeAdd} disabled={adding}>Cancel</button>
        <button
          class="modal-submit-btn"
          disabled={!addNameValid || addNameTaken || adding}
          onclick={submitAdd}
        >{adding ? 'Adding...' : 'Add module'}</button>
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

  .breadcrumb {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.875rem;
    margin-bottom: 1rem;

    a {
      color: var(--theme-text-muted);
      text-decoration: none;

      &:hover {
        color: var(--theme-primary);
      }
    }

    .separator {
      color: var(--theme-border);
    }

    .current {
      color: var(--theme-text);
      font-weight: 500;
    }
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

    &.warn {
      color: var(--theme-warning, #d97706);
    }
  }

  .dot {
    opacity: 0.5;
  }

  .info-box {
    padding: 1rem 1.25rem;
    border-radius: var(--rounded-md, 0.5rem);
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    margin-bottom: 1.5rem;

    h3 {
      margin: 0 0 0.25rem;
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
    }

    p {
      margin: 0;
      color: var(--theme-text-muted);
      font-size: 0.875rem;
    }

    &.error h3 {
      color: var(--theme-danger, #ef4444);
    }
  }

  .section {
    margin-bottom: 2rem;
  }

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
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 0.5rem;
  }

  .tile {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.75rem 0.875rem;
    border-radius: var(--rounded-md, 0.375rem);
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    text-decoration: none;
    color: var(--theme-text);
    font-size: 0.875rem;
  }

  .tile-name {
    font-weight: 500;
  }

  .tile-status {
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-full, 999px);
  }

  .tile.configurable {
    border-color: color-mix(in srgb, var(--theme-primary) 35%, transparent);
    background: color-mix(in srgb, var(--theme-primary) 6%, var(--theme-surface));
    transition: background 120ms;

    &:hover {
      background: color-mix(in srgb, var(--theme-primary) 14%, var(--theme-surface));
    }

    .tile-status {
      color: var(--theme-primary);
      background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
    }
  }

  .tile.coming-soon {
    opacity: 0.7;

    .tile-status {
      color: var(--theme-text-muted);
      background: color-mix(in srgb, var(--theme-border) 50%, transparent);
    }
  }

  .tile.bus-driven {
    opacity: 0.55;

    .tile-name {
      color: var(--theme-text-muted);
    }

    .tile-status {
      color: var(--theme-text-muted);
      background: transparent;
      border: 1px solid var(--theme-border);
    }
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

  .muted {
    color: var(--theme-text-muted);
  }

  .mono {
    font-family: var(--font-mono, monospace);
  }

  .section-head {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 1rem;
    margin-bottom: 0.875rem;

    .section-title { margin-bottom: 0.25rem; }
    .section-hint { margin: 0; }
  }

  .btn-primary {
    padding: 0.4rem 0.875rem;
    font-size: 0.8125rem;
    font-weight: 500;
    color: white;
    background: var(--theme-primary);
    border: none;
    border-radius: var(--rounded-md, 0.375rem);
    cursor: pointer;
    white-space: nowrap;

    &:hover { opacity: 0.9; }
  }

  .modules-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.875rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.5rem);
    overflow: hidden;

    th, td {
      padding: 0.625rem 0.875rem;
      text-align: left;
      border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    }

    th {
      font-weight: 500;
      font-size: 0.75rem;
      letter-spacing: 0.05em;
      text-transform: uppercase;
      color: var(--theme-text-muted);
      background: color-mix(in srgb, var(--theme-surface) 80%, var(--theme-border) 20%);
    }

    tbody tr:last-child td { border-bottom: none; }
  }

  .row-actions {
    text-align: right;
    display: flex;
    justify-content: flex-end;
    gap: 0.375rem;
  }

  .version-btn {
    font-size: 0.8125rem;
    padding: 0.125rem 0.4rem;
    background: transparent;
    border: 1px dashed transparent;
    border-radius: var(--rounded-sm, 0.25rem);
    color: var(--theme-text);
    cursor: text;

    &:hover {
      border-color: var(--theme-border);
      background: color-mix(in srgb, var(--theme-text) 4%, transparent);
    }
  }

  .version-input {
    font-size: 0.8125rem;
    padding: 0.125rem 0.4rem;
    border: 1px solid var(--theme-primary);
    border-radius: var(--rounded-sm, 0.25rem);
    background: var(--theme-input-bg, var(--theme-surface));
    color: var(--theme-text);
    width: 9rem;
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

  .ghost-btn {
    padding: 0.25rem 0.625rem;
    font-size: 0.8125rem;
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.375rem);
    color: var(--theme-text);
    cursor: pointer;

    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface)); }
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
    max-width: 480px;
    width: 100%;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 0.5rem;
    }
  }

  .modal-hint {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 1rem;
    line-height: 1.5;

    code { font-size: 0.75rem; }
  }

  .form-label {
    display: block;
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
    margin: 0.75rem 0 0.25rem;
  }

  .form-input {
    width: 100%;
    padding: 0.375rem 0.5rem;
    font-size: 0.875rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--theme-input-bg, var(--theme-surface));
    color: var(--theme-text);
    box-sizing: border-box;
  }

  .form-error {
    margin: 0.375rem 0 0;
    font-size: 0.75rem;
    color: var(--theme-danger, #ef4444);
  }

  .form-check {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin: 1rem 0 0;
    font-size: 0.8125rem;
    color: var(--theme-text);
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1.25rem;
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
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .modal-submit-btn {
    padding: 0.375rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--theme-primary);
    color: white;
    cursor: pointer;

    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }
</style>
