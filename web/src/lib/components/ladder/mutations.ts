/**
 * Diagram mutation helpers.
 *
 * All helpers are pure: they take a Diagram and return a new Diagram with
 * the requested change applied. Targeting is via EditPath (see types.ts).
 *
 * Logic-tree edits walk Series/Parallel.items by index. Output edits work
 * on the rung's `outputs` array directly. Mutations that don't apply
 * (e.g. setOperand on a Series node) return the diagram unchanged so the
 * caller can stay simple.
 */

import {
  type Coil,
  type Contact,
  type Diagram,
  type EditPath,
  type Element,
  type FBCall,
  type Output,
  type Parallel,
  type Rung,
  type Series,
} from './types.js';

// =============================================================================
// Read helpers — walk to the element identified by an EditPath.
// =============================================================================

export function getElementAt(diagram: Diagram, path: EditPath): Element | null {
  if (path.kind !== 'logic') return null;
  const rung = diagram.rungs[path.rung];
  if (!rung) return null;
  return walkLogic(rung.logic, path.logic);
}

export function getOutputAt(diagram: Diagram, path: EditPath): Output | null {
  if (path.kind !== 'output') return null;
  const rung = diagram.rungs[path.rung];
  return rung?.outputs?.[path.output] ?? null;
}

function walkLogic(root: Element, path: number[]): Element | null {
  let cur: Element = root;
  for (const idx of path) {
    if (cur.kind !== 'series' && cur.kind !== 'parallel') return null;
    cur = cur.items[idx];
    if (!cur) return null;
  }
  return cur;
}

// =============================================================================
// Write helpers — produce a new Diagram with one targeted change.
// =============================================================================

/** Replace the element at a logic path. Returns a new logic root. */
function replaceLogic(root: Element, path: number[], next: Element): Element {
  if (path.length === 0) return next;
  if (root.kind !== 'series' && root.kind !== 'parallel') {
    // Path overruns a leaf — caller is wrong; return root unchanged.
    return root;
  }
  const [head, ...rest] = path;
  const items = root.items.map((item, i) => (i === head ? replaceLogic(item, rest, next) : item));
  return root.kind === 'series'
    ? { kind: 'series', items }
    : { kind: 'parallel', items };
}

function updateRung(diagram: Diagram, rungIdx: number, fn: (r: Rung) => Rung): Diagram {
  if (rungIdx < 0 || rungIdx >= diagram.rungs.length) return diagram;
  const rungs = diagram.rungs.map((r, i) => (i === rungIdx ? fn(r) : r));
  return { ...diagram, rungs };
}

/**
 * Set the operand on the targeted Contact or Coil. Series/Parallel/FB
 * nodes are no-ops since they don't have a single operand.
 */
export function setOperand(diagram: Diagram, path: EditPath, operand: string): Diagram {
  if (path.kind === 'logic') {
    return updateRung(diagram, path.rung, r => {
      const target = walkLogic(r.logic, path.logic);
      if (!target || target.kind !== 'contact') return r;
      const next: Contact = { ...target, operand };
      return { ...r, logic: replaceLogic(r.logic, path.logic, next) };
    });
  }
  return updateRung(diagram, path.rung, r => {
    const out = r.outputs?.[path.output];
    if (!out || out.kind !== 'coil') return r;
    const next: Coil = { ...out, operand };
    const outputs = (r.outputs ?? []).map((o, i) => (i === path.output ? next : o));
    return { ...r, outputs };
  });
}

/**
 * Set the form (NO/NC for contacts, OTE/OTL/OTU for coils). Other
 * targets are no-ops.
 */
