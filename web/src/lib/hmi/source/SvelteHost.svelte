<script lang="ts">
  import { onDestroy } from 'svelte';
  import { compileComponent, mountComponent } from './svelteRuntime';

  interface Props {
    /** Svelte source. Re-compile + re-mount when this changes. */
    source: string;
    /** Reactive props passed through to the mounted component. Mutating the
     * input object triggers reactivity inside the component. */
    componentProps?: Record<string, unknown>;
    /** Debounce ms for re-compiling. 0 = compile immediately. */
    debounceMs?: number;
    /** Called once mount completes (or fails) for the latest source. */
    onStatus?: (s: { compiling: boolean; error: string | null }) => void;
  }

  let { source, componentProps = {}, debounceMs = 0, onStatus }: Props = $props();

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
    const result = await compileComponent(source);
    if (!result.ok) {
      onStatus?.({ compiling: false, error: result.error.message });
      return;
    }
    try {
      unmount = mountComponent(result.Component, host, liveProps);
      lastCompiledSource = source;
      onStatus?.({ compiling: false, error: null });
    } catch (e: any) {
      onStatus?.({ compiling: false, error: `Mount failed: ${e?.message ?? String(e)}` });
    }
  }

  // Re-compile when the source changes (debounced).
  $effect(() => {
    void source;
    if (source === lastCompiledSource) return;
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
