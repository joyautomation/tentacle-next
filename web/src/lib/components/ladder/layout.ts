/**
 * Ladder Diagram layout pass.
 *
 * Walks a Diagram and computes positions, wires, and branch lines so the
 * SVG renderer can draw the rungs without doing geometry inline. Layout
 * is bottom-up: each element reports its bounding box, parents stack
 * children. Series flows left→right (max height), Parallel stacks
 * top→bottom (max width). The result mirrors RSLogix-style auto-layout.
 */

import {
  type Coil,
  type Contact,
  type Diagram,
  type EditPath,
  type Element,
  type FBCall,
  type FBPin,
  type LayoutBranchLine,
  type LayoutNode,
  type LayoutWire,
  type Output,
  type Rung,
  type RungLayout,
  LAYOUT,
  exprLabel,
} from './types.js';

type LogicLayoutResult = {
  nodes: LayoutNode[];
  wires: LayoutWire[];
  branchLines: LayoutBranchLine[];
  width: number;
  /** Vertical extent above the centerline (for Parallel half-heights). */
  ascent: number;
  /** Vertical extent below the centerline. */
  descent: number;
  /** Y coordinate where parents should connect power flow into/out of this node. */
  connectY: number;
};

type OutputLayoutResult = {
  nodes: LayoutNode[];
  wires: LayoutWire[];
  width: number;
  /** Centerline-based extents like LogicLayoutResult. */
  ascent: number;
  descent: number;
  /** Y where the rung wire connects to this output. */
  connectY: number;
};

/**
 * Lay out a logic element (Contact / Series / Parallel) centered on `centerY`.
 * `path` is the EditPath that this node would receive on click.
 */
function layoutLogic(
  el: Element,
  x: number,
  centerY: number,
  rungIdx: number,
  logicPath: number[],
): LogicLayoutResult {
  switch (el.kind) {
    case 'contact': {
      const w = LAYOUT.CONTACT_WIDTH;
      const h = LAYOUT.CONTACT_HEIGHT;
      const half = h / 2;
      const node: LayoutNode = {
        kind: 'contact',
        element: el,
        path: { kind: 'logic', rung: rungIdx, logic: logicPath },
        x,
        y: centerY - half,
        width: w,
        height: h,
      };
      return {
        nodes: [node],
        wires: [],
        branchLines: [],
        width: w,
        ascent: half + LAYOUT.LABEL_TOP_SPACE,
        descent: half + LAYOUT.LABEL_BOTTOM_SPACE,
        connectY: centerY,
      };
    }

    case 'series': {
      const nodes: LayoutNode[] = [];
      const wires: LayoutWire[] = [];
      const branchLines: LayoutBranchLine[] = [];
      let cursor = x;
      let ascent = 0;
      let descent = 0;

      el.items.forEach((child, i) => {
        if (i > 0) {
          wires.push({ x1: cursor, y1: centerY, x2: cursor + LAYOUT.WIRE_GAP, y2: centerY });
          cursor += LAYOUT.WIRE_GAP;
        }
        const r = layoutLogic(child, cursor, centerY, rungIdx, [...logicPath, i]);
        nodes.push(...r.nodes);
        wires.push(...r.wires);
        branchLines.push(...r.branchLines);
        cursor += r.width;
        if (r.ascent > ascent) ascent = r.ascent;
        if (r.descent > descent) descent = r.descent;
      });

      return {
        nodes,
        wires,
        branchLines,
        width: cursor - x,
        ascent,
        descent,
        connectY: centerY,
      };
    }

    case 'parallel': {
      // First pass: lay out each branch at origin to measure width.
      type Measured = { result: LogicLayoutResult; height: number };
      const measured: Measured[] = el.items.map((child, i) =>
        ({
          result: layoutLogic(child, 0, 0, rungIdx, [...logicPath, i]),
          height: 0, // filled below
        })
      );
      let maxBranchWidth = 0;
      measured.forEach(m => {
        m.height = m.result.ascent + m.result.descent;
        if (m.result.width > maxBranchWidth) maxBranchWidth = m.result.width;
      });

      // Position branches: first branch on the centerline; subsequent
      // branches stack downward by their (descent of previous + gap +
      // ascent of next).
      const nodes: LayoutNode[] = [];
      const wires: LayoutWire[] = [];
      const branchLines: LayoutBranchLine[] = [];
      const branchYs: number[] = [];
      let prevDescent = 0;
      let pen = centerY;
      el.items.forEach((child, i) => {
        const m = measured[i];
        if (i === 0) {
          pen = centerY;
        } else {
          pen += prevDescent + LAYOUT.BRANCH_GAP + m.result.ascent;
        }
        branchYs.push(pen);

        // Re-lay out this branch at the resolved (x, pen) position with
        // its proper EditPath so click targets are correct.
        const r = layoutLogic(child, x, pen, rungIdx, [...logicPath, i]);
        nodes.push(...r.nodes);
        wires.push(...r.wires);
        branchLines.push(...r.branchLines);

        // Right-pad shorter branches so they all reach the same x.
        if (r.width < maxBranchWidth) {
          wires.push({ x1: x + r.width, y1: pen, x2: x + maxBranchWidth, y2: pen });
        }

        prevDescent = m.result.descent;
      });

      // Vertical branch rails on the left and right edges.
      if (branchYs.length > 1) {
        const top = branchYs[0];
        const bottom = branchYs[branchYs.length - 1];
        branchLines.push({ x, y1: top, y2: bottom });
        branchLines.push({ x: x + maxBranchWidth, y1: top, y2: bottom });
      }

      const ascentTotal = centerY - branchYs[0] + measured[0].result.ascent;
      const lastIdx = branchYs.length - 1;
      const descentTotal = branchYs[lastIdx] - centerY + measured[lastIdx].result.descent;

      return {
        nodes,
        wires,
        branchLines,
        width: maxBranchWidth,
        ascent: ascentTotal,
        descent: descentTotal,
        connectY: centerY,
      };
    }
  }
}

