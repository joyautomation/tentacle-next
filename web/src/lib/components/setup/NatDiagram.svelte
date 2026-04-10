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
      // Private side (left)
      { id: 'priv-1', label: '192.168.1.10', abbr: '.10', layer: 0, row: 0.5, color: 'var(--color-amber-500, #f59e0b)' },
      { id: 'priv-2', label: '192.168.1.20', abbr: '.20', layer: 0, row: 2, color: 'var(--color-amber-500, #f59e0b)' },
      { id: 'priv-3', label: '192.168.1.30', abbr: '.30', layer: 0, row: 3.5, color: 'var(--color-amber-500, #f59e0b)' },
      // NAT (center)
      { id: 'nat', label: 'NAT', abbr: 'NAT', layer: 1, row: 2, color: 'var(--color-purple-500, #a855f7)' },
      // Public side (right)
      { id: 'pub-1', label: '10.0.0.50', abbr: '.50', layer: 2, row: 0.5, color: 'var(--color-sky-500, #0ea5e9)' },
      { id: 'pub-2', label: '10.0.0.60', abbr: '.60', layer: 2, row: 2, color: 'var(--color-sky-500, #0ea5e9)' },
      { id: 'pub-3', label: '10.0.0.70', abbr: '.70', layer: 2, row: 3.5, color: 'var(--color-sky-500, #0ea5e9)' },
    ];

    const links: DiagramLink[] = [
      { source: 'priv-1', target: 'nat' },
      { source: 'priv-2', target: 'nat' },
      { source: 'priv-3', target: 'nat' },
      { source: 'nat', target: 'pub-1' },
      { source: 'nat', target: 'pub-2' },
      { source: 'nat', target: 'pub-3' },
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
    const layerCount = 3;
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
      .attr('r', d => d.id === 'nat' ? nodeRadius * 1.4 : nodeRadius)
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
