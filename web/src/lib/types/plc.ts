export interface PlcVariableSource {
  protocol: string;
  deviceId: string;
  tag: string;
  cipType?: string;
  functionCode?: number | null;
  modbusDatatype?: string;
  byteOrder?: string;
  address?: number | null;
}

export interface PlcVariableConfig {
  id: string;
  description?: string;
  datatype: string;
  default: unknown;
  direction: string;
  source?: PlcVariableSource | null;
  deadband?: { value: number; minTime?: number | null; maxTime?: number | null } | null;
  disableRBE?: boolean;
}

export interface PlcDeviceConfig {
  protocol: string;
  host?: string;
  port?: number | null;
  slot?: number | null;
  endpointUrl?: string;
  version?: string;
  community?: string;
  unitId?: number | null;
  scanRate?: number | null;
}

export interface PlcTaskConfig {
  name: string;
  description?: string;
  scanRateMs: number;
  programRef: string;
  enabled: boolean;
}

export interface PlcConfig {
  plcId: string;
  devices: Record<string, PlcDeviceConfig>;
  variables: Record<string, PlcVariableConfig>;
  udtTemplates?: Record<string, unknown>;
  tasks: Record<string, PlcTaskConfig>;
  updatedAt: number;
}

export interface ProgramListItem {
  name: string;
  language: string;
  updatedAt: number;
  updatedBy?: string;
}
