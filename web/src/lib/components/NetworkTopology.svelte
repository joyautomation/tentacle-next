<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { goto } from '$app/navigation';
  import * as d3 from 'd3';

  interface Service {
    serviceType: string;
    moduleId: string;
    uptime: number;
    metadata?: Record<string, unknown>;
  }

  interface Device {
    id: string;
    host: string;
    port: number;
    enabled: boolean;
  }

  interface Props {
    services: Service[];
    devices: Device[];
  }

  let { services, devices }: Props = $props();

  type NodeType = 'nats' | 'ethernetip' | 'graphql' | 'mqtt' | 'plc';

  type NodeDatum = {
    id: string;
    name: string;
    type: NodeType;
    subtitle?: string;
    enabled?: boolean;
    x?: number;
    y?: number;
    vx?: number;
    vy?: number;
    fx?: number | null;
    fy?: number | null;
  };

  type LinkDatum = d3.SimulationLinkDatum<NodeDatum> & {
    source: NodeDatum | string;
    target: NodeDatum | string;
  };

  let container: HTMLDivElement;
  let simulation: d3.Simulation<NodeDatum, LinkDatum> | null = null;

  function getNodeColor(type: NodeType): string {
    switch (type) {
      case 'nats': return 'var(--color-purple-500, #a855f7)';
      default: return 'var(--color-teal-500, #14b8a6)';
    }
  }

  function getNodeRadius(type: NodeType): number {
    switch (type) {
      case 'nats': return 50;
      case 'ethernetip':
      case 'graphql':
      case 'mqtt': return 40;
      case 'plc': return 30;
      default: return 30;
    }
  }

  function buildGraph(): { nodes: NodeDatum[]; links: LinkDatum[] } {
    const nodes: NodeDatum[] = [];
    const links: LinkDatum[] = [];

    // Central NATS node
    nodes.push({
      id: 'nats',
      name: 'NATS',
      type: 'nats',
      subtitle: 'Message Bus'
    });

    // Group services by type
    const ethernetipServices = services.filter(s => s.serviceType === 'ethernetip');
    const graphqlServices = services.filter(s => s.serviceType === 'graphql');
    const mqttServices = services.filter(s => s.serviceType === 'mqtt');

    // Add ethernetip services
    ethernetipServices.forEach((service, i) => {
      const nodeId = `ethernetip-${service.moduleId}`;
      const plcCount = (service.metadata?.plcCount as number) || 0;
      nodes.push({
        id: nodeId,
        name: 'EtherNet/IP',
        type: 'ethernetip',
        subtitle: `${plcCount} PLC${plcCount !== 1 ? 's' : ''}`
      });
      links.push({ source: 'nats', target: nodeId });
    });

    // Add graphql services
    graphqlServices.forEach((service) => {
      const nodeId = `graphql-${service.moduleId}`;
      nodes.push({
        id: nodeId,
        name: 'GraphQL',
        type: 'graphql',
        subtitle: 'API'
      });
      links.push({ source: 'nats', target: nodeId });
    });

    // Add mqtt services
    mqttServices.forEach((service) => {
      const nodeId = `mqtt-${service.moduleId}`;
      nodes.push({
        id: nodeId,
        name: 'MQTT',
        type: 'mqtt',
        subtitle: 'Bridge'
      });
      links.push({ source: 'nats', target: nodeId });
    });

    // Add PLCs (devices) - connect to first ethernetip service if exists
    const ethernetipNodeId = ethernetipServices.length > 0
      ? `ethernetip-${ethernetipServices[0].moduleId}`
      : null;

    devices.forEach((device) => {
      const nodeId = `plc-${device.id}`;
      nodes.push({
        id: nodeId,
        name: device.id,
        type: 'plc',
        subtitle: `${device.host}:${device.port}`,
        enabled: device.enabled
      });
      if (ethernetipNodeId) {
        links.push({ source: ethernetipNodeId, target: nodeId });
      }
    });

    return { nodes, links };
  }

  function handleNodeClick(node: NodeDatum) {
    switch (node.type) {
      case 'plc':
        const deviceId = node.id.replace('plc-', '');
        goto(`/devices/${deviceId}`);
        break;
      case 'ethernetip':
        goto(`/devices`);
        break;
      case 'mqtt':
        goto(`/mqtt`);
        break;
      default:
        // No navigation for nats or graphql
        break;
    }
  }

  function render() {
    if (!container) return;

    // Clear previous
    d3.select(container).selectAll('*').remove();

    const { nodes, links } = buildGraph();

    if (nodes.length === 0) return;

    const width = container.clientWidth || 800;
    const height = 500;

    const svg = d3.select(container)
      .append('svg')
      .attr('width', '100%')
      .attr('height', height)
      .attr('viewBox', `0 0 ${width} ${height}`)
      .attr('preserveAspectRatio', 'xMidYMid meet');

    // Add defs for filters and markers
    const defs = svg.append('defs');

    // Glow filter for nodes
    const filter = defs.append('filter')
      .attr('id', 'glow')
      .attr('x', '-50%')
      .attr('y', '-50%')
      .attr('width', '200%')
      .attr('height', '200%');

    filter.append('feGaussianBlur')
      .attr('stdDeviation', '3')
      .attr('result', 'coloredBlur');

    const feMerge = filter.append('feMerge');
    feMerge.append('feMergeNode').attr('in', 'coloredBlur');
    feMerge.append('feMergeNode').attr('in', 'SourceGraphic');

    // Create container group for zoom
    const g = svg.append('g');

    // Zoom behavior
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.5, 2])
      .on('zoom', (event) => {
        g.attr('transform', event.transform);
      });

    svg.call(zoom);

    // Force simulation
    simulation = d3.forceSimulation<NodeDatum>(nodes)
      .force('link', d3.forceLink<NodeDatum, LinkDatum>(links)
        .id(d => d.id)
        .distance(150)
      )
      .force('charge', d3.forceManyBody().strength(-800))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide<NodeDatum>()
        .radius(d => getNodeRadius(d.type) + 30)
      );

    // Draw links
    const link = g.append('g')
      .attr('class', 'links')
      .selectAll('line')
      .data(links)
      .join('line')
      .attr('stroke', 'var(--theme-border)')
      .attr('stroke-width', 2)
      .attr('stroke-opacity', 0.6);

    // Draw nodes
    const nodeGroups = g.append('g')
      .attr('class', 'nodes')
      .selectAll('g')
      .data(nodes)
      .join('g')
      .attr('class', 'node')
      .style('cursor', d => ['plc', 'ethernetip', 'mqtt'].includes(d.type) ? 'pointer' : 'default')
      .on('click', (event, d) => {
        event.stopPropagation();
        handleNodeClick(d);
      });

    // Node circles
    nodeGroups.append('circle')
      .attr('r', d => getNodeRadius(d.type))
      .attr('fill', 'var(--theme-surface)')
      .attr('stroke', d => getNodeColor(d.type))
      .attr('stroke-width', 3)
      .attr('filter', 'url(#glow)')
      .attr('opacity', d => d.enabled === false ? 0.5 : 1);

    // Node icons (simple text for now)
    nodeGroups.append('text')
      .attr('text-anchor', 'middle')
      .attr('dominant-baseline', 'middle')
      .attr('fill', d => getNodeColor(d.type))
      .attr('font-size', d => d.type === 'nats' ? '12px' : '10px')
      .attr('font-weight', '600')
      .text(d => {
        switch (d.type) {
          case 'nats': return 'NATS';
          case 'ethernetip': return 'EIP';
          case 'graphql': return 'GQL';
          case 'mqtt': return 'MQTT';
          case 'plc': return 'PLC';
          default: return '';
        }
      });

    // Node labels (below)
    nodeGroups.append('text')
      .attr('text-anchor', 'middle')
      .attr('y', d => getNodeRadius(d.type) + 16)
      .attr('fill', 'var(--theme-text)')
      .attr('font-size', '12px')
      .attr('font-weight', '500')
      .text(d => d.name);

    // Node subtitles
    nodeGroups.append('text')
      .attr('text-anchor', 'middle')
      .attr('y', d => getNodeRadius(d.type) + 30)
      .attr('fill', 'var(--theme-text-muted)')
      .attr('font-size', '10px')
      .text(d => d.subtitle || '');

    // Drag behavior
    const drag = d3.drag<SVGGElement, NodeDatum>()
      .on('start', (event, d) => {
        if (!event.active) simulation?.alphaTarget(0.3).restart();
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

    nodeGroups.call(drag);

    // Update positions on tick
    simulation.on('tick', () => {
      link
        .attr('x1', d => (d.source as NodeDatum).x ?? 0)
        .attr('y1', d => (d.source as NodeDatum).y ?? 0)
        .attr('x2', d => (d.target as NodeDatum).x ?? 0)
        .attr('y2', d => (d.target as NodeDatum).y ?? 0);

      nodeGroups.attr('transform', d => `translate(${d.x ?? 0},${d.y ?? 0})`);
    });
  }

  // Re-render when data changes
  $effect(() => {
    // Track dependencies
    services;
    devices;

    if (container) {
      render();
    }
  });

  onMount(() => {
    render();

    // Handle resize
    const resizeObserver = new ResizeObserver(() => {
      render();
    });
    resizeObserver.observe(container);

    return () => {
      resizeObserver.disconnect();
    };
  });

  onDestroy(() => {
    simulation?.stop();
  });
</script>

{#if services.length === 0}
  <div class="topology-empty">
    <div class="empty-icon">
      <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
        <circle cx="12" cy="12" r="10"/>
        <path d="M12 8v4M12 16h.01"/>
      </svg>
    </div>
    <h3>No Services Connected</h3>
    <p>Start a tentacle service to see the network topology.</p>
  </div>
{:else}
  <div class="topology-container" bind:this={container}></div>
{/if}

<style lang="scss">
  .topology-empty {
    width: 100%;
    min-height: 300px;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-xl);
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1rem;
    padding: 2rem;
    text-align: center;

    .empty-icon {
      color: var(--theme-text-muted);
      opacity: 0.5;
    }

    h3 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }

    p {
      font-size: 0.875rem;
      color: var(--theme-text-muted);
      margin: 0;
    }
  }
  .topology-container {
    width: 100%;
    min-height: 500px;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-xl);
    overflow: hidden;

    :global(svg) {
      display: block;
    }

    :global(.node:hover circle) {
      stroke-width: 4;
    }

    :global(.node) {
      transition: opacity 0.2s;
    }
  }
</style>
