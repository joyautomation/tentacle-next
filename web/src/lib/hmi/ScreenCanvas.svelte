<script lang="ts">
  import type { HmiAppConfig, HmiScreenConfig, HmiComponentConfig } from '$lib/types/hmi';
  import { useLiveTags } from './tagStore.svelte';
  import WidgetView from './WidgetView.svelte';
  import { setHmiStyleContext, widgetClassString } from './styles/styleContext';
  import { compileScopedCss } from './styles/cssScope';

  interface Props {
    screen: HmiScreenConfig;
    components?: Record<string, HmiComponentConfig>;
    /** App-wide CSS classes — emitted once at the canvas root. */
    appClasses?: Record<string, string>;
  }

  let { screen, components, appClasses }: Props = $props();

  // Subscribe to live tag values for the lifetime of this canvas.
  useLiveTags();

  // Make app classes available to every nested widget without prop drilling.
  $effect(() => {
    setHmiStyleContext({ appClasses });
  });

  const width = $derived(screen.width && screen.width > 0 ? `${screen.width}px` : '100%');
  const height = $derived(screen.height && screen.height > 0 ? `${screen.height}px` : '600px');
  const css = $derived(compileScopedCss(appClasses, ''));
</script>

{#if css}
  {@html `<style data-hmi-app-classes>${css}</style>`}
{/if}

<div class="canvas" style:width style:height>
  {#each screen.widgets as widget (widget.id)}
    <div
      class="widget-slot {widgetClassString(widget.props?.$classes, { appClasses })}"
      style:left="{widget.x}px"
      style:top="{widget.y}px"
      style:width="{widget.w}px"
      style:height="{widget.h}px"
    >
      <WidgetView {widget} {components} />
    </div>
  {/each}
  {#if screen.widgets.length === 0}
    <div class="empty">This screen is empty. Use the API or builder to add widgets.</div>
  {/if}
</div>

<style lang="scss">
  .canvas {
    position: relative;
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: auto;
  }
  .widget-slot {
    position: absolute;
  }
  .empty {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--theme-text-muted);
    font-size: 0.875rem;
  }
</style>
