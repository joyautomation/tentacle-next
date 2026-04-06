<script lang="ts">
  import { LadderEditor } from '$lib/components/ladder/index.js';
  import type { LadderProgram, Rung } from '$lib/components/ladder/types.js';

  // Demo program — motor start/stop with seal-in and timer
  const demoRungs: Rung[] = [
    {
      id: 'rung_0',
      comment: 'Motor start/stop with seal-in',
      conditions: [
        { type: 'NO', tag: 'startButton' },
        {
          type: 'branch',
          paths: [
            { type: 'NC', tag: 'stopButton' },
            { type: 'NO', tag: 'motorRunning' },
          ],
        },
      ],
      outputs: [
        { type: 'OTE', tag: 'motorRunning' },
      ],
    },
    {
      id: 'rung_1',
      comment: 'Motor run timer',
      conditions: [
        { type: 'NO', tag: 'motorRunning' },
      ],
      outputs: [
        { type: 'TON', tag: 'runTimer', preset: 5000 },
        { type: 'OTE', tag: 'timerDone' },
      ],
    },
    {
      id: 'rung_2',
      comment: 'Alarm latch',
      conditions: [
        { type: 'NO', tag: 'highTemp' },
        { type: 'NC', tag: 'alarmAck' },
      ],
      outputs: [
        { type: 'OTL', tag: 'tempAlarm' },
      ],
    },
    {
      id: 'rung_3',
      comment: 'Alarm reset',
      conditions: [
        { type: 'NO', tag: 'alarmAck' },
      ],
      outputs: [
        { type: 'OTU', tag: 'tempAlarm' },
      ],
    },
  ];

  let program = $state<LadderProgram>({
    name: 'Motor Control',
    rungs: demoRungs,
  });

  function handleProgramChange(updated: LadderProgram) {
    program = updated;
  }

  // Generate TypeScript preview
  const tsPreview = $derived(generateTypeScript(program));

  function generateTypeScript(prog: LadderProgram): string {
    const lines: string[] = [
      `import { createLadderProgram, NO, NC, OTE, OTL, OTU, TON, TOF, CTU, CTD, branch, series } from '@tentacle/plc/ladder';`,
      '',
      `export const program = createLadderProgram(({ rung }) => {`,
    ];

    for (const r of prog.rungs) {
      if (r.comment) {
        lines.push(`  // ${r.comment}`);
      }
      const args: string[] = [];
      for (const c of r.conditions) {
        args.push(conditionToTs(c, 4));
      }
      for (const o of r.outputs) {
        args.push(outputToTs(o, 4));
      }
      lines.push(`  rung(`);
      for (let i = 0; i < args.length; i++) {
        lines.push(`    ${args[i]}${i < args.length - 1 ? ',' : ','}`);
      }
      lines.push(`  );`);
      lines.push('');
    }

    lines.push('});');
    return lines.join('\n');
  }

  function conditionToTs(c: import('$lib/components/ladder/types.js').LadderCondition, indent: number): string {
    switch (c.type) {
      case 'NO': return `NO('${c.tag}')`;
      case 'NC': return `NC('${c.tag}')`;
      case 'branch': {
        const paths = c.paths.map(p => conditionToTs(p, indent + 2));
        if (paths.length <= 2 && paths.every(p => p.length < 30)) {
          return `branch(${paths.join(', ')})`;
        }
        const pad = ' '.repeat(indent + 2);
        return `branch(\n${paths.map(p => `${pad}${p}`).join(',\n')},\n${' '.repeat(indent)})`;
      }
      case 'series': {
        const els = c.elements.map(e => conditionToTs(e, indent + 2));
        return `series(${els.join(', ')})`;
      }
    }
  }

  function outputToTs(o: import('$lib/components/ladder/types.js').LadderOutput, indent: number): string {
    switch (o.type) {
      case 'OTE': return `OTE('${o.tag}')`;
      case 'OTL': return `OTL('${o.tag}')`;
      case 'OTU': return `OTU('${o.tag}')`;
      case 'TON': return `TON('${o.tag}', ${o.preset})`;
      case 'TOF': return `TOF('${o.tag}', ${o.preset})`;
      case 'CTU': return `CTU('${o.tag}', ${o.preset})`;
      case 'CTD': return `CTD('${o.tag}', ${o.preset})`;
    }
  }
</script>

<div class="ladder-page">
  <div class="page-header">
    <h1>Ladder Logic Editor</h1>
    <span class="program-name">{program.name}</span>
  </div>

  <div class="editor-layout">
    <div class="editor-pane">
      <LadderEditor
        {program}
        onProgramChange={handleProgramChange}
      />
    </div>

    <div class="code-pane">
      <div class="code-header">
        <span class="code-title">Generated TypeScript</span>
        <span class="code-filename">{program.name.toLowerCase().replace(/\s+/g, '_')}.lad.ts</span>
      </div>
      <pre class="code-preview"><code>{tsPreview}</code></pre>
    </div>
  </div>
</div>

<style lang="scss">
  .ladder-page {
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    max-width: 100%;
  }

  .page-header {
    display: flex;
    align-items: baseline;
    gap: 1rem;

    h1 {
      font-size: 1.25rem;
      font-weight: 600;
      color: var(--theme-text, #ccc);
      margin: 0;
    }

    .program-name {
      font-size: 0.875rem;
      color: var(--theme-text-muted, #888);
      font-family: monospace;
    }
  }

  .editor-layout {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;
    min-height: 500px;

    @media (max-width: 1024px) {
      grid-template-columns: 1fr;
    }
  }

  .editor-pane {
    min-width: 0;
  }

  .code-pane {
    display: flex;
    flex-direction: column;
    background: var(--theme-surface, #1a1a1a);
    border: 1px solid var(--theme-border, #333);
    border-radius: var(--rounded-lg, 8px);
    overflow: hidden;
  }

  .code-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    background: var(--theme-neutral-200, #262626);
    border-bottom: 1px solid var(--theme-border, #333);
  }

  .code-title {
    font-size: 11px;
    font-weight: 600;
    color: var(--theme-text-muted, #888);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .code-filename {
    font-size: 11px;
    color: var(--color-cyan-400, #22d3ee);
    font-family: monospace;
  }

  .code-preview {
    flex: 1;
    margin: 0;
    padding: 12px;
    overflow: auto;
    font-size: 12px;
    line-height: 1.6;
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text, #ccc);
    background: transparent;

    code {
      white-space: pre;
    }
  }
</style>
