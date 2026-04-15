/** Unified display names for service types (keyed by serviceType from heartbeats) */
export const SERVICE_NAMES: Record<string, string> = {
  api: 'API',
  caddy: 'Caddy',
  ethernetip: 'EtherNet/IP',
  'ethernetip-server': 'EtherNet/IP Server',
  gateway: 'Gateway',
  gitops: 'GitOps',
  graphql: 'GraphQL',
  mqtt: 'MQTT',
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
  'tentacle-ethernetip': 'EtherNet/IP',
  'tentacle-ethernetip-server': 'EtherNet/IP Server',
  gitops: 'GitOps',
  'tentacle-history': 'History',
  'tentacle-modbus': 'Modbus',
  'tentacle-modbus-server': 'Modbus Server',
  'tentacle-mqtt': 'MQTT',
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

/** Get a display name for a moduleId, falling back to a cleaned-up version */
export function getModuleName(moduleId: string): string {
  return MODULE_NAMES[moduleId] ?? MODULE_NAMES[`tentacle-${moduleId}`] ?? moduleId.replace('tentacle-', '');
}
