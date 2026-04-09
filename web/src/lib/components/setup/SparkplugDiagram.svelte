<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import * as d3 from 'd3';

  interface Props {
    /** Which scanner protocols to highlight as active (empty = show all dimmed) */
    activeProtocols?: Set<string>;
    /** Compact mode for card preview vs expanded for detail view */
    compact?: boolean;
  }

  let { activeProtocols = new Set(), compact = true }: Props = $props();

  let container: HTMLDivElement;
  let resizeObserver: ResizeObserver | null = null;

  type DiagramNode = {
    id: string;
    label: string;
    abbr: string;
    layer: number; // 0=devices, 1=scanners, 2=bus, 3=gateway, 4=mqtt, 5=broker
    row: number;   // vertical position within layer
    color: string;
    protocol?: string; // for scanner nodes, which protocol they represent
  };

  type DiagramLink = {
    source: string;
    target: string;
    active: boolean;
  };

  function buildDiagram(): { nodes: DiagramNode[]; links: DiagramLink[] } {
    const protocols = [
      { id: 'ethernetip', label: 'EtherNet/IP', abbr: 'EIP', color: 'var(--color-teal-500, #14b8a6)' },
      { id: 'opcua', label: 'OPC UA', abbr: 'OPC', color: 'var(--color-teal-500, #14b8a6)' },
      { id: 'modbus', label: 'Modbus', abbr: 'MOD', color: 'var(--color-teal-500, #14b8a6)' },
      { id: 'snmp', label: 'SNMP', abbr: 'SNMP', color: 'var(--color-teal-500, #14b8a6)' },
    ];

    const nodes: DiagramNode[] = [];
    const links: DiagramLink[] = [];

    // Device nodes (layer 0)
    protocols.forEach((p, i) => {
      nodes.push({
        id: `device-${p.id}`,
        label: 'Device',
        abbr: 'DEV',
        layer: 0,
        row: i,
        color: 'var(--color-amber-500, #f59e0b)',
        protocol: p.id,
      });
    });

    // Scanner nodes (layer 1)
    protocols.forEach((p, i) => {
      nodes.push({
        id: `scanner-${p.id}`,
        label: p.label,
        abbr: p.abbr,
        layer: 1,
        row: i,
        color: p.color,
        protocol: p.id,
      });
    });

    // NATS bus (layer 2)
    nodes.push({
      id: 'nats',
      label: 'NATS Bus',
      abbr: 'NATS',
      layer: 2,
      row: 1.5,
      color: 'var(--color-purple-500, #a855f7)',
    });

    // Gateway (layer 3)
    nodes.push({
      id: 'gateway',
      label: 'Gateway',
      abbr: 'GW',
      layer: 3,
      row: 1.5,
      color: 'var(--color-teal-500, #14b8a6)',
    });

    // MQTT Bridge (layer 4)
    nodes.push({
      id: 'mqtt',
      label: 'MQTT Bridge',
      abbr: 'MQTT',
      layer: 4,
      row: 1.5,
      color: 'var(--color-teal-500, #14b8a6)',
    });

    // Broker (layer 5)
    nodes.push({
      id: 'broker',
      label: 'MQTT Broker',
      abbr: 'BRK',
      layer: 5,
      row: 1.5,
      color: 'var(--color-sky-500, #0ea5e9)',
    });

    // Links: device → scanner → nats → gateway → mqtt → broker
    const showAll = activeProtocols.size === 0;
    protocols.forEach((p) => {
      const active = showAll || activeProtocols.has(p.id);
      links.push({ source: `device-${p.id}`, target: `scanner-${p.id}`, active });
      links.push({ source: `scanner-${p.id}`, target: 'nats', active });
    });

    links.push({ source: 'nats', target: 'gateway', active: true });
    links.push({ source: 'gateway', target: 'mqtt', active: true });
    links.push({ source: 'mqtt', target: 'broker', active: true });

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

    // Glow filter
    const defs = svg.append('defs');
    const filter = defs.append('filter')
      .attr('id', 'diagram-glow')
      .attr('x', '-50%').attr('y', '-50%')
      .attr('width', '200%').attr('height', '200%');
    filter.append('feGaussianBlur')
      .attr('stdDeviation', '2.5')
      .attr('result', 'coloredBlur');
    const merge = filter.append('feMerge');
    merge.append('feMergeNode').attr('in', 'coloredBlur');
    merge.append('feMergeNode').attr('in', 'SourceGraphic');

    const g = svg.append('g');

    // Layout: distribute layers across width, rows across height
    const padX = compact ? 40 : 60;
    const padY = compact ? 30 : 40;
    const layerCount = 6;
    const maxRows = 4;
    const layerSpacing = (width - padX * 2) / (layerCount - 1);
    const rowSpacing = (height - padY * 2) / (maxRows - 1);

    function nodeX(n: DiagramNode): number {
      return padX + n.layer * layerSpacing;
    }
    function nodeY(n: DiagramNode): number {
      return padY + n.row * rowSpacing;
    }

    const nodeRadius = compact ? 16 : 22;

    function isNodeActive(n: DiagramNode): boolean {
      if (!n.protocol) return true;
      return activeProtocols.size === 0 || activeProtocols.has(n.protocol);
    }

    // Draw links
    const linkGroup = g.append('g').attr('class', 'links');

    // Static base lines
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
      .attr('stroke-dasharray', d => d.active ? 'none' : '4 4')
      .attr('opacity', d => d.active ? 0.5 : 0.2);

    // Animated flow overlay
    const flowGroup = g.append('g').attr('class', 'flow-overlay');
    flowGroup.selectAll('line.flow')
      .data(links.filter(l => l.active))
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

    // Draw nodes
    const nodeGroup = g.append('g').attr('class', 'nodes');
    const nodeGs = nodeGroup.selectAll('g')
      .data(nodes)
      .join('g')
      .attr('transform', d => `translate(${nodeX(d)}, ${nodeY(d)})`);

    nodeGs.append('circle')
      .attr('r', d => d.id === 'nats' ? nodeRadius * 1.3 : nodeRadius)
      .attr('fill', 'var(--theme-surface)')
      .attr('stroke', d => isNodeActive(d) ? d.color : 'var(--theme-text-muted)')
      .attr('stroke-width', 2.5)
      .attr('stroke-dasharray', d => isNodeActive(d) ? 'none' : '4 3')
      .attr('opacity', d => isNodeActive(d) ? 1 : 0.35)
      .attr('filter', d => isNodeActive(d) ? 'url(#diagram-glow)' : 'none');

    nodeGs.append('text')
      .attr('text-anchor', 'middle')
      .attr('dominant-baseline', 'middle')
      .attr('fill', d => isNodeActive(d) ? d.color : 'var(--theme-text-muted)')
      .attr('font-size', compact ? '7px' : '9px')
      .attr('font-weight', '700')
      .attr('opacity', d => isNodeActive(d) ? 1 : 0.35)
      .text(d => d.abbr);

    // Labels below nodes (non-compact only)
    if (!compact) {
      nodeGs.append('text')
        .attr('text-anchor', 'middle')
        .attr('y', nodeRadius + 14)
        .attr('fill', d => isNodeActive(d) ? 'var(--theme-text-muted)' : 'var(--theme-text-muted)')
        .attr('font-size', '8px')
        .attr('opacity', d => isNodeActive(d) ? 0.8 : 0.3)
        .text(d => d.label);
    }
  }

  $effect(() => {
    // Re-render when activeProtocols changes
    activeProtocols;
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
