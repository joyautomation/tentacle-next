<script lang="ts">
  import { onDestroy } from 'svelte';
  import { compileComponent, mountComponent } from './svelteRuntime';
  import { injectMarkers } from './markupTools';

  interface Props {
    /** User markup. Re-compile + re-mount when this changes. */
    source: string;
    /** Auto-injected `<script>` body. Glued onto the markup before compile so
     * the user's source stays markup-only. */
    scriptHeader?: string;
    /** When true, every element open tag in the markup is augmented with a
     * `data-hmi-el="N"` attribute so a host can map DOM clicks back to
     * source positions. */
    markElements?: boolean;
    /** Reactive props passed through to the mounted component. Mutating the
     * input object triggers reactivity inside the component. */
    componentProps?: Record<string, unknown>;
    /** Debounce ms for re-compiling. 0 = compile immediately. */
    debounceMs?: number;
    /** Called once mount completes (or fails) for the latest source. */
    onStatus?: (s: { compiling: boolean; error: string | null }) => void;
  }

  let {
    source,
    scriptHeader,
    markElements = false,
    componentProps = {},
    debounceMs = 0,
    onStatus,
  }: Props = $props();

  function buildFullSource(): string {
    const markup = markElements ? injectMarkers(source) : source;
    if (!scriptHeader) return markup;
    return `<script>\n${scriptHeader}\n</` + `script>\n\n${markup}`;
  }

  let host: HTMLDivElement | undefined = $state();
  let unmount: (() => void) | null = null;
  // Live, reactive snapshot of props handed to the mounted component. We
  // mutate this object's keys so the component re-renders on prop changes
  // without us having to re-mount on every tick.
  let liveProps = $state<Record<string, unknown>>({});
  let pendingTimer: ReturnType<typeof setTimeout> | null = null;
  let lastCompiledSource = '';

  function syncProps(next: Record<string, unknown>) {
    // Add/update keys
    for (const k of Object.keys(next)) liveProps[k] = next[k];
    // Drop keys that disappeared
    for (const k of Object.keys(liveProps)) {
      if (!(k in next)) delete liveProps[k];
    }
  }

  async function recompile() {
    if (!host) return;
    onStatus?.({ compiling: true, error: null });
    if (unmount) {
      unmount();
      unmount = null;
    }
    host.innerHTML = '';
    if (!source.trim()) {
      onStatus?.({ compiling: false, error: null });
      return;
    }
    syncProps(componentProps);
    const fullSource = buildFullSource();
    const result = await compileComponent(fullSource);
    if (!result.ok) {
      onStatus?.({ compiling: false, error: result.error.message });
      return;
    }
    try {
      unmount = mountComponent(result.Component, host, liveProps);
      lastCompiledSource = fullSource;
      onStatus?.({ compiling: false, error: null });
    } catch (e: any) {
      onStatus?.({ compiling: false, error: `Mount failed: ${e?.message ?? String(e)}` });
    }
  }

  // Re-compile when the source / scriptHeader / marker mode changes (debounced).
  $effect(() => {
    void source;
    void scriptHeader;
    void markElements;
    const next = buildFullSource();
    if (next === lastCompiledSource) return;
    if (pendingTimer) clearTimeout(pendingTimer);
    if (debounceMs > 0) {
      pendingTimer = setTimeout(recompile, debounceMs);
    } else {
      recompile();
    }
  });

  // Push prop updates into the live, mounted component.
  $effect(() => {
    syncProps(componentProps);
  });

  onDestroy(() => {
    if (pendingTimer) clearTimeout(pendingTimer);
    unmount?.();
  });
</script>

<div class="svelte-host" bind:this={host}></div>

<style lang="scss">
  .svelte-host { width: 100%; height: 100%; }
</style>
