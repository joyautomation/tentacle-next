// PROFINET IO Controller types

export interface ControllerTag {
  tagId: string;
  byteOffset: number;
  bitOffset: number;
  datatype: string;
  direction: 'input' | 'output';
}

export interface SubslotSubscription {
  subslotNumber: number;
  submoduleIdentNo: number;
  inputSize: number;
  outputSize: number;
  tags: ControllerTag[];
}

export interface SlotSubscription {
  slotNumber: number;
  moduleIdentNo: number;
  subslots: SubslotSubscription[];
}

export interface ControllerSubscription {
  subscriberId: string;
  deviceId: string;
  stationName: string;
  ip: string;
  interfaceName: string;
  vendorId?: number;
  deviceIdPn?: number;
  cycleTimeMs: number;
  slots: SlotSubscription[];
}

// PROFINET IO Device types

export type ProfinetDirection = 'input' | 'output' | 'inputOutput';

export type ProfinetType =
  | 'bool'
  | 'uint8'
  | 'int8'
  | 'uint16'
  | 'int16'
  | 'uint32'
  | 'int32'
  | 'float32'
  | 'uint64'
  | 'int64'
  | 'float64';

export const PROFINET_TYPES: ProfinetType[] = [
  'bool',
  'uint8',
  'int8',
  'uint16',
  'int16',
  'uint32',
  'int32',
  'float32',
  'uint64',
  'int64',
  'float64',
];

export function typeSize(t: ProfinetType): number {
  switch (t) {
    case 'bool':
    case 'uint8':
    case 'int8':
      return 1;
    case 'uint16':
    case 'int16':
      return 2;
    case 'uint32':
    case 'int32':
    case 'float32':
      return 4;
    case 'uint64':
    case 'int64':
    case 'float64':
      return 8;
  }
}

export interface TagMapping {
  tagId: string;
  byteOffset: number;
  bitOffset: number;
  datatype: ProfinetType;
  source: string;
}

export interface SubslotConfig {
  subslotNumber: number;
  submoduleIdentNo: number;
  direction: ProfinetDirection;
  inputSize: number;
  outputSize: number;
  tags: TagMapping[];
}

export interface SlotConfig {
  slotNumber: number;
  moduleIdentNo: number;
  subslots: SubslotConfig[];
}

export interface ProfinetConfig {
  stationName: string;
  interfaceName: string;
  vendorId: number;
  deviceId: number;
  deviceName: string;
  cycleTimeUs: number;
  slots: SlotConfig[];
}

export interface ProfinetStatus {
  connected: boolean;
  stationName: string;
  interfaceName: string;
  controllerIp: string;
  arep: number;
  inputSlots: number;
  outputSlots: number;
}

// Network types (for interface picker)
export interface NetworkInterface {
  name: string;
  operstate: string;
  carrier: boolean;
  speed?: number | null;
  mac: string;
  type?: number;
}

export interface NetworkState {
  interfaces: NetworkInterface[];
}