/**
 * Lay out an output (Coil or FBCall) centered on `centerY`.
 *
 * For FBCall, the box is anchored so the chosen power-flow pin sits on
 * the rung wire. Other pins extend below; the box header sits above.
 */
function layoutOutput(
  out: Output,
  x: number,
  centerY: number,
  rungIdx: number,
  outputIdx: number,
): OutputLayoutResult {
  const path: EditPath = { kind: 'output', rung: rungIdx, output: outputIdx };

  if (out.kind === 'coil') {
    const w = LAYOUT.COIL_WIDTH;
    const h = LAYOUT.COIL_HEIGHT;
    const half = h / 2;
    return {
      nodes: [{
        kind: 'coil',
        element: out,
        path,
        x,
        y: centerY - half,
        width: w,
        height: h,
      }],
      wires: [],
      width: w,
      ascent: half + LAYOUT.LABEL_TOP_SPACE,
      descent: half + LAYOUT.LABEL_BOTTOM_SPACE,
      connectY: centerY,
    };
  }

  // FBCall — generic box with header + pin rows.
  const inputKeys = Object.keys(out.inputs ?? {}).sort();
  const powerKey = out.powerInput || inputKeys[0] || 'EN';
  const orderedKeys = [
    powerKey,
    ...inputKeys.filter(k => k !== powerKey),
  ].filter(k => k.length > 0);

  // Width: text-based estimate; the renderer can override with a measured
  // value later, but for v1 we approximate with character counts.
  const longest = Math.max(
    out.instance.length,
    ...orderedKeys.map(k => k.length + (out.inputs?.[k] ? exprLabel(out.inputs[k]).length + 4 : 0)),
  );
  const approxCharPx = 7;
  const width = Math.max(
    LAYOUT.FB_MIN_WIDTH,
    longest * approxCharPx + LAYOUT.FB_HORIZONTAL_PADDING * 2,
  );

  const headerH = LAYOUT.FB_HEADER_HEIGHT;
  const rowH = LAYOUT.FB_PIN_ROW_HEIGHT;
  const rows = Math.max(orderedKeys.length, 1);
  const totalH = headerH + rows * rowH;

  // Anchor: power pin sits on the rung wire. Power pin = first row.
  const powerPinY = headerH + rowH / 2; // relative to box top
  const yTop = centerY - powerPinY;

  const pins: FBPin[] = orderedKeys.map((name, i) => {
    const expr = out.inputs?.[name];
    return {
      name,
      isPower: name === powerKey,
      y: headerH + i * rowH + rowH / 2,
      valueText: expr ? exprLabel(expr) : undefined,
    };
  });

  return {
    nodes: [{
      kind: 'fb',
      element: out,
      path,
      x,
      y: yTop,
      width,
      height: totalH,
      pins,
    }],
    wires: [],
    width,
    ascent: powerPinY,
    descent: totalH - powerPinY,
    connectY: centerY,
  };
}

