<script lang="ts">
  import type { PageData } from './$types';
  import { onMount } from 'svelte';
  import { subscribe } from '$lib/api/subscribe';
  import Sunburst from '$lib/components/Sunburst.svelte';
  import TidyTree from '$lib/components/TidyTree.svelte';
  import DiagramSelector from '$lib/components/DiagramSelector.svelte';
  import type { VizMode } from '$lib/components/DiagramSelector.svelte';
  import { getFreshnessColor as _getFreshnessColor, getGlowStyle as _getGlowStyle, formatAge as _formatAge, formatAgeShort as _formatAgeShort } from '$lib/utils/freshness';

  let { data }: { data: PageData } = $props();

  type MetricInfo = {
    name: string;
    sparkplugType: string;
    value: unknown;
    moduleId: string;
    deviceId: string;
    datatype: string;
    templateRef: string | null;
    lastUpdated?: number | null;
  };

  type TemplateInfo = {
    name: string;
    version: string | null;
    members: { name: string; datatype: string }[];
  };

  // Live-updating metric map keyed by deviceId:name to prevent collisions
  // when different devices have metrics with the same name (e.g. "uptime").
  let metricMap: Map<string, MetricInfo> = $state(new Map());
  let templates: TemplateInfo[] = $state(data.templates as TemplateInfo[]);
  let deviceId: string = $state(data.deviceId);

  function metricKey(metric: MetricInfo): string {
    return `${metric.deviceId || ''}:${metric.name}`;
  }

  // Initialize from server data
  $effect(() => {
    const m = new Map<string, MetricInfo>();
    for (const metric of data.metrics as MetricInfo[]) {
      m.set(metricKey(metric), metric);
    }
    metricMap = m;
  });

  // Track expanded state for template instances and template definitions
  let expandedInstances: Record<string, boolean> = $state({});
  let expandedTemplates: Record<string, boolean> = $state({});

  // Debounced version counter — incremented by flush, drives $derived recomputation
  let updateVersion = $state(0);
  let pendingUpdates = 0;
  let flushTimer: ReturnType<typeof setTimeout> | null = null;

  function scheduleFlush() {
    pendingUpdates++;
    if (!flushTimer) {
      flushTimer = setTimeout(() => {
        flushTimer = null;
        pendingUpdates = 0;
        updateVersion++;
      }, 500);
    }
  }

  // Ticking "now" for freshness calculations
  let now = $state(Date.now());
  onMount(() => {
    const tickInterval = setInterval(() => { now = Date.now(); }, 1000);

    // Stream MQTT metrics via SSE — replaces invalidateAll() polling.
    // Backend only pushes when data actually changes.
    const unsub = subscribe<{ metrics: MetricInfo[]; templates: TemplateInfo[]; deviceId: string }>(
      '/mqtt/metrics/stream',
      (msg) => {
        if (msg.templates) templates = msg.templates;
        if (msg.deviceId) deviceId = msg.deviceId;
        if (msg.metrics) {
          for (const metric of msg.metrics) {
            metricMap.set(metricKey(metric), metric);
          }
          scheduleFlush();
        }
      },
    );

    return () => {
      clearInterval(tickInterval);
      if (flushTimer) clearTimeout(flushTimer);
      unsub();
    };
  });

  // Thin wrappers that bind `now` into the shared freshness utilities
  const getFreshnessColor = (ts: number | null | undefined) => _getFreshnessColor(ts, now);
  const getGlowStyle = (ts: number | null | undefined) => _getGlowStyle(ts, now);
  const formatAge = (ts: number | null | undefined) => _formatAge(ts, now);
  const formatAgeShort = (ts: number | null | undefined) => _formatAgeShort(ts, now);

  let vizMode: VizMode = $state('tree');

  // Organize metrics by device, then split into template instances and scalars within each device
  type DeviceMetrics = {
    templateInstances: Record<string, MetricInfo[]>; // templateName → instances
    scalars: MetricInfo[];
  };
  const organized = $derived.by(() => {
    void updateVersion; // depend on debounced version
    const allMetrics = [...metricMap.values()];
    const byDevice: Record<string, DeviceMetrics> = {};

    for (const metric of allMetrics) {
      const dev = metric.deviceId || 'unknown';
      if (!byDevice[dev]) byDevice[dev] = { templateInstances: {}, scalars: [] };

      if (metric.templateRef) {
        if (!byDevice[dev].templateInstances[metric.templateRef]) {
          byDevice[dev].templateInstances[metric.templateRef] = [];
        }
        byDevice[dev].templateInstances[metric.templateRef].push(metric);
      } else if (metric.sparkplugType !== 'template') {
        byDevice[dev].scalars.push(metric);
      }
    }

    // Sort within each device group
    for (const dm of Object.values(byDevice)) {
      dm.scalars.sort((a, b) => a.name.localeCompare(b.name));
      for (const instances of Object.values(dm.templateInstances)) {
        instances.sort((a, b) => a.name.localeCompare(b.name));
      }
    }
    return byDevice;
  });

  // Build D3 hierarchy for sunburst visualization
  type SunburstNode = { name: string; children?: SunburstNode[]; value?: number; displayValue?: string };
  const sunburstData = $derived.by((): SunburstNode => {
    const org = organized;
    const deviceChildren: SunburstNode[] = [];

    for (const [devId, dm] of Object.entries(org).sort(([a], [b]) => a.localeCompare(b))) {
      const devChildren: SunburstNode[] = [];

      // Template instances
      for (const [templateName, instances] of Object.entries(dm.templateInstances)) {
        devChildren.push({
          name: templateName,
          children: instances.map(inst => {
            const members: SunburstNode[] = [];
            if (typeof inst.value === 'object' && inst.value !== null) {
              if ('metrics' in (inst.value as Record<string, unknown>)) {
                for (const m of (inst.value as { metrics: { name: string; value?: unknown }[] }).metrics) {
                  members.push({ name: m.name, value: 1, displayValue: formatValue(m.value) });
                }
              } else {
                for (const [key, val] of Object.entries(inst.value as Record<string, unknown>)) {
                  members.push({ name: key, value: 1, displayValue: formatValue(val) });
                }
              }
            }
            return members.length > 0
              ? { name: inst.name, children: members }
              : { name: inst.name, value: 1 };
          }),
        });
      }

      // Scalars
      for (const m of dm.scalars) {
        devChildren.push({ name: m.name, value: 1, displayValue: formatValue(m.value) });
      }

      if (devChildren.length > 0) {
        deviceChildren.push({ name: devId, children: devChildren });
      }
    }

    return { name: 'Metrics', children: deviceChildren };
  });

  function toggleInstance(name: string) {
    expandedInstances[name] = !expandedInstances[name];
  }

  function toggleTemplate(name: string) {
    expandedTemplates[name] = !expandedTemplates[name];
  }

  function formatValue(value: unknown): string {
    if (value === null || value === undefined) return '—';
    if (typeof value === 'number') {
      if (Number.isInteger(value)) return value.toString();
      return value.toFixed(3);
    }
    if (typeof value === 'object') {
      try { return JSON.stringify(value); }
      catch { return String(value); }
    }
    return String(value);
  }

