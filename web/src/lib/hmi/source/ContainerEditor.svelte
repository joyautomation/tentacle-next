<script lang="ts">
  import { slide } from 'svelte/transition';
  import ExpansionPanel from '$lib/components/ExpansionPanel.svelte';

  interface Props {
    props: Record<string, string>;
    css: string;
    onChange: (next: { props: Record<string, string>; css: string }) => void;
  }

  let { props, css, onChange }: Props = $props();

  // `fixed` is intentionally omitted — it pins the wrapper to the viewport
  // and traps the editor UI behind it (no way back out). Components live
  // inside screens; their wrapper should never escape that context.
  const POSITION_OPTS = ['', 'static', 'relative', 'absolute', 'sticky'] as const;
  const DISPLAY_OPTS = ['', 'block', 'inline', 'inline-block', 'flex', 'inline-flex', 'grid', 'inline-grid', 'none'] as const;
  const FLEX_DIR_OPTS = ['', 'row', 'row-reverse', 'column', 'column-reverse'] as const;
  const FLEX_WRAP_OPTS = ['', 'nowrap', 'wrap', 'wrap-reverse'] as const;
  const JUSTIFY_OPTS = ['', 'flex-start', 'center', 'flex-end', 'space-between', 'space-around', 'space-evenly'] as const;
  const ALIGN_OPTS = ['', 'stretch', 'flex-start', 'center', 'flex-end', 'baseline'] as const;

  const isFlex = $derived(props.display === 'flex' || props.display === 'inline-flex');
  const isGrid = $derived(props.display === 'grid' || props.display === 'inline-grid');
  const isAbsolute = $derived(props.position === 'absolute');

  function setProp(key: string, value: string) {
    const next = { ...props };
    if (value === '') delete next[key];
    else next[key] = value;
    onChange({ props: next, css });
  }

  function setCss(value: string) {
    onChange({ props, css: value });
  }
</script>

