/**
 * Single-rung text DSL printer. Mirrors `internal/plc/lad/print.go` so
 * the per-rung edit textarea seeds with the same canonical form the
 * server emits for full diagrams.
 *
 * Keep precedence in sync: contact=100, series=2, parallel=1.
 */

import {
  type Coil,
  type Contact,
  type Element,
  type Expr,
  type FBCall,
  type Output,
  type Parallel,
  type Rung,
  type Series,
} from './types.js';

export function printRung(r: Rung): string {
  const parts = [`rung ${printElement(r.logic, 0)}`];
  for (const o of r.outputs ?? []) {
    parts.push(`-> ${printOutput(o)}`);
  }
  return parts.join(' ');
}

function elementPrec(e: Element): number {
  switch (e.kind) {
    case 'contact':
      return 100;
    case 'series':
      return 2;
    case 'parallel':
      return 1;
  }
}

function printElement(e: Element, parentPrec: number): string {
  const inner = printElementInner(e);
  return elementPrec(e) < parentPrec ? `(${inner})` : inner;
}

function printElementInner(e: Element): string {
  switch (e.kind) {
    case 'contact':
      return printContact(e as Contact);
    case 'series':
      return (e as Series).items.map((it) => printElement(it, 2)).join(' & ');
    case 'parallel':
      return (e as Parallel).items.map((it) => printElement(it, 1)).join(' | ');
  }
}

function printContact(c: Contact): string {
  return `${c.form}(${c.operand})`;
}

function printOutput(o: Output): string {
  switch (o.kind) {
    case 'coil':
      return printCoil(o as Coil);
    case 'fb':
      return printFB(o as FBCall);
  }
}

function printCoil(c: Coil): string {
  return `${c.form}(${c.operand})`;
}

function printFB(fb: FBCall): string {
  const head = fb.powerInput ? `${fb.instance}@${fb.powerInput}` : fb.instance;
  if (!fb.inputs || Object.keys(fb.inputs).length === 0) {
    return `${head}()`;
  }
  const keys = Object.keys(fb.inputs).sort();
  const args = keys.map((k) => `${k} := ${printExpr(fb.inputs![k])}`).join(', ');
  return `${head}(${args})`;
}

function printExpr(e: Expr): string {
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
      return `'${e.value.replace(/'/g, "\\'")}'`;
  }
}
