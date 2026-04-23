<script lang="ts">
  import SvelteHost from './SvelteHost.svelte';
  import { setHmiStyleContext } from '../styles/styleContext';
  import { compileScopedCss } from '../styles/cssScope';

  interface Props {
    source: string;
    /** Props passed into the compiled component (e.g. `{ udt: {...} }`). */
    props?: Record<string, unknown>;
    /** App-wide CSS classes (selectors stay bare). */
    appClasses?: Record<string, string>;
    /** Component-private CSS classes (scoped under `prefix`). */
    componentClasses?: Record<string, string>;
    /** Selector prefix for component-scoped classes, e.g. `cmp-pump`. */
    prefix?: string;
    /** Debounce ms for recompiling on source changes. */
    debounceMs?: number;
  }

  let {
    source,
    props = {},
    appClasses,
    componentClasses,
    prefix = '',
    debounceMs = 300,
  }: Props = $props();

  let compiling = $state(false);
  let error = $state<string | null>(null);

  const css = $derived.by(() => {
    const parts: string[] = [];
    const a = compileScopedCss(appClasses, '');
    if (a) parts.push(a);
    if (prefix) {
      const c = compileScopedCss(componentClasses, prefix);
      if (c) parts.push(c);
    }
    return parts.join('\n\n');
  });

  // Make widget classes (if the user's source uses HMI widgets) resolve.
  $effect(() => {
    const ctx: any = { appClasses };
    if (prefix && componentClasses) {
      ctx.component = { prefix, classes: componentClasses };
    }
    setHmiStyleContext(ctx);
  });
</script>

{#if css}
  {@html `<style data-hmi-preview-classes>${css}</style>`}
{/if}

<div class="preview-shell">
  <div class="surface">
    <SvelteHost
      {source}
      componentProps={props}
      {debounceMs}
      onStatus={(s) => {
        compiling = s.compiling;
        error = s.error;
      }}
    />
  </div>
  {#if compiling}
    <div class="badge compiling">compiling…</div>
  {/if}
  {#if error}
    <div class="error">
      <strong>compile error</strong>
      <pre>{error}</pre>
    </div>
  {/if}
</div>

<style lang="scss">
  .preview-shell {
    position: relative;
    width: 100%;
    height: 100%;
    background:
      linear-gradient(to right, color-mix(in srgb, var(--theme-border) 40%, transparent) 1px, transparent 1px) 0 0 / 16px 16px,
      linear-gradient(to bottom, color-mix(in srgb, var(--theme-border) 40%, transparent) 1px, transparent 1px) 0 0 / 16px 16px,
      var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    overflow: auto;
  }
  .surface {
    width: 100%;
    height: 100%;
    box-sizing: border-box;
  }
  .badge {
    position: absolute;
    top: 0.5rem;
    right: 0.5rem;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-sm, 4px);
    font-size: 0.6875rem;
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text-muted);
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
  }
  .error {
    position: absolute;
    inset: auto 0.5rem 0.5rem 0.5rem;
    padding: 0.5rem 0.75rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    max-height: 50%;
    overflow: auto;
    strong { display: block; margin-bottom: 0.25rem; font-size: 0.6875rem; text-transform: uppercase; letter-spacing: 0.04em; }
    pre { margin: 0; white-space: pre-wrap; }
  }
</style>
