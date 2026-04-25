<script lang="ts">
  import type { PageData } from './$types';
  import {
    SERVICE_NAMES,
    REMOTE_CONFIG_STATUS,
    type RemoteConfigStatus,
  } from '$lib/constants/services';

  let { data }: { data: PageData } = $props();

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
</style>
