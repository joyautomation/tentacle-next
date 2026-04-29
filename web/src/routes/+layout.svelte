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
  import { goto, invalidateAll, onNavigate } from "$app/navigation";
  import { themeState, type Theme } from "./theme.svelte";
  import { api } from "$lib/api/client";
  import { subscribe } from "$lib/api/subscribe";
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

    // Listen for gitops applied events. When the gitops module converges KV
    // with a remote commit, every load function re-runs so config-driven
    // pages reflect the new state without a manual refresh.
    const unsubscribeApplied = subscribe('/gitops/applied/stream', () => {
      invalidateAll();
    });

    return () => {
      clearInterval(poll);
      unsubscribeApplied();
    };
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
  <a href="/" class="logo" aria-label="{$brandName} home">
    {#if $role === 'mantle'}
      <svg class="logo-svg" viewBox="0 0 44.868027 15.634651" xmlns="http://www.w3.org/2000/svg">
        <g transform="translate(-103.53065,-74.255726)">
          <path d="m 111.09716,74.255727 a 5.239012,6.1640247 0 0 0 -5.23226,6.16384 5.239012,6.1640247 0 0 0 1.28247,4.04016 4.1842682,4.0969078 0 0 0 -0.22769,1.33366 4.1842682,4.0969078 0 0 0 4.18418,4.09699 4.1842682,4.0969078 0 0 0 4.18454,-4.09699 4.1842682,4.0969078 0 0 0 -0.22805,-1.33331 5.239012,6.1640247 0 0 0 1.28283,-4.04051 5.239012,6.1640247 0 0 0 -5.23932,-6.16384 5.239012,6.1640247 0 0 0 -0.007,0 z" style="fill:#ad4806"/>
          <path d="m 109.82097,74.443767 c -0.0967,0.0287 -0.19232,0.0605 -0.28732,0.0956 -0.095,0.0351 -0.18904,0.0733 -0.28215,0.11472 -0.0911,0.0405 -0.18167,0.0838 -0.27079,0.13023 -0.002,0.001 -0.004,0.002 -0.006,0.003 -0.0847,0.0443 -0.168,0.0915 -0.25063,0.14108 a 5.2390119,6.1640246 0 0 0 -0.008,0.005 5.2390119,6.1640246 0 0 0 -0.0103,0.006 c -0.364,4.2852 -0.0337,10.48539 -0.90744,13.36507 a 4.1842682,4.0969078 0 0 0 1.65365,1.25264 l 0.0853,-10.29446 1.31104,5.66839 h 0.61701 l 1.32085,-5.66839 0.0858,10.2433 a 4.1842682,4.0969078 0 0 0 5.3e-4,0 4.1842682,4.0969078 0 0 0 1.5322,-1.19528 c -0.70008,-2.20841 -0.60415,-9.29433 -0.79633,-13.30564 -0.001,-8e-4 -0.002,-0.001 -0.004,-0.002 -0.0839,-0.0536 -0.16912,-0.10435 -0.25528,-0.15245 a 5.2390119,6.1640246 0 0 0 -0.009,-0.006 c -0.003,-0.002 -0.007,-0.003 -0.0104,-0.005 -0.0826,-0.0456 -0.16566,-0.0887 -0.25011,-0.12919 a 5.2390119,6.1640246 0 0 0 -0.0279,-0.0129 c -0.0842,-0.0397 -0.16944,-0.0771 -0.25528,-0.11162 a 5.2390119,6.1640246 0 0 0 -0.0243,-0.01 c -0.002,-7.9e-4 -0.004,-0.001 -0.006,-0.002 -0.0851,-0.0335 -0.17079,-0.0647 -0.25734,-0.093 a 5.2390119,6.1640246 0 0 0 -0.0134,-0.004 l -1.33894,8.9793 z" style="fill:#fd9754"/>
          <ellipse transform="matrix(0.94932758,-0.31428833,0.21194001,0.97728268,0,0)" ry="2.972568" rx="1.4668432" cy="113.56931" cx="86.981812" style="fill:#fd9754"/>
          <ellipse transform="matrix(0.94345397,-0.33150355,0.20019595,0.97975588,0,0)" ry="1.8613584" rx="0.87301087" cy="115.43396" cx="88.192162" style="fill:#000000;stroke:#000000;stroke-width:1.19571"/>
          <ellipse transform="matrix(0.99991247,-0.01323089,0.06258591,0.99803958,0,0)" ry="1.3219733" rx="0.71169496" cy="84.892426" cx="100.60207" style="fill:#ffffff"/>
          <ellipse transform="scale(-1,1)" ry="2.9725683" rx="1.4668432" cy="43.323303" cx="-131.44887" style="fill:#fd9754"/>
          <ellipse transform="matrix(-0.94345397,-0.33150355,-0.20019595,0.97975588,0,0)" ry="1.8613584" rx="0.87301087" cy="41.063457" cx="-131.60925" style="fill:#000000;stroke:#000000;stroke-width:1.19571"/>
          <ellipse transform="matrix(-0.99991247,-0.01323089,-0.06258591,0.99803958,0,0)" ry="1.3219733" rx="0.71169496" cy="81.948044" cx="-121.49981" style="fill:#ffffff"/>
          <path class="logo-letter" d="m 118.65252,87.788047 2.42711,-11.42999 h 1.53811 l 2.44123,11.42999 h -1.5099 l -0.52211,-2.87866 h -2.32833 l -0.55033,2.87866 z m 2.25778,-4.02166 h 1.905 l -0.95956,-5.15055 z"/>
          <path class="logo-letter" d="m 125.05897,87.788047 v -11.42999 h 1.08654 l 3.24555,7.59177 v -7.59177 h 1.32645 v 11.42999 h -1.01601 l -3.28787,-7.80344 v 7.80344 z"/>
          <path class="logo-letter" d="m 133.76551,87.788047 v -10.24466 h -1.93323 v -1.18533 h 5.40455 v 1.18533 h -1.87678 v 10.24466 z"/>
          <path class="logo-letter" d="m 138.33747,87.788047 v -11.42999 h 1.59455 v 10.28699 h 2.921 v 1.143 z"/>
          <path class="logo-letter" d="m 143.93957,87.788047 v -11.42999 h 4.43088 v 1.18533 h -2.83633 v 3.78178 h 2.30011 v 1.12888 h -2.30011 v 4.191 h 2.86456 v 1.143 z"/>
        </g>
      </svg>
    {:else}
      <img src="/logo.png" alt={$brandName} />
    {/if}
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

    .logo-svg {
      height: 2rem;
      width: auto;
    }

    .logo-svg :global(.logo-letter) {
      fill: var(--theme-text);
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
