<script lang="ts">
  import type { HmiWidget, HmiComponentConfig } from '$lib/types/hmi';
  import { tagStore } from './tagStore.svelte';
  import Label from './widgets/Label.svelte';
  import NumericDisplay from './widgets/NumericDisplay.svelte';
  import Indicator from './widgets/Indicator.svelte';
  import Bar from './widgets/Bar.svelte';
  import ComponentInstance from './widgets/ComponentInstance.svelte';

  interface Props {
    widget: HmiWidget;
    /** When this widget renders inside a UDT-typed component, supplies the
     * resolved instance so member-only bindings can find their data. */
    udtContext?: { moduleId: string; udtVariable: string };
    /** App-level component templates, needed to render `componentInstance`. */
    components?: Record<string, HmiComponentConfig>;
  }

  let { widget, udtContext, components }: Props = $props();

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

  // For componentInstance: resolve component template + derive nested udtContext
  // from the `root` binding (which points at a gateway/UDT instance variable).
  const instanceComponent = $derived(
    widget.type === 'componentInstance'
      ? components?.[(widget.props?.componentId as string) ?? '']
      : undefined,
  );
  const nestedUdtContext = $derived.by<{ moduleId: string; udtVariable: string } | undefined>(() => {
    if (widget.type !== 'componentInstance') return undefined;
    const root = widget.bindings?.root;
    if (!root) return undefined;
    if (root.kind === 'variable' && root.gateway && root.variable) {
      return { moduleId: root.gateway, udtVariable: root.variable };
    }
    if (root.kind === 'udtMember' && root.gateway && root.udtVariable) {
      return { moduleId: root.gateway, udtVariable: root.udtVariable };
    }
    return undefined;
  });
</script>

{#if widget.type === 'componentInstance'}
  <ComponentInstance component={instanceComponent} udtContext={nestedUdtContext} {components} />
{:else if Component}
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
