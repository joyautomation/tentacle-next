<script lang="ts">
  import { onMount } from 'svelte';
  import { TrendChartContainer } from '@joyautomation/cortex/charts';
  import { getMetricKey } from '@joyautomation/cortex/charts/utils';
  import type {
    MetricIdentifier,
    MetricHistory,
    MetricInfo,
    PaneConfig,
  } from '@joyautomation/cortex/charts';
  import { fade, slide } from 'svelte/transition';

  type HistoryMetricRef = {
    groupId: string;
    nodeId: string;
    deviceId: string;
    metricId: string;
  };
  type FlatMetric = MetricInfo & { path: string };
  type Device = { id: string; metrics: string[] };
  type Node = { id: string; metrics: string[]; devices: Device[] };
  type Group = { id: string; nodes: Node[] };

  let availableMetrics = $state<HistoryMetricRef[]>([]);
  let currentPanes = $state<PaneConfig[]>([]);
  let addMetricFn = $state<((paneId: string, metric: MetricInfo) => void) | null>(null);
  let metricFilter = $state('');
  let expandedPanels = $state<Set<string>>(new Set());
  let popoverMetric = $state<FlatMetric | null>(null);
  let popoverPosition = $state<{ top: number; left: number } | null>(null);

  async function fetchHistory(params: {
    start: Date;
    end: Date;
    metrics: MetricIdentifier[];
    samples: number;
    raw: boolean;
  }): Promise<MetricHistory[]> {
    const qs = new URLSearchParams({
      start: String(params.start.getTime()),
      end: String(params.end.getTime()),
      metrics: JSON.stringify(
        params.metrics.map((m) => ({
          groupId: m.groupId,
          nodeId: m.nodeId,
          deviceId: m.deviceId,
          metricId: m.metricId,
        }))
      ),
      samples: String(params.samples ?? 500),
      raw: String(!!params.raw),
    });
    const resp = await fetch(`/api/v1/history?${qs.toString()}`);
    if (!resp.ok) return [];
    const body = await resp.json();
    if (!body?.success || !Array.isArray(body.results)) return [];

    return body.results.map((r: any) => {
      const points = (r.points ?? []).map((p: any) => {
        let value: number;
        if (typeof p.avg === 'number') value = p.avg;
        else if (typeof p.floatValue === 'number') value = p.floatValue;
        else if (typeof p.intValue === 'number') value = p.intValue;
        else if (typeof p.boolValue === 'boolean') value = p.boolValue ? 1 : 0;
        else if (typeof p.stringValue === 'string') value = Number(p.stringValue);
        else value = NaN;
        return { value, timestamp: new Date(p.timestamp) };
      });
      return {
        groupId: r.groupId,
        nodeId: r.nodeId,
        deviceId: r.deviceId,
        metricId: r.metricId,
        history: points,
      } as MetricHistory;
    });
  }

  function subscribeRealtime(
    metrics: MetricInfo[],
    onData: (
      updates: Array<{
        groupId: string;
        nodeId: string;
        deviceId: string;
        metricId: string;
        value: string;
        timestamp: number;
      }>
    ) => void
  ): () => void {
    const key = (m: { nodeId: string; deviceId: string; metricId: string }) =>
      `${m.nodeId}|${m.deviceId}|${m.metricId}`;
    const wanted = new Map(metrics.map((m) => [key(m), m]));
    if (wanted.size === 0) return () => {};

    const es = new EventSource('/api/v1/history/stream');
    es.addEventListener('data', (ev) => {
      try {
        const d = JSON.parse((ev as MessageEvent).data);
        const match = wanted.get(key(d));
        if (!match) return;
        onData([
          {
            groupId: match.groupId,
            nodeId: d.nodeId,
            deviceId: d.deviceId,
            metricId: d.metricId,
            value: String(d.value),
            timestamp: Number(d.timestamp),
          },
        ]);
      } catch {
        // ignore malformed events
      }
    });

    return () => es.close();
  }

  async function loadMetrics() {
    const resp = await fetch('/api/v1/history/metrics');
    if (!resp.ok) return;
    const body = await resp.json();
    if (body?.success && Array.isArray(body.metrics)) {
      availableMetrics = body.metrics;
    }
  }

  function buildFilterRegex(filter: string): RegExp | null {
    const trimmed = filter.trim();
    if (!trimmed) return null;
    const escaped = trimmed.replace(/[.+?^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*');
    return new RegExp(escaped, 'i');
  }

  const naturalSort = (a: string, b: string) =>
    a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' });

  function togglePanel(key: string) {
    if (expandedPanels.has(key)) {
      expandedPanels.delete(key);
    } else {
      expandedPanels.add(key);
    }
    expandedPanels = new Set(expandedPanels);
  }

  function expandAll() {
    const keys: string[] = [];
    for (const g of filteredGroups) {
      for (const n of g.nodes) {
        const nodeKey = `${g.id}|${n.id}`;
        keys.push(nodeKey);
        for (const d of n.devices) {
          keys.push(`${nodeKey}|${d.id}`);
        }
      }
    }
    expandedPanels = new Set(keys);
  }

  function collapseAll() {
    expandedPanels = new Set();
  }

  function openChartPopover(metric: FlatMetric, event: MouseEvent) {
    const button = event.currentTarget as HTMLElement;
    if (popoverMetric === metric) {
      closeChartPopover();
      return;
    }
    const rect = button.getBoundingClientRect();
    popoverMetric = metric;
    popoverPosition = { top: rect.bottom + 4, left: rect.left };
  }

  function closeChartPopover() {
    popoverMetric = null;
    popoverPosition = null;
  }

  function addToPane(paneId: string) {
    if (!popoverMetric || !addMetricFn) return;
    addMetricFn(paneId, popoverMetric);
    closeChartPopover();
  }

  function isMetricInAllPanes(metric: FlatMetric): boolean {
    if (currentPanes.length === 0) return false;
    const key = getMetricKey(metric);
    return currentPanes.every((pane) => pane.metrics.some((pm) => getMetricKey(pm) === key));
  }

  $effect(() => {
    if (!popoverMetric) return;
    function handleClick(e: MouseEvent) {
      const target = e.target as globalThis.Node;
      const popover = document.querySelector('.chart-popover');
      if (popover && !popover.contains(target)) {
        closeChartPopover();
      }
    }
    document.addEventListener('click', handleClick, true);
    return () => document.removeEventListener('click', handleClick, true);
  });

  // Rebuild Group → Node → Device tree from flat metric refs.
  // deviceId === '' means the metric belongs to the node directly.
  let groups = $derived.by((): Group[] => {
    const gMap = new Map<
      string,
      Map<string, { metrics: Set<string>; devices: Map<string, Set<string>> }>
    >();
    for (const m of availableMetrics) {
      let g = gMap.get(m.groupId);
      if (!g) {
        g = new Map();
        gMap.set(m.groupId, g);
      }
      let n = g.get(m.nodeId);
      if (!n) {
        n = { metrics: new Set(), devices: new Map() };
        g.set(m.nodeId, n);
      }
      if (!m.deviceId) {
        n.metrics.add(m.metricId);
      } else {
        let d = n.devices.get(m.deviceId);
        if (!d) {
          d = new Set();
          n.devices.set(m.deviceId, d);
        }
        d.add(m.metricId);
      }
    }
    const result: Group[] = [];
    for (const [gid, g] of gMap) {
      const nodes: Node[] = [];
      for (const [nid, n] of g) {
        const devices: Device[] = [];
        for (const [did, metrics] of n.devices) {
          devices.push({ id: did, metrics: [...metrics] });
        }
        nodes.push({ id: nid, metrics: [...n.metrics], devices });
      }
      result.push({ id: gid, nodes });
    }
    return result;
  });

  let filterRegex = $derived(buildFilterRegex(metricFilter));

  type FilteredDevice = { id: string; metrics: FlatMetric[]; totalCount: number };
  type FilteredNode = {
    id: string;
    nodeMetrics: FlatMetric[];
    devices: FilteredDevice[];
    totalNodeMetrics: number;
  };
  type FilteredGroup = { id: string; nodes: FilteredNode[] };

  let filteredGroups = $derived.by((): FilteredGroup[] => {
    const result: FilteredGroup[] = [];
    for (const group of groups) {
      const filteredNodes: FilteredNode[] = [];
      for (const node of group.nodes) {
        const allNodeMetrics: FlatMetric[] = node.metrics.map((name) => ({
          groupId: group.id,
          nodeId: node.id,
          deviceId: '',
          metricId: name,
          name,
          type: '',
          path: `${group.id}|${node.id}`,
        }));
        const nodeMetrics = filterRegex
          ? allNodeMetrics.filter((m) => filterRegex!.test(m.name))
          : allNodeMetrics;

        const filteredDevices: FilteredDevice[] = [];
        for (const device of node.devices) {
          const allDevMetrics: FlatMetric[] = device.metrics.map((name) => ({
            groupId: group.id,
            nodeId: node.id,
            deviceId: device.id,
            metricId: name,
            name,
            type: '',
            path: `${group.id}|${node.id}|${device.id}`,
          }));
          const devMetrics = filterRegex
            ? allDevMetrics.filter((m) => filterRegex!.test(m.name))
            : allDevMetrics;
          if (devMetrics.length > 0) {
            filteredDevices.push({
              id: device.id,
              metrics: devMetrics.sort((a, b) => naturalSort(a.name, b.name)),
              totalCount: allDevMetrics.length,
            });
          }
        }
        filteredDevices.sort((a, b) => naturalSort(a.id, b.id));

        if (nodeMetrics.length > 0 || filteredDevices.length > 0) {
          filteredNodes.push({
            id: node.id,
            nodeMetrics: nodeMetrics.sort((a, b) => naturalSort(a.name, b.name)),
            devices: filteredDevices,
            totalNodeMetrics: allNodeMetrics.length,
          });
        }
      }
      filteredNodes.sort((a, b) => naturalSort(a.id, b.id));
      if (filteredNodes.length > 0) {
        result.push({ id: group.id, nodes: filteredNodes });
      }
    }
    return result.sort((a, b) => naturalSort(a.id, b.id));
  });

  let hasMetrics = $derived(
    groups.some((g) =>
      g.nodes.some((n) => n.metrics.length > 0 || n.devices.some((d) => d.metrics.length > 0))
    )
  );
  let hasFilterResults = $derived(filteredGroups.length > 0);
  let showGroupHeaders = $derived(filteredGroups.length > 1);

  onMount(() => {
    loadMetrics();
    const interval = setInterval(loadMetrics, 30_000);
    return () => clearInterval(interval);
  });
</script>

<div class="history-layout">
  <div class="chart-main">
    <TrendChartContainer
      storageKey="tentacle-history-trends"
      {fetchHistory}
      {subscribeRealtime}
      exposePanes={(panes) => {
        currentPanes = panes;
      }}
      exposeAddMetric={(fn) => {
        addMetricFn = fn;
      }}
    />
  </div>

  <section class="metric-section">
    <div class="section-header">
      <h3>Metrics</h3>
      {#if hasMetrics}
        <div class="expand-controls">
          <button type="button" class="expand-btn" onclick={expandAll}>Expand All</button>
          <button type="button" class="expand-btn" onclick={collapseAll}>Collapse All</button>
        </div>
      {/if}
    </div>
    {#if hasMetrics}
      <div class="filter-box">
        <input
          type="text"
          class="filter-input"
          placeholder="Filter metrics... (use * as wildcard)"
          bind:value={metricFilter}
          aria-label="Filter metrics"
        />
        {#if metricFilter}
          <button
            type="button"
            class="filter-clear"
            onclick={() => (metricFilter = '')}
            title="Clear filter"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        {/if}
      </div>
    {/if}
    <div class="metric-list">
      {#if !hasMetrics}
        <p class="empty-hint">No metrics available</p>
      {:else if metricFilter && !hasFilterResults}
        <p class="empty-hint">No metrics match "{metricFilter}"</p>
      {/if}

      {#each filteredGroups as group (group.id)}
        <div class="group-section">
          {#if showGroupHeaders}
            <div class="group-header">{group.id}</div>
          {/if}
          <div class="expansion-panels">
            {#each group.nodes as node (node.id)}
              {@const nodeKey = `${group.id}|${node.id}`}
              <div class="expansion-panel" class:expansion-panel--expanded={expandedPanels.has(nodeKey)}>
                <div
                  class="expansion-panel__header"
                  role="button"
                  tabindex="0"
                  onclick={() => togglePanel(nodeKey)}
                  onkeydown={(e) => e.key === 'Enter' && togglePanel(nodeKey)}
                >
                  <div class="expansion-panel__title">
                    <span class="expansion-panel__name">{node.id}</span>
                    <span class="expansion-panel__subtitle">
                      {#if filterRegex}
                        {node.nodeMetrics.length + node.devices.reduce((sum, d) => sum + d.metrics.length, 0)} matching
                      {:else}
                        {node.nodeMetrics.length} node metrics &middot; {node.devices.length} devices
                      {/if}
                    </span>
                  </div>
                  <span class="expansion-panel__arrow" class:expansion-panel__arrow--expanded={expandedPanels.has(nodeKey)}>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
                      <polyline points="6 9 12 15 18 9" />
                    </svg>
                  </span>
                </div>
                {#if expandedPanels.has(nodeKey)}
                  <div class="expansion-panel__body" transition:slide={{ duration: 200 }}>
                    {#if node.nodeMetrics.length > 0}
                      {#each node.nodeMetrics as metric (`${nodeKey}|${metric.metricId}`)}
                        <div class="metric-row">
                          <div class="metric-name" title={metric.name}>{metric.name}</div>
                          <div class="metric-actions">
                            <button
                              class="chart-btn"
                              title={isMetricInAllPanes(metric) ? 'In all panes' : 'Add to chart'}
                              disabled={isMetricInAllPanes(metric)}
                              onclick={(e) => openChartPopover(metric, e)}
                            >
                              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                                <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
                              </svg>
                            </button>
                          </div>
                        </div>
                      {/each}
                    {/if}

                    {#each node.devices as device (device.id)}
                      {@const deviceKey = `${nodeKey}|${device.id}`}
                      <div class="device-panel" class:device-panel--expanded={expandedPanels.has(deviceKey)}>
                        <div
                          class="device-panel__header"
                          role="button"
                          tabindex="0"
                          onclick={() => togglePanel(deviceKey)}
                          onkeydown={(e) => e.key === 'Enter' && togglePanel(deviceKey)}
                        >
                          <span class="device-panel__name">{device.id}</span>
                          <span class="device-panel__count">
                            {#if filterRegex}{device.metrics.length}/{device.totalCount}{:else}{device.metrics.length}{/if}
                          </span>
                          <span class="device-panel__arrow" class:device-panel__arrow--expanded={expandedPanels.has(deviceKey)}>
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
                              <polyline points="6 9 12 15 18 9" />
                            </svg>
                          </span>
                        </div>
                        {#if expandedPanels.has(deviceKey)}
                          <div class="device-panel__body" transition:slide={{ duration: 150 }}>
                            {#each device.metrics as metric (`${deviceKey}|${metric.metricId}`)}
                              <div class="metric-row">
                                <div class="metric-name" title={metric.name}>{metric.name}</div>
                                <div class="metric-actions">
                                  <button
                                    class="chart-btn"
                                    title={isMetricInAllPanes(metric) ? 'In all panes' : 'Add to chart'}
                                    disabled={isMetricInAllPanes(metric)}
                                    onclick={(e) => openChartPopover(metric, e)}
                                  >
                                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                                      <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
                                    </svg>
                                  </button>
                                </div>
                              </div>
                            {/each}
                          </div>
                        {/if}
                      </div>
                    {/each}
                  </div>
                {/if}
              </div>
            {/each}
          </div>
        </div>
      {/each}
    </div>
  </section>

  {#if popoverMetric && popoverPosition}
    {@const popoverKey = getMetricKey(popoverMetric)}
    <div
      class="chart-popover"
      style="position: fixed; top: {popoverPosition.top}px; left: {popoverPosition.left}px;"
      transition:fade={{ duration: 100 }}
    >
      <div class="chart-popover__title">Add to pane</div>
      {#each currentPanes as pane, i}
        {@const alreadyInPane = pane.metrics.some((pm) => getMetricKey(pm) === popoverKey)}
        <button
          type="button"
          class="chart-popover__option"
          disabled={alreadyInPane}
          onclick={() => addToPane(pane.id)}
        >
          {pane.title || `Pane ${i + 1}`}
          {#if alreadyInPane}
            <span class="chart-popover__check">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <polyline points="20 6 9 17 4 12" />
              </svg>
            </span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style lang="scss">
  .history-layout {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }

  .chart-main {
    min-width: 0;
  }

  .metric-section {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
  }

  .section-header {
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--theme-border);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;

    h3 {
      margin: 0;
      font-size: 0.875rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--theme-text-muted);
    }
  }

  .expand-controls {
    display: flex;
    gap: 0.25rem;
  }

  .expand-btn {
    padding: 0.1875rem 0.5rem;
    font-size: 0.6875rem;
    background: transparent;
    color: var(--theme-text-muted);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm);
    cursor: pointer;

    &:hover {
      background: var(--theme-surface-hover);
      color: var(--theme-text);
    }
  }

  .filter-box {
    padding: 0.5rem;
    border-bottom: 1px solid var(--theme-border);
    position: relative;
  }

  .filter-input {
    width: 100%;
    padding: 0.5rem 2rem 0.5rem 0.75rem;
    font-size: 0.875rem;
    font-family: 'IBM Plex Mono', monospace;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    background: var(--theme-input-bg, var(--theme-bg));
    color: var(--theme-text);

    &::placeholder {
      color: var(--theme-text-muted);
    }

    &:focus {
      outline: none;
      border-color: var(--theme-primary);
    }
  }

  .filter-clear {
    position: absolute;
    right: 0.625rem;
    top: 50%;
    transform: translateY(-50%);
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: none;
    border-radius: var(--rounded-sm);
    background: transparent;
    color: var(--theme-text-muted);
    cursor: pointer;

    &:hover {
      color: var(--theme-text);
      background: var(--theme-surface-hover);
    }
  }

  .metric-list {
    padding: 0.75rem 1rem;
  }

  .empty-hint {
    padding: 1rem;
    color: var(--theme-text-muted);
    font-size: 0.875rem;
    text-align: center;
  }

  .group-section {
    margin-bottom: 1rem;

    &:last-child {
      margin-bottom: 0;
    }
  }

  .group-header {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    border-bottom: 1px solid var(--theme-border);
    padding: 0 0 0.25rem 0;
    margin-bottom: 0.5rem;
  }

  .expansion-panels {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .expansion-panel {
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
    background: var(--theme-surface);
  }

  .expansion-panel__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    background: var(--theme-surface-hover);
    padding: 0.5rem 0.75rem;
    font-weight: 600;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.9375rem;
    cursor: pointer;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 8%, var(--theme-surface));
    }
  }

  .expansion-panel--expanded .expansion-panel__header {
    border-bottom: 1px solid var(--theme-border);
  }

  .expansion-panel__title {
    display: flex;
    flex-direction: column;
    gap: 0.0625rem;
    flex: 1;
    min-width: 0;
  }

  .expansion-panel__name {
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--theme-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .expansion-panel__subtitle {
    font-size: 0.75rem;
    font-weight: 400;
    color: var(--theme-text-muted);
  }

  .expansion-panel__arrow {
    display: flex;
    align-items: center;
    justify-content: center;
    transition: transform 0.3s ease;
    flex-shrink: 0;
    color: var(--theme-text-muted);
  }

  .expansion-panel__header:hover .expansion-panel__arrow {
    color: var(--theme-primary);
  }

  .expansion-panel__arrow--expanded {
    transform: rotate(-180deg);
  }

  .expansion-panel__body {
    padding: 0;
  }

  .device-panel {
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
  }

  .device-panel__header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.375rem 0.75rem 0.375rem 1.25rem;
    cursor: pointer;
    background: color-mix(in srgb, var(--theme-primary) 4%, transparent);

    &:hover {
      background: color-mix(in srgb, var(--theme-primary) 8%, transparent);
    }
  }

  .device-panel--expanded .device-panel__header {
    border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 40%, transparent);
  }

  .device-panel__name {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--theme-text);
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .device-panel__count {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .device-panel__arrow {
    display: flex;
    align-items: center;
    transition: transform 0.2s ease;
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .device-panel__header:hover .device-panel__arrow {
    color: var(--theme-primary);
  }

  .device-panel__arrow--expanded {
    transform: rotate(-180deg);
  }

  .device-panel__body {
    padding-left: 0.5rem;
  }

  .metric-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.5rem;
    padding: 0.375rem 0.75rem;
    border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 40%, transparent);

    &:last-child {
      border-bottom: none;
    }

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 3%, transparent);
    }
  }

  .metric-name {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    min-width: 0;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .metric-actions {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    flex-shrink: 0;
  }

  .chart-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    background: transparent;
    color: var(--theme-text-muted);
    border: none;
    border-radius: var(--rounded-sm);
    cursor: pointer;

    &:hover:not(:disabled) {
      color: var(--theme-primary);
      background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    }

    &:disabled {
      opacity: 0.3;
      cursor: default;
    }
  }

  .chart-popover {
    z-index: 200;
    min-width: 140px;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.15), 0 2px 4px -1px rgba(0, 0, 0, 0.1);
    overflow: hidden;
  }

  .chart-popover__title {
    padding: 0.375rem 0.5rem;
    font-size: 0.6875rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
    border-bottom: 1px solid var(--theme-border);
  }

  .chart-popover__option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 0.375rem 0.5rem;
    border: none;
    background: transparent;
    color: var(--theme-text);
    font-size: 0.875rem;
    text-align: left;
    cursor: pointer;

    &:hover:not(:disabled) {
      background: var(--theme-surface-hover);
    }

    &:disabled {
      color: var(--theme-text-muted);
      cursor: default;
    }
  }

  .chart-popover__check {
    color: var(--theme-primary);
    display: flex;
    align-items: center;
  }
</style>
