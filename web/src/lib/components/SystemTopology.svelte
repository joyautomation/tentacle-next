<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { goto } from '$app/navigation';
  import * as d3 from 'd3';
  import { getServiceName } from '$lib/constants/services';

  interface Service {
    serviceType: string;
    moduleId: string;
    startedAt: number;
    version: string | null;
    metadata: Record<string, unknown> | null;
    enabled?: boolean;
  }

  interface Props {
    services: Service[];
    apiConnected: boolean;
    monolith?: boolean;
    /** Compact mode: smaller nodes, no subtitles, tighter labels. For embeds. */
    compact?: boolean;
  }

  let { services, apiConnected, monolith = false, compact = false }: Props = $props();

  type NodeType = 'nats' | 'bus' | 'api' | 'web' | 'caddy' | 'ethernetip' | 'gitserver' | 'history' | 'database' | 'mqtt' | 'plc' | 'network' | 'nftables' | 'snmp' | 'device' | 'orchestrator';

  type NodeDatum = {
    id: string;
    name: string;
    type: NodeType;
    subtitle?: string;
    depth: number;
    connected: boolean;
    enabled: boolean;
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
    /** Whether data is actively flowing on this link */
    active?: boolean;
    /** 1 = source→target, -1 = target→source, 0 = bidirectional */
    flowDirection?: 1 | -1 | 0;
  };

  let container: HTMLDivElement;
  let simulation: d3.Simulation<NodeDatum, LinkDatum> | null = null;
  let svgSelection: d3.Selection<SVGSVGElement, unknown, null, undefined> | null = null;
  let currentNodes: NodeDatum[] = [];

  function getNodeColor(type: NodeType): string {
    switch (type) {
      case 'nats':
      case 'bus': return 'var(--purple-500, #a855f7)';
      case 'device': return 'var(--orange-500, #f97316)';
      case 'database': return 'var(--sky-500, #0ea5e9)';
      default: return 'var(--teal-500, #14b8a6)';
    }
  }

  function getNodeRadius(type: NodeType): number {
    const base = (() => {
      switch (type) {
        case 'nats':
        case 'bus': return 50;
        case 'api':
        case 'web': return 40;
        case 'caddy':
        case 'ethernetip':
        case 'ethernetip-server':
        case 'gateway':
        case 'mqtt':
        case 'plc':
        case 'snmp': return 35;
        case 'database': return 28;
        case 'device': return 25;
        default: return 30;
      }
    })();
    return compact ? Math.round(base * 0.55) : base;
  }

  /** Parse a numeric metadata field that may arrive as number or string. */
  function metaNum(v: unknown): number {
    if (typeof v === 'number') return v;
    if (typeof v === 'string') {
      const n = Number(v);
      return Number.isFinite(n) ? n : 0;
    }
    return 0;
  }

  function formatUptime(startedAt: number): string {
    const seconds = Math.floor((Date.now() - startedAt) / 1000);
    if (seconds < 0 || !isFinite(seconds)) return '';
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
    return `${Math.floor(seconds / 86400)}d`;
  }

  function buildGraph(): { nodes: NodeDatum[]; links: LinkDatum[] } {
    const nodes: NodeDatum[] = [];
    const links: LinkDatum[] = [];

    // Central hub — "Bus" in monolith mode, "NATS" in distributed mode
    const hubType: NodeType = monolith ? 'bus' : 'nats';
    nodes.push({
      id: 'nats',
      name: monolith ? 'Bus' : 'NATS',
      type: hubType,
      subtitle: apiConnected ? 'Message Bus' : 'Disconnected',
      connected: apiConnected,
      enabled: true,
      depth: 0
    });

    // Orchestrator fallback — the orchestrator publishes its own heartbeat like
    // every other module, so it normally renders through the services loop below.
    // But if its heartbeat is missing (stale KV, bus desync), still show it as a
    // static node so the topology isn't lying about what's running.
    const orchestratorService = services.find(s => s.serviceType === 'orchestrator');
    if (!orchestratorService) {
      nodes.push({
        id: 'orchestrator',
        name: 'Orchestrator',
        type: 'orchestrator',
        subtitle: apiConnected ? 'Reconcile Loop' : 'Disconnected',
        connected: apiConnected,
        enabled: true,
        depth: 1,
      });
      links.push({ source: 'nats', target: 'orchestrator' });
    }

    // Identify key services for topology wiring
    const apiService = services.find(s => s.serviceType === 'api');
    const caddyService = services.find(s => s.serviceType === 'caddy');

    // Additional discovered services from heartbeats
    const knownStaticTypes = new Set<string>();
    services
      .filter(s => !knownStaticTypes.has(s.serviceType))
      .forEach(service => {
        const nodeId = `${service.serviceType}-${service.moduleId}`;
        // Skip if we already have this node
        if (nodes.some(n => n.id === nodeId)) return;

        const name = getServiceName(service.serviceType);

        const serviceEnabled = service.enabled !== false;
        // MQTT: show broker disconnection warning
        const mqttDisconnected = service.serviceType === 'mqtt' &&
          serviceEnabled &&
          service.metadata?.connected === false;

        // Caddy sits outside UI — depth 3, linked to UI node (not Bus)
        const isCaddy = service.serviceType === 'caddy';
        const depth = isCaddy ? 3 : 1;

        nodes.push({
          id: nodeId,
          name,
          type: service.serviceType as NodeType,
          subtitle: !serviceEnabled ? 'Disabled' : mqttDisconnected ? 'Broker disconnected' : `Up ${formatUptime(service.startedAt)}`,
          connected: !mqttDisconnected,
          enabled: serviceEnabled,
          depth,
        });

        if (isCaddy) {
          // Caddy links to UI (it's the outermost layer)
          // Link is added after the UI node is created below
        } else {
          links.push({ source: 'nats', target: nodeId });
        }

        // Add device nodes downstream of EtherNet/IP
        if (service.serviceType === 'ethernetip' && service.metadata?.devices) {
          try {
            const devicesStr = service.metadata.devices as string;
            const devices: Array<{ deviceId: string; host: string; port: number; tagCount: number }> =
              typeof devicesStr === 'string' ? JSON.parse(devicesStr) : devicesStr;
            for (const device of devices) {
              const deviceNodeId = `device-${device.deviceId}`;
              if (!nodes.some(n => n.id === deviceNodeId)) {
                nodes.push({
                  id: deviceNodeId,
                  name: device.deviceId,
                  type: 'device',
                  subtitle: `${device.host}:${device.port} (${device.tagCount} tags)`,
                  connected: true,
                  enabled: serviceEnabled,
                  depth: 2
                });
                links.push({ source: nodeId, target: deviceNodeId });
              }
            }
          } catch { /* ignore malformed devices metadata */ }
        }

        // Add a database node downstream of history — shown whenever history is up.
        // Active-flow detection below decides whether the edges animate.
        if (service.serviceType === 'history') {
          const dbNodeId = `history-db-${service.moduleId}`;
          if (!nodes.some(n => n.id === dbNodeId)) {
            const lastFlush = metaNum(service.metadata?.lastFlushTime);
            const flushedRecently = lastFlush > 0 && Date.now() - lastFlush < 60_000;
            nodes.push({
              id: dbNodeId,
              name: 'History DB',
              type: 'database',
              subtitle: flushedRecently ? 'Writing' : (lastFlush > 0 ? 'Idle' : 'No writes yet'),
              connected: serviceEnabled,
              enabled: serviceEnabled,
              depth: 2,
            });
            links.push({ source: nodeId, target: dbNodeId });
          }
        }

        // Add device nodes downstream of SNMP
        if (service.serviceType === 'snmp' && service.metadata?.devices) {
          try {
            const devicesStr = service.metadata.devices as string;
            const devices: Array<{ deviceId: string; host: string; port: number; oidCount: number }> =
              typeof devicesStr === 'string' ? JSON.parse(devicesStr) : devicesStr;
            for (const device of devices) {
              const deviceNodeId = `device-${device.deviceId}`;
              if (!nodes.some(n => n.id === deviceNodeId)) {
                nodes.push({
                  id: deviceNodeId,
                  name: device.deviceId,
                  type: 'device',
                  subtitle: `${device.host}:${device.port} (${device.oidCount} OIDs)`,
                  connected: true,
                  enabled: serviceEnabled,
                  depth: 2
                });
                links.push({ source: nodeId, target: deviceNodeId });
              }
            }
          } catch { /* ignore malformed devices metadata */ }
        }
      });

    // UI node — always present (we're rendering it)
    // Chain: Bus → API → UI → Caddy (when caddy is running)
    const webParent = apiService ? `api-${apiService.moduleId}` : 'nats';
    nodes.push({
      id: 'web',
      name: 'UI',
      type: 'web',
      subtitle: 'This App',
      connected: true,
      enabled: true,
      depth: apiService ? 2 : 1
    });
    links.push({ source: webParent, target: 'web' });

    // Caddy links to UI (outermost layer in the proxy chain)
    const caddyNode = caddyService ? nodes.find(n => n.type === 'caddy') : null;
    if (caddyNode) {
      links.push({ source: 'web', target: caddyNode.id });
    }

    // Build a lookup of history services so we can check last-flush activity per link.
    const historyServiceByNodeId = new Map<string, Service>();
    for (const s of services) {
      if (s.serviceType === 'history') {
        historyServiceByNodeId.set(`history-${s.moduleId}`, s);
      }
    }
    const historyIsFlowing = (svc: Service | undefined) => {
      if (!svc) return false;
      const lastFlush = metaNum(svc.metadata?.lastFlushTime);
      return lastFlush > 0 && Date.now() - lastFlush < 60_000;
    };

    // Mark data-flow links as active and set flow direction
    // EtherNet/IP: data flows from device → EIP → NATS (inbound to NATS)
    // MQTT: data flows from NATS → MQTT (outbound from NATS)
    const dataFlowTypes = new Set<NodeType>(['ethernetip', 'gateway', 'mqtt', 'snmp', 'plc', 'device']);
    for (const l of links) {
      const srcId = typeof l.source === 'string' ? l.source : (l.source as NodeDatum).id;
      const tgtId = typeof l.target === 'string' ? l.target : (l.target as NodeDatum).id;
      const src = nodes.find(n => n.id === srcId);
      const tgt = nodes.find(n => n.id === tgtId);
      if (!src || !tgt) continue;

      // History edges (bus→history and history→database): animate only when the
      // module has actually flushed a batch to the DB recently.
      if (src.type === 'history' || tgt.type === 'history' || src.type === 'database' || tgt.type === 'database') {
        const historyNodeId = src.type === 'history' ? src.id : (tgt.type === 'history' ? tgt.id : null);
        const svc = historyNodeId ? historyServiceByNodeId.get(historyNodeId) : undefined;
        if (src.connected && tgt.connected && src.enabled && tgt.enabled && historyIsFlowing(svc)) {
          l.active = true;
          // bus→history→database all flow source→target
          l.flowDirection = 1;
        }
        continue;
      }

      if (src.connected && tgt.connected && src.enabled && tgt.enabled &&
          (dataFlowTypes.has(src.type) || dataFlowTypes.has(tgt.type))) {
        l.active = true;
        // Determine flow direction based on service types
        if (tgt.type === 'device' || src.type === 'device') {
          // device → scanner: flow from device toward NATS
          l.flowDirection = -1;
        } else if (tgt.type === 'plc' || src.type === 'plc' || tgt.type === 'gateway' || src.type === 'gateway') {
          // PLC/Gateway ↔ NATS: bidirectional (reads from scanners, writes commands back)
          l.flowDirection = 0;
        } else if (tgt.type === 'ethernetip' || src.type === 'ethernetip') {
          // EIP → NATS: inbound
          l.flowDirection = -1;
        } else if (tgt.type === 'snmp' || src.type === 'snmp') {
          // SNMP → NATS: inbound
          l.flowDirection = -1;
        } else if (tgt.type === 'mqtt' || src.type === 'mqtt') {
          // NATS → MQTT: outbound
          l.flowDirection = 1;
        } else {
          l.flowDirection = 1;
        }
      }
    }

    return { nodes, links };
  }

  function handleResize() {
    if (!container || !svgSelection || !simulation) return;

    const width = container.clientWidth || 800;
    const height = container.clientHeight || 600;
    const layerRadius = Math.max(Math.min(width, height) * 0.2, 120);

    // Update viewBox without destroying anything
    svgSelection.attr('viewBox', `0 0 ${width} ${height}`);

    // Re-center NATS pin
    const natsNode = currentNodes.find(n => n.id === 'nats');
    if (natsNode) {
      natsNode.x = width / 2;
      natsNode.y = height / 2;
      natsNode.fx = width / 2;
      natsNode.fy = height / 2;
    }

    // Update radial force center and radius, then gently reheat
    simulation
      .force('radial', d3.forceRadial<NodeDatum>(
        d => d.depth * layerRadius,
        width / 2,
        height / 2
      ).strength(1.5))
      .alpha(0.3)
      .restart();
  }

  function render() {
    if (!container) return;

    // Stop previous simulation
    simulation?.stop();

    // Clear previous
    d3.select(container).selectAll('*').remove();
    svgSelection = null;

    const { nodes, links } = buildGraph();
    if (nodes.length === 0) return;

    currentNodes = nodes;

    const width = container.clientWidth || 800;
    const height = container.clientHeight || 600;

    const svg = d3.select(container)
      .append('svg')
      .attr('width', '100%')
      .attr('height', '100%')
      .attr('viewBox', `0 0 ${width} ${height}`)
      .attr('preserveAspectRatio', 'xMidYMid meet');

    svgSelection = svg;

    // Defs for glow filter
    const defs = svg.append('defs');

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

    // Container group for zoom
    const g = svg.append('g');

    // Zoom behavior — wider range for small screens
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.2, 3])
      .on('zoom', (event) => {
        g.attr('transform', event.transform);
      });

    svg.call(zoom);

    // Auto-fit helper — zooms to fit all nodes with padding
    const zoomToFit = (animate = true) => {
      if (nodes.length === 0) return;
      let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
      for (const n of nodes) {
        const r = getNodeRadius(n.type) + 40;
        if ((n.x ?? 0) - r < x0) x0 = (n.x ?? 0) - r;
        if ((n.y ?? 0) - r < y0) y0 = (n.y ?? 0) - r;
        if ((n.x ?? 0) + r > x1) x1 = (n.x ?? 0) + r;
        if ((n.y ?? 0) + r > y1) y1 = (n.y ?? 0) + r;
      }
      const padding = 80;
      const bw = x1 - x0 + padding * 2;
      const bh = y1 - y0 + padding * 2;
      const scale = Math.min(1.0, Math.min(width / bw, height / bh));
      const tx = width / 2 - scale * ((x0 + x1) / 2);
      const ty = height / 2 - scale * ((y0 + y1) / 2);
      const transform = d3.zoomIdentity.translate(tx, ty).scale(scale);
      if (animate) {
        svg.transition().duration(750).call(zoom.transform, transform);
      } else {
        svg.call(zoom.transform, transform);
      }
    };

    const cx = width / 2;
    const cy = height / 2;

    // Pin NATS to center
    const natsNode = nodes.find(n => n.id === 'nats');
    if (natsNode) {
      natsNode.x = cx;
      natsNode.y = cy;
      natsNode.fx = cx;
      natsNode.fy = cy;
    }

    // Seed initial positions so connected nodes start in the same quadrant
    const layer1 = nodes.filter(n => n.depth === 1);
    const angleStep = (2 * Math.PI) / Math.max(layer1.length, 1);
    layer1.forEach((n, i) => {
      const angle = angleStep * i - Math.PI / 2;
      n.x = cx + Math.cos(angle) * 120;
      n.y = cy + Math.sin(angle) * 120;
    });
    // Place depth-2 nodes near their parent (same angle, further out)
    nodes.filter(n => n.depth === 2).forEach(n => {
      const parentLink = links.find(l => {
        const targetId = typeof l.target === 'string' ? l.target : (l.target as NodeDatum).id;
        return targetId === n.id;
      });
      if (parentLink) {
        const sourceId = typeof parentLink.source === 'string' ? parentLink.source : (parentLink.source as NodeDatum).id;
        const parent = nodes.find(p => p.id === sourceId);
        if (parent?.x != null && parent?.y != null) {
          const angle = Math.atan2(parent.y - cy, parent.x - cx);
          n.x = cx + Math.cos(angle) * 200;
          n.y = cy + Math.sin(angle) * 200;
        }
      }
    });

    // Radial distance per depth layer — floor prevents cramping on small screens
    const layerRadius = Math.max(Math.min(width, height) * 0.2, 120);

    // Force simulation with radial star layout (mantle-style parameters)
    simulation = d3.forceSimulation<NodeDatum>(nodes)
      .force('link', d3.forceLink<NodeDatum, LinkDatum>(links)
        .id(d => d.id)
        .distance(layerRadius * 0.9)
        .strength(0.6)
      )
      .force('charge', d3.forceManyBody().strength(-600))
      .force('radial', d3.forceRadial<NodeDatum>(
        d => d.depth * layerRadius,
        cx,
        cy
      ).strength(1.2))
      .force('collision', d3.forceCollide<NodeDatum>()
        .radius(d => getNodeRadius(d.type) + 30)
      );

    // Pre-settle the simulation (150 ticks) for a stable initial layout
    simulation.stop();
    for (let i = 0; i < 150; i++) simulation.tick();

    // Draw links
    const link = g.append('g')
      .attr('class', 'links')
      .selectAll('line')
      .data(links)
      .join('line')
      .attr('stroke', 'var(--theme-border)')
      .attr('stroke-width', 2)
      .attr('stroke-opacity', d => {
        const targetNode = nodes.find(n => n.id === (typeof d.target === 'string' ? d.target : (d.target as NodeDatum).id));
        const sourceNode = nodes.find(n => n.id === (typeof d.source === 'string' ? d.source : (d.source as NodeDatum).id));
        if (!targetNode?.connected || !sourceNode?.connected) return 0.2;
        if (!targetNode?.enabled || !sourceNode?.enabled) return 0.3;
        return 0.6;
      })
      .attr('stroke-dasharray', d => {
        const targetNode = nodes.find(n => n.id === (typeof d.target === 'string' ? d.target : (d.target as NodeDatum).id));
        const sourceNode = nodes.find(n => n.id === (typeof d.source === 'string' ? d.source : (d.source as NodeDatum).id));
        if (!targetNode?.connected || !sourceNode?.connected) return '4 4';
        if (!targetNode?.enabled || !sourceNode?.enabled) return '4 4';
        return 'none';
      });

    // Animated marching-dash overlay on active data-flow links (drawn before nodes so nodes cover them)
    const activeLinks = links.filter(l => l.active);
    // Unidirectional links: single animated line
    const uniLinks = activeLinks.filter(l => l.flowDirection !== 0);
    const flowOverlay = g.append('g')
      .attr('class', 'flow-overlay')
      .selectAll('line.flow-uni')
      .data(uniLinks)
      .join('line')
      .attr('class', d => d.flowDirection === -1 ? 'flow-line flow-reverse' : 'flow-line')
      .attr('stroke', d => {
        const tgtId = typeof d.target === 'string' ? d.target : (d.target as NodeDatum).id;
        const srcId = typeof d.source === 'string' ? d.source : (d.source as NodeDatum).id;
        const tgt = nodes.find(n => n.id === tgtId);
        const src = nodes.find(n => n.id === srcId);
        if (tgt?.type === 'mqtt' || src?.type === 'mqtt') return 'var(--amber-400, #fbbf24)';
        if (tgt?.type === 'history' || src?.type === 'history' || tgt?.type === 'database' || src?.type === 'database') {
          return 'var(--orange-500, #f97316)';
        }
        return 'var(--sky-400, #38bdf8)';
      })
      .attr('stroke-width', 2)
      .attr('stroke-opacity', 0.7)
      .attr('stroke-dasharray', '6 8');

    // Bidirectional links: two parallel animated lines offset from each other
    const biLinks = activeLinks.filter(l => l.flowDirection === 0);
    const flowBiForward = g.append('g')
      .attr('class', 'flow-overlay-bi')
      .selectAll('line.flow-bi-fwd')
      .data(biLinks)
      .join('line')
      .attr('class', 'flow-line flow-bi-fwd')
      .attr('stroke', 'var(--sky-400, #38bdf8)')
      .attr('stroke-width', 1.5)
      .attr('stroke-opacity', 0.6)
      .attr('stroke-dasharray', '5 7');

    const flowBiReverse = g.append('g')
      .attr('class', 'flow-overlay-bi')
      .selectAll('line.flow-bi-rev')
      .data(biLinks)
      .join('line')
      .attr('class', 'flow-line flow-reverse flow-bi-rev')
      .attr('stroke', 'var(--amber-400, #fbbf24)')
      .attr('stroke-width', 1.5)
      .attr('stroke-opacity', 0.6)
      .attr('stroke-dasharray', '5 7');

    // Draw nodes
    const nodeGroups = g.append('g')
      .attr('class', 'nodes')
      .selectAll('g')
      .data(nodes)
      .join('g')
      .attr('class', 'node');

    // Node circles — dashed + dimmed when disconnected or disabled
    nodeGroups.append('circle')
      .attr('r', d => getNodeRadius(d.type))
      .attr('fill', 'var(--theme-surface)')
      .attr('stroke', d => !d.enabled ? 'var(--theme-text-muted)' : getNodeColor(d.type))
      .attr('stroke-width', 3)
      .attr('stroke-dasharray', d => (!d.connected || !d.enabled) ? '6 4' : 'none')
      .attr('opacity', d => !d.connected ? 0.4 : !d.enabled ? 0.5 : 1)
      .attr('filter', d => (d.connected && d.enabled) ? 'url(#glow)' : 'none');

    // Node abbreviation text
    nodeGroups.append('text')
      .attr('text-anchor', 'middle')
      .attr('dominant-baseline', 'middle')
      .attr('fill', d => !d.enabled ? 'var(--theme-text-muted)' : getNodeColor(d.type))
      .attr('font-size', d => d.type === 'nats' ? '12px' : '10px')
      .attr('font-weight', '600')
      .attr('opacity', d => !d.connected ? 0.4 : !d.enabled ? 0.5 : 1)
      .text(d => {
        switch (d.type) {
          case 'nats': return 'NATS';
          case 'bus': return 'BUS';
          case 'api': return 'API';
          case 'web': return 'UI';
          case 'caddy': return 'CDY';
          case 'ethernetip': return 'EIP';
          case 'ethernetip-server': return 'EIPS';
          case 'gateway': return 'GW';
          case 'mqtt': return 'MQTT';
          case 'plc': return 'PLC';
          case 'network': return 'NET';
          case 'nftables': return 'NAT';
          case 'snmp': return 'SNMP';
          case 'history': return 'HIST';
          case 'database': return 'DB';
          case 'device': return 'DEV';
          case 'orchestrator': return 'ORCH';
          default: return d.name.slice(0, 4).toUpperCase();
        }
      });

    // Node labels below
    nodeGroups.append('text')
      .attr('text-anchor', 'middle')
      .attr('y', d => getNodeRadius(d.type) + (compact ? 11 : 16))
      .attr('fill', 'var(--theme-text)')
      .attr('font-size', compact ? '10px' : '12px')
      .attr('font-weight', '500')
      .attr('opacity', d => !d.connected ? 0.4 : !d.enabled ? 0.5 : 1)
      .text(d => d.name);

    // Node subtitles — hidden in compact mode to keep small embeds readable.
    if (!compact) {
      nodeGroups.append('text')
        .attr('text-anchor', 'middle')
        .attr('y', d => getNodeRadius(d.type) + 30)
        .attr('fill', d => !d.connected ? '#ef4444' : !d.enabled ? 'var(--amber-500, #f59e0b)' : 'var(--theme-text-muted)')
        .attr('font-size', '10px')
        .attr('opacity', d => !d.connected ? 0.6 : !d.enabled ? 0.7 : 1)
        .text(d => d.subtitle || '');
    }

    // Drag behavior with click-vs-drag discrimination
    let dragMoved = false;
    const drag = d3.drag<SVGGElement, NodeDatum>()
      .on('start', (event, d) => {
        dragMoved = false;
        if (!event.active) simulation?.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
      })
      .on('drag', (event, d) => {
        dragMoved = true;
        d.fx = event.x;
        d.fy = event.y;
      })
      .on('end', (event, d) => {
        if (!event.active) simulation?.alphaTarget(0);
        d.fx = null;
        d.fy = null;
        // Navigate on click (not drag) — skip non-navigable nodes
        const skipTypes = new Set(['device', 'database', 'web', 'nats', 'bus']);
        if (!dragMoved && !skipTypes.has(d.type)) {
          goto(`/services/${d.type}`);
        }
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

      flowOverlay
        .attr('x1', d => (d.source as NodeDatum).x ?? 0)
        .attr('y1', d => (d.source as NodeDatum).y ?? 0)
        .attr('x2', d => (d.target as NodeDatum).x ?? 0)
        .attr('y2', d => (d.target as NodeDatum).y ?? 0);

      // Bidirectional flow lines — offset perpendicular to the link
      const offset = 3;
      const biTickFn = (sel: any, dir: number) => {
        sel.each(function(this: SVGLineElement, d: LinkDatum) {
          const sx = (d.source as NodeDatum).x ?? 0;
          const sy = (d.source as NodeDatum).y ?? 0;
          const tx = (d.target as NodeDatum).x ?? 0;
          const ty = (d.target as NodeDatum).y ?? 0;
          const dx = tx - sx;
          const dy = ty - sy;
          const len = Math.sqrt(dx * dx + dy * dy) || 1;
          const nx = (-dy / len) * offset * dir;
          const ny = (dx / len) * offset * dir;
          d3.select(this)
            .attr('x1', sx + nx).attr('y1', sy + ny)
            .attr('x2', tx + nx).attr('y2', ty + ny);
        });
      };
      biTickFn(flowBiForward, 1);
      biTickFn(flowBiReverse, -1);
    });

    // Auto-fit on simulation end
    simulation.on('end', () => {
      zoomToFit(true);
    });

    // Fit to viewport immediately after pre-settling, then gently restart
    zoomToFit(false);
    simulation.alpha(0.1).restart();
  }

  // Re-render when data changes
  $effect(() => {
    services;
    apiConnected;

    if (container) {
      render();
    }
  });

  onMount(() => {
    render();

    const resizeObserver = new ResizeObserver(() => {
      handleResize();
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

<div class="topology-container" class:compact bind:this={container}></div>

<style lang="scss">
  .topology-container {
    width: 100%;
    height: calc(100vh - var(--header-height) - 4rem);
    min-height: 400px;
    overflow: hidden;

    /* In compact mode the parent decides the size — fill it instead of
       trying to claim the viewport. Used by embeds (e.g. fleet detail). */
    &.compact {
      height: 100%;
      min-height: 0;
    }

    :global(svg) {
      display: block;
    }

    :global(.node) {
      cursor: pointer;
      transition: opacity 0.2s;
    }

    :global(.node:active) {
      cursor: grabbing;
    }

    :global(.node:hover circle) {
      stroke-width: 4;
      filter: url(#glow) brightness(1.2);
    }

    :global(.flow-line) {
      animation: march 1.2s linear infinite;
    }

    :global(.flow-line.flow-reverse) {
      animation: march-reverse 1.2s linear infinite;
    }
  }

  @keyframes march {
    to { stroke-dashoffset: -14; }
  }

  @keyframes march-reverse {
    to { stroke-dashoffset: 14; }
  }
</style>
