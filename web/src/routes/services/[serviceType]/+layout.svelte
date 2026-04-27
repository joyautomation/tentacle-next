<script lang="ts">
  import { page } from '$app/stores';
  import {
    getServiceName,
    getRemoteConfigStatus,
    getServiceTabs,
  } from '$lib/constants/services';
  import { setRemoteTargetContext } from '$lib/contexts/remote-target';

  let { children } = $props();

  const serviceType = $derived($page.params.serviceType ?? '');
  const serviceName = $derived(getServiceName(serviceType));
  const remoteConfigStatus = $derived(getRemoteConfigStatus(serviceType));

  // Remote-target awareness: when ?target=group/node is present, every
  // configurator on this layout is reading/writing the named edge tentacle's
  // git repo via mantle, not the local KV. We surface that prominently so an
  // operator can never confuse "configuring this tentacle" with "configuring
  // a remote one", and we keep the param sticky on tab links.
  const target = $derived($page.url?.searchParams.get('target') ?? null);
  const targetSuffix = $derived(target ? `?target=${encodeURIComponent(target)}` : '');

  // Expose target state to every child page via context so they can route API
  // calls through ?target=... and disable live-only UI when remote.
  setRemoteTargetContext(() => ({
    target,
    isRemote: target !== null,
    targetSuffix,
  }));

  // When remote, the back-link returns to the per-node fleet landing rather
  // than the fleet list, so the operator can hop between configurators for
  // the same edge tentacle without losing their place.
  const backHref = $derived(() => {
    if (!target) return '/';
    const [group, node] = target.split('/', 2);
    if (!group || !node) return '/fleet';
    return `/fleet/${encodeURIComponent(group)}/${encodeURIComponent(node)}`;
  });

  const allTabs = $derived(getServiceTabs(serviceType));

  // Tabs visible in the current mode. In remote mode we hide live tabs (they
  // read runtime state from the *local* tentacle) but keep the Overview tab
  // (path === '') because it carries identity + enable/disable, which mantle
  // can drive remotely via the fleet endpoints.
  const visibleTabs = $derived(
    target ? allTabs.filter((t) => t.scope === 'config' || t.path === '') : allTabs,
  );

  // Match the current pathname to a tab, so we can both highlight it and
  // know whether the active page is live- or config-scoped.
  const currentTab = $derived.by(() => {
    const path = $page.url?.pathname ?? '';
    const segment = path.replace(`/services/${serviceType}`, '').replace(/^\//, '');
    return allTabs.find((t) => t.path === segment) ?? null;
  });

  // The Overview route renders remote-aware content itself, so it never gets
  // a layout-level placeholder — even for bus-driven / coming-soon modules,
  // the operator should still see identity and the enable/disable toggle.
  const isOverviewRoute = $derived(currentTab?.path === '');

  // In remote mode, if the user lands on a live-scoped page (other than
  // Overview) we show a placeholder rather than the page content (which would
  // either be empty or, worse, reflect the local mantle's state).
  const showLivePlaceholder = $derived(
    target !== null && currentTab !== null && currentTab.scope === 'live' && !isOverviewRoute,
  );

  // First config tab on this module — used as the redirect target from the
  // live placeholder so the operator gets to something useful in one click.
  const firstConfigTab = $derived(allTabs.find((t) => t.scope === 'config') ?? null);
  const firstConfigHref = $derived(
    firstConfigTab
      ? `/services/${serviceType}${firstConfigTab.path ? '/' + firstConfigTab.path : ''}${targetSuffix}`
      : null,
  );
</script>

<div class="service-layout" class:remote={target}>
  {#if target}
    <div class="remote-banner" role="status">
      <span class="remote-dot" aria-hidden="true"></span>
      <span class="remote-label">Configuring remote tentacle</span>
      <span class="remote-target mono">{target}</span>
      <span class="remote-hint">Changes are committed to mantle's git repo for this edge node.</span>
    </div>
  {/if}
  <nav class="service-nav">
    <a href={backHref()} class="back-link">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M19 12H5M12 19l-7-7 7-7"/>
      </svg>
      {target ? 'Fleet' : 'Topology'}
    </a>
    <span class="separator">/</span>
    {#if target}
      <span class="target-chip mono" title="Remote tentacle target">{target}</span>
      <span class="separator">/</span>
    {/if}
    <span class="current">{serviceName}</span>
  </nav>

  <div class="service-tabs">
    {#each visibleTabs as tab (tab.path)}
      <a
        href="/services/{serviceType}{tab.path ? '/' + tab.path : ''}{targetSuffix}"
        class="tab"
        class:active={currentTab?.path === tab.path}
      >
        {tab.label}
      </a>
    {/each}
  </div>

  {#if target && remoteConfigStatus === 'bus-driven' && !isOverviewRoute}
    <div class="remote-placeholder">
      <h2>No remote configuration for this module</h2>
      <p>
        <strong>{serviceName}</strong> has no standalone configuration — its behavior is driven by other modules over the bus. For example, EtherNet/IP and PROFINET scanners are configured via Gateway sources, not their own settings.
      </p>
      <p>
        Configure this edge tentacle's <a href="/services/gateway{targetSuffix}">Gateway</a> instead.
      </p>
    </div>
  {:else if target && remoteConfigStatus === 'coming-soon' && !isOverviewRoute}
    <div class="remote-placeholder">
      <h2>Remote config not yet wired</h2>
      <p>
        <strong>{serviceName}</strong> owns its own settings, but mantle doesn't yet have target-aware endpoints for it. Backend support is planned — this page will activate when it lands.
      </p>
    </div>
  {:else if showLivePlaceholder}
    <div class="remote-placeholder">
      <h2>Live data not available remotely</h2>
      <p>
        The <strong>{currentTab?.label ?? 'this'}</strong> tab reads runtime state from a local module. Streaming live data from the remote tentacle (logs, status, metrics) is planned for a later phase.
      </p>
      {#if firstConfigTab && firstConfigHref}
        <p>
          <a href={firstConfigHref}>Open {firstConfigTab.label} →</a>
        </p>
      {/if}
    </div>
  {:else}
    {@render children()}
  {/if}
</div>

<style lang="scss">
  .service-layout {
    display: flex;
    flex-direction: column;
    min-height: calc(100vh - var(--header-height));
    position: relative;
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

  .mono {
    font-family: var(--font-mono, monospace);
  }

  .target-chip {
    padding: 0.125rem 0.5rem;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-accent);
    background: color-mix(in srgb, var(--theme-accent) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--theme-accent) 35%, transparent);
    border-radius: var(--rounded-md, 0.375rem);
  }

  .remote-banner {
    display: flex;
    align-items: center;
    gap: 0.625rem;
    padding: 0.625rem 2rem;
    font-size: 0.8125rem;
    color: var(--theme-text);
    background: color-mix(in srgb, var(--theme-accent) 14%, transparent);
    border-bottom: 1px solid color-mix(in srgb, var(--theme-accent) 40%, transparent);
  }

  .remote-dot {
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 999px;
    background: var(--theme-accent);
    box-shadow: 0 0 0 0.25rem color-mix(in srgb, var(--theme-accent) 25%, transparent);
    flex-shrink: 0;
  }

  .remote-label {
    font-weight: 600;
    text-transform: uppercase;
    font-size: 0.7rem;
    letter-spacing: 0.06em;
    color: var(--theme-accent);
  }

  .remote-target {
    padding: 0.125rem 0.5rem;
    font-weight: 500;
    color: var(--theme-text);
    background: var(--theme-surface);
    border: 1px solid color-mix(in srgb, var(--theme-accent) 30%, transparent);
    border-radius: var(--rounded-md, 0.375rem);
  }

  .remote-hint {
    color: var(--theme-text-muted);
    font-size: 0.75rem;
  }

  .service-layout.remote::after {
    content: '';
    position: absolute;
    inset: 0;
    pointer-events: none;
    border: 2px solid color-mix(in srgb, var(--theme-accent) 35%, transparent);
    z-index: 5;
  }

  .remote-placeholder {
    margin: 2.5rem auto;
    padding: 2rem 2.5rem;
    max-width: 640px;
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    border-radius: var(--rounded-md, 0.5rem);
    text-align: center;

    h2 {
      margin: 0 0 0.75rem;
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
    }

    p {
      margin: 0.5rem 0;
      color: var(--theme-text-muted);
      font-size: 0.875rem;
      line-height: 1.5;
    }

    a {
      color: var(--theme-primary);
      text-decoration: none;

      &:hover {
        text-decoration: underline;
      }
    }
  }

  .service-tabs {
    display: flex;
    gap: 0.25rem;
    padding: 0 2rem;
    border-bottom: 1px solid var(--theme-border);
    background: var(--theme-surface);
  }

  .tab {
    padding: 0.875rem 1.25rem;
    font-size: 0.875rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    text-decoration: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    transition: all 0.15s ease;

    &:hover {
      color: var(--theme-text);
    }

    &.active {
      color: var(--theme-primary);
      border-bottom-color: var(--theme-primary);
    }
  }
</style>
