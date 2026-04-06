/**
 * Ladder Logic Layout Algorithm
 *
 * Walks the rung tree and computes (x, y) positions for each element.
 * The layout flows left-to-right, with branches stacking vertically.
 * This is NOT a grid — positions are computed from the tree structure
 * just like RSLogix auto-layout.
 */

import {
  type LadderCondition,
  type LadderElement,
  type LadderOutput,
  type LayoutBranchLine,
  type LayoutElement,
  type LayoutWire,
  type Rung,
  type RungLayout,
  LAYOUT,
} from './types.js';

type LayoutResult = {
  elements: LayoutElement[];
  wires: LayoutWire[];
  branchLines: LayoutBranchLine[];
  width: number;
  height: number;
};

let _idCounter = 0;
function nextId(): string {
  return `el_${_idCounter++}`;
}

function elementWidth(el: LadderElement): number {
  switch (el.type) {
    case 'NO':
    case 'NC':
      return LAYOUT.CONTACT_WIDTH;
    case 'OTE':
    case 'OTL':
    case 'OTU':
      return LAYOUT.COIL_WIDTH;
    case 'TON':
    case 'TOF':
      return LAYOUT.TIMER_WIDTH;
    case 'CTU':
    case 'CTD':
      return LAYOUT.COUNTER_WIDTH;
    case 'series':
    case 'branch':
      return 0; // computed recursively
  }
}

function elementHeight(el: LadderElement): number {
  switch (el.type) {
    case 'NO':
    case 'NC':
      return LAYOUT.CONTACT_HEIGHT;
    case 'OTE':
    case 'OTL':
    case 'OTU':
      return LAYOUT.COIL_HEIGHT;
    case 'TON':
    case 'TOF':
      return LAYOUT.TIMER_HEIGHT;
    case 'CTU':
    case 'CTD':
      return LAYOUT.COUNTER_HEIGHT;
    case 'series':
    case 'branch':
      return 0; // computed recursively
  }
}

/**
 * Lay out a single condition element (contact, series, or branch).
 * Returns positioned elements, wires, and branch lines.
 */
function layoutCondition(
  condition: LadderCondition,
  x: number,
  y: number,
): LayoutResult {
  switch (condition.type) {
    case 'NO':
    case 'NC': {
      const w = elementWidth(condition);
      const h = elementHeight(condition);
      return {
        elements: [{
          id: nextId(),
          element: condition,
          x,
          y: y - h / 2,
          width: w,
          height: h,
        }],
        wires: [],
        branchLines: [],
        width: w,
        height: h,
      };
    }

    case 'series': {
      const elements: LayoutElement[] = [];
      const wires: LayoutWire[] = [];
      const branchLines: LayoutBranchLine[] = [];
      let cursor = x;
      let maxHeight: number = LAYOUT.CONTACT_HEIGHT;

      for (let i = 0; i < condition.elements.length; i++) {
        const child = condition.elements[i];

        // Wire before element (except first)
        if (i > 0) {
          wires.push({ x1: cursor, y1: y, x2: cursor + LAYOUT.WIRE_GAP, y2: y });
          cursor += LAYOUT.WIRE_GAP;
        }

        const result = layoutCondition(child, cursor, y);
        elements.push(...result.elements);
        wires.push(...result.wires);
        branchLines.push(...result.branchLines);
        cursor += result.width;
        maxHeight = Math.max(maxHeight, result.height);
      }

      return {
        elements,
        wires,
        branchLines,
        width: cursor - x,
        height: maxHeight,
      };
    }

    case 'branch': {
      const elements: LayoutElement[] = [];
      const wires: LayoutWire[] = [];
      const branchLines: LayoutBranchLine[] = [];

      // Layout each path, stacking vertically
      const pathResults: LayoutResult[] = [];
      let totalHeight = 0;
      let maxWidth = 0;

      for (let i = 0; i < condition.paths.length; i++) {
        const path = condition.paths[i];
        // Temporary layout to measure
        const result = layoutCondition(path, 0, 0);
        pathResults.push(result);
        maxWidth = Math.max(maxWidth, result.width);
        if (i > 0) totalHeight += LAYOUT.BRANCH_GAP;
        totalHeight += result.height;
      }

      // Now position each path
      // First path is at the main wire level (y)
      // Subsequent paths are below
      let pathY = y;
      const branchX = x;
      const branchEndX = x + maxWidth;
      const pathYPositions: number[] = [];

      for (let i = 0; i < condition.paths.length; i++) {
        const path = condition.paths[i];
        pathYPositions.push(pathY);

        const result = layoutCondition(path, branchX, pathY);
        elements.push(...result.elements);
        wires.push(...result.wires);
        branchLines.push(...result.branchLines);

        // Wire from element end to branch end (align all paths to same width)
        if (result.width < maxWidth) {
          wires.push({
            x1: branchX + result.width,
            y1: pathY,
            x2: branchEndX,
            y2: pathY,
          });
        }

        pathY += result.height + LAYOUT.BRANCH_GAP;
      }

      // Vertical branch lines on left and right
      if (pathYPositions.length > 1) {
        const firstY = pathYPositions[0];
        const lastY = pathYPositions[pathYPositions.length - 1];
        branchLines.push({ x: branchX, y1: firstY, y2: lastY });
        branchLines.push({ x: branchEndX, y1: firstY, y2: lastY });
      }

      return {
        elements,
        wires,
        branchLines,
        width: maxWidth,
        height: totalHeight,
      };
    }
  }
}

