<script lang="ts">
  import { invalidateAll } from '$app/navigation';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  let refreshing = $state(false);

  async function refresh() {
    refreshing = true;
    await invalidateAll();
    refreshing = false;
  }

  // Group services by serviceType
  type Service = (typeof data.services)[number];
  const servicesByType = $derived(() => {
    const grouped: Record<string, Service[]> = {};
    for (const service of data.services) {
      if (!grouped[service.serviceType]) {
        grouped[service.serviceType] = [];
      }
      grouped[service.serviceType].push(service);
    }
    return grouped;
  });

  // Format uptime
  function formatUptime(ms: number): string {
    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    if (days > 0) return `${days}d ${hours % 24}h`;
    if (hours > 0) return `${hours}h ${minutes % 60}m`;
    if (minutes > 0) return `${minutes}m ${seconds % 60}s`;
    return `${seconds}s`;
  }

  // Format timestamp
  function formatTime(timestamp: number | null): string {
    if (!timestamp) return 'Never';
    return new Date(timestamp).toLocaleString();
  }

  // Get service icon
  function getServiceIcon(type: string): string {
    switch (type) {
      case 'ethernetip': return '⚡';
      case 'graphql': return '◉';
      case 'mqtt': return '📡';
      default: return '●';
    }
  }
</script>

