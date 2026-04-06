/**
 * Ladder Logic Editor Types
 *
 * These mirror the runtime types from tentacle-plc/ladder.ts so the editor
 * can parse .lad.ts files and render them, and write edits back as valid TS.
 */

// =============================================================================
// Ladder Element Types (same as runtime)
// =============================================================================

export type LadderContact = {
  type: 'NO' | 'NC';
  tag: string;
};

export type LadderSeries = {
  type: 'series';
  elements: LadderCondition[];
};

export type LadderBranch = {
  type: 'branch';
  paths: LadderCondition[];
};

export type LadderCondition = LadderContact | LadderBranch | LadderSeries;

export type LadderCoil = {
  type: 'OTE' | 'OTL' | 'OTU';
  tag: string;
};

export type LadderTimer = {
  type: 'TON' | 'TOF';
  tag: string;
  preset: number;
};

export type LadderCounter = {
  type: 'CTU' | 'CTD';
  tag: string;
  preset: number;
};

export type LadderOutput = LadderCoil | LadderTimer | LadderCounter;

export type LadderElement = LadderCondition | LadderOutput;

// =============================================================================
// Editor-specific Types
// =============================================================================

export type Rung = {
  id: string;
  comment: string;
  conditions: LadderCondition[];
  outputs: LadderOutput[];
};

export type LadderProgram = {
  name: string;
  rungs: Rung[];
};

/** Selection state for the editor */
export type Selection = {
  rungId: string;
  /** Path into the rung tree — array of indices */
  path: number[];
  type: 'condition' | 'output';
} | null;

/** Live monitoring values for tags */
export type TagValues = Record<string, {
  value: unknown;
  energized?: boolean;
}>;

// =============================================================================
// Layout Types (computed from the data model)
// =============================================================================

export type LayoutElement = {
  id: string;
  element: LadderElement;
  x: number;
  y: number;
  width: number;
  height: number;
};

export type LayoutWire = {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
};

export type LayoutBranchLine = {
  x: number;
  y1: number;
  y2: number;
};

export type RungLayout = {
  elements: LayoutElement[];
  wires: LayoutWire[];
  branchLines: LayoutBranchLine[];
  totalWidth: number;
  totalHeight: number;
};

// =============================================================================
// Layout Constants
// =============================================================================

export const LAYOUT = {
  RAIL_LEFT: 30,
  RAIL_RIGHT_MARGIN: 30,
  CONTACT_WIDTH: 100,
  CONTACT_HEIGHT: 40,
  COIL_WIDTH: 100,
  COIL_HEIGHT: 40,
  TIMER_WIDTH: 140,
  TIMER_HEIGHT: 70,
  COUNTER_WIDTH: 140,
  COUNTER_HEIGHT: 70,
  WIRE_GAP: 16,
  BRANCH_GAP: 12,
  RUNG_PADDING_Y: 16,
  RUNG_GAP: 8,
  TAG_FONT_SIZE: 11,
  LABEL_FONT_SIZE: 9,
} as const;
