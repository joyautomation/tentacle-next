<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import * as d3 from 'd3';
  import type { HistoryDiffChange } from '$lib/types/gitops-history';

  let {
    changes,
    onselect,
  }: {
    changes: HistoryDiffChange[];
    onselect: (change: HistoryDiffChange) => void;
  } = $props();

  type DiffNodeDatum = {
    id: string;
    name: string;
    kind: string;
    action: string; // 'added' | 'removed' | 'modified' | 'unchanged' | 'hub' | 'kind-group'
    depth: number;
    change?: HistoryDiffChange;
    x?: number;
    y?: number;
    vx?: number;
    vy?: number;
    fx?: number | null;
    fy?: number | null;
  };

  type DiffLinkDatum = d3.SimulationLinkDatum<DiffNodeDatum> & {
    source: DiffNodeDatum | string;
    target: DiffNodeDatum | string;
  };

  let container: HTMLDivElement;
  let simulation: d3.Simulation<DiffNodeDatum, DiffLinkDatum> | null = null;
  let svgSelection: d3.Selection<SVGSVGElement, unknown, null, undefined> | null = null;
  let resizeObserver: ResizeObserver | null = null;

  function getActionColor(action: string): string {
    switch (action) {
      case 'added': return 'var(--green-500, #22c55e)';
      case 'modified': return 'var(--amber-500, #f59e0b)';
      case 'removed': return 'var(--red-500, #ef4444)';
      case 'hub': return 'var(--purple-500, #a855f7)';
      case 'kind-group': return 'var(--teal-500, #14b8a6)';
      default: return 'var(--theme-text-muted, #6b7280)';
    }
  }

  function getNodeRadius(action: string): number {
    switch (action) {
      case 'hub': return 45;
      case 'kind-group': return 30;
      default: return 22;
    }
  }

  function getNodeOpacity(action: string): number {
    return action === 'unchanged' ? 0.35 : 1;
  }

  function buildGraph(): { nodes: DiffNodeDatum[]; links: DiffLinkDatum[] } {
    const nodes: DiffNodeDatum[] = [];
    const links: DiffLinkDatum[] = [];

    // Center hub.
    nodes.push({
      id: 'config-hub',
      name: 'Config',
      kind: '',
      action: 'hub',
      depth: 0,
    });

    // Group changes by kind.
    const kindMap = new Map<string, HistoryDiffChange[]>();
    for (const change of changes) {
      const existing = kindMap.get(change.kind) ?? [];
      existing.push(change);
      kindMap.set(change.kind, existing);
    }

    for (const [kind, items] of kindMap) {
      const kindId = `kind-${kind}`;
      nodes.push({
        id: kindId,
        name: kind,
        kind,
        action: 'kind-group',
        depth: 1,
      });
      links.push({ source: 'config-hub', target: kindId });

      for (const change of items) {
        const nodeId = `${change.kind}/${change.name}`;
        nodes.push({
          id: nodeId,
          name: change.name,
          kind: change.kind,
          action: change.action,
          depth: 2,
          change,
        });
        links.push({ source: kindId, target: nodeId });
      }
    }

    return { nodes, links };
  }

  function render() {
    if (!container) return;

    // Clean up previous.
    if (simulation) simulation.stop();
    d3.select(container).selectAll('svg').remove();

    const width = container.clientWidth;
    const height = Math.max(container.clientHeight, 400);
    const layerRadius = Math.max(Math.min(width, height) * 0.2, 100);

    const { nodes, links } = buildGraph();

    if (nodes.length <= 1) return; // only hub, nothing to show

    const svg = d3.select(container)
      .append('svg')
      .attr('width', '100%')
      .attr('height', '100%')
      .attr('viewBox', `${-width / 2} ${-height / 2} ${width} ${height}`)
      .attr('preserveAspectRatio', 'xMidYMid meet');

    svgSelection = svg;

    // Defs: glow filter.
    const defs = svg.append('defs');
    const filter = defs.append('filter').attr('id', 'diff-glow');
    filter.append('feGaussianBlur').attr('stdDeviation', '3').attr('result', 'blur');
    const merge = filter.append('feMerge');
    merge.append('feMergeNode').attr('in', 'blur');
    merge.append('feMergeNode').attr('in', 'SourceGraphic');

    const g = svg.append('g');

    // Zoom.
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.2, 3])
      .on('zoom', (event) => g.attr('transform', event.transform));
    svg.call(zoom);

    // Links.
    const link = g.append('g')
      .selectAll<SVGLineElement, DiffLinkDatum>('line')
      .data(links)
      .join('line')
      .attr('stroke', 'var(--theme-border, #374151)')
      .attr('stroke-width', 1.5)
      .attr('stroke-opacity', 0.4);

    // Nodes.
    const node = g.append('g')
      .selectAll<SVGGElement, DiffNodeDatum>('g')
      .data(nodes)
      .join('g')
      .style('cursor', (d) => d.depth === 2 ? 'pointer' : 'default')
      .on('click', (_event, d) => {
        if (d.change) onselect(d.change);
      });

    // Node circles.
    node.append('circle')
      .attr('r', (d) => getNodeRadius(d.action))
      .attr('fill', (d) => {
        const color = getActionColor(d.action);
        return color;
      })
      .attr('fill-opacity', (d) => {
        if (d.action === 'unchanged') return 0.15;
        if (d.action === 'hub' || d.action === 'kind-group') return 0.2;
        return 0.25;
      })
      .attr('stroke', (d) => getActionColor(d.action))
      .attr('stroke-width', (d) => d.action === 'hub' ? 3 : 2)
      .attr('stroke-opacity', (d) => getNodeOpacity(d.action))
      .attr('stroke-dasharray', (d) => d.action === 'removed' ? '4 4' : 'none')
      .attr('filter', (d) => {
        if (d.action === 'added' || d.action === 'modified' || d.action === 'hub') return 'url(#diff-glow)';
        return 'none';
      });

    // Node labels.
    node.append('text')
      .text((d) => d.name)
      .attr('text-anchor', 'middle')
      .attr('dy', (d) => d.depth === 0 ? '0.35em' : getNodeRadius(d.action) + 14)
      .attr('font-size', (d) => d.depth === 0 ? '12px' : d.depth === 1 ? '11px' : '10px')
      .attr('font-weight', (d) => d.depth <= 1 ? '600' : '500')
      .attr('fill', (d) => {
        if (d.action === 'unchanged') return 'var(--theme-text-muted, #6b7280)';
        if (d.action === 'removed') return 'var(--red-500, #ef4444)';
        return 'var(--theme-text, #e5e7eb)';
      })
      .attr('fill-opacity', (d) => getNodeOpacity(d.action));

    // Action label inside node (for resource nodes).
    node.filter((d) => d.depth === 2 && d.action !== 'unchanged')
      .append('text')
      .text((d) => {
        switch (d.action) {
          case 'added': return '+';
          case 'modified': return '~';
          case 'removed': return '-';
          default: return '';
        }
      })
      .attr('text-anchor', 'middle')
      .attr('dy', '0.35em')
      .attr('font-size', '14px')
      .attr('font-weight', '700')
      .attr('fill', (d) => getActionColor(d.action));

    // Force simulation.
    simulation = d3.forceSimulation<DiffNodeDatum>(nodes)
      .force('link', d3.forceLink<DiffNodeDatum, DiffLinkDatum>(links)
        .id((d) => d.id)
        .distance(layerRadius * 0.85)
        .strength(0.6)
      )
      .force('charge', d3.forceManyBody<DiffNodeDatum>().strength(-400))
      .force('radial', d3.forceRadial<DiffNodeDatum>(
        (d) => d.depth * layerRadius,
        0, 0
      ).strength(1.2))
      .force('collide', d3.forceCollide<DiffNodeDatum>(
        (d) => getNodeRadius(d.action) + 20
      ))
      .on('tick', () => {
        link
          .attr('x1', (d) => (d.source as DiffNodeDatum).x ?? 0)
          .attr('y1', (d) => (d.source as DiffNodeDatum).y ?? 0)
          .attr('x2', (d) => (d.target as DiffNodeDatum).x ?? 0)
          .attr('y2', (d) => (d.target as DiffNodeDatum).y ?? 0);

        node.attr('transform', (d) => `translate(${d.x ?? 0},${d.y ?? 0})`);
      });

    // Pre-settle.
    simulation.tick(150);
    simulation.alpha(0.01).restart();

    // Auto-fit.
    const bounds = (g.node() as SVGGElement)?.getBBox();
    if (bounds) {
      const pad = 60;
      const fullW = bounds.width + pad * 2;
      const fullH = bounds.height + pad * 2;
      const scale = Math.min(width / fullW, height / fullH, 1.5);
      const cx = bounds.x + bounds.width / 2;
      const cy = bounds.y + bounds.height / 2;
      svg.call(zoom.transform, d3.zoomIdentity.scale(scale).translate(-cx, -cy));
    }

    // Drag behavior.
    const drag = d3.drag<SVGGElement, DiffNodeDatum>()
      .on('start', (event, d) => {
        if (!event.active) simulation?.alphaTarget(0.1).restart();
        d.fx = d.x;
        d.fy = d.y;
      })
      .on('drag', (event, d) => {
        d.fx = event.x;
        d.fy = event.y;
      })
      .on('end', (event, d) => {
        if (!event.active) simulation?.alphaTarget(0);
        d.fx = null;
        d.fy = null;
      });
    node.call(drag);

    // Hover effects.
    node
      .on('mouseenter', function (_, d) {
        d3.select(this).select('circle')
          .attr('stroke-width', d.action === 'hub' ? 4 : 3)
          .style('filter', 'brightness(1.2)');
      })
      .on('mouseleave', function (_, d) {
        d3.select(this).select('circle')
          .attr('stroke-width', d.action === 'hub' ? 3 : 2)
          .style('filter', null);
      });
  }

  $effect(() => {
    // Re-render when changes update.
    changes;
    if (container) render();
  });

  onMount(() => {
    resizeObserver = new ResizeObserver(() => render());
    resizeObserver.observe(container);
  });

  onDestroy(() => {
    if (simulation) simulation.stop();
    if (resizeObserver) resizeObserver.disconnect();
  });
</script>

<div class="topology-container" bind:this={container}>
  {#if changes.length === 0}
    <div class="empty">No changes between selected commits.</div>
  {/if}
</div>

<style lang="scss">
  .topology-container {
    width: 100%;
    min-height: 400px;
    height: 60vh;
    position: relative;

    :global(svg) {
      display: block;
    }

    :global(text) {
      user-select: none;
      pointer-events: none;
    }
  }

  .empty {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    color: var(--theme-text-muted);
    font-size: 0.875rem;
  }
</style>