/**
 * Lay out a single output element (coil, timer, counter).
 */
function layoutOutput(
  output: LadderOutput,
  x: number,
  y: number,
): LayoutResult {
  const w = elementWidth(output);
  const h = elementHeight(output);
  return {
    elements: [{
      id: nextId(),
      element: output,
      x,
      y: y - h / 2,
      width: w,
      height: h,
    }],
    wires: [],
    branchLines: [],
    width: w,
    height: h,
  };
}

/**
 * Compute the full layout for a rung.
 */
export function layoutRung(rung: Rung): RungLayout {
  _idCounter = 0;

  const elements: LayoutElement[] = [];
  const wires: LayoutWire[] = [];
  const branchLines: LayoutBranchLine[] = [];

  const wireY = LAYOUT.RUNG_PADDING_Y + LAYOUT.CONTACT_HEIGHT / 2;
  let cursor = LAYOUT.RAIL_LEFT;

  // Wire from left rail
  wires.push({ x1: 0, y1: wireY, x2: cursor, y2: wireY });

  // Layout conditions
  for (let i = 0; i < rung.conditions.length; i++) {
    if (i > 0) {
      wires.push({ x1: cursor, y1: wireY, x2: cursor + LAYOUT.WIRE_GAP, y2: wireY });
      cursor += LAYOUT.WIRE_GAP;
    }

    const result = layoutCondition(rung.conditions[i], cursor, wireY);
    elements.push(...result.elements);
    wires.push(...result.wires);
    branchLines.push(...result.branchLines);
    cursor += result.width;
  }

  // Wire gap before outputs
  if (rung.conditions.length > 0 && rung.outputs.length > 0) {
    wires.push({ x1: cursor, y1: wireY, x2: cursor + LAYOUT.WIRE_GAP, y2: wireY });
    cursor += LAYOUT.WIRE_GAP;
  }

  // Layout outputs
  for (let i = 0; i < rung.outputs.length; i++) {
    if (i > 0) {
      wires.push({ x1: cursor, y1: wireY, x2: cursor + LAYOUT.WIRE_GAP, y2: wireY });
      cursor += LAYOUT.WIRE_GAP;
    }

    const result = layoutOutput(rung.outputs[i], cursor, wireY);
    elements.push(...result.elements);
    wires.push(...result.wires);
    branchLines.push(...result.branchLines);
    cursor += result.width;
  }

  // Wire to right rail
  const rightRail = cursor + LAYOUT.RAIL_RIGHT_MARGIN;
  wires.push({ x1: cursor, y1: wireY, x2: rightRail, y2: wireY });

  // Compute total height from all elements
  let maxBottom = wireY + LAYOUT.CONTACT_HEIGHT / 2;
  for (const el of elements) {
    maxBottom = Math.max(maxBottom, el.y + el.height);
  }

  return {
    elements,
    wires,
    branchLines,
    totalWidth: rightRail,
    totalHeight: maxBottom + LAYOUT.RUNG_PADDING_Y,
  };
}

/**
 * Compute layouts for all rungs in a program, stacking vertically.
 */
export function layoutProgram(
  rungs: Rung[],
): { layouts: RungLayout[]; totalWidth: number; totalHeight: number } {
  const layouts: RungLayout[] = [];
  let maxWidth = 0;

  for (const rung of rungs) {
    const layout = layoutRung(rung);
    layouts.push(layout);
    maxWidth = Math.max(maxWidth, layout.totalWidth);
  }

  let totalHeight = 0;
  for (const layout of layouts) {
    totalHeight += layout.totalHeight + LAYOUT.RUNG_GAP;
  }

  return { layouts, totalWidth: maxWidth, totalHeight };
}