export function setForm(diagram: Diagram, path: EditPath, form: string): Diagram {
  if (path.kind === 'logic') {
    return updateRung(diagram, path.rung, r => {
      const target = walkLogic(r.logic, path.logic);
      if (!target || target.kind !== 'contact') return r;
      if (form !== 'NO' && form !== 'NC') return r;
      const next: Contact = { ...target, form };
      return { ...r, logic: replaceLogic(r.logic, path.logic, next) };
    });
  }
  return updateRung(diagram, path.rung, r => {
    const out = r.outputs?.[path.output];
    if (!out || out.kind !== 'coil') return r;
    if (form !== 'OTE' && form !== 'OTL' && form !== 'OTU') return r;
    const next: Coil = { ...out, form };
    const outputs = (r.outputs ?? []).map((o, i) => (i === path.output ? next : o));
    return { ...r, outputs };
  });
}

/**
 * Delete the element at the path. Logic deletions collapse: if the
 * containing Series/Parallel ends up with one child, the parent is
 * replaced by that child. Removing the rung's last logic leaves an
 * empty NO contact behind so the rung remains valid.
 */
export function deleteAtPath(diagram: Diagram, path: EditPath): Diagram {
  if (path.kind === 'output') {
    return updateRung(diagram, path.rung, r => {
      const outputs = (r.outputs ?? []).filter((_, i) => i !== path.output);
      return { ...r, outputs };
    });
  }
  if (path.logic.length === 0) {
    // Deleting the rung root → reset to placeholder contact.
    return updateRung(diagram, path.rung, r => ({
      ...r,
      logic: { kind: 'contact', form: 'NO', operand: '' },
    }));
  }
  return updateRung(diagram, path.rung, r => ({
    ...r,
    logic: removeFromGroup(r.logic, path.logic),
  }));
}

function removeFromGroup(root: Element, path: number[]): Element {
  // Walk to parent, drop child at last index, collapse single-child groups.
  if (root.kind !== 'series' && root.kind !== 'parallel') return root;
  if (path.length === 1) {
    const idx = path[0];
    const items = root.items.filter((_, i) => i !== idx);
    if (items.length === 1) return items[0];
    if (items.length === 0) {
      return { kind: 'contact', form: 'NO', operand: '' };
    }
    return root.kind === 'series'
      ? { kind: 'series', items }
      : { kind: 'parallel', items };
  }
  const [head, ...rest] = path;
  const items = root.items.map((item, i) =>
    i === head ? removeFromGroup(item, rest) : item,
  );
  return root.kind === 'series'
    ? { kind: 'series', items }
    : { kind: 'parallel', items };
}

/**
 * Append a new contact in series with the targeted element. If the
 * target's parent is already a Series, the contact is added as a sibling;
 * otherwise the target is wrapped in a Series with the new contact.
 */
export function appendContactInSeries(
  diagram: Diagram,
  path: EditPath,
  contact: Contact,
): Diagram {
  if (path.kind !== 'logic') return diagram;
  return updateRung(diagram, path.rung, r => {
    const target = walkLogic(r.logic, path.logic);
    if (!target) return r;
    // Special case: target is already a Series → append.
    if (target.kind === 'series') {
      const next: Series = { kind: 'series', items: [...target.items, contact] };
      return { ...r, logic: replaceLogic(r.logic, path.logic, next) };
    }
    // Otherwise wrap target in a new Series with the new contact.
    const next: Series = { kind: 'series', items: [target, contact] };
    return { ...r, logic: replaceLogic(r.logic, path.logic, next) };
  });
}

/**
 * Wrap the targeted element in a Parallel branch with an empty contact
 * sibling. Used to introduce an "OR" path next to existing logic.
 */
export function wrapInParallel(diagram: Diagram, path: EditPath): Diagram {
  if (path.kind !== 'logic') return diagram;
  return updateRung(diagram, path.rung, r => {
    const target = walkLogic(r.logic, path.logic);
    if (!target) return r;
    if (target.kind === 'parallel') {
      const next: Parallel = {
        kind: 'parallel',
        items: [...target.items, { kind: 'contact', form: 'NO', operand: '' }],
      };
      return { ...r, logic: replaceLogic(r.logic, path.logic, next) };
    }
    const next: Parallel = {
      kind: 'parallel',
      items: [target, { kind: 'contact', form: 'NO', operand: '' }],
    };
    return { ...r, logic: replaceLogic(r.logic, path.logic, next) };
  });
}