/**
 * Compute layout for a single rung. Rungs are positioned starting at y=0
 * (caller adds vertical offset between rungs).
 *
 * Logic is left-justified at RAIL_LEFT; outputs are right-justified
 * against `outputsRight`. The connecting wire fills whatever gap remains
 * between them. When `outputsRight` is omitted, outputs pack immediately
 * after the logic with WIRE_GAP padding (the legacy compact form).
 */
export function layoutRung(
  rung: Rung,
  rungIdx: number,
  outputsRight?: number,
): RungLayout {
  // Probe pass: measure the rung's vertical extents so we know how far
  // above the wire we have to push for parallels with branches above the
  // main path, and so we can compute the outputs total width.
  const probeLogic = layoutLogic(rung.logic, 0, 0, rungIdx, []);
  const probeOutputs = (rung.outputs ?? []).map((o, i) => layoutOutput(o, 0, 0, rungIdx, i));

  let ascent = probeLogic.ascent;
  let descent = probeLogic.descent;
  for (const po of probeOutputs) {
    if (po.ascent > ascent) ascent = po.ascent;
    if (po.descent > descent) descent = po.descent;
  }

  const wireY = LAYOUT.RUNG_PADDING_Y + ascent;
  const nodes: LayoutNode[] = [];
  const wires: LayoutWire[] = [];
  const branchLines: LayoutBranchLine[] = [];

  // Left rail tap.
  wires.push({ x1: 0, y1: wireY, x2: LAYOUT.RAIL_LEFT, y2: wireY });

  const logic = layoutLogic(rung.logic, LAYOUT.RAIL_LEFT, wireY, rungIdx, []);
  nodes.push(...logic.nodes);
  wires.push(...logic.wires);
  branchLines.push(...logic.branchLines);

  const logicEnd = LAYOUT.RAIL_LEFT + logic.width;

  // Compute outputs total width (sum + inter-output WIRE_GAPs).
  let outputsTotalWidth = 0;
  probeOutputs.forEach((po, i) => {
    if (i > 0) outputsTotalWidth += LAYOUT.WIRE_GAP;
    outputsTotalWidth += po.width;
  });

  // Decide where the outputs block starts. If a target right edge is
  // given, anchor against it; otherwise pack tight after the logic.
  const fallbackOutputsStart = logicEnd + (probeOutputs.length > 0 ? LAYOUT.WIRE_GAP : 0);
  let outputsStart: number;
  if (outputsRight !== undefined) {
    const desiredStart = outputsRight - outputsTotalWidth;
    outputsStart = Math.max(desiredStart, fallbackOutputsStart);
  } else {
    outputsStart = fallbackOutputsStart;
  }

  if (probeOutputs.length > 0) {
    // Wire connects logic end → outputs start (whatever the gap is).
    wires.push({ x1: logicEnd, y1: wireY, x2: outputsStart, y2: wireY });
  }

  let cursor = outputsStart;
  (rung.outputs ?? []).forEach((output, i) => {
    if (i > 0) {
      wires.push({ x1: cursor, y1: wireY, x2: cursor + LAYOUT.WIRE_GAP, y2: wireY });
      cursor += LAYOUT.WIRE_GAP;
    }
    const r = layoutOutput(output, cursor, wireY, rungIdx, i);
    nodes.push(...r.nodes);
    wires.push(...r.wires);
    cursor += r.width;
  });

  // Right-rail tap is added in layoutDiagram once the shared right-rail
  // x-coordinate is known across all rungs.
  const contentRight = probeOutputs.length > 0 ? cursor : logicEnd;
  const totalHeight = wireY + descent + LAYOUT.RUNG_PADDING_Y;

  return {
    nodes,
    wires,
    branchLines,
    wireY,
    contentRight,
    totalWidth: contentRight,
    totalHeight,
  };
}

