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
    PlusCircle,
    RocketLaunch,
    ArrowPath
  } from '@joyautomation/salt/icons';
  import { getServiceName, getModuleName } from '$lib/constants/services';
  import { apiPost } from '$lib/api/client';
  import { goto } from '$app/navigation';

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
      .sort((a, b) => getServiceName(a.serviceType).localeCompare(getServiceName(b.serviceType)))
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
    api: ServerStack,
    plc: CpuChip,
    ethernetip: CpuChip,
    mqtt: Signal,
    nats: ServerStack,
    gateway: ArrowsRightLeft,
    network: GlobeAlt,
    nftables: ShieldCheck,
    orchestrator: ArrowsRightLeft,
    snmp: ComputerDesktop,
    opcua: CircleStack
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

  function getIcon(serviceType: string) {
    return serviceIcons[serviceType.toLowerCase()] ?? Squares2x2;
  }

  function getModuleIcon(moduleId: string) {
    return moduleIcons[moduleId] ?? Squares2x2;
  }

  let showResetModal = $state(false);
  let resetConfirmInput = $state('');
  let resetting = $state(false);

  async function performFactoryReset() {
    resetting = true;
    const result = await apiPost<{ success: boolean }>('/system/factory-reset');
    resetting = false;
    if (result.data?.success) {
      showResetModal = false;
      resetConfirmInput = '';
      sessionStorage.removeItem('setup_dismissed');
      goto('/setup');
    }
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
    <li>
      <a
        href="/setup"
        class="sidebar-item"
        class:active={$page.url.pathname === '/setup'}
        onclick={close}
      >
        <RocketLaunch size="1.25rem" />
        <span>Setup</span>
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
            <span>{getServiceName(service.serviceType)}</span>
            {#if !service.enabled}
              <span class="disabled-badge">off</span>
            {/if}
          </a>
        </li>
      {/each}
    {/if}

  </ul>

  {#if uninstalledModules().length > 0}
    <ul class="sidebar-nav sidebar-modules">
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
            <span>{getModuleName(mod.moduleId)}</span>
            <span class="available-badge">
              <PlusCircle size="0.875rem" />
            </span>
          </a>
        </li>
      {/each}
    </ul>
  {/if}

  <div class="sidebar-footer">
    <button class="sidebar-item reset-btn" onclick={() => { showResetModal = true; }}>
      <ArrowPath size="1.25rem" />
      <span>Factory Reset</span>
    </button>
  </div>
</nav>

{#if showResetModal}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onkeydown={(e) => { if (e.key === 'Escape') { showResetModal = false; resetConfirmInput = ''; } }} onclick={() => { showResetModal = false; resetConfirmInput = ''; }}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h2>Factory Reset</h2>
      <p class="modal-warning">This will erase all configuration and return Tentacle to its initial state. All modules, gateways, devices, variables, and settings will be permanently removed.</p>
      <p class="modal-confirm-label">Type <strong>RESET</strong> to confirm:</p>
      <input
        class="modal-input"
        bind:value={resetConfirmInput}
        placeholder="RESET"
        onkeydown={(e) => { if (e.key === 'Enter' && resetConfirmInput === 'RESET') performFactoryReset(); }}
      />
      <div class="modal-actions">
        <button class="modal-cancel-btn" onclick={() => { showResetModal = false; resetConfirmInput = ''; }}>Cancel</button>
        <button
          class="modal-delete-btn"
          disabled={resetConfirmInput !== 'RESET' || resetting}
          onclick={performFactoryReset}
        >{resetting ? 'Resetting...' : 'Factory Reset'}</button>
      </div>
    </div>
  </div>
{/if}

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

  .sidebar-modules {
    flex: 0;
    border-top: 1px solid var(--theme-border);
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

  .sidebar-footer {
    border-top: 1px solid var(--theme-border);
    padding: 0.5rem 0;
    flex-shrink: 0;
  }

  .reset-btn {
    width: 100%;
    background: none;
    border: none;
    cursor: pointer;
    font: inherit;
    color: var(--theme-text-muted);

    &:hover {
      color: var(--badge-red-text, #ef4444);
    }
  }

  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 300;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .modal {
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1.5rem;
    max-width: 28rem;
    width: 90%;

    h2 {
      margin: 0 0 1rem;
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
    }
  }

  .modal-warning {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 1rem;
    line-height: 1.5;
  }

  .modal-confirm-label {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 0.5rem;
  }

  .modal-input {
    width: 100%;
    padding: 0.5rem 0.75rem;
    font-size: 0.875rem;
    font-family: 'IBM Plex Mono', monospace;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
    color: var(--theme-text);
    box-sizing: border-box;

    &:focus {
      outline: none;
      border-color: var(--theme-primary);
    }
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }

  .modal-cancel-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;

    &:hover {
      background: var(--theme-border);
    }
  }

  .modal-delete-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: var(--rounded-md);
    background: #ef4444;
    color: white;
    cursor: pointer;

    &:hover:not(:disabled) {
      background: #dc2626;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }
</style>
