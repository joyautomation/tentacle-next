<script lang="ts">
  import type { HmiWidget } from '$lib/types/hmi';
  import { tagStore } from './tagStore.svelte';
  import Label from './widgets/Label.svelte';
  import NumericDisplay from './widgets/NumericDisplay.svelte';
  import Indicator from './widgets/Indicator.svelte';
  import Bar from './widgets/Bar.svelte';

  interface Props {
    widget: HmiWidget;
    /** When this widget renders inside a UDT-typed component, supplies the
     * resolved instance so member-only bindings can find their data. */
    udtContext?: { moduleId: string; udtVariable: string };
  }

  let { widget, udtContext }: Props = $props();

  // Resolve all bindings into props each render. Keep widget props as base,
  // then overlay binding-resolved values.
  const resolved = $derived.by(() => {
    const out: Record<string, unknown> = { ...(widget.props ?? {}) };
    if (widget.bindings) {
      for (const [propName, binding] of Object.entries(widget.bindings)) {
        out[propName] = tagStore.resolve(binding, udtContext);
      }
    }
    return out;
  });

  const componentMap: Record<string, any> = {
    label: Label,
    numeric: NumericDisplay,
    indicator: Indicator,
    bar: Bar,
  };

  const Component = $derived(componentMap[widget.type]);
</script>

{#if Component}
  <Component {...resolved} />
{:else}
  <div class="unknown">unknown widget: {widget.type}</div>
{/if}

<style lang="scss">
  .unknown {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--theme-surface);
    border: 1px dashed var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace;
  }
</style>