/**
 * Compute layouts for all rungs in a diagram, stacked vertically.
 * Aligns every rung to a shared right rail x and emits the two long
 * vertical power rails so the renderer can draw them.
 *
 * `availableWidth`, when provided, lets rungs flex-grow to fill the
 * editor canvas: outputs justify against the right rail and the wire
 * between contacts and outputs stretches to fill the gap. Without it
 * (e.g. tests / static rendering) the layout falls back to the tight
 * legacy form.
 */
export function layoutDiagram(
  diagram: Diagram,
  availableWidth?: number,
): {
  rungs: { layout: RungLayout; yOffset: number }[];
  rails: { leftX: number; rightX: number; topY: number; bottomY: number };
  totalWidth: number;
  totalHeight: number;
} {
  // Probe pass: lay each rung out tight to discover its content right
  // edge. The next pass uses these to choose a shared rightX that all
  // rungs anchor their outputs against.
  const probes = diagram.rungs.map((r, i) => layoutRung(r, i));
  const maxContentRight = probes.reduce(
    (m, p) => (p.contentRight > m ? p.contentRight : m),
    0,
  );

  // Shared right rail. Padded so even an empty rung has a visible bus.
  // When availableWidth is provided, prefer the outer canvas edge so
  // outputs justify all the way right.
  const tightRightX = Math.max(maxContentRight, LAYOUT.RAIL_LEFT + 120) + LAYOUT.RAIL_RIGHT_MARGIN;
  const targetRightX = availableWidth !== undefined
    ? Math.max(tightRightX, availableWidth - LAYOUT.RAIL_RIGHT_MARGIN)
    : tightRightX;
  const rightX = targetRightX;
  const leftX = LAYOUT.RAIL_LEFT;

  // The actual rightmost edge of an output sits a small inset before the
  // right rail so the connecting wire is visible (the rail isn't drawn
  // through the output itself).
  const outputsRight = rightX - LAYOUT.WIRE_GAP;

  const rungs: { layout: RungLayout; yOffset: number }[] = [];
  let yOffset = 0;
  diagram.rungs.forEach((r, i) => {
    const layout = layoutRung(r, i, outputsRight);
    rungs.push({ layout, yOffset });
    yOffset += layout.totalHeight + LAYOUT.RUNG_GAP;
  });

  // Patch each rung's wire to extend from its content to the shared rail
  // and tag totalWidth so the SVG sizes correctly.
  for (const r of rungs) {
    r.layout.wires.push({
      x1: r.layout.contentRight,
      y1: r.layout.wireY,
      x2: rightX,
      y2: r.layout.wireY,
    });
    r.layout.totalWidth = rightX;
  }

  // Vertical rails span from the top of the first rung to the bottom of
  // the last (or a sensible default when there are no rungs).
  const topY = 0;
  const bottomY =
    rungs.length === 0
      ? 80
      : rungs[rungs.length - 1].yOffset + rungs[rungs.length - 1].layout.totalHeight;

  return {
    rungs,
    rails: { leftX, rightX, topY, bottomY },
    totalWidth: rightX,
    totalHeight: Math.max(bottomY, 80),
  };
}
