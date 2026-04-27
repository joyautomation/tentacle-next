/**
 * Ladder Diagram editor types.
 *
 * These mirror the canonical JSON AST emitted by `internal/plc/lad` on
 * the Go side. The wire format is one-for-one with the Go shape so the
 * server can `Parse(json)` and `Print(ast)` round-trip cleanly.
 *
 * Keep in sync with internal/plc/lad/ast.go and parser.go.
 */

// =============================================================================
// Wire types — serialized to JSON, matching Go AST.
// =============================================================================

export type Diagram = {
  name?: string;
  variables?: VarDecl[];
  rungs: Rung[];
};

export type VarDecl = {
  name: string;
  type: string;
  /** "" (default VAR) | "global" | "input" | "output" */
  kind?: '' | 'global' | 'input' | 'output';
  init?: string;
  retain?: boolean;
};

export type Rung = {
  comment?: string;
  logic: Element;
  outputs?: Output[];
};

export type Element = Contact | Series | Parallel;

export type Contact = {
  kind: 'contact';
  form: 'NO' | 'NC';
  operand: string;
};

export type Series = {
  kind: 'series';
  items: Element[];
};

export type Parallel = {
  kind: 'parallel';
  items: Element[];
};

export type Output = Coil | FBCall;

export type Coil = {
  kind: 'coil';
  form: 'OTE' | 'OTL' | 'OTU';
  operand: string;
};

export type FBCall = {
  kind: 'fb';
  instance: string;
  /** Override for which input receives rung power flow; default = first input. */
  powerInput?: string;
  inputs?: Record<string, Expr>;
};

export type Expr = Ref | IntLit | RealLit | BoolLit | TimeLit | StringLit;

export type Ref = { kind: 'ref'; name: string };
export type IntLit = { kind: 'int'; value: number };
export type RealLit = { kind: 'real'; value: number };
export type BoolLit = { kind: 'bool'; value: boolean };
export type TimeLit = { kind: 'time'; raw?: string; ms?: number };
export type StringLit = { kind: 'string'; value: string };

// =============================================================================
// Editor-only types
// =============================================================================

/**
 * Path identifies a node within a Diagram for selection/edit dispatch.
 *
 * - rung: index into Diagram.rungs
 * - logic: array of indices walking into Series/Parallel.items; empty means
 *   the rung's root logic node
 * - output: index into rung.outputs (when targeting an output)
 *
 * The two are mutually exclusive: a path either points at a logic element
 * or at an output. `kind` disambiguates.
 */
export type EditPath =
  | { kind: 'logic'; rung: number; logic: number[] }
  | { kind: 'output'; rung: number; output: number };

export type Selection = EditPath | null;

/** Live monitoring values for tags (placeholder until the live runtime wires up). */
export type TagValues = Record<string, { value: unknown; energized?: boolean }>;

// =============================================================================
// Layout types — produced by the layout pass, consumed by the renderer.
// =============================================================================

export type LayoutNode =
  | { kind: 'contact'; element: Contact; path: EditPath; x: number; y: number; width: number; height: number }
  | { kind: 'coil'; element: Coil; path: EditPath; x: number; y: number; width: number; height: number }
  | { kind: 'fb'; element: FBCall; path: EditPath; x: number; y: number; width: number; height: number; pins: FBPin[] };

export type FBPin = {
  name: string;
  isPower: boolean;
  /** y offset within the FB box (for wire termination). */
  y: number;
  /** Pre-rendered value text shown to the right of the pin name. */
  valueText?: string;
};

export type LayoutWire = { x1: number; y1: number; x2: number; y2: number };

export type LayoutBranchLine = { x: number; y1: number; y2: number };

export type RungLayout = {
  nodes: LayoutNode[];
  wires: LayoutWire[];
  branchLines: LayoutBranchLine[];
  /** y-coordinate of the main power-flow wire (left rail → outputs). */
  wireY: number;
  /** x where the rung's content ends (last output's right edge). The
   *  diagram pass extends this to the shared right rail. */
  contentRight: number;
  totalWidth: number;
  totalHeight: number;
};

// =============================================================================
// Layout constants. Tuned for legibility at default scale.
// =============================================================================

export const LAYOUT = {
  RAIL_LEFT: 30,
  RAIL_RIGHT_MARGIN: 30,
  CONTACT_WIDTH: 80,
  CONTACT_HEIGHT: 24,
  COIL_WIDTH: 80,
  COIL_HEIGHT: 24,
  FB_MIN_WIDTH: 140,
  FB_HEADER_HEIGHT: 24,
  FB_PIN_ROW_HEIGHT: 22,
  FB_HORIZONTAL_PADDING: 12,
  WIRE_GAP: 8,
  BRANCH_GAP: 12,
  RUNG_PADDING_Y: 24,
  RUNG_GAP: 8,
  /** Vertical room reserved above the symbol for the form label (NO/OTE/...). */
  LABEL_TOP_SPACE: 12,
  /** Vertical room reserved below the symbol for the truncated operand label. */
  LABEL_BOTTOM_SPACE: 16,
  /** Visible operand label character budget before middle-truncation. */
  OPERAND_LABEL_CHARS: 10,
  TAG_FONT_SIZE: 11,
  LABEL_FONT_SIZE: 10,
} as const;

// =============================================================================
// Helpers — building blocks for editor mutations.
// =============================================================================

export function emptyDiagram(name = ''): Diagram {
  return { name, variables: [], rungs: [] };
}

export function newRung(): Rung {
  return {
    logic: { kind: 'contact', form: 'NO', operand: '' },
    outputs: [],
  };
}

export function newContact(form: 'NO' | 'NC' = 'NO', operand = ''): Contact {
  return { kind: 'contact', form, operand };
}

export function newCoil(form: 'OTE' | 'OTL' | 'OTU' = 'OTE', operand = ''): Coil {
  return { kind: 'coil', form, operand };
}

/** Pretty label for an Expr — used in FB pin value rendering. */
export function exprLabel(e: Expr): string {
  switch (e.kind) {
    case 'ref':
      return e.name;
    case 'int':
      return String(e.value);
    case 'real':
      return String(e.value);
    case 'bool':
      return e.value ? 'TRUE' : 'FALSE';
    case 'time':
      return e.raw ?? `T#${e.ms ?? 0}ms`;
    case 'string':
      return `'${e.value}'`;
  }
}
