/** Unified display names for service types (keyed by serviceType from heartbeats) */
export const SERVICE_NAMES: Record<string, string> = {
  api: 'API',
  caddy: 'Caddy',
  ethernetip: 'EtherNet/IP',
  'ethernetip-server': 'EtherNet/IP Server',
  gateway: 'Gateway',
  gitops: 'GitOps',
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

/** Get a display name for a moduleId, falling back to a cleaned-up version */
export function getModuleName(moduleId: string): string {
  return MODULE_NAMES[moduleId] ?? MODULE_NAMES[`tentacle-${moduleId}`] ?? moduleId.replace('tentacle-', '');
}
