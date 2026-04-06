export interface DeadBandConfig {
  value: number;
  minTime?: number | null;
  maxTime?: number | null;
  disableRBE?: boolean | null;
}

export interface GatewayDevice {
  deviceId: string;
  protocol: string;
  config: Record<string, unknown>;
  scanRate?: number | null;
  deadband?: DeadBandConfig | null;
  disableRBE?: boolean | null;
  templateNameOverrides?: Record<string, string> | null;
}

export interface GatewayVariable {
  id: string;
  description: string | null;
  datatype: string;
  default?: unknown;
  deviceId: string;
  tag: string;
  bidirectional: boolean | null;
  cipType?: string;
  deadband?: DeadBandConfig | null;
  disableRBE?: boolean | null;
}

export interface GatewayUdtTemplateMember {
  name: string;
  datatype: string;
  templateRef: string | null;
  defaultDeadband?: DeadBandConfig | null;
}

export interface GatewayUdtTemplate {
  name: string;
  version: string | null;
  members: GatewayUdtTemplateMember[];
}

export interface GatewayUdtVariable {
  id: string;
  deviceId: string;
  tag: string;
  templateName: string;
  memberTags?: Record<string, string>;
  memberCipTypes?: Record<string, string> | null;
  memberDeadbands?: Record<string, DeadBandConfig> | null;
}

export interface BrowseCacheItem {
  tag: string;
  name: string;
  datatype: string;
  value: unknown;
  protocolType: string;
}

export interface BrowseCacheUdtMember {
  name: string;
  datatype: string;
  cipType: string;
  udtType: string;
  isArray: boolean;
}

export interface BrowseCacheUdt {
  name: string;
  members: BrowseCacheUdtMember[];
}

export interface BrowseCache {
  deviceId: string;
  protocol: string;
  items: BrowseCacheItem[];
  udts: BrowseCacheUdt[];
  structTags: Record<string, string>;
  cachedAt: string | null;
}

export interface GatewayConfig {
  gatewayId: string;
  devices: GatewayDevice[];
  variables: GatewayVariable[];
  udtTemplates?: GatewayUdtTemplate[];
  udtVariables?: GatewayUdtVariable[];
  availableProtocols?: string[];
  updatedAt: string;
}

export interface GatewayBrowseState {
  deviceId: string;
  browseId: string;
  protocol: string;
  status: "browsing" | "completed" | "failed";
  phase: string;
  discoveredCount: number;
  totalCount: number;
  message: string;
  startedAt: string;
  updatedAt: string;
  error?: string | null;
}

export interface Variable {
  variableId: string;
  value: unknown;
  datatype: string;
  cipType?: string | null;
  udtType: string | null;
  quality: string;
  moduleId: string;
  deviceId: string | null;
  lastUpdated: string;
}

export interface ActiveDevice {
  deviceId: string;
  host: string;
  port: number;
  tagCount: number;
}
