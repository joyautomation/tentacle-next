<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { TrendChartContainer } from '@joyautomation/cortex/charts';
  import type {
    MetricIdentifier,
    MetricHistory,
    MetricInfo,
  } from '@joyautomation/cortex/charts';
  import HistoryMetricBrowser from './HistoryMetricBrowser.svelte';
  import type { HistoryMetricRef } from './HistoryMetricBrowser.svelte';

  // History module uses 4-tuple (group, node, device, metric) matching mantle.
  // fetchHistory hits /api/v1/history?start=&end=&metrics=...&samples=&raw=
  // subscribeRealtime listens to /api/v1/history/stream (SSE) and filters.

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
        // Prefer avg (bucketed), then float, then int. Bool/string coerced to number where possible.
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

  let availableMetrics = $state<HistoryMetricRef[]>([]);
  let addMetricFn: ((paneId: string, metric: MetricInfo) => void) | null = null;
  let panes = $state<Array<{ id: string; title?: string; metrics: MetricInfo[] }>>([]);

  function exposeAddMetric(fn: (paneId: string, metric: MetricInfo) => void) {
    addMetricFn = fn;
  }
  function exposePanes(p: typeof panes) {
    panes = p;
  }

  async function loadMetrics() {
    const resp = await fetch('/api/v1/history/metrics');
    if (!resp.ok) return;
    const body = await resp.json();
    if (body?.success && Array.isArray(body.metrics)) {
      availableMetrics = body.metrics;
    }
  }

  function handleAddMetric(ref: HistoryMetricRef) {
    if (!addMetricFn) return;
    const paneId = panes[0]?.id;
    if (!paneId) return;
    addMetricFn(paneId, {
      groupId: ref.groupId,
      nodeId: ref.nodeId,
      deviceId: ref.deviceId,
      metricId: ref.metricId,
      name: ref.metricId,
      type: 'number',
    });
  }

  onMount(() => {
    loadMetrics();
    const interval = setInterval(loadMetrics, 30_000);
    return () => clearInterval(interval);
  });
</script>

<div class="history-trends">
  <aside class="browser">
    <HistoryMetricBrowser
      metrics={availableMetrics}
      onAdd={handleAddMetric}
    />
  </aside>
  <section class="chart">
    <TrendChartContainer
      storageKey="tentacle-history-trends"
      {fetchHistory}
      {subscribeRealtime}
      {exposeAddMetric}
      {exposePanes}
    />
  </section>
</div>

<style lang="scss">
  .history-trends {
    display: grid;
    grid-template-columns: 260px 1fr;
    gap: 1rem;
    min-height: 480px;
  }

  .browser {
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 0.75rem;
    overflow-y: auto;
    max-height: 640px;
    background: var(--theme-surface);
  }

  .chart {
    min-width: 0;
  }

  @media (max-width: 768px) {
    .history-trends {
      grid-template-columns: 1fr;
    }
  }
</style>
