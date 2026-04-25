<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import * as d3 from 'd3';

  interface Props {
    compact?: boolean;
  }

  let { compact = true }: Props = $props();

  let container: HTMLDivElement;
  let resizeObserver: ResizeObserver | null = null;

  type DiagramNode = {
    id: string;
    label: string;
    abbr: string;
    layer: number;
    row: number;
    color: string;
  };

  type DiagramLink = {
    source: string;
    target: string;
  };

  function buildDiagram(): { nodes: DiagramNode[]; links: DiagramLink[] } {
    const nodes: DiagramNode[] = [
      { id: 'broker', label: 'MQTT Broker', abbr: 'BRK', layer: 0, row: 1, color: 'var(--sky-500, #0ea5e9)' },
      { id: 'sparkplug', label: 'Sparkplug Host', abbr: 'SPB', layer: 1, row: 1, color: 'var(--purple-500, #a855f7)' },
      { id: 'history', label: 'History', abbr: 'HIST', layer: 2, row: 0, color: 'var(--teal-500, #14b8a6)' },
      { id: 'git', label: 'Git VC', abbr: 'GIT', layer: 2, row: 2, color: 'var(--teal-500, #14b8a6)' },
    ];

    const links: DiagramLink[] = [
      { source: 'broker', target: 'sparkplug' },
      { source: 'sparkplug', target: 'history' },
      { source: 'sparkplug', target: 'git' },
    ];

    return { nodes, links };
  }

  function render() {
    if (!container) return;

    d3.select(container).selectAll('*').remove();

    const { nodes, links } = buildDiagram();

    const width = container.clientWidth || 500;
    const height = container.clientHeight || 260;

    const svg = d3.select(container)
      .append('svg')
      .attr('width', '100%')
      .attr('height', '100%')
      .attr('viewBox', `0 0 ${width} ${height}`)
      .attr('preserveAspectRatio', 'xMidYMid meet');

    const defs = svg.append('defs');
    const filter = defs.append('filter')
      .attr('id', 'mantle-glow')
      .attr('x', '-50%').attr('y', '-50%')
      .attr('width', '200%').attr('height', '200%');
    filter.append('feGaussianBlur')
      .attr('stdDeviation', '2.5')
      .attr('result', 'coloredBlur');
    const merge = filter.append('feMerge');
    merge.append('feMergeNode').attr('in', 'coloredBlur');
    merge.append('feMergeNode').attr('in', 'SourceGraphic');

    const g = svg.append('g');

    const padX = compact ? 40 : 60;
    const padY = compact ? 30 : 40;
    const layerCount = 3;
    const maxRows = 3;
    const layerSpacing = (width - padX * 2) / (layerCount - 1);
    const rowSpacing = (height - padY * 2) / (maxRows - 1);

    const nodeX = (n: DiagramNode) => padX + n.layer * layerSpacing;
    const nodeY = (n: DiagramNode) => padY + n.row * rowSpacing;

    const nodeRadius = compact ? 16 : 22;

    const linkGroup = g.append('g').attr('class', 'links');

    linkGroup.selectAll('line.base')
      .data(links)
      .join('line')
      .attr('class', 'base')
      .attr('x1', d => nodeX(nodes.find(n => n.id === d.source)!))
      .attr('y1', d => nodeY(nodes.find(n => n.id === d.source)!))
      .attr('x2', d => nodeX(nodes.find(n => n.id === d.target)!))
      .attr('y2', d => nodeY(nodes.find(n => n.id === d.target)!))
      .attr('stroke', 'var(--theme-border)')
      .attr('stroke-width', 1.5)
      .attr('opacity', 0.5);

    const flowGroup = g.append('g').attr('class', 'flow-overlay');
    flowGroup.selectAll('line.flow')
      .data(links)
      .join('line')
      .attr('class', 'flow-line')
      .attr('x1', d => nodeX(nodes.find(n => n.id === d.source)!))
      .attr('y1', d => nodeY(nodes.find(n => n.id === d.source)!))
      .attr('x2', d => nodeX(nodes.find(n => n.id === d.target)!))
      .attr('y2', d => nodeY(nodes.find(n => n.id === d.target)!))
      .attr('stroke', 'var(--sky-400, #38bdf8)')
      .attr('stroke-width', 2)
      .attr('stroke-opacity', 0.7)
      .attr('stroke-dasharray', '6 8');

    const nodeGroup = g.append('g').attr('class', 'nodes');
    const nodeGs = nodeGroup.selectAll('g')
      .data(nodes)
      .join('g')
      .attr('transform', d => `translate(${nodeX(d)}, ${nodeY(d)})`);

    nodeGs.append('circle')
      .attr('r', d => d.id === 'sparkplug' ? nodeRadius * 1.3 : nodeRadius)
      .attr('fill', 'var(--theme-surface)')
      .attr('stroke', d => d.color)
      .attr('stroke-width', 2.5)
      .attr('filter', 'url(#mantle-glow)');

    nodeGs.append('text')
      .attr('text-anchor', 'middle')
      .attr('dominant-baseline', 'middle')
      .attr('fill', d => d.color)
      .attr('font-size', compact ? '7px' : '9px')
      .attr('font-weight', '700')
      .text(d => d.abbr);

    if (!compact) {
      nodeGs.append('text')
        .attr('text-anchor', 'middle')
        .attr('y', nodeRadius + 14)
        .attr('fill', 'var(--theme-text-muted)')
        .attr('font-size', '8px')
        .attr('opacity', 0.8)
        .text(d => d.label);
    }
  }

  $effect(() => {
    compact;
    render();
  });

  onMount(() => {
    resizeObserver = new ResizeObserver(() => render());
    resizeObserver.observe(container);
  });

  onDestroy(() => {
    resizeObserver?.disconnect();
  });
</script>

<div class="diagram-container" class:compact bind:this={container}></div>

<style lang="scss">
  .diagram-container {
    width: 100%;
    aspect-ratio: 2 / 1;
    min-height: 120px;

    &.compact {
      aspect-ratio: 2.2 / 1;
    }
  }

  :global(.flow-line) {
    animation: march-flow 1.2s linear infinite;
  }

  @keyframes march-flow {
    to { stroke-dashoffset: -14; }
  }
</style>
