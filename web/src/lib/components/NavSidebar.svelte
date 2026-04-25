<script lang="ts">
  import { page } from '$app/stores';
  import { slide } from 'svelte/transition';
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
    ArrowPath,
    ArrowDownTray,
    ArrowUpTray,
    ChevronRight
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
    experimental?: boolean;
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
    appVersion?: string;
    mode?: string;
  }

  let { services, availableModules = [], desiredServices = [], open = $bindable(false), appVersion = '', mode = 'unknown' }: Props = $props();

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

  // Modules in desired_services but NOT running (no heartbeat) — failed or starting.
  // These need sidebar entries so users can reach the module page to fix config.
  const pendingModules = $derived(() => {
    const runningModuleIds = new Set(services.map((s) => s.moduleId));
    const registryById = new Map(availableModules.map((m) => [m.moduleId, m]));
    return desiredServices
      .filter((d) => !runningModuleIds.has(d.moduleId))
      .map((d) => ({
        moduleId: d.moduleId,
        info: registryById.get(d.moduleId),
      }));
  });

  type ModuleRole = 'client' | 'server' | 'data';

  const MODULE_ROLES: Record<string, ModuleRole> = {
    caddy: 'data',
    ethernetip: 'client',
    modbus: 'client',
    opcua: 'client',
    snmp: 'client',
    profinetcontroller: 'client',
    'ethernetip-server': 'server',
    'modbus-server': 'server',
    profinet: 'server',
    mqtt: 'client',
    history: 'data',
    network: 'data',
    nftables: 'data',
    gitops: 'data',
  };

  const ROLE_LABELS: Record<string, string> = {
    client: 'Protocol Clients',
    server: 'Protocol Servers',
  };

  const FOLDER_ROLES: ModuleRole[] = ['client', 'server'];

  /** Modules shown flat at the top level (no folder) */
  const rootModules = $derived(() =>
    uninstalledModules().filter((m) => (MODULE_ROLES[m.moduleId] ?? 'data') === 'data')
  );

  /** Modules grouped into collapsible folders */
  const folderGroups = $derived(() => {
    const mods = uninstalledModules();
    const groups: Record<string, ModuleRegistryInfo[]> = { client: [], server: [] };
    for (const m of mods) {
      const role = MODULE_ROLES[m.moduleId] ?? 'data';
      if (role !== 'data') groups[role].push(m);
    }
    return FOLDER_ROLES
      .filter((r) => groups[r].length > 0)
      .map((r) => ({ role: r, label: ROLE_LABELS[r], modules: groups[r] }));
  });

  let moduleSectionOpen = $state(false);
  let expandedRoles = $state<Set<ModuleRole>>(new Set());

  function toggleModuleSection() {
    moduleSectionOpen = !moduleSectionOpen;
  }

  function toggleRole(role: ModuleRole) {
    if (expandedRoles.has(role)) {
      expandedRoles.delete(role);
    } else {
      expandedRoles.add(role);
    }
    expandedRoles = new Set(expandedRoles);
  }

  // Lookup set for experimental modules — used to badge both running services and available modules
  const experimentalModuleIds = $derived(
    new Set(availableModules.filter((m) => m.experimental).map((m) => m.moduleId))
  );

  const serviceIcons: Record<string, typeof Squares2x2> = {
    api: ServerStack,
    caddy: ShieldCheck,
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

  // Export config
  function exportConfig() {
    close();
    window.location.href = '/api/v1/export';
  }

  // Import config
  interface ApplyResult {
    applied: { kind: string; name: string }[];
    skipped?: { kind: string; name: string; reason: string }[];
  }

  let showImportModal = $state(false);
  let importFileName = $state('');
  let importYaml = $state('');
  let importing = $state(false);
  let importResult = $state<ApplyResult | null>(null);
  let importError = $state('');
  let fileInput: HTMLInputElement;

  function openFilePicker() {
    close();
    fileInput.click();
  }

  function handleFileSelected(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importFileName = file.name;
    const reader = new FileReader();
    reader.onload = () => {
      importYaml = reader.result as string;
      importResult = null;
      importError = '';
      showImportModal = true;
    };
    reader.readAsText(file);
    input.value = '';
  }

  async function performImport() {
    importing = true;
    importError = '';
    try {
      const response = await fetch('/api/v1/apply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-yaml', 'X-Config-Source': 'gui' },
        body: importYaml,
      });
      if (!response.ok) {
        const text = await response.text();
        importError = text;
      } else {
        importResult = await response.json();
      }
    } catch (err) {
      importError = err instanceof Error ? err.message : 'Network error';
    }
    importing = false;
  }

  function closeImportModal() {
    showImportModal = false;
    importFileName = '';
    importYaml = '';
    importResult = null;
    importError = '';
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

    {#if services.some((s) => s.serviceType === 'sparkplug-host')}
      <li>
        <a
          href="/fleet"
          class="sidebar-item"
          class:active={$page.url.pathname === '/fleet' || $page.url.pathname.startsWith('/fleet/')}
          onclick={close}
        >
          <ServerStack size="1.25rem" />
          <span>Fleet</span>
        </a>
      </li>
    {/if}

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
            {#if experimentalModuleIds.has(service.moduleId) || !service.enabled}
              <span class="sidebar-item-badges">
                {#if experimentalModuleIds.has(service.moduleId)}
                  <span class="experimental-badge">exp</span>
                {/if}
                {#if !service.enabled}
                  <span class="disabled-badge">off</span>
                {/if}
              </span>
            {/if}
          </a>
        </li>
      {/each}
    {/if}

    {#if pendingModules().length > 0}
      <li class="sidebar-section-label">Not Running</li>
      {#each pendingModules() as pending}
        {@const Icon = getModuleIcon(pending.moduleId)}
        <li>
          <a
            href="/modules/{pending.moduleId}"
            class="sidebar-item"
            class:active={$page.url.pathname.startsWith('/modules/' + pending.moduleId)}
            onclick={close}
          >
            <Icon size="1.25rem" />
            <span>{getModuleName(pending.moduleId)}</span>
            <span class="sidebar-item-badges">
              {#if pending.info?.experimental}
                <span class="experimental-badge">exp</span>
              {/if}
              <span class="disabled-badge">down</span>
            </span>
          </a>
        </li>
      {/each}
    {/if}

  </ul>

  {#if uninstalledModules().length > 0}
    <div class="sidebar-modules">
      <button class="module-section-header" onclick={toggleModuleSection}>
        <span class="module-group-chevron" class:expanded={moduleSectionOpen}>
          <ChevronRight size="0.625rem" />
        </span>
        <span>Available Modules</span>
      </button>
      {#if moduleSectionOpen}
        <div transition:slide|local={{ duration: 150 }}>
          {#if rootModules().length > 0}
            <ul class="module-group-list">
              {#each rootModules() as mod}
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
                    <span class="sidebar-item-badges">
                      {#if mod.experimental}
                        <span class="experimental-badge">exp</span>
                      {/if}
                      <span class="available-badge">
                        <PlusCircle size="0.875rem" />
                      </span>
                    </span>
                  </a>
                </li>
              {/each}
            </ul>
          {/if}
          {#each folderGroups() as group}
            <button class="module-group-header" onclick={() => toggleRole(group.role)}>
              <span class="module-group-chevron" class:expanded={expandedRoles.has(group.role)}>
                <ChevronRight size="0.625rem" />
              </span>
              <span>{group.label}</span>
            </button>
            {#if expandedRoles.has(group.role)}
              <ul class="module-group-list" transition:slide|local={{ duration: 150 }}>
                {#each group.modules as mod}
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
                      <span class="sidebar-item-badges">
                        {#if mod.experimental}
                          <span class="experimental-badge">exp</span>
                        {/if}
                        <span class="available-badge">
                          <PlusCircle size="0.875rem" />
                        </span>
                      </span>
                    </a>
                  </li>
                {/each}
              </ul>
            {/if}
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <div class="sidebar-footer">
    <span class="sidebar-section-label">System {#if appVersion}<span class="version-label">{appVersion}</span>{/if}</span>
    <div class="sidebar-item runtime-item" title="Deployment mode: {mode}">
      <ServerStack size="1.25rem" />
      <span>Runtime</span>
      <span class="sidebar-item-badges">
        <span
          class="mode-badge"
          class:mode-dev={mode === 'dev'}
          class:mode-systemd={mode === 'systemd'}
          class:mode-docker={mode === 'docker'}
          class:mode-kubernetes={mode === 'kubernetes'}
          class:mode-unknown={mode === 'unknown'}
        >{mode}</span>
      </span>
    </div>
    <a href="/system" class="sidebar-item footer-btn" onclick={close}>
      <ComputerDesktop size="1.25rem" />
      <span>Updates</span>
    </a>
    <button class="sidebar-item footer-btn" onclick={exportConfig}>
      <ArrowDownTray size="1.25rem" />
      <span>Export Config</span>
    </button>
    <button class="sidebar-item footer-btn" onclick={openFilePicker}>
      <ArrowUpTray size="1.25rem" />
      <span>Import Config</span>
    </button>
    <button class="sidebar-item reset-btn" onclick={() => { showResetModal = true; }}>
      <ArrowPath size="1.25rem" />
      <span>Factory Reset</span>
    </button>
  </div>
</nav>

<input type="file" accept=".yaml,.yml" bind:this={fileInput} onchange={handleFileSelected} style="display:none" />

{#if showImportModal}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onkeydown={(e) => { if (e.key === 'Escape') closeImportModal(); }} onclick={closeImportModal}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal modal-wide" onclick={(e) => e.stopPropagation()}>
      {#if importResult}
        <h2>Import Complete</h2>
        <div class="import-results">
          {#if importResult.applied.length > 0}
            <p class="result-label">Applied ({importResult.applied.length}):</p>
            <ul class="result-list">
              {#each importResult.applied as r}
                <li class="result-applied">{r.kind}/{r.name}</li>
              {/each}
            </ul>
          {/if}
          {#if importResult.skipped && importResult.skipped.length > 0}
            <p class="result-label">Skipped ({importResult.skipped.length}):</p>
            <ul class="result-list">
              {#each importResult.skipped as r}
                <li class="result-skipped">{r.kind}/{r.name} — {r.reason}</li>
              {/each}
            </ul>
          {/if}
        </div>
        <div class="modal-actions">
          <button class="modal-cancel-btn" onclick={closeImportModal}>Close</button>
        </div>
      {:else}
        <h2>Import Config</h2>
        <p class="modal-warning">Apply configuration from <strong>{importFileName}</strong>. This will overwrite any matching resources in the current system.</p>
        {#if importError}
          <div class="import-error">{importError}</div>
        {/if}
        <div class="yaml-preview"><pre>{importYaml}</pre></div>
        <div class="modal-actions">
          <button class="modal-cancel-btn" onclick={closeImportModal}>Cancel</button>
          <button class="modal-apply-btn" onclick={performImport} disabled={importing}>
            {importing ? 'Applying...' : 'Apply'}
          </button>
        </div>
      {/if}
    </div>
  </div>
{/if}

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
    overflow-x: hidden;

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
    border-top: 1px solid var(--theme-border);
  }

  .module-section-header {
    border-radius: 0;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.6875rem;
    font-weight: 600;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    transition: color 0.15s;

    &:hover {
      color: var(--theme-text);
    }
  }

  .module-group-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.375rem 1rem;
    border-radius: 0;
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    transition: color 0.15s;

    &:hover {
      color: var(--theme-text);
    }
  }

  .module-group-chevron {
    display: inline-flex;
    transition: transform 0.15s ease;

    &.expanded {
      transform: rotate(90deg);
    }
  }

  .module-group-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .sidebar-section-label {
    display: flex;
    align-items: baseline;
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

  .sidebar-item-badges {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 0.375rem;
    flex-shrink: 0;
  }

  .disabled-badge {
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

  .experimental-badge {
    font-size: 0.5625rem;
    font-weight: 600;
    padding: 0.0625rem 0.3125rem;
    border-radius: var(--rounded-full);
    background: var(--badge-amber-bg);
    color: var(--badge-amber-text);
    border: 1px solid var(--badge-amber-border);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    flex-shrink: 0;
  }

  .available-badge {
    display: flex;
    align-items: center;
    color: var(--theme-text-muted);
    opacity: 0.5;
    transition: opacity 0.15s, color 0.15s;
  }

  .runtime-item {
    cursor: default;

    &:hover {
      background: none;
      color: var(--theme-text-muted);
    }
  }

  .mode-badge {
    font-size: 0.625rem;
    font-weight: 600;
    padding: 0.125rem 0.4375rem;
    border-radius: var(--rounded-full);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    border: 1px solid transparent;

    &.mode-dev {
      background: var(--badge-amber-bg);
      color: var(--badge-amber-text);
      border-color: var(--badge-amber-border);
    }

    &.mode-systemd {
      background: var(--badge-sky-bg);
      color: var(--badge-sky-text);
      border-color: var(--badge-sky-border);
    }

    &.mode-docker {
      background: var(--badge-blue-bg);
      color: var(--badge-blue-text);
      border-color: var(--badge-blue-border);
    }

    &.mode-kubernetes {
      background: var(--badge-purple-bg);
      color: var(--badge-purple-text);
      border-color: var(--badge-purple-border);
    }

    &.mode-unknown {
      background: var(--badge-muted-bg);
      color: var(--badge-muted-text);
      border-color: var(--badge-muted-border);
    }
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

  .footer-btn {
    width: 100%;
    background: none;
    border: none;
    cursor: pointer;
    font: inherit;
    color: var(--theme-text-muted);
    text-decoration: none;
  }

  .version-label {
    margin-left: auto;
    font-size: 0.625rem;
    font-family: 'IBM Plex Mono', monospace;
    font-weight: 400;
    text-transform: none;
    letter-spacing: normal;
    color: var(--theme-text-muted);
    opacity: 0.7;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
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

  .modal-apply-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: var(--rounded-md);
    background: var(--theme-primary);
    color: white;
    cursor: pointer;

    &:hover:not(:disabled) {
      filter: brightness(1.1);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .modal-wide {
    max-width: 36rem;
  }

  .yaml-preview {
    max-height: 16rem;
    overflow: auto;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    padding: 0.75rem;
    margin-top: 0.5rem;

    pre {
      margin: 0;
      font-size: 0.75rem;
      font-family: 'IBM Plex Mono', monospace;
      color: var(--theme-text);
      white-space: pre-wrap;
      word-break: break-word;
    }
  }

  .import-error {
    padding: 0.625rem 0.75rem;
    margin-top: 0.5rem;
    font-size: 0.8125rem;
    background: rgba(239, 68, 68, 0.08);
    border: 1px solid rgba(239, 68, 68, 0.25);
    border-radius: var(--rounded-md);
    color: #ef4444;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .import-results {
    margin-bottom: 1rem;
  }

  .result-label {
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--theme-text);
    margin: 0.75rem 0 0.25rem;
  }

  .result-list {
    list-style: none;
    margin: 0;
    padding: 0;

    li {
      font-size: 0.8125rem;
      font-family: 'IBM Plex Mono', monospace;
      padding: 0.25rem 0;
      color: var(--theme-text-muted);
    }
  }

  .result-applied::before {
    content: '+ ';
    color: #22c55e;
  }

  .result-skipped::before {
    content: '~ ';
    color: #eab308;
  }
</style>
