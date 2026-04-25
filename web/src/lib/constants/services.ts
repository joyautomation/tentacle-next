/** Unified display names for service types (keyed by serviceType from heartbeats) */
export const SERVICE_NAMES: Record<string, string> = {
  api: 'API',
  caddy: 'Caddy',
  ethernetip: 'EtherNet/IP',
  'ethernetip-server': 'EtherNet/IP Server',
  gateway: 'Gateway',
  gitops: 'GitOps',
  gitserver: 'Git Server',
  graphql: 'GraphQL',
  history: 'History',
  mqtt: 'MQTT',
  'mqtt-broker': 'MQTT Broker',
  'sparkplug-host': 'Sparkplug Host',
  nats: 'NATS',
  network: 'Network',
  nftables: 'NAT / Firewall',
  opcua: 'OPC UA',
  orchestrator: 'Orchestrator',
  plc: 'PLC',
  profinet: 'PROFINET Device',
  profinetcontroller: 'PROFINET Controller',
  snmp: 'SNMP',
  telemetry: 'Telemetry',
  web: 'Web UI',
};

/** Display names for module IDs (keyed by full moduleId like "tentacle-mqtt") */
export const MODULE_NAMES: Record<string, string> = {
  caddy: 'Caddy',
  gateway: 'Gateway',
  gitops: 'GitOps',
  gitserver: 'Git Server',
  plc: 'PLC',
  'tentacle-ethernetip': 'EtherNet/IP',
  'tentacle-ethernetip-server': 'EtherNet/IP Server',
  'tentacle-history': 'History',
  'tentacle-modbus': 'Modbus',
  'tentacle-modbus-server': 'Modbus Server',
  'tentacle-mqtt': 'MQTT',
  'tentacle-mqtt-broker': 'MQTT Broker',
  'tentacle-sparkplug-host': 'Sparkplug Host',
  'tentacle-network': 'Network',
  'tentacle-nftables': 'NFTables',
  'tentacle-opcua': 'OPC UA',
  'tentacle-profinet': 'PROFINET Device',
  'tentacle-profinetcontroller': 'PROFINET Controller',
  'tentacle-snmp': 'SNMP',
  'tentacle-telemetry': 'Telemetry',
};

/** Get a display name for a serviceType, falling back to the raw string */
export function getServiceName(serviceType: string): string {
  return SERVICE_NAMES[serviceType.toLowerCase()] ?? serviceType;
}

/**
 * How a serviceType behaves when configuring a *remote* tentacle via mantle:
 *
 * - `configurable`: backend target-aware endpoints exist; the page can write
 *   to the edge's git repo today.
 * - `coming-soon`: the module owns config (its settings live in gitops on
 *   edge), but mantle doesn't yet have target-aware endpoints. The page
 *   should render a placeholder when ?target=... is set.
 * - `bus-driven`: the module has no standalone config — it's instructed by
 *   another module over the bus (e.g. EIP/Profinet/Modbus scanners are
 *   driven by Gateway sources). Links to these are hidden in remote mode.
 */
export type RemoteConfigStatus = 'configurable' | 'coming-soon' | 'bus-driven';

export const REMOTE_CONFIG_STATUS: Record<string, RemoteConfigStatus> = {
  gateway: 'configurable',
  modbus: 'configurable', // tag-config is part of the gateway resource
  gitserver: 'bus-driven', // mantle infra; not configured per-edge
  orchestrator: 'coming-soon',
  telemetry: 'coming-soon',
  gitops: 'coming-soon',
  caddy: 'coming-soon',
  'mqtt-broker': 'coming-soon',
  mqtt: 'coming-soon',
  'sparkplug-host': 'coming-soon',
  plc: 'coming-soon',
  history: 'coming-soon',
  network: 'coming-soon',
  nftables: 'coming-soon',
  ethernetip: 'bus-driven',
  'ethernetip-server': 'bus-driven',
  profinet: 'bus-driven',
  profinetcontroller: 'bus-driven',
  snmp: 'bus-driven',
  opcua: 'bus-driven',
  nats: 'bus-driven',
  api: 'bus-driven',
  graphql: 'bus-driven',
  web: 'bus-driven',
};

export function getRemoteConfigStatus(serviceType: string): RemoteConfigStatus {
  return REMOTE_CONFIG_STATUS[serviceType.toLowerCase()] ?? 'bus-driven';
}

/**
 * Per-service tab definition driving the configurator layout. Each tab is
 * either `live` (reads runtime state from a local module — has no meaning in
 * remote mode) or `config` (reads/writes gitops-managed config — works the
 * same way locally and remotely). When a configurator is opened with
 * ?target=..., the layout filters out `live` tabs.
 */
export type TabScope = 'live' | 'config';

export interface ServiceTab {
  /** path segment after /services/<serviceType>; empty string = default route */
  path: string;
  label: string;
  scope: TabScope;
}

export const SERVICE_TABS: Record<string, ServiceTab[]> = {
  plc: [
    { path: '', label: 'Config', scope: 'config' },
    { path: 'info', label: 'Variables', scope: 'live' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  network: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'status', label: 'Status', scope: 'live' },
    { path: 'config', label: 'Config', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  nftables: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'status', label: 'Status', scope: 'live' },
    { path: 'config', label: 'Config', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  nats: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'traffic', label: 'Traffic', scope: 'live' },
  ],
  mqtt: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'metrics', label: 'Metrics', scope: 'live' },
    { path: 'settings', label: 'Settings', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  ethernetip: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'devices', label: 'Devices', scope: 'live' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  profinetcontroller: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'devices', label: 'Devices', scope: 'live' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  profinet: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'config', label: 'Config', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  gateway: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'devices', label: 'Sources', scope: 'config' },
    { path: 'tag-config', label: 'Variables', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  snmp: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'oids', label: 'OIDs', scope: 'live' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  orchestrator: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'modules', label: 'Modules', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  caddy: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'settings', label: 'Settings', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  gitops: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'history', label: 'History', scope: 'live' },
    { path: 'settings', label: 'Settings', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  telemetry: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'settings', label: 'Settings', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  history: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'trends', label: 'Trends', scope: 'live' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  'sparkplug-host': [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'settings', label: 'Settings', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  modbus: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'tag-config', label: 'Tags', scope: 'config' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
  gitserver: [
    { path: '', label: 'Overview', scope: 'live' },
    { path: 'repos', label: 'Repos', scope: 'live' },
    { path: 'logs', label: 'Logs', scope: 'live' },
  ],
};

const DEFAULT_TABS: ServiceTab[] = [
  { path: '', label: 'Overview', scope: 'live' },
  { path: 'logs', label: 'Logs', scope: 'live' },
];

export function getServiceTabs(serviceType: string): ServiceTab[] {
  return SERVICE_TABS[serviceType.toLowerCase()] ?? DEFAULT_TABS;
}

/** Get a display name for a moduleId, falling back to a cleaned-up version */
export function getModuleName(moduleId: string): string {
  return MODULE_NAMES[moduleId] ?? MODULE_NAMES[`tentacle-${moduleId}`] ?? moduleId.replace('tentacle-', '');
}
