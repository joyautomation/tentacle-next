<script lang="ts">
  import type { HmiComponentConfig } from '$lib/types/hmi';
  import { widgetSchemas, type WidgetSchema } from '../widgetSchema';

  interface Props {
    /** Hide these widget types (e.g. 'componentInstance' inside the component editor). */
    excludeTypes?: string[];
    /** When provided, the palette also lists these components as draggable items
     * that drop a `componentInstance` widget pre-bound to the chosen componentId. */
    components?: HmiComponentConfig[];
  }

  let { excludeTypes = [], components = [] }: Props = $props();

  const visibleSchemas = $derived(widgetSchemas.filter((s) => !excludeTypes.includes(s.type)));

  function onDragStart(e: DragEvent, schema: WidgetSchema) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'copy';
    e.dataTransfer.setData('application/x-hmi-widget', schema.type);
  }

  function onDragStartComponent(e: DragEvent, c: HmiComponentConfig) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'copy';
    e.dataTransfer.setData('application/x-hmi-widget', 'componentInstance');
    e.dataTransfer.setData('application/x-hmi-component-id', c.componentId);
  }
</script>

<aside class="palette">
  <h3>Widgets</h3>
  <ul>
    {#each visibleSchemas as schema (schema.type)}
      <li
        class="palette-item"
        draggable="true"
        ondragstart={(e) => onDragStart(e, schema)}
        title="Drag onto the canvas"
      >
        <div class="ico" data-type={schema.type}></div>
        <div class="meta">
          <div class="name">{schema.label}</div>
          <div class="type">{schema.type}</div>
        </div>
      </li>
    {/each}
  </ul>

  {#if components.length > 0}
    <h3>Components</h3>
    <ul>
      {#each components as c (c.componentId)}
        <li
          class="palette-item"
          draggable="true"
          ondragstart={(e) => onDragStartComponent(e, c)}
          title={c.udtTemplate ? `Component bound to UDT ${c.udtTemplate}` : 'Component'}
        >
          <div class="ico component"></div>
          <div class="meta">
            <div class="name">{c.name}</div>
            <div class="type">{c.udtTemplate || 'freeform'}</div>
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  <p class="hint">Drag onto the canvas to add.</p>
</aside>

<style lang="scss">
  .palette {
    width: 14rem;
    flex-shrink: 0;
    padding: 1rem;
    border-right: 1px solid var(--theme-border);
    background: var(--theme-surface);
    overflow-y: auto;
  }
  h3 {
    margin: 0 0 0.75rem;
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
    & + ul { margin-bottom: 1rem; }
    &:not(:first-child) { margin-top: 0.25rem; }
  }
  ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.375rem; }
  .palette-item {
    display: flex;
    align-items: center;
    gap: 0.625rem;
    padding: 0.5rem 0.625rem;
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    cursor: grab;
    user-select: none;
    transition: border-color 0.1s;
    &:hover { border-color: var(--theme-text); }
    &:active { cursor: grabbing; }
  }
  .ico {
    width: 1.5rem;
    height: 1.5rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    background: var(--theme-surface);
    flex-shrink: 0;
    &[data-type="label"]::before { content: 'T'; display: flex; align-items: center; justify-content: center; height: 100%; font-weight: 600; color: var(--theme-text-muted); font-size: 0.875rem; }
    &[data-type="numeric"]::before { content: '#'; display: flex; align-items: center; justify-content: center; height: 100%; font-weight: 600; color: var(--theme-text-muted); font-size: 0.875rem; }
    &[data-type="indicator"]::before { content: '●'; display: flex; align-items: center; justify-content: center; height: 100%; color: var(--theme-text-muted); font-size: 0.75rem; }
    &[data-type="bar"]::before { content: '▮'; display: flex; align-items: center; justify-content: center; height: 100%; color: var(--theme-text-muted); font-size: 0.75rem; }
    &[data-type="componentInstance"]::before { content: '◧'; display: flex; align-items: center; justify-content: center; height: 100%; color: var(--theme-text-muted); font-size: 0.875rem; }
    &.component::before { content: '◧'; display: flex; align-items: center; justify-content: center; height: 100%; color: var(--theme-text-muted); font-size: 0.875rem; }
  }
  .meta { display: flex; flex-direction: column; min-width: 0; }
  .name { font-size: 0.875rem; color: var(--theme-text); }
  .type { font-family: 'IBM Plex Mono', monospace; font-size: 0.6875rem; color: var(--theme-text-muted); }
  .hint { margin-top: 1rem; color: var(--theme-text-muted); font-size: 0.75rem; }
</style>
