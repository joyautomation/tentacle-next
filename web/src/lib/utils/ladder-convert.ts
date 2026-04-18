import type {
  LadderProgram,
  Rung,
  LadderCondition,
  LadderOutput,
  LadderContact,
  LadderBranch,
  LadderSeries,
  LadderCoil,
  LadderTimer,
  LadderCounter,
} from '$lib/components/ladder/types';

export interface GoLadderElement {
  type: string;
  tag?: string;
  preset?: number;
  children?: GoLadderElement[][];
}

export interface GoLadderRung {
  comment?: string;
  conditions: GoLadderElement[];
  outputs: GoLadderElement[];
}

export interface GoLadderProgram {
  rungs: GoLadderRung[];
}

const CONDITION_TYPES = new Set(['NO', 'NC', 'branch', 'series']);

function goToTsCondition(e: GoLadderElement): LadderCondition {
  if (e.type === 'NO' || e.type === 'NC') {
    return { type: e.type, tag: e.tag ?? '' } satisfies LadderContact;
  }
  if (e.type === 'branch') {
    const paths: LadderCondition[] = (e.children ?? []).map((path) => {
      if (path.length === 1) {
        return goToTsCondition(path[0]);
      }
      return {
        type: 'series' as const,
        elements: path.map(goToTsCondition),
      } satisfies LadderSeries;
    });
    return { type: 'branch', paths } satisfies LadderBranch;
  }
  if (e.type === 'series') {
    const elements = (e.children?.[0] ?? []).map(goToTsCondition);
    return { type: 'series', elements } satisfies LadderSeries;
  }
  return { type: 'NO', tag: e.tag ?? '' } satisfies LadderContact;
}

function goToTsOutput(e: GoLadderElement): LadderOutput {
  if (e.type === 'OTE' || e.type === 'OTL' || e.type === 'OTU') {
    return { type: e.type, tag: e.tag ?? '' } satisfies LadderCoil;
  }
  if (e.type === 'TON' || e.type === 'TOF') {
    return { type: e.type, tag: e.tag ?? '', preset: e.preset ?? 0 } satisfies LadderTimer;
  }
  if (e.type === 'CTU' || e.type === 'CTD') {
    return { type: e.type, tag: e.tag ?? '', preset: e.preset ?? 0 } satisfies LadderCounter;
  }
  return { type: 'OTE', tag: e.tag ?? '' } satisfies LadderCoil;
}

export function goToTsProgram(go: GoLadderProgram, name: string): LadderProgram {
  return {
    name,
    rungs: (go.rungs ?? []).map((r, i): Rung => ({
      id: `rung_${i}`,
      comment: r.comment ?? '',
      conditions: (r.conditions ?? []).map(goToTsCondition),
      outputs: (r.outputs ?? []).map(goToTsOutput),
    })),
  };
}

function tsToGoCondition(c: LadderCondition): GoLadderElement {
  if (c.type === 'NO' || c.type === 'NC') {
    return { type: c.type, tag: c.tag };
  }
  if (c.type === 'branch') {
    const children: GoLadderElement[][] = c.paths.map((path) => {
      if (path.type === 'series') {
        return path.elements.map(tsToGoCondition);
      }
      return [tsToGoCondition(path)];
    });
    return { type: 'branch', children };
  }
  if (c.type === 'series') {
    return { type: 'series', children: [c.elements.map(tsToGoCondition)] };
  }
  return { type: 'NO', tag: '' };
}

function tsToGoOutput(o: LadderOutput): GoLadderElement {
  if (o.type === 'OTE' || o.type === 'OTL' || o.type === 'OTU') {
    return { type: o.type, tag: o.tag };
  }
  if (o.type === 'TON' || o.type === 'TOF' || o.type === 'CTU' || o.type === 'CTD') {
    return { type: o.type, tag: o.tag, preset: o.preset };
  }
  return { type: 'OTE', tag: '' };
}

export function tsToGoProgram(ts: LadderProgram): GoLadderProgram {
  return {
    rungs: ts.rungs.map((r): GoLadderRung => ({
      comment: r.comment || undefined,
      conditions: r.conditions.map(tsToGoCondition),
      outputs: r.outputs.map(tsToGoOutput),
    })),
  };
}