</script>

<div class="metrics-page">
  {#if data.error}
    <div class="error-box">
      <p>{data.error}</p>
    </div>
  {/if}

  <div class="metrics-header">
    <h1>Metrics</h1>
    {#if deviceId}
      <span class="device-badge">Device: {deviceId}</span>
    {/if}
    <span class="count-badge">{metricMap.size} metrics</span>
    <DiagramSelector bind:mode={vizMode} />
  </div>

  {#if vizMode === 'tree'}
  <div class="tree-content">
  {#each Object.entries(organized).sort(([a], [b]) => a.localeCompare(b)) as [devId, dm]}
    {@const hasTemplates = Object.keys(dm.templateInstances).length > 0}
    {@const hasScalars = dm.scalars.length > 0}
    {#if hasTemplates || hasScalars}
      <section class="section">
        <h2>{devId}</h2>

        <!-- Template Instances for this device -->
        {#if hasTemplates}
          <div class="tree" style:margin-bottom={hasScalars ? '0.75rem' : undefined}>
            {#each Object.entries(dm.templateInstances).sort(([a], [b]) => a.localeCompare(b)) as [templateName, instances]}
              {@const template = templates.find(t => t.name === templateName)}
              <div class="tree-node">
                <button class="tree-toggle" onclick={() => toggleTemplate(`${devId}:${templateName}`)}>
                  <svg class="chevron" class:expanded={expandedTemplates[`${devId}:${templateName}`]} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M9 18l6-6-6-6"/>
                  </svg>
                  <span class="template-icon">T</span>
                  <span class="tree-label">{templateName}</span>
                  {#if template?.version}
                    <span class="version-badge">v{template.version}</span>
                  {/if}
                  <span class="member-count">{instances.length} {instances.length === 1 ? 'instance' : 'instances'} · {template?.members.length ?? 0} members</span>
                </button>
                {#if expandedTemplates[`${devId}:${templateName}`]}
                  <div class="tree-children">
                    {#each instances as metric}
                      <div class="tree-node">
                        <button class="tree-toggle" onclick={() => toggleInstance(metric.name)}>
                          <svg class="chevron" class:expanded={expandedInstances[metric.name]} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M9 18l6-6-6-6"/>
                          </svg>
                          <span class="freshness-dot" style="--dot-color: var(--color-purple-400, #c084fc); --dot-glow: none;"></span>
                          <span class="tree-label">{metric.name}</span>
                          <span class="leaf-type">template</span>
                        </button>
                        {#if expandedInstances[metric.name]}
                          <div class="tree-children">
                            {#if typeof metric.value === 'object' && metric.value !== null}
                              {#if template && typeof metric.value === 'object' && 'metrics' in (metric.value as Record<string, unknown>)}
                                {#each (metric.value as { metrics: Array<{ name: string; value: unknown; type: string }> }).metrics as member}
                                  {@const memberDef = template.members.find(m => m.name === member.name)}
                                  <div class="tree-leaf">
                                    <span
                                      class="freshness-dot"
                                      title={formatAge(metric.lastUpdated)}
                                      style="--dot-color: {getFreshnessColor(metric.lastUpdated)}; --dot-glow: {getGlowStyle(metric.lastUpdated)};"
                                    ></span>
                                    <span class="staleness-label" style="color: {getFreshnessColor(metric.lastUpdated)}">{formatAgeShort(metric.lastUpdated)}</span>
                                    <span class="leaf-name">{member.name}</span>
                                    <span class="leaf-value">{formatValue(member.value)}</span>
                                    <span class="leaf-type">{memberDef?.datatype ?? member.type}</span>
                                  </div>
                                {/each}
                              {:else}
                                {#each Object.entries(metric.value as Record<string, unknown>) as [key, val]}
                                  {@const memberDef = template?.members.find(m => m.name === key)}
                                  <div class="tree-leaf">
                                    <span
                                      class="freshness-dot"
                                      title={formatAge(metric.lastUpdated)}
                                      style="--dot-color: {getFreshnessColor(metric.lastUpdated)}; --dot-glow: {getGlowStyle(metric.lastUpdated)};"
                                    ></span>
                                    <span class="staleness-label" style="color: {getFreshnessColor(metric.lastUpdated)}">{formatAgeShort(metric.lastUpdated)}</span>
                                    <span class="leaf-name">{key}</span>
                                    <span class="leaf-value">{formatValue(val)}</span>
                                    {#if memberDef}
                                      <span class="leaf-type">{memberDef.datatype}</span>
                                    {/if}
                                  </div>
                                {/each}
                              {/if}
                            {:else}
                              <div class="tree-leaf">
                                <span class="leaf-value">{formatValue(metric.value)}</span>
                              </div>
                            {/if}
                          </div>
                        {/if}
                      </div>
                    {/each}
                  </div>
                {/if}
              </div>
            {/each}
          </div>
        {/if}

        <!-- Atomic metrics for this device -->
        {#if hasScalars}
          <div class="tree">
            {#each dm.scalars as metric}
              <div class="tree-leaf">
                <span
                  class="freshness-dot"
                  title={formatAge(metric.lastUpdated)}
                  style="--dot-color: {getFreshnessColor(metric.lastUpdated)}; --dot-glow: {getGlowStyle(metric.lastUpdated)};"
                ></span>
                <span class="staleness-label" style="color: {getFreshnessColor(metric.lastUpdated)}">{formatAgeShort(metric.lastUpdated)}</span>
                <span class="leaf-name">{metric.name}</span>
                <span class="leaf-value">{formatValue(metric.value)}</span>
                <span class="leaf-type">{metric.sparkplugType}</span>
              </div>
            {/each}
          </div>
        {/if}
      </section>
    {/if}
  {/each}

  {#if metricMap.size === 0 && !data.error}
    <div class="empty-state">
      <p>No metrics being published. Start a PLC project to see metrics here.</p>
    </div>
  {/if}
  </div>
  {:else if vizMode === 'sunburst'}
    {#if sunburstData.children && sunburstData.children.length > 0}
      <div class="diagram-content">
        <Sunburst data={sunburstData} />
      </div>
    {:else if !data.error}
      <div class="empty-state">
        <p>No metrics being published. Start a PLC project to see metrics here.</p>
      </div>
    {/if}
  {:else if vizMode === 'tidy'}
    {#if sunburstData.children && sunburstData.children.length > 0}
      <TidyTree data={sunburstData} />
    {:else if !data.error}
      <div class="empty-state">
        <p>No metrics being published. Start a PLC project to see metrics here.</p>
      </div>
    {/if}
  {/if}
</div>

<style lang="scss">
  .metrics-page {
    padding: 2rem;
  }

  .tree-content {
    max-width: 900px;
  }

  .diagram-content {
    display: flex;
    justify-content: center;
  }

  .metrics-header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 1.5rem;

    h1 {
      font-size: 1.5rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }
  }

  .device-badge, .count-badge {
    padding: 0.2rem 0.5rem;
    border-radius: var(--rounded-md);
    font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace;
  }

  .device-badge {
    background: var(--badge-purple-bg);
    color: var(--badge-purple-text);
  }

  .count-badge {
    background: var(--badge-teal-bg);
    color: var(--badge-teal-text);
  }

  .section {
    margin-bottom: 1.5rem;

    h2 {
      font-size: 0.8125rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--theme-text-muted);
      margin: 0 0 0.75rem;
    }
  }

  .tree {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
  }

  .tree-node {
    &:not(:last-child) {
      border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    }
  }

  .tree-toggle {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.625rem 1rem;
    background: none;
    border: none;
    color: var(--theme-text);
    font-size: 0.8125rem;
    cursor: pointer;
    text-align: left;
    font-family: inherit;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 5%, transparent);
    }
  }

  .chevron {
    flex-shrink: 0;
    color: var(--theme-text-muted);
    transition: transform 0.15s ease;

    &.expanded {
      transform: rotate(90deg);
    }
  }

  .template-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    border-radius: var(--rounded-sm);
    background: var(--badge-purple-bg);
    color: var(--badge-purple-text);
    font-size: 0.6875rem;
    font-weight: 700;
    flex-shrink: 0;
  }

  .tree-label {
    font-family: 'IBM Plex Mono', monospace;
    font-weight: 500;
  }

  .version-badge {
    font-size: 0.6875rem;
    color: var(--badge-muted-text);
    padding: 0.1rem 0.35rem;
    border-radius: var(--rounded-sm);
    background: var(--badge-muted-bg);
  }

  .member-count {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    margin-left: auto;
  }

  .tree-children {
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);

    .tree-node {
      padding-left: 1rem;
    }

    .tree-leaf {
      padding-left: 2.5rem;
    }
  }

  .tree-leaf {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;

    &:not(:last-child) {
      border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 30%, transparent);
    }
  }

  .freshness-dot {
    position: relative;
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    cursor: help;

    &::before {
      content: '';
      position: absolute;
      top: 50%;
      left: 50%;
      transform: translate(-50%, -50%);
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background-color: var(--dot-color, rgb(156, 163, 175));
      box-shadow: var(--dot-glow, none);
      transition: background-color 1s ease, box-shadow 1s ease;
    }
  }

  .leaf-name {
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text);
  }

  .leaf-value {
    margin-left: auto;
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .staleness-label {
    font-size: 0.6875rem;
    font-family: 'IBM Plex Mono', monospace;
    flex-shrink: 0;
    min-width: 1.5rem;
    text-align: left;
    transition: color 1s ease;
  }

  .leaf-type {
    font-size: 0.6875rem;
    color: var(--badge-muted-text);
    padding: 0.1rem 0.35rem;
    border-radius: var(--rounded-sm);
    background: var(--badge-muted-bg);
    flex-shrink: 0;
  }

  .leaf-desc {
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    font-style: italic;
  }

  .template-ref {
    font-size: 0.6875rem;
    color: var(--badge-purple-text);
  }

  .error-box {
    padding: 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444);
    margin-bottom: 1.5rem;

    p {
      margin: 0;
      font-size: 0.875rem;
      color: var(--color-red-500, #ef4444);
    }
  }

  .empty-state {
    padding: 3rem 2rem;
    text-align: center;

    p {
      color: var(--theme-text-muted);
      font-size: 0.875rem;
    }
  }

</style>
