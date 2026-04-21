<script lang="ts">
  import { onMount } from 'svelte';
  import * as d3 from 'd3';
  import { api } from '$lib/api/client';

  type TimelinePoint = { timestamp: string; state: string };
  type SFStatus = {
    state: string;
    primaryHostId: string | null;
    primaryHostOnline: boolean;
    primaryHostConfigured: boolean;
    primaryHostSeen: boolean;
    brokerReachable: boolean;
    nodeBorn: boolean;
    bufferedCount: number;
    maxRecords: number;
    usagePercent: number;
    draining: boolean;
    drainEtaSeconds: number;
    totalBuffered: number;
    totalDrained: number;
    totalEvicted: number;
    publishRate: number;
    timeline: TimelinePoint[];
  };

  let { initialStatus = null }: { initialStatus?: SFStatus | null } = $props();

  let status: SFStatus | null = $state(initialStatus);
  let loaded = $state(initialStatus !== null);
  let gaugeContainer: SVGSVGElement;
  let timelineContainer: SVGSVGElement;
  let evictionDismissedAt = $state(0); // totalEvicted count when dismissed

  type BannerKind = 'online' | 'waiting' | 'offline' | 'draining' | 'no-broker';
  type Banner = { kind: BannerKind; title: string; subtitle: string };

  function computeBanner(s: SFStatus): Banner {
    if (s.draining) {
      return { kind: 'draining', title: 'Backfilling', subtitle: `~${formatEta(s.drainEtaSeconds)} remaining` };
    }
    if (!s.brokerReachable) {
      return { kind: 'no-broker', title: 'MQTT Broker Unreachable', subtitle: 'Buffering data locally' };
    }
    if (!s.nodeBorn) {
      return { kind: 'no-broker', title: 'Sparkplug Session Not Established', subtitle: 'Buffering until NBIRTH completes' };
    }
    if (!s.primaryHostConfigured) {
      return { kind: 'online', title: 'Publishing Live', subtitle: 'No primary host configured' };
    }
    if (!s.primaryHostSeen) {
      return {
        kind: 'waiting',
        title: 'Waiting for Primary Host',
        subtitle: `Buffering until ${s.primaryHostId ?? 'primary host'} announces ONLINE`,
      };
    }
    if (s.primaryHostOnline) {
      return { kind: 'online', title: 'Primary Host Online', subtitle: `Publishing live${s.primaryHostId ? ` to ${s.primaryHostId}` : ''}` };
    }
    return {
      kind: 'offline',
      title: 'Primary Host Offline',
      subtitle: `Buffering data${s.primaryHostId ? ` (${s.primaryHostId})` : ''}`,
    };
  }

  async function poll() {
    try {
      const result = await api<SFStatus | null>('/mqtt/store-forward');
      if (result.data) {
        status = result.data;
        loaded = true;
      }
    } catch { /* ignore */ }
  }

  onMount(() => {
    poll();
    const interval = setInterval(poll, 2000);
    return () => clearInterval(interval);
  });

  // Re-render d3 visuals after the DOM has committed. Using $effect ensures
  // the svg elements mounted by {#if loaded} have bound refs before d3 runs.
  $effect(() => {
    if (!loaded || !status) return;
    if (gaugeContainer) renderGauge();
    if (timelineContainer) renderTimeline();
  });

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1048576).toFixed(1)} MB`;
  }

  function formatEta(seconds: number): string {
    if (seconds < 60) return `${Math.ceil(seconds)}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.ceil(seconds % 60)}s`;
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
  }

  function getUsedPercent(): number {
    if (!status) return 0;
    return status.usagePercent;
  }

  function getGaugeColor(pct: number): string {
    if (pct < 50) return 'var(--green-500, #22c55e)';
    if (pct < 80) return 'var(--amber-500, #f59e0b)';
    return 'var(--red-500, #ef4444)';
  }

  function renderGauge() {
    if (!gaugeContainer || !status) return;

    const svg = d3.select(gaugeContainer);
    svg.selectAll('*').remove();

    const width = 180;
    const height = 120;
    const radius = 70;
    const thickness = 14;

    svg.attr('viewBox', `0 0 ${width} ${height}`);

    const g = svg.append('g')
      .attr('transform', `translate(${width / 2}, ${height - 15})`);

    const pct = getUsedPercent();
    const startAngle = -Math.PI / 2 - 0.4;
    const endAngle = Math.PI / 2 + 0.4;
    const scale = d3.scaleLinear().domain([0, 100]).range([startAngle, endAngle]).clamp(true);

    // Background arc
    const bgArc = d3.arc<any>()
      .innerRadius(radius - thickness)
      .outerRadius(radius)
      .startAngle(startAngle)
      .endAngle(endAngle)
      .cornerRadius(thickness / 2);

    g.append('path')
      .attr('d', bgArc({}) as string)
      .attr('fill', 'var(--theme-border)')
      .attr('opacity', 0.3);

    // Value arc
    const valueArc = d3.arc<any>()
      .innerRadius(radius - thickness)
      .outerRadius(radius)
      .startAngle(startAngle)
      .endAngle(scale(pct))
      .cornerRadius(thickness / 2);

    g.append('path')
      .attr('d', valueArc({}) as string)
      .attr('fill', getGaugeColor(pct));

    // Glow effect when buffering
    if (!status.primaryHostOnline && pct > 0) {
      g.append('path')
        .attr('d', valueArc({}) as string)
        .attr('fill', 'none')
        .attr('stroke', getGaugeColor(pct))
        .attr('stroke-width', 2)
        .attr('opacity', 0.4)
        .attr('filter', 'blur(4px)');
    }

    // Center text — record count (large)
    g.append('text')
      .attr('text-anchor', 'middle')
      .attr('y', -14)
      .attr('fill', 'var(--theme-text)')
      .attr('font-size', '20px')
      .attr('font-weight', '600')
      .attr('font-family', "'IBM Plex Mono', monospace")
      .text(status.bufferedCount.toLocaleString());

    // Sub text — percentage
    g.append('text')
      .attr('text-anchor', 'middle')
      .attr('y', 4)
      .attr('fill', 'var(--theme-text-muted)')
      .attr('font-size', '10px')
      .attr('font-family', "'IBM Plex Mono', monospace")
      .text(`${pct.toFixed(0)}%`);
  }

  const stateColors: Record<string, string> = {
    online: '#22c55e',
    buffering: '#ef4444',
    draining: '#38bdf8',
  };

  function renderTimeline() {
    if (!timelineContainer || !status) return;

    const svg = d3.select(timelineContainer);
    svg.selectAll('*').remove();

    const rect = timelineContainer.getBoundingClientRect();
    const width = Math.max(rect.width, 200);
    const height = 24;

    svg.attr('width', width).attr('height', height);

    const data = status.timeline;
    const now = Date.now();
    const hourAgo = now - 3600_000;

    // Background — unknown/no data
    svg.append('rect')
      .attr('x', 0).attr('y', 0)
      .attr('width', width).attr('height', height)
      .attr('fill', 'var(--theme-border)')
      .attr('rx', 3)
      .attr('opacity', 0.3);

    if (data.length === 0) return;

    const x = d3.scaleTime()
      .domain([new Date(hourAgo), new Date(now)])
      .range([0, width]);

    // Draw colored segments for each timeline sample
    for (let i = 0; i < data.length; i++) {
      const ts = new Date(data[i].timestamp).getTime();
      const nextTs = i < data.length - 1 ? new Date(data[i + 1].timestamp).getTime() : now;

      if (nextTs < hourAgo) continue;

      const x1 = Math.max(0, x(new Date(Math.max(ts, hourAgo))));
      const x2 = Math.min(width, x(new Date(nextTs)));
      const segWidth = Math.max(1, x2 - x1);

      svg.append('rect')
        .attr('x', x1)
        .attr('y', 0)
        .attr('width', segWidth)
        .attr('height', height)
        .attr('fill', stateColors[data[i].state] ?? '#666')
        .attr('opacity', 0.8);
    }
  }
</script>

<div class="sf-container">
  <div class="sf-main">
    <!-- Buffer Gauge + Publish Rate -->
    <div class="sf-gauges">
      <div class="sf-gauge-wrapper">
        {#if loaded}
          <svg bind:this={gaugeContainer} class="sf-gauge"></svg>
        {:else}
          <div class="sf-gauge sf-skeleton sf-skeleton-gauge" aria-hidden="true"></div>
        {/if}
        <span class="sf-gauge-label">Buffer</span>
      </div>
      <div class="sf-rate">
        {#if loaded && status}
          <span class="sf-rate-value">{status.publishRate.toFixed(1)}</span>
          <span class="sf-rate-unit">metrics/s</span>
        {:else}
          <span class="sf-rate-value sf-skeleton sf-skeleton-text" aria-hidden="true">&nbsp;&nbsp;&nbsp;&nbsp;</span>
          <span class="sf-rate-unit">metrics/s</span>
        {/if}
      </div>
    </div>

    <!-- Status + details to the right of gauge -->
    <div class="sf-right">
      {#if loaded && status}
        {@const banner = computeBanner(status)}
        <div
          class="sf-banner"
          class:online={banner.kind === 'online'}
          class:offline={banner.kind === 'offline' || banner.kind === 'no-broker'}
          class:waiting={banner.kind === 'waiting'}
          class:draining={banner.kind === 'draining'}
        >
          <div class="sf-banner-dot"></div>
          <div class="sf-banner-text">
            <strong>{banner.title}</strong>
            <span>{banner.subtitle}</span>
          </div>
        </div>

        {#if status.totalEvicted > evictionDismissedAt}
          <button class="sf-eviction-warning" onclick={() => evictionDismissedAt = status!.totalEvicted}>
            {(status.totalEvicted - evictionDismissedAt).toLocaleString()} records lost (buffer full) <span class="sf-dismiss">dismiss</span>
          </button>
        {/if}
      {:else}
        <div class="sf-banner sf-skeleton-banner" aria-hidden="true">
          <div class="sf-banner-dot sf-skeleton"></div>
          <div class="sf-banner-text">
            <strong class="sf-skeleton sf-skeleton-text">&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;</strong>
            <span class="sf-skeleton sf-skeleton-text">&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;</span>
          </div>
        </div>
      {/if}

      <!-- Status Timeline (last hour) -->
      <div class="sf-timeline">
        <div class="sf-timeline-header">
          <span class="sf-timeline-label">Service Status (1 hour)</span>
          <div class="sf-timeline-legend">
            <span class="sf-legend-item"><span class="sf-legend-dot" style="background: #22c55e"></span>Online</span>
            <span class="sf-legend-item"><span class="sf-legend-dot" style="background: #ef4444"></span>Buffering</span>
            <span class="sf-legend-item"><span class="sf-legend-dot" style="background: #38bdf8"></span>Backfilling</span>
          </div>
        </div>
        {#if loaded}
          <svg bind:this={timelineContainer} class="sf-timeline-svg"></svg>
        {:else}
          <div class="sf-timeline-svg sf-skeleton" aria-hidden="true"></div>
        {/if}
      </div>
    </div>
  </div>
</div>

<style lang="scss">
  .sf-container {
    margin-top: 1.5rem;
  }

  .sf-main {
    display: flex;
    align-items: flex-start;
    gap: 1.5rem;

    @media (max-width: 640px) {
      flex-direction: column;
      align-items: center;
    }
  }

  .sf-right {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-width: 0;
  }

  .sf-banner {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    border-radius: var(--rounded-lg);

    &.online {
      background: rgba(34, 197, 94, 0.08);
      border: 1px solid rgba(34, 197, 94, 0.25);
      .sf-banner-dot { background: #22c55e; box-shadow: 0 0 6px #22c55e; }
    }

    &.offline {
      background: rgba(239, 68, 68, 0.08);
      border: 1px solid rgba(239, 68, 68, 0.25);
      animation: pulse-border 2s ease-in-out infinite;
      .sf-banner-dot { background: #ef4444; box-shadow: 0 0 6px #ef4444; animation: pulse-dot 2s ease-in-out infinite; }
    }

    &.waiting {
      background: rgba(245, 158, 11, 0.08);
      border: 1px solid rgba(245, 158, 11, 0.25);
      .sf-banner-dot { background: #f59e0b; box-shadow: 0 0 6px #f59e0b; animation: pulse-dot 2s ease-in-out infinite; }
    }

    &.draining {
      background: rgba(56, 189, 248, 0.08);
      border: 1px solid rgba(56, 189, 248, 0.25);
      .sf-banner-dot { background: #38bdf8; box-shadow: 0 0 6px #38bdf8; }
    }
  }

  .sf-skeleton {
    background: linear-gradient(
      90deg,
      var(--theme-border) 0%,
      var(--theme-surface-hover, rgba(255, 255, 255, 0.05)) 50%,
      var(--theme-border) 100%
    );
    background-size: 200% 100%;
    animation: skeleton-shimmer 1.4s ease-in-out infinite;
    border-radius: var(--rounded-md);
    color: transparent !important;
  }

  .sf-skeleton-gauge {
    border-radius: 50% 50% 0 0 / 100% 100% 0 0;
    opacity: 0.4;
  }

  .sf-skeleton-text {
    display: inline-block;
    border-radius: var(--rounded-sm, 3px);
  }

  .sf-skeleton-banner {
    background: var(--theme-surface, transparent);
    border: 1px solid var(--theme-border);
    .sf-banner-dot { animation: skeleton-shimmer 1.4s ease-in-out infinite; box-shadow: none; }
  }

  @keyframes skeleton-shimmer {
    0% { background-position: 200% 0; }
    100% { background-position: -200% 0; }
  }

  .sf-banner-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .sf-banner-text {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;

    strong { font-size: 0.8125rem; color: var(--theme-text); }
    span { font-size: 0.75rem; color: var(--theme-text-muted); }
  }

  .sf-drain {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 1rem;
  }

  .sf-drain-bar {
    flex: 1;
    height: 6px;
    background: var(--theme-border);
    border-radius: 3px;
    overflow: hidden;
  }

  .sf-drain-fill {
    height: 100%;
    background: var(--sky-400, #38bdf8);
    border-radius: 3px;
    transition: width 0.5s ease;
    background-image: repeating-linear-gradient(
      -45deg,
      transparent,
      transparent 6px,
      rgba(255,255,255,0.15) 6px,
      rgba(255,255,255,0.15) 12px
    );
    background-size: 17px 100%;
    animation: march-stripes 0.8s linear infinite;
  }

  .sf-drain-label {
    font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text-muted);
    min-width: 2.5rem;
    text-align: right;
  }

  .sf-gauges {
    display: flex;
    flex-direction: column;
    align-items: center;
    flex-shrink: 0;
    gap: 0.5rem;
  }

  .sf-gauge-wrapper {
    display: flex;
    flex-direction: column;
    align-items: center;
  }

  .sf-rate {
    display: flex;
    align-items: baseline;
    gap: 0.25rem;
  }

  .sf-rate-value {
    font-size: 1.5rem;
    font-weight: 600;
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text);
    line-height: 1;
  }

  .sf-rate-unit {
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
  }

  .sf-gauge {
    width: 180px;
    height: 120px;
  }

  .sf-gauge-label {
    font-size: 0.6875rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
    margin-top: -0.25rem;
  }



  .sf-eviction-warning {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.6875rem;
    font-family: inherit;
    color: var(--red-500, #ef4444);
    background: none;
    border: none;
    cursor: pointer;
    margin-top: 0.375rem;
    padding: 0;

    .sf-dismiss {
      font-size: 0.625rem;
      color: var(--theme-text-muted);
      text-decoration: underline;
      opacity: 0.7;

      &:hover {
        opacity: 1;
      }
    }
  }

  .sf-timeline {
    margin-top: 0.25rem;
  }

  .sf-timeline-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.375rem;
  }

  .sf-timeline-label {
    font-size: 0.6875rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
  }

  .sf-timeline-legend {
    display: flex;
    gap: 0.75rem;
  }

  .sf-legend-item {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    font-size: 0.625rem;
    color: var(--theme-text-muted);
  }

  .sf-legend-dot {
    width: 8px;
    height: 8px;
    border-radius: 2px;
  }

  .sf-timeline-svg {
    display: block;
    width: 100%;
    height: 24px;
    border-radius: var(--rounded-md);
    overflow: hidden;
  }

  @keyframes pulse-border {
    0%, 100% { border-color: rgba(239, 68, 68, 0.25); }
    50% { border-color: rgba(239, 68, 68, 0.5); }
  }

  @keyframes pulse-dot {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }

  @keyframes march-stripes {
    to { background-position: 17px 0; }
  }
</style>
