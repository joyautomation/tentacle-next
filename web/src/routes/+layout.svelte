<script lang="ts">
  import "@fontsource/righteous";
  import "@fontsource/space-grotesk";
  import "@fontsource/ibm-plex-mono";
  import "@joyautomation/salt/styles.scss";
  import "../app.scss";
  import { Toast } from "@joyautomation/salt";
  import { Bars3 } from "@joyautomation/salt/icons";
  import ThemeSwitch from "$lib/components/ThemeSwitch.svelte";
  import NavSidebar from "$lib/components/NavSidebar.svelte";
  import { onMount } from "svelte";
  import { goto, onNavigate } from "$app/navigation";
  import { themeState, type Theme } from "./theme.svelte";
  import { api } from "$lib/api/client";
  import { isMonolith, role, brandName } from "$lib/stores/mode";
  import { get } from "svelte/store";

  interface Service {
    serviceType: string;
    moduleId: string;
    enabled: boolean;
    metadata?: Record<string, unknown>;
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

  const { children } = $props();

  let sidebarOpen = $state(false);
  let mode = $state('unknown');
  let appVersion = $state('');
  let services = $state<Service[]>([]);
  let availableModules = $state<ModuleRegistryInfo[]>([]);
  let desiredServices = $state<DesiredService[]>([]);
  let apiConnected = $state(false);

  async function fetchServices() {
    try {
      const result = await api<Service[]>('/services');
      if (result.data) {
        services = result.data;
        isMonolith.set(
          result.data.some(
            (s) => s.serviceType === 'orchestrator' && (s.metadata as any)?.mode === 'monolith'
          )
        );
      }
    } catch {
      // API unreachable
    }
  }

  async function fetchModules() {
    try {
      const [modulesResult, desiredResult] = await Promise.all([
        api<ModuleRegistryInfo[]>('/orchestrator/modules'),
        api<DesiredService[]>('/orchestrator/desired-services'),
      ]);
      if (modulesResult.data) availableModules = modulesResult.data;
      if (desiredResult.data) desiredServices = desiredResult.data;
    } catch {
      // Orchestrator queries not available yet
    }
  }

  onMount(() => {
    // Initialize theme from cookie (no server-side cookie access in SPA mode)
    themeState.initialize();

    // Initial fetch
    async function init() {
      try {
        const [modeResult, versionResult] = await Promise.all([
          api<{ mode: string; role?: string }>('/mode'),
          api<{ version: string }>('/system/version'),
        ]);
        if (modeResult.data) {
          mode = modeResult.data.mode;
          apiConnected = true;
          const r = modeResult.data.role === 'mantle' ? 'mantle' : 'tentacle';
          role.set(r);
          document.title = r === 'mantle' ? 'Mantle UI' : 'Tentacle UI';
        }
        if (versionResult.data) {
          appVersion = versionResult.data.version;
        }
      } catch {
        // API unreachable — mode stays 'unknown'
      }

      await fetchServices();

      if (apiConnected) {
        await fetchModules();

        // Sanity-hint only — role drives identity, but if the running module
        // set strongly disagrees the operator probably picked the wrong
        // build. Silent in the UI; warning in the console for diagnosis.
        const mantleModules = new Set(['gitserver', 'mqtt-broker', 'sparkplug-host']);
        const runningMantle = desiredServices
          .filter((d) => d.running)
          .some((d) => mantleModules.has(d.moduleId));
        const r = get(role);
        if (r === 'tentacle' && runningMantle) {
          console.warn(
            'role=tentacle but mantle-only modules (gitserver/mqtt-broker/sparkplug-host) are running — did you mean to build with -tags mantle?',
          );
        } else if (r === 'mantle' && desiredServices.length > 0 && !runningMantle) {
          console.warn(
            'role=mantle but no mantle-only modules are in desired state — this binary may not be doing anything mantle-shaped.',
          );
        }

        // First-boot redirect: if no services are configured,
        // redirect to the setup wizard (once per session)
        if (desiredServices.length === 0) {
          const currentPath = window.location.pathname;
          if (currentPath !== '/setup' && !sessionStorage.getItem('setup_dismissed')) {
            goto('/setup');
          }
        }
      }
    }

    init();

    // Poll for service/module changes so the sidebar stays current
    const poll = setInterval(() => {
      fetchServices();
      if (apiConnected) fetchModules();
    }, 5000);

    return () => clearInterval(poll);
  });

  onNavigate((navigation) => {
    if (!document.startViewTransition) return;

    return new Promise((resolve) => {
      document.startViewTransition(async () => {
        resolve();
        await navigation.complete;
      });
    });
  });
</script>

<NavSidebar
  {services}
  {availableModules}
  {desiredServices}
  {appVersion}
  {mode}
  bind:open={sidebarOpen}
/>

<header class="app-header">
  <button class="menu-toggle" onclick={() => (sidebarOpen = !sidebarOpen)} aria-label="Open navigation">
    <Bars3 size="1.25rem" />
  </button>
  <a href="/" class="logo">
    <img src="/logo.png" alt={$brandName} />
  </a>
  <nav class="header-nav">
  </nav>
  <div class="header-actions">
    <ThemeSwitch />
  </div>
</header>

<main class="main-content">
  {@render children()}
</main>
<Toast />

<style lang="scss">
  .app-header {
    display: flex;
    align-items: center;
    height: var(--header-height);
    padding: 0 1.5rem;
    background-color: var(--theme-background);
    border-bottom: none;
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 100;
  }

  .menu-toggle {
    display: flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--theme-text-muted);
    padding: 0.375rem;
    border-radius: var(--rounded-lg);
    flex-shrink: 0;
    margin-right: 0.5rem;
    transition:
      background 0.15s,
      color 0.15s;

    &:hover {
      background: var(--theme-surface);
      color: var(--theme-text);
    }
  }

  .logo {
    display: flex;
    align-items: center;
    text-decoration: none;
    flex-shrink: 0;

    img {
      height: 2rem;
      width: auto;
    }
  }

  .header-nav {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 0.25rem;
    margin-left: 2rem;
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 1rem;
    margin-left: auto;
  }

  .main-content {
    margin-top: var(--header-height);
    min-height: calc(100vh - var(--header-height));
  }
</style>
