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
  import { onNavigate } from "$app/navigation";
  import { themeState, type Theme } from "./theme.svelte";
  import { api } from "$lib/api/client";

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

  const { children } = $props();

  let sidebarOpen = $state(false);
  let mode = $state('unknown');
  let services = $state<Service[]>([]);
  let availableModules = $state<ModuleRegistryInfo[]>([]);
  let desiredServices = $state<DesiredService[]>([]);
  let apiConnected = $state(false);

  onMount(() => {
    // Initialize theme from cookie (no server-side cookie access in SPA mode)
    themeState.initialize();

    // Fetch initial data from REST API
    async function fetchData() {
      // Core queries: mode and services
      try {
        const [modeResult, servicesResult] = await Promise.all([
          api<{ mode: string }>('/mode'),
          api<Service[]>('/services'),
        ]);

        if (modeResult.data) {
          mode = modeResult.data.mode;
          apiConnected = true;
        }
        if (servicesResult.data) {
          services = servicesResult.data;
        }
      } catch {
        // API unreachable — mode stays 'unknown'
      }

      // Module management queries — separate so they don't break the core layout
      if (apiConnected) {
        try {
          const [modulesResult, desiredResult] = await Promise.all([
            api<ModuleRegistryInfo[]>('/orchestrator/modules'),
            api<DesiredService[]>('/orchestrator/desired-services'),
          ]);

          if (modulesResult.data) {
            availableModules = modulesResult.data;
          }
          if (desiredResult.data) {
            desiredServices = desiredResult.data;
          }
        } catch {
          // Orchestrator queries not available yet — graceful degradation
        }
      }
    }

    fetchData();
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
  bind:open={sidebarOpen}
/>

<header class="app-header">
  <button class="menu-toggle" onclick={() => (sidebarOpen = !sidebarOpen)} aria-label="Open navigation">
    <Bars3 size="1.25rem" />
  </button>
  <a href="/" class="logo">
    <img src="/logo.png" alt="Tentacle" />
  </a>
  <nav class="header-nav">
  </nav>
  <div class="header-actions">
    <span
      class="mode-badge"
      class:mode-dev={mode === "dev"}
      class:mode-systemd={mode === "systemd"}
      class:mode-docker={mode === "docker"}
      class:mode-kubernetes={mode === "kubernetes"}
      class:mode-unknown={mode === "unknown"}
      title="Deployment mode: {mode}"
    >
      {mode}
    </span>
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

  .mode-badge {
    font-size: 0.6875rem;
    font-weight: 600;
    padding: 0.1875rem 0.5rem;
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

  .main-content {
    margin-top: var(--header-height);
    min-height: calc(100vh - var(--header-height));
  }
</style>
