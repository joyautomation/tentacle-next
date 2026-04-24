<script lang="ts">
  import type { HmiWidget, HmiComponentConfig } from '$lib/types/hmi';
  import WidgetView from '../WidgetView.svelte';
  import { widgetClassString } from '../styles/styleContext';

  interface Props {
    widget: HmiWidget;
    udtContext?: { moduleId: string; udtVariable: string };
    components?: Record<string, HmiComponentConfig>;
  }

  let { widget, udtContext, components }: Props = $props();

  const p = $derived(widget.props ?? {});
  const direction = $derived((p.direction as string) ?? 'column');
  const gap = $derived(`${(p.gap as number) ?? 8}px`);
  const padding = $derived(`${(p.padding as number) ?? 8}px`);
  const align = $derived(mapAlign((p.align as string) ?? 'stretch'));
  const justify = $derived(mapJustify((p.justify as string) ?? 'start'));
  const wrap = $derived(p.wrap === 'yes' ? 'wrap' : 'nowrap');

  function mapAlign(v: string): string {
    if (v === 'start' || v === 'end') return `flex-${v}`;
    return v;
  }
  function mapJustify(v: string): string {
    if (v === 'start' || v === 'end') return `flex-${v}`;
    return v;
  }

  function childStyle(c: HmiWidget): string {
    const cp = c.props ?? {};
    const parts: string[] = [];
    const grow = cp.$grow;
    if (typeof grow === 'number' && grow > 0) parts.push(`flex-grow:${grow}`);
    const basis = cp.$basis as string | undefined;
    if (basis) parts.push(`flex-basis:${basis}`);
    const alignSelf = cp.$alignSelf as string | undefined;
    if (alignSelf) parts.push(`align-self:${mapAlign(alignSelf)}`);
    // Default: respect the child's intrinsic size (w/h) when present.
    if (!basis) {
      if (direction === 'row' && c.w) parts.push(`width:${c.w}px`);
      if (direction === 'column' && c.h) parts.push(`height:${c.h}px`);
    }
    if (direction === 'column' && c.w && align !== 'stretch') parts.push(`width:${c.w}px`);
    if (direction === 'row' && c.h && align !== 'stretch') parts.push(`height:${c.h}px`);
    return parts.join(';');
  }
</script>

<div
  class="stack"
  style:flex-direction={direction}
  style:gap
  style:padding
  style:align-items={align}
  style:justify-content={justify}
  style:flex-wrap={wrap}
>
  {#each widget.children ?? [] as child (child.id)}
    <div class="child {widgetClassString(child.props?.$classes)}" style={childStyle(child)}>
      <WidgetView widget={child} {udtContext} {components} />
    </div>
  {/each}
</div>

<style lang="scss">
  .stack {
    display: flex;
    width: 100%;
    height: 100%;
    box-sizing: border-box;
  }
  .child {
    min-width: 0;
    min-height: 0;
  }
</style>
