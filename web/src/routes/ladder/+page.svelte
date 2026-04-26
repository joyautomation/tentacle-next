<script lang="ts">
  import { LadderEditor } from '$lib/components/ladder/index.js';
  import type { Diagram } from '$lib/components/ladder/types.js';

  // Standalone preview/playground for the LAD editor. Used to iterate on
  // rendering and editing without the workspace shell. Not linked from
  // the main nav — open `/ladder` directly.
  let diagram = $state<Diagram>({
    name: 'motor',
    variables: [
      { name: 'start', type: 'BOOL', kind: 'global' },
      { name: 'stop', type: 'BOOL', kind: 'global' },
      { name: 'motor', type: 'BOOL', kind: 'global' },
      { name: 'latch', type: 'BOOL' },
      { name: 't1', type: 'TON' },
    ],
    rungs: [
      {
        comment: 'Self-latching motor with stop interlock',
        logic: {
          kind: 'series',
          items: [
            {
              kind: 'parallel',
              items: [
                { kind: 'contact', form: 'NO', operand: 'start' },
                { kind: 'contact', form: 'NO', operand: 'latch' },
              ],
            },
            { kind: 'contact', form: 'NC', operand: 'stop' },
          ],
        },
        outputs: [
          { kind: 'coil', form: 'OTE', operand: 'latch' },
          { kind: 'coil', form: 'OTE', operand: 'motor' },
        ],
      },
      {
        comment: 'Run-time accumulator',
        logic: { kind: 'contact', form: 'NO', operand: 'motor' },
        outputs: [
          {
            kind: 'fb',
            instance: 't1',
            inputs: { PT: { kind: 'time', raw: 'T#5s', ms: 5000 } },
          },
        ],
      },
      {
        comment: 'Alarm latch on timer expiry',
        logic: { kind: 'contact', form: 'NO', operand: 't1.Q' },
        outputs: [{ kind: 'coil', form: 'OTL', operand: 'alarm' }],
      },
    ],
  });

  function onChange(next: Diagram) {
    diagram = next;
  }
</script>

<svelte:head>
  <title>LAD Editor Preview</title>
</svelte:head>

<div class="page">
  <header>
    <h1>Ladder Diagram Editor</h1>
    <p class="hint">Standalone preview — not the workspace integration. Click rungs to select, use toolbar to edit.</p>
  </header>

  <div class="frame">
    <LadderEditor {diagram} {onChange} />
  </div>

  <details class="json-dump">
    <summary>Diagram JSON</summary>
    <pre>{JSON.stringify(diagram, null, 2)}</pre>
  </details>
</div>

<style lang="scss">
  .page {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 20px;
    height: 100vh;
    box-sizing: border-box;
    background: var(--theme-background, #111);
    color: var(--theme-text, #ddd);
  }

  header h1 {
    margin: 0;
    font-size: 18px;
  }

  .hint {
    color: var(--theme-text-muted, #888);
    font-size: 13px;
    margin: 4px 0 0;
  }

  .frame {
    flex: 1;
    min-height: 320px;
    display: flex;
  }

  .json-dump {
    background: var(--theme-surface, #181818);
    border: 1px solid var(--theme-border, #333);
    border-radius: 6px;
    padding: 8px 12px;

    summary {
      cursor: pointer;
      font-size: 12px;
      color: var(--theme-text-muted, #888);
    }

    pre {
      margin: 8px 0 0;
      font-size: 11px;
      max-height: 240px;
      overflow: auto;
      color: var(--theme-text, #ddd);
    }
  }
</style>
