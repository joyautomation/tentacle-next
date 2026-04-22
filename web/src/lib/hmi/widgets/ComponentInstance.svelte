<script lang="ts">
  import type { HmiComponentConfig } from '$lib/types/hmi';
  import WidgetView from '../WidgetView.svelte';

  interface Props {
    /** Resolved component config (looked up by `componentId` in WidgetView). */
    component?: HmiComponentConfig;
    /** Resolved root binding value — gateway emits the full UDT object as the
     * variable value. We don't read it here; we just need {moduleId, udtVariable}
     * from the binding, which WidgetView passes via udtContext. */
    udtContext?: { moduleId: string; udtVariable: string };
    components?: Record<string, HmiComponentConfig>;
  }

  let { component, udtContext, components }: Props = $props();
</script>

{#if !component}
  <div class="missing">Component not found</div>
{:else}
  <div class="root">
    {#each component.widgets ?? [] as w (w.id)}
      <div
        class="slot"
        style:left="{w.x}px"
        style:top="{w.y}px"
        style:width="{w.w}px"
        style:height="{w.h}px"
      >
        <WidgetView widget={w} {udtContext} {components} />
      </div>
    {/each}
  </div>
{/if}

<style lang="scss">
  .root { position: relative; width: 100%; height: 100%; }
  .slot { position: absolute; }
  .missing {
    width: 100%; height: 100%;
    display: flex; align-items: center; justify-content: center;
    background: var(--theme-surface);
    border: 1px dashed var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace;
  }
</style>