<ExpansionPanel title="Container">
  <div class="body">
  <div class="row">
    <label for="ce-position">position</label>
    <select id="ce-position" value={props.position ?? ''} onchange={(e) => setProp('position', e.currentTarget.value)}>
      {#each POSITION_OPTS as opt}
        <option value={opt}>{opt || '— inherit —'}</option>
      {/each}
    </select>
  </div>

  {#if isAbsolute}
    <div class="subgroup" transition:slide={{ duration: 120 }}>
      <div class="subhdr">offsets</div>
      <div class="grid-2">
        <div class="row">
          <label for="ce-top">top</label>
          <input id="ce-top" type="text" value={props.top ?? ''} placeholder="0" oninput={(e) => setProp('top', e.currentTarget.value)} />
        </div>
        <div class="row">
          <label for="ce-right">right</label>
          <input id="ce-right" type="text" value={props.right ?? ''} placeholder="auto" oninput={(e) => setProp('right', e.currentTarget.value)} />
        </div>
        <div class="row">
          <label for="ce-bottom">bottom</label>
          <input id="ce-bottom" type="text" value={props.bottom ?? ''} placeholder="auto" oninput={(e) => setProp('bottom', e.currentTarget.value)} />
        </div>
        <div class="row">
          <label for="ce-left">left</label>
          <input id="ce-left" type="text" value={props.left ?? ''} placeholder="0" oninput={(e) => setProp('left', e.currentTarget.value)} />
        </div>
      </div>
    </div>
  {/if}

  <div class="row">
    <label for="ce-display">display</label>
    <select id="ce-display" value={props.display ?? ''} onchange={(e) => setProp('display', e.currentTarget.value)}>
      {#each DISPLAY_OPTS as opt}
        <option value={opt}>{opt || '— inherit —'}</option>
      {/each}
    </select>
  </div>

  {#if isFlex}
    <div class="subgroup" transition:slide={{ duration: 120 }}>
      <div class="subhdr">flex</div>
      <div class="row">
        <label for="ce-flex-dir">direction</label>
        <select id="ce-flex-dir" value={props['flex-direction'] ?? ''} onchange={(e) => setProp('flex-direction', e.currentTarget.value)}>
          {#each FLEX_DIR_OPTS as opt}
            <option value={opt}>{opt || '— default —'}</option>
          {/each}
        </select>
      </div>
      <div class="row">
        <label for="ce-flex-wrap">wrap</label>
        <select id="ce-flex-wrap" value={props['flex-wrap'] ?? ''} onchange={(e) => setProp('flex-wrap', e.currentTarget.value)}>
          {#each FLEX_WRAP_OPTS as opt}
            <option value={opt}>{opt || '— default —'}</option>
          {/each}
        </select>
      </div>
      <div class="row">
        <label for="ce-justify">justify</label>
        <select id="ce-justify" value={props['justify-content'] ?? ''} onchange={(e) => setProp('justify-content', e.currentTarget.value)}>
          {#each JUSTIFY_OPTS as opt}
            <option value={opt}>{opt || '— default —'}</option>
          {/each}
        </select>
      </div>
      <div class="row">
        <label for="ce-align">align</label>
        <select id="ce-align" value={props['align-items'] ?? ''} onchange={(e) => setProp('align-items', e.currentTarget.value)}>
          {#each ALIGN_OPTS as opt}
            <option value={opt}>{opt || '— default —'}</option>
          {/each}
        </select>
      </div>
      <div class="row">
        <label for="ce-gap">gap</label>
        <input id="ce-gap" type="text" value={props.gap ?? ''} placeholder="0.5rem" oninput={(e) => setProp('gap', e.currentTarget.value)} />
      </div>
    </div>
  {/if}

  {#if isGrid}
    <div class="subgroup" transition:slide={{ duration: 120 }}>
      <div class="subhdr">grid</div>
      <div class="row">
        <label for="ce-gtc">columns</label>
        <input id="ce-gtc" type="text" value={props['grid-template-columns'] ?? ''} placeholder="repeat(3, 1fr)" oninput={(e) => setProp('grid-template-columns', e.currentTarget.value)} />
      </div>
      <div class="row">
        <label for="ce-gtr">rows</label>
        <input id="ce-gtr" type="text" value={props['grid-template-rows'] ?? ''} placeholder="auto" oninput={(e) => setProp('grid-template-rows', e.currentTarget.value)} />
      </div>
      <div class="row">
        <label for="ce-grid-gap">gap</label>
        <input id="ce-grid-gap" type="text" value={props.gap ?? ''} placeholder="0.5rem" oninput={(e) => setProp('gap', e.currentTarget.value)} />
      </div>
    </div>
  {/if}

  <div class="freeform">
    <label for="ce-css">extra CSS</label>
    <textarea
      id="ce-css"
      spellcheck="false"
      placeholder={'padding: 0.5rem;\nbackground: #222;'}
      value={css}
      oninput={(e) => setCss(e.currentTarget.value)}
    ></textarea>
    <p class="hint">Applied as inline <code>style</code> on the wrapper.</p>
  </div>
  </div>
</ExpansionPanel>

<style lang="scss">
  .body {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .subgroup {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    padding: 0.5rem;
    border: 1px dashed var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
  }
  .subhdr {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.625rem;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .grid-2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.375rem;
    .row { grid-template-columns: 3.5rem 1fr; }
  }
  .row {
    display: grid;
    grid-template-columns: 5rem 1fr;
    align-items: center;
    gap: 0.5rem;
    label {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
      color: var(--theme-text-muted);
    }
    select, input[type="text"] {
      background: var(--theme-background);
      border: 1px solid var(--theme-border);
      color: var(--theme-text);
      padding: 0.25rem 0.375rem;
      border-radius: var(--rounded-sm, 4px);
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
      width: 100%;
      box-sizing: border-box;
      &:focus { outline: none; border-color: var(--theme-text); }
    }
  }
  .freeform {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    label {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
      color: var(--theme-text-muted);
    }
    textarea {
      width: 100%;
      min-height: 5rem;
      background: var(--theme-background);
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-sm, 4px);
      color: var(--theme-text);
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
      padding: 0.5rem;
      resize: vertical;
      line-height: 1.5;
      &:focus { outline: none; border-color: var(--theme-text); }
    }
  }
  .hint {
    margin: 0;
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    code { font-family: 'IBM Plex Mono', monospace; }
  }
</style>
