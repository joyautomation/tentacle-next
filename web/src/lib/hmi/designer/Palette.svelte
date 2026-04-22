<script lang="ts">
  import { widgetSchemas, type WidgetSchema } from '../widgetSchema';

  function onDragStart(e: DragEvent, schema: WidgetSchema) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'copy';
    e.dataTransfer.setData('application/x-hmi-widget', schema.type);
  }
</script>

<aside class="palette">
  <h3>Widgets</h3>
  <ul>
    {#each widgetSchemas as schema (schema.type)}
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
  <p class="hint">Drag a widget onto the canvas to add it.</p>
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
  }
  .meta { display: flex; flex-direction: column; min-width: 0; }
  .name { font-size: 0.875rem; color: var(--theme-text); }
  .type { font-family: 'IBM Plex Mono', monospace; font-size: 0.6875rem; color: var(--theme-text-muted); }
  .hint { margin-top: 1rem; color: var(--theme-text-muted); font-size: 0.75rem; }
</style>
