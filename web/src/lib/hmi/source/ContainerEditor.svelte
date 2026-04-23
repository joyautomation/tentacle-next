<script lang="ts">
  interface Props {
    props: Record<string, string>;
    css: string;
    onChange: (next: { props: Record<string, string>; css: string }) => void;
  }

  let { props, css, onChange }: Props = $props();

  const POSITION_OPTS = ['', 'static', 'relative', 'absolute', 'fixed', 'sticky'] as const;
  const DISPLAY_OPTS = ['', 'block', 'inline', 'inline-block', 'flex', 'inline-flex', 'grid', 'inline-grid', 'none'] as const;

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

<div class="container-editor">
  <header class="hdr"><h3>Container</h3></header>

  <div class="row">
    <label for="ce-position">position</label>
    <select id="ce-position" value={props.position ?? ''} onchange={(e) => setProp('position', e.currentTarget.value)}>
      {#each POSITION_OPTS as opt}
        <option value={opt}>{opt || '— inherit —'}</option>
      {/each}
    </select>
  </div>

  <div class="row">
    <label for="ce-display">display</label>
    <select id="ce-display" value={props.display ?? ''} onchange={(e) => setProp('display', e.currentTarget.value)}>
      {#each DISPLAY_OPTS as opt}
        <option value={opt}>{opt || '— inherit —'}</option>
      {/each}
    </select>
  </div>

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

<style lang="scss">
  .container-editor {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.75rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
  }
  .hdr h3 {
    margin: 0;
    font-size: 0.6875rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
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
    select {
      background: var(--theme-background);
      border: 1px solid var(--theme-border);
      color: var(--theme-text);
      padding: 0.25rem 0.375rem;
      border-radius: var(--rounded-sm, 4px);
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
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
