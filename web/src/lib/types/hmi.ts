// HMI types — mirror internal/types/hmi.go.

export interface HmiBinding {
  /** "variable" | "udtMember" */
  kind: 'variable' | 'udtMember';
  gateway?: string;
  /** Variable id (kind="variable") */
  variable?: string;
  /** UDT instance id (kind="udtMember") — omit to defer resolution to a UDT-typed component */
  udtVariable?: string;
  /** UDT member name (kind="udtMember") */
  member?: string;
}

export interface HmiWidget {
  id: string;
  /** Widget renderer id, e.g. "label", "numeric", "indicator", "stack" */
  type: string;
  x: number;
  y: number;
  w: number;
  h: number;
  props?: Record<string, unknown>;
  bindings?: Record<string, HmiBinding>;
  /** Container widgets (stack) hold ordered children that render in document
   * flow inside the container. Children's x/y/w/h are ignored. */
  children?: HmiWidget[];
}

export interface HmiScreenConfig {
  screenId: string;
  name: string;
  width?: number;
  height?: number;
  widgets: HmiWidget[];
}

export interface HmiComponentConfig {
  componentId: string;
  name: string;
  /** UDT template name this component is bound to (empty = freeform) */
  udtTemplate?: string;
  width?: number;
  height?: number;
  widgets: HmiWidget[];
}

export interface HmiAppConfig {
  appId: string;
  name: string;
  description?: string;
  screens: Record<string, HmiScreenConfig>;
  components?: Record<string, HmiComponentConfig>;
  updatedAt: number;
}

// UDT discovery payload from /api/v1/hmi/udts.

export interface HmiUdtMember {
  name: string;
  datatype: string;
  templateRef?: string;
}

export interface HmiUdtInstance {
  gatewayId: string;
  id: string;
  tag: string;
  deviceId: string;
}

export interface HmiUdtTemplate {
  name: string;
  version?: string;
  members: HmiUdtMember[];
  gateways: string[];
  instances: HmiUdtInstance[];
}
