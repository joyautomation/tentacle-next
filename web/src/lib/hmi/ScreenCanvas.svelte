<script lang="ts">
  import type { HmiScreenConfig } from '$lib/types/hmi';
  import { useLiveTags } from './tagStore.svelte';
  import WidgetView from './WidgetView.svelte';

  interface Props {
    screen: HmiScreenConfig;
  }

  let { screen }: Props = $props();

  // Subscribe to live tag values for the lifetime of this canvas.
  useLiveTags();

  const width = $derived(screen.width && screen.width > 0 ? `${screen.width}px` : '100%');
  const height = $derived(screen.height && screen.height > 0 ? `${screen.height}px` : '600px');
</script>

<div class="canvas" style:width style:height>
  {#each screen.widgets as widget (widget.id)}
    <div
      class="widget-slot"
      style:left="{widget.x}px"
      style:top="{widget.y}px"
      style:width="{widget.w}px"
      style:height="{widget.h}px"
    >
      <WidgetView {widget} />
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