<div class="page">
  <header class="page-header">
    <div class="header-content">
      <h1>NATS Diagnostics</h1>
      <p class="subtitle">Monitor NATS connection, services, and KV buckets</p>
    </div>
    <div class="header-actions">
      <button class="btn btn-secondary" onclick={refresh} disabled={refreshing}>
        {refreshing ? 'Refreshing...' : 'Refresh'}
      </button>
    </div>
  </header>

  {#if data.error}
    <div class="info-box error">
      <h3>Connection Error</h3>
      <p>{data.error}</p>
      <p class="hint">Make sure NATS and tentacle-graphql are running.</p>
    </div>
  {:else}
    <div class="content">
      <!-- Connection Status -->
      <section class="section">
        <h2>Connection Status</h2>
        <div class="status-card">
          <div class="status-indicator" class:connected={data.services.length > 0}>
            <span class="dot"></span>
            {data.services.length > 0 ? 'Connected' : 'No Services'}
          </div>
          <div class="status-stats">
            <div class="stat">
              <span class="stat-value">{data.services.length}</span>
              <span class="stat-label">Active Services</span>
            </div>
          </div>
        </div>
      </section>

      <!-- Active Services -->
      <section class="section">
        <h2>Active Services</h2>
        {#if data.services.length === 0}
          <div class="empty-state">
            <p>No active services detected.</p>
            <p class="hint">Start a tentacle service to see it here.</p>
          </div>
        {:else}
          <div class="services-grid">
            {#each Object.entries(servicesByType()) as [serviceType, typeServices]}
              <div class="project-card">
                <div class="project-header">
                  <h3>{serviceType}</h3>
                  <span class="service-count">{typeServices.length} instance{typeServices.length !== 1 ? 's' : ''}</span>
                </div>
                <div class="service-list">
                  {#each typeServices as service}
                    <div class="service-item">
                      <span class="service-icon">{getServiceIcon(service.serviceType)}</span>
                      <div class="service-info">
                        <span class="service-type">{service.serviceType}</span>
                        <span class="service-instance">{service.moduleId}</span>
                      </div>
                      <div class="service-uptime">
                        <span class="uptime-value">{formatUptime(service.uptime)}</span>
                        <span class="uptime-label">uptime</span>
                      </div>
                    </div>
                  {/each}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </section>
    </div>
  {/if}
</div>

<style lang="scss">
  .page {
    padding: 2rem;
    max-width: 1200px;
  }

  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 2rem;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .header-content {
    h1 {
      margin: 0;
      font-size: 1.5rem;
      font-weight: 600;
    }

    .subtitle {
      margin: 0.25rem 0 0;
      font-size: 0.875rem;
      color: var(--theme-text-muted);
    }
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 1rem;
  }

  .last-refresh {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
  }

  .btn {
    padding: 0.5rem 1rem;
    border-radius: var(--rounded-lg);
    font-size: 0.875rem;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s ease;
    border: none;

    &.btn-secondary {
      background: var(--theme-surface);
      color: var(--theme-text);
      border: 1px solid var(--theme-border);

      &:hover:not(:disabled) {
        background: var(--theme-surface-hover);
      }

      &:disabled {
        opacity: 0.5;
        cursor: not-allowed;
      }
    }
  }

  .info-box {
    padding: 1.5rem;
    border-radius: var(--rounded-xl);
    margin-bottom: 1.5rem;

    &.error {
      background: rgba(239, 68, 68, 0.1);
      border: 1px solid rgba(239, 68, 68, 0.3);

      h3 {
        color: #ef4444;
        margin: 0 0 0.5rem;
        font-size: 1rem;
      }

      p {
        margin: 0;
        color: var(--theme-text);
      }

      .hint {
        margin-top: 0.5rem;
        font-size: 0.875rem;
        color: var(--theme-text-muted);
      }
    }
  }

  .section {
    margin-bottom: 2rem;

    h2 {
      font-size: 1rem;
      font-weight: 600;
      margin: 0 0 1rem;
      color: var(--theme-text);
    }
  }

  .status-card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-xl);
    padding: 1.5rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 1rem;
  }

  .status-indicator {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-weight: 500;

    .dot {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      background: var(--color-red-500, #ef4444);
    }

    &.connected .dot {
      background: var(--color-green-500, #22c55e);
    }
  }

  .status-stats {
    display: flex;
    gap: 2rem;
  }

  .stat {
    text-align: center;

    .stat-value {
      display: block;
      font-size: 1.5rem;
      font-weight: 600;
      color: var(--theme-primary);
    }

    .stat-label {
      font-size: 0.75rem;
      color: var(--theme-text-muted);
    }
  }

  .loading-placeholder,
  .empty-state {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-xl);
    padding: 2rem;
    text-align: center;
    color: var(--theme-text-muted);

    .hint {
      font-size: 0.875rem;
      margin-top: 0.5rem;
    }
  }

  .services-grid {
    display: grid;
    gap: 1rem;
    grid-template-columns: repeat(auto-fill, minmax(350px, 1fr));
  }

  .project-card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-xl);
    overflow: hidden;
  }

  .project-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1rem 1.25rem;
    border-bottom: 1px solid var(--theme-border);
    background: var(--theme-bg);

    h3 {
      margin: 0;
      font-size: 0.875rem;
      font-weight: 600;
    }

    .service-count {
      font-size: 0.75rem;
      color: var(--theme-text-muted);
    }
  }

  .service-list {
    padding: 0.5rem;
  }

  .service-item {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem;
    border-radius: var(--rounded-lg);

    &:hover {
      background: var(--theme-surface-hover);
    }
  }

  .service-icon {
    font-size: 1.25rem;
    width: 2rem;
    text-align: center;
  }

  .service-info {
    flex: 1;
    min-width: 0;

    .service-type {
      display: block;
      font-weight: 500;
      font-size: 0.875rem;
      text-transform: capitalize;
    }

    .service-instance {
      display: block;
      font-size: 0.75rem;
      color: var(--theme-text-muted);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }

  .service-uptime {
    text-align: right;

    .uptime-value {
      display: block;
      font-size: 0.875rem;
      font-weight: 500;
      color: var(--color-green-500, #22c55e);
    }

    .uptime-label {
      font-size: 0.625rem;
      color: var(--theme-text-muted);
      text-transform: uppercase;
    }
  }

  .buckets-table {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-xl);
    overflow: hidden;
  }

  .table-header,
  .table-row {
    display: grid;
    grid-template-columns: 2fr 1fr 1fr 1.5fr;
    gap: 1rem;
    padding: 0.875rem 1.25rem;
    align-items: center;
  }

  .table-header {
    background: var(--theme-bg);
    border-bottom: 1px solid var(--theme-border);
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--theme-text-muted);
  }

  .table-row {
    font-size: 0.875rem;
    border-bottom: 1px solid var(--theme-border);

    &:last-child {
      border-bottom: none;
    }

    &:hover {
      background: var(--theme-surface-hover);
    }
  }

  .bucket-name code {
    font-family: 'Space Grotesk', monospace;
    font-size: 0.8125rem;
    background: var(--theme-bg);
    padding: 0.25rem 0.5rem;
    border-radius: var(--rounded-md);
  }

  .status-badge {
    display: inline-block;
    padding: 0.25rem 0.5rem;
    border-radius: var(--rounded-full);
    font-size: 0.75rem;
    font-weight: 500;
    background: rgba(239, 68, 68, 0.1);
    color: #ef4444;

    &.active {
      background: rgba(34, 197, 94, 0.1);
      color: #22c55e;
    }
  }

  .bucket-vars {
    font-weight: 500;
  }

  .bucket-activity {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
  }
</style>
