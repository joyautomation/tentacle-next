<script lang="ts">
  import { page } from '$app/stores';
  import { getServiceName } from '$lib/constants/services';
  import Tabs, { type TabItem } from '$lib/components/Tabs.svelte';

  let { children } = $props();

  const serviceType = $derived($page.params.serviceType ?? '');
  const serviceName = $derived(getServiceName(serviceType));

  const currentTab = $derived.by(() => {
    const path = $page.url?.pathname ?? '';
    const suffixes = [
      'logs', 'traffic', 'info', 'tag-config', 'status', 'config', 'settings',
      'metrics', 'devices', 'oids', 'modules', 'history', 'tasks', 'programs',
      'workspace', 'trends'
    ];
    for (const s of suffixes) if (path.endsWith(`/${s}`)) return s;
    return 'default';
  });

  const tabConfig: Record<string, TabItem[]> = $derived({
    plc: [
      { id: 'default', label: 'Config', href: `/services/${serviceType}` },
      { id: 'workspace', label: 'Workspace', href: `/services/${serviceType}/workspace` },
      { id: 'info', label: 'Variables', href: `/services/${serviceType}/info` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    network: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'status', label: 'Status', href: `/services/${serviceType}/status` },
      { id: 'config', label: 'Config', href: `/services/${serviceType}/config` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    nftables: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'status', label: 'Status', href: `/services/${serviceType}/status` },
      { id: 'config', label: 'Config', href: `/services/${serviceType}/config` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    nats: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'traffic', label: 'Traffic', href: `/services/${serviceType}/traffic` }
    ],
    mqtt: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'metrics', label: 'Metrics', href: `/services/${serviceType}/metrics` },
      { id: 'settings', label: 'Settings', href: `/services/${serviceType}/settings` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    ethernetip: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'devices', label: 'Devices', href: `/services/${serviceType}/devices` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    profinetcontroller: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'devices', label: 'Devices', href: `/services/${serviceType}/devices` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    profinet: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'config', label: 'Config', href: `/services/${serviceType}/config` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    gateway: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'devices', label: 'Sources', href: `/services/${serviceType}/devices` },
      { id: 'tag-config', label: 'Variables', href: `/services/${serviceType}/tag-config` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    snmp: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'oids', label: 'OIDs', href: `/services/${serviceType}/oids` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    orchestrator: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'modules', label: 'Modules', href: `/services/${serviceType}/modules` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    caddy: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'settings', label: 'Settings', href: `/services/${serviceType}/settings` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    gitops: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'history', label: 'History', href: `/services/${serviceType}/history` },
      { id: 'settings', label: 'Settings', href: `/services/${serviceType}/settings` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    telemetry: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'settings', label: 'Settings', href: `/services/${serviceType}/settings` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    history: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'trends', label: 'Trends', href: `/services/${serviceType}/trends` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ],
    modbus: [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'tag-config', label: 'Tags', href: `/services/${serviceType}/tag-config` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ]
  });

  const tabs = $derived(
    tabConfig[serviceType] ?? [
      { id: 'default', label: 'Overview', href: `/services/${serviceType}` },
      { id: 'logs', label: 'Logs', href: `/services/${serviceType}/logs` }
    ]
  );
</script>

<div class="service-layout">
  <nav class="service-nav">
    <a href="/" class="back-link">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M19 12H5M12 19l-7-7 7-7"/>
      </svg>
      Topology
    </a>
    <span class="separator">/</span>
    <span class="current">{serviceName}</span>
  </nav>

  <div class="service-tabs">
    <Tabs {tabs} active={currentTab} ariaLabel="Service sections" />
  </div>

  {@render children()}
</div>

<style lang="scss">
  .service-layout {
    display: flex;
    flex-direction: column;
    min-height: calc(100vh - var(--header-height));
    overflow-x: hidden;
  }

  .service-nav {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 1rem 2rem;
    border-bottom: 1px solid var(--theme-border);
    font-size: 0.875rem;
  }

  .back-link {
    display: flex;
    align-items: center;
    gap: 0.375rem;
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

  .service-tabs {
    background: var(--theme-surface);
  }

  @media (max-width: 640px) {
    .service-nav {
      padding: 0.75rem 1rem;
    }
  }
</style>
