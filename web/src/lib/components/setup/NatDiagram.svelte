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
      { id: 'lan-1', label: 'LAN Device', abbr: 'LAN', layer: 0, row: 0.5, color: 'var(--color-amber-500, #f59e0b)' },
      { id: 'lan-2', label: 'LAN Device', abbr: 'LAN', layer: 0, row: 2, color: 'var(--color-amber-500, #f59e0b)' },
      { id: 'lan-3', label: 'LAN Device', abbr: 'LAN', layer: 0, row: 3.5, color: 'var(--color-amber-500, #f59e0b)' },
      { id: 'network', label: 'Network Mgr', abbr: 'NET', layer: 1, row: 1.25, color: 'var(--color-teal-500, #14b8a6)' },
      { id: 'nftables', label: 'Firewall', abbr: 'NFT', layer: 1, row: 2.75, color: 'var(--color-teal-500, #14b8a6)' },
      { id: 'tentacle', label: 'Tentacle', abbr: 'NATS', layer: 2, row: 2, color: 'var(--color-purple-500, #a855f7)' },
      { id: 'wan', label: 'WAN / Internet', abbr: 'WAN', layer: 3, row: 2, color: 'var(--color-sky-500, #0ea5e9)' },
    ];

    const links: DiagramLink[] = [
      { source: 'lan-1', target: 'network' },
      { source: 'lan-2', target: 'nftables' },
      { source: 'lan-3', target: 'nftables' },
      { source: 'network', target: 'tentacle' },
      { source: 'nftables', target: 'tentacle' },
      { source: 'tentacle', target: 'wan' },
    ];

    return { nodes, links };
  }

  function render() {
    if (!container) return;

    d3.select(container).selectAll('*').remove();

    const { nodes, links } = buildDiagram();

    const width = container.clientWidth || 400;
    const height = container.clientHeight || 200;

    const svg = d3.select(container)
      .append('svg')
      .attr('width', '100%')
      .attr('height', '100%')
      .attr('viewBox', `0 0 ${width} ${height}`)
      .attr('preserveAspectRatio', 'xMidYMid meet');

    // Glow filter
    const defs = svg.append('defs');
    const filter = defs.append('filter')
      .attr('id', 'nat-glow')
      .attr('x', '-50%').attr('y', '-50%')
      .attr('width', '200%').attr('height', '200%');
    filter.append('feGaussianBlur')
      .attr('stdDeviation', '2.5')
      .attr('result', 'coloredBlur');
    const merge = filter.append('feMerge');
    merge.append('feMergeNode').attr('in', 'coloredBlur');
    merge.append('feMergeNode').attr('in', 'SourceGraphic');

    const g = svg.append('g');

    const padX = compact ? 35 : 50;
    const padY = compact ? 20 : 30;
    const layerCount = 4;
    const maxRows = 4;
    const layerSpacing = (width - padX * 2) / (layerCount - 1);
    const rowSpacing = (height - padY * 2) / (maxRows - 1);

    function nodeX(n: DiagramNode): number {
      return padX + n.layer * layerSpacing;
    }
    function nodeY(n: DiagramNode): number {
      return padY + n.row * rowSpacing;
    }

    const nodeRadius = compact ? 15 : 20;

    // Links
    const linkGroup = g.append('g');
    linkGroup.selectAll('line.base')
      .data(links)
      .join('line')
      .attr('x1', d => nodeX(nodes.find(n => n.id === d.source)!))
      .attr('y1', d => nodeY(nodes.find(n => n.id === d.source)!))
      .attr('x2', d => nodeX(nodes.find(n => n.id === d.target)!))
      .attr('y2', d => nodeY(nodes.find(n => n.id === d.target)!))
      .attr('stroke', 'var(--theme-border)')
      .attr('stroke-width', 1.5)
      .attr('opacity', 0.5);

    // Animated flow
    const flowGroup = g.append('g');
    flowGroup.selectAll('line.flow')
      .data(links)
      .join('line')
      .attr('class', 'flow-line')
      .attr('x1', d => nodeX(nodes.find(n => n.id === d.source)!))
      .attr('y1', d => nodeY(nodes.find(n => n.id === d.source)!))
      .attr('x2', d => nodeX(nodes.find(n => n.id === d.target)!))
      .attr('y2', d => nodeY(nodes.find(n => n.id === d.target)!))
      .attr('stroke', 'var(--color-sky-400, #38bdf8)')
      .attr('stroke-width', 2)
      .attr('stroke-opacity', 0.7)
      .attr('stroke-dasharray', '6 8');

    // Nodes
    const nodeGroup = g.append('g');
    const nodeGs = nodeGroup.selectAll('g')
      .data(nodes)
      .join('g')
      .attr('transform', d => `translate(${nodeX(d)}, ${nodeY(d)})`);

    nodeGs.append('circle')
      .attr('r', d => d.id === 'tentacle' ? nodeRadius * 1.3 : nodeRadius)
      .attr('fill', 'var(--theme-surface)')
      .attr('stroke', d => d.color)
      .attr('stroke-width', 2.5)
      .attr('filter', 'url(#nat-glow)');

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
