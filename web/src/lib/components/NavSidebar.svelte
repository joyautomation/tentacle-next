<script lang="ts">
  import { page } from '$app/stores';
  import {
    XMark,
    Home,
    CpuChip,
    Signal,
    ServerStack,
    ArrowsRightLeft,
    GlobeAlt,
    ComputerDesktop,
    ShieldCheck,
    CircleStack,
    Squares2x2,
    PlusCircle
  } from '@joyautomation/salt/icons';

  interface Service {
    serviceType: string;
    moduleId: string;
    enabled: boolean;
  }

  interface ModuleRegistryInfo {
    moduleId: string;
    repo: string;
    description: string;
    category: string;
    runtime: string;
  }

  interface DesiredService {
    moduleId: string;
    version: string;
    running: boolean;
  }

  interface Props {
    services: Service[];
    availableModules: ModuleRegistryInfo[];
    desiredServices: DesiredService[];
    open: boolean;
  }

  let { services, availableModules = [], desiredServices = [], open = $bindable(false) }: Props = $props();

  const uniqueServices = $derived(
    [...new Map(services.map((s) => [s.serviceType, s])).values()]
  );

  // Modules that are in the registry but not currently running (no heartbeat)
  // and not already in desired_services (not pending install)
  const uninstalledModules = $derived(() => {
    const runningModuleIds = new Set(services.map((s) => s.moduleId));
    const desiredModuleIds = new Set(desiredServices.map((d) => d.moduleId));
    // Exclude core modules (graphql, web, orchestrator) — they're always present
    return availableModules.filter(
      (m) => m.category === 'optional' && !runningModuleIds.has(m.moduleId) && !desiredModuleIds.has(m.moduleId)
    );
  });

  const serviceIcons: Record<string, typeof Squares2x2> = {
    plc: CpuChip,
    ethernetip: CpuChip,
    mqtt: Signal,
    nats: ServerStack,
    gateway: ArrowsRightLeft,
    network: GlobeAlt,
    nftables: ShieldCheck,
    snmp: ComputerDesktop,
    opcua: CircleStack
  };

  /** Map moduleId to a display label */
  const moduleLabels: Record<string, string> = {
    'tentacle-ethernetip': 'EtherNet/IP',
    'tentacle-opcua': 'OPC UA',
    'tentacle-snmp': 'SNMP',
    'tentacle-mqtt': 'MQTT',
    'tentacle-history': 'History',
    'tentacle-modbus': 'Modbus',
    'tentacle-modbus-server': 'Modbus Server',
    'tentacle-network': 'Network',
    'tentacle-nftables': 'NFTables',
  };

  /** Map moduleId to an icon */
  const moduleIcons: Record<string, typeof Squares2x2> = {
    'tentacle-ethernetip': CpuChip,
    'tentacle-opcua': CircleStack,
    'tentacle-snmp': ComputerDesktop,
    'tentacle-mqtt': Signal,
    'tentacle-history': ServerStack,
    'tentacle-modbus': CpuChip,
    'tentacle-modbus-server': CpuChip,
    'tentacle-network': GlobeAlt,
    'tentacle-nftables': ShieldCheck,
  };

  const serviceLabels: Record<string, string> = {
    plc: 'PLC',
    ethernetip: 'EtherNet/IP',
    mqtt: 'MQTT',
    nats: 'NATS',
    gateway: 'Gateway',
    network: 'Network',
    nftables: 'NFTables',
    snmp: 'SNMP',
    opcua: 'OPC-UA'
  };

  function getIcon(serviceType: string) {
    return serviceIcons[serviceType.toLowerCase()] ?? Squares2x2;
  }

  function getLabel(serviceType: string) {
    return serviceLabels[serviceType.toLowerCase()] ?? serviceType;
  }

  function getModuleIcon(moduleId: string) {
    return moduleIcons[moduleId] ?? Squares2x2;
  }

  function getModuleLabel(moduleId: string) {
    return moduleLabels[moduleId] ?? moduleId.replace('tentacle-', '');
  }

  function close() {
    open = false;
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="sidebar-backdrop" class:visible={open} onclick={close} role="presentation" aria-hidden="true"></div>

<nav class="sidebar" class:open aria-label="Service navigation" aria-hidden={!open}>
  <div class="sidebar-header">
    <span class="sidebar-title">Navigation</span>
    <button class="sidebar-close" onclick={close} aria-label="Close navigation">
      <XMark size="1.25rem" />
    </button>
  </div>

  <ul class="sidebar-nav">
    <li>
      <a
        href="/"
        class="sidebar-item"
        class:active={$page.url.pathname === '/'}
        onclick={close}
      >
        <Home size="1.25rem" />
        <span>Topology</span>
      </a>
    </li>

    {#if uniqueServices.length > 0}
      <li class="sidebar-section-label">Services</li>
      {#each uniqueServices as service}
        {@const Icon = getIcon(service.serviceType)}
        <li>
          <a
            href="/services/{service.serviceType}"
            class="sidebar-item"
            class:active={$page.url.pathname.startsWith('/services/' + service.serviceType)}
            onclick={close}
          >
            <Icon size="1.25rem" />
            <span>{getLabel(service.serviceType)}</span>
            {#if !service.enabled}
              <span class="disabled-badge">off</span>
            {/if}
          </a>
        </li>
      {/each}
    {/if}

    {#if uninstalledModules().length > 0}
      <li class="sidebar-section-label">Available Modules</li>
      {#each uninstalledModules() as mod}
        {@const Icon = getModuleIcon(mod.moduleId)}
        <li>
          <a
            href="/modules/{mod.moduleId}"
            class="sidebar-item"
            class:active={$page.url.pathname.startsWith('/modules/' + mod.moduleId)}
            onclick={close}
          >
            <Icon size="1.25rem" />
            <span>{getModuleLabel(mod.moduleId)}</span>
            <span class="available-badge">
              <PlusCircle size="0.875rem" />
            </span>
          </a>
        </li>
      {/each}
    {/if}
  </ul>
</nav>

<style lang="scss">
  .sidebar-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    z-index: 199;
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.25s ease;

    &.visible {
      opacity: 1;
      pointer-events: auto;
    }
  }

  .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    width: 16rem;
    background: var(--theme-background);
    border-right: 1px solid var(--theme-border);
    z-index: 200;
    display: flex;
    flex-direction: column;
    transform: translateX(-100%);
    will-change: transform;
    transition: transform 0.25s ease;
    overflow-y: auto;

    &.open {
      transform: translateX(0);
    }
  }

  .sidebar-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 1rem;
    height: var(--header-height);
    border-bottom: 1px solid var(--theme-border);
    flex-shrink: 0;
  }

  .sidebar-title {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .sidebar-close {
    display: flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--theme-text-muted);
    padding: 0.25rem;
    border-radius: var(--rounded-lg);
    transition:
      background 0.15s,
      color 0.15s;

    &:hover {
      background: var(--theme-surface);
      color: var(--theme-text);
    }
  }

  .sidebar-nav {
    list-style: none;
    margin: 0;
    padding: 0.5rem 0;
    flex: 1;
  }

  .sidebar-section-label {
    font-size: 0.6875rem;
    font-weight: 600;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding: 0.75rem 1rem 0.25rem;
  }

  .sidebar-item {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.5rem 1rem;
    color: var(--theme-text-muted);
    text-decoration: none;
    font-size: 0.9375rem;
    font-weight: 500;
    transition:
      background 0.15s,
      color 0.15s;

    &:hover {
      background: var(--theme-surface);
      color: var(--theme-text);
    }

    &.active {
      background: var(--theme-surface);
      color: var(--theme-primary);
    }
  }

  .disabled-badge {
    margin-left: auto;
    font-size: 0.625rem;
    font-weight: 600;
    padding: 0.125rem 0.375rem;
    border-radius: var(--rounded-full);
    background: var(--badge-muted-bg);
    color: var(--badge-muted-text);
    border: 1px solid var(--badge-muted-border);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .available-badge {
    margin-left: auto;
    display: flex;
    align-items: center;
    color: var(--theme-text-muted);
    opacity: 0.5;
    transition: opacity 0.15s, color 0.15s;
  }

  .sidebar-item:hover .available-badge {
    opacity: 1;
    color: var(--theme-primary);
  }
</style>
