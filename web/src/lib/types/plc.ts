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
  entryFn?: string;
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

export interface PlcFunctionParam {
  name: string;
  type: string;
  description?: string;
  required?: boolean;
  default?: unknown;
}

export interface PlcFunctionReturn {
  type: string;
  description?: string;
}

export interface PlcFunctionSig {
  params?: PlcFunctionParam[];
  returns?: PlcFunctionReturn | null;
}

export interface ProgramListItem {
  name: string;
  description?: string;
  module?: string;
  language: string;
  signature?: PlcFunctionSig | null;
  updatedAt: number;
  updatedBy?: string;
  hasPending?: boolean;
  pendingUpdatedAt?: number;
  pendingUpdatedBy?: string;
}

export interface PlcTestResult {
  name: string;
  status: 'pass' | 'fail' | 'error';
  message?: string;
  logs?: string[];
  durationMs: number;
  startedAt: number;
}

export interface PlcTest {
  name: string;
  description?: string;
  source: string;
  updatedAt: number;
  updatedBy?: string;
  lastResult?: PlcTestResult;
}

export interface TestListItem {
  name: string;
  description?: string;
  updatedAt: number;
  updatedBy?: string;
  lastResult?: PlcTestResult;
}

export interface PlcTemplateField {
  name: string;
  type: string;
  default?: unknown;
  description?: string;
}

export interface PlcFunctionRef {
  module: string;
  name: string;
}

export interface PlcTemplateMethod {
  name: string;
  function: PlcFunctionRef;
  description?: string;
}

export interface PlcTemplate {
  name: string;
  description?: string;
  tags?: string[];
  fields: PlcTemplateField[];
  methods?: PlcTemplateMethod[];
  updatedAt?: number;
  updatedBy?: string;
}
