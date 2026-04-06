<script lang="ts">
  import type { PageData } from './$types';
  import { invalidateAll } from '$app/navigation';
  import { onMount } from 'svelte';
  import Sunburst from '$lib/components/Sunburst.svelte';
  import TidyTree from '$lib/components/TidyTree.svelte';
  import DiagramSelector from '$lib/components/DiagramSelector.svelte';
  import type { VizMode } from '$lib/components/DiagramSelector.svelte';

  let { data }: { data: PageData } = $props();

  type MetricInfo = {
    name: string;
    sparkplugType: string;
    value: unknown;
    moduleId: string;
    datatype: string;
    templateRef: string | null;
    lastUpdated?: number | null;
  };

  type TemplateInfo = {
    name: string;
    version: string | null;
    members: { name: string; datatype: string }[];
  };

  // Live-updating metric map keyed by name
  let metricMap: Map<string, MetricInfo> = $state(new Map());
  let templates: TemplateInfo[] = $state(data.templates as TemplateInfo[]);
  let deviceId: string = $state(data.deviceId);

  // Initialize from server data
  $effect(() => {
    const m = new Map<string, MetricInfo>();
    for (const metric of data.metrics as MetricInfo[]) {
      m.set(metric.name, metric);
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

    // Poll mqttMetrics every 2.5s via SvelteKit invalidation.
    // MQTT metrics use Sparkplug naming (not variable IDs), so variable
    // subscriptions don't match — invalidateAll re-runs the page load.
    const pollInterval = setInterval(() => {
      invalidateAll();
    }, 2500);

    return () => {
      clearInterval(tickInterval);
      clearInterval(pollInterval);
      if (flushTimer) clearTimeout(flushTimer);
    };
  });

  const FADE_DURATION_SECONDS = 300;

  let vizMode: VizMode = $state('tree');

  // Organize metrics into: template instances (grouped by templateRef) and scalar metrics
  const organized = $derived(() => {
    void updateVersion; // depend on debounced version
    const allMetrics = [...metricMap.values()];
    const templateInstances: Record<string, MetricInfo[]> = {};
    const scalars: MetricInfo[] = [];

    for (const metric of allMetrics) {
      if (metric.templateRef) {
        if (!templateInstances[metric.templateRef]) {
          templateInstances[metric.templateRef] = [];
        }
        templateInstances[metric.templateRef].push(metric);
      } else if (metric.sparkplugType !== 'template') {
        scalars.push(metric);
      }
    }

    return { templateInstances, scalars };
  });

  // Build D3 hierarchy for sunburst visualization
  type SunburstNode = { name: string; children?: SunburstNode[]; value?: number; displayValue?: string };
  const sunburstData = $derived((): SunburstNode => {
    const org = organized();
    const children: SunburstNode[] = [];

    if (templates.length > 0) {
      const templateChildren = templates
        .map(t => ({
          name: t.name,
          children: t.members.map((m: any) => ({ name: m.name, value: 1 })),
        }))
        .filter(t => t.children.length > 0);
      if (templateChildren.length > 0) {
        children.push({ name: 'Template Definitions', children: templateChildren });
      }
    }

    const instEntries = Object.entries(org.templateInstances);
    if (instEntries.length > 0) {
      const instanceChildren = instEntries.map(([templateName, instances]) => ({
        name: templateName,
        children: instances.map(inst => {
          const members: SunburstNode[] = [];
          if (typeof inst.value === 'object' && inst.value !== null && 'metrics' in (inst.value as Record<string, unknown>)) {
            for (const m of (inst.value as { metrics: { name: string; value?: unknown }[] }).metrics) {
              members.push({ name: m.name, value: 1, displayValue: formatValue(m.value) });
            }
          }
          return members.length > 0
            ? { name: inst.name, children: members }
            : { name: inst.name, value: 1 };
        }),
      }));
      children.push({ name: 'Template Instances', children: instanceChildren });
    }

    if (org.scalars.length > 0) {
      children.push({
        name: 'Scalar Metrics',
        children: org.scalars.map(m => ({ name: m.name, value: 1, displayValue: formatValue(m.value) })),
      });
    }

    return { name: 'Metrics', children };
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

  function getFreshnessColor(timestamp: number | null | undefined): string {
    if (!timestamp) return 'rgb(156, 163, 175)'; // gray
    const ageSeconds = (now - timestamp) / 1000;
    if (ageSeconds <= 0) return 'rgb(34, 197, 94)'; // green
    if (ageSeconds >= FADE_DURATION_SECONDS) return 'rgb(156, 163, 175)'; // gray
    const t = 1 - ageSeconds / FADE_DURATION_SECONDS;
    const r = Math.round(156 + (34 - 156) * t);
    const g = Math.round(163 + (197 - 163) * t);
    const b = Math.round(175 + (94 - 175) * t);
    return `rgb(${r}, ${g}, ${b})`;
  }

  function getGlowStyle(timestamp: number | null | undefined): string {
    if (!timestamp) return 'none';
    const ageSeconds = (now - timestamp) / 1000;
    const opacity = ageSeconds <= 0 ? 1 : ageSeconds >= FADE_DURATION_SECONDS ? 0 : 1 - ageSeconds / FADE_DURATION_SECONDS;
    if (opacity < 0.5) return 'none';
    const glowOpacity = (opacity - 0.5) * 2;
    return `0 0 ${6 + glowOpacity * 4}px rgba(34, 197, 94, ${glowOpacity * 0.5})`;
  }

  function formatAge(timestamp: number | null | undefined): string {
    if (!timestamp) return 'No data';
    const ageMs = Math.max(0, now - timestamp);
    const ageSeconds = Math.floor(ageMs / 1000);
    if (ageSeconds < 60) return `${ageSeconds}s ago`;
    const ageMinutes = Math.floor(ageSeconds / 60);
    if (ageMinutes < 60) return `${ageMinutes}m ago`;
    const ageHours = Math.floor(ageMinutes / 60);
    if (ageHours < 24) return `${ageHours}h ago`;
    const ageDays = Math.floor(ageHours / 24);
    return `${ageDays}d ago`;
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
  <!-- Template Definitions -->
  {#if templates.length > 0}
    <section class="section">
      <h2>Template Definitions</h2>
      <div class="tree">
        {#each templates as template}
          <div class="tree-node">
            <button class="tree-toggle" onclick={() => toggleTemplate(template.name)}>
              <svg class="chevron" class:expanded={expandedTemplates[template.name]} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M9 18l6-6-6-6"/>
              </svg>
              <span class="template-icon">T</span>
              <span class="tree-label">{template.name}</span>
              {#if template.version}
                <span class="version-badge">v{template.version}</span>
              {/if}
              <span class="member-count">{template.members.length} members</span>
            </button>
            {#if expandedTemplates[template.name]}
              <div class="tree-children">
                {#each template.members as member}
                  <div class="tree-leaf">
                    <span class="freshness-dot" style="--dot-color: rgb(156, 163, 175); --dot-glow: none;" title="Definition"></span>
                    <span class="leaf-name">{member.name}</span>
                    <span class="leaf-type">{member.datatype}</span>
                    {#if member.templateRef}
                      <span class="template-ref">-> {member.templateRef}</span>
                    {/if}
                    {#if member.description}
                      <span class="leaf-desc">{member.description}</span>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </section>
  {/if}

  <!-- Template Instances -->
  {#if Object.keys(organized().templateInstances).length > 0}
    <section class="section">
      <h2>Template Instances</h2>
      <div class="tree">
        {#each Object.entries(organized().templateInstances) as [templateName, instances]}
          <div class="tree-node">
            <button class="tree-toggle" onclick={() => toggleTemplate('inst-' + templateName)}>
              <svg class="chevron" class:expanded={expandedTemplates['inst-' + templateName]} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M9 18l6-6-6-6"/>
              </svg>
              <span class="template-icon">T</span>
              <span class="tree-label">{templateName}</span>
              <span class="member-count">{instances.length} instances</span>
            </button>
            {#if expandedTemplates['inst-' + templateName]}
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
                          {@const template = templates.find(t => t.name === templateName)}
                          {#if template && typeof metric.value === 'object' && 'metrics' in (metric.value as Record<string, unknown>)}
                            {#each (metric.value as { metrics: Array<{ name: string; value: unknown; type: string; timestamp?: number | null }> }).metrics as member}
                              {@const memberDef = template.members.find(m => m.name === member.name)}
                              <div class="tree-leaf">
                                <span
                                  class="freshness-dot"
                                  title={formatAge(member.timestamp)}
                                  style="--dot-color: {getFreshnessColor(member.timestamp)}; --dot-glow: {getGlowStyle(member.timestamp)};"
                                ></span>
                                <span class="leaf-name">{member.name}</span>
                                <span class="leaf-value">{formatValue(member.value)}</span>
                                <span class="leaf-type">{memberDef?.datatype ?? member.type}</span>
                              </div>
                            {/each}
                          {:else}
                            {#each Object.entries(metric.value as Record<string, unknown>) as [key, val]}
                              <div class="tree-leaf">
                                <span class="freshness-dot" style="--dot-color: rgb(156, 163, 175); --dot-glow: none;"></span>
                                <span class="leaf-name">{key}</span>
                                <span class="leaf-value">{formatValue(val)}</span>
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
    </section>
  {/if}

  <!-- Scalar Metrics -->
  {#if organized().scalars.length > 0}
    <section class="section">
      <h2>Scalar Metrics</h2>
      <div class="tree">
        {#each organized().scalars as metric}
          <div class="tree-leaf">
            <span
              class="freshness-dot"
              title={formatAge(metric.timestamp)}
              style="--dot-color: {getFreshnessColor(metric.timestamp)}; --dot-glow: {getGlowStyle(metric.timestamp)};"
            ></span>
            <span class="leaf-name">{metric.name}</span>
            <span class="leaf-value">{formatValue(metric.value)}</span>
            <span class="leaf-type">{metric.sparkplugType}</span>
          </div>
        {/each}
      </div>
    </section>
  {/if}

  {#if metricMap.size === 0 && !data.error}
    <div class="empty-state">
      <p>No metrics being published. Start a PLC project to see metrics here.</p>
    </div>
  {/if}
  </div>
  {:else if vizMode === 'sunburst'}
    {#if sunburstData().children && sunburstData().children.length > 0}
      <div class="diagram-content">
        <Sunburst data={sunburstData()} />
      </div>
    {:else if !data.error}
      <div class="empty-state">
        <p>No metrics being published. Start a PLC project to see metrics here.</p>
      </div>
    {/if}
  {:else if vizMode === 'tidy'}
    {#if sunburstData().children && sunburstData().children.length > 0}
      <TidyTree data={sunburstData()} />
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
