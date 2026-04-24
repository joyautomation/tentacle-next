<script lang="ts">
  interface Props {
    title: string;
    classes: Record<string, string> | undefined;
    accent: 'app' | 'component';
    /** Optional href to "Edit" link. */
    editHref?: string;
  }

  let { title, classes, accent, editHref }: Props = $props();

  const names = $derived(classes ? Object.keys(classes).sort() : []);

  function onDragStart(e: DragEvent, name: string) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'copy';
    e.dataTransfer.setData('application/x-hmi-class', JSON.stringify({ scope: accent, name }));
    e.dataTransfer.setData('text/plain', name);
  }
</script>

<details class="rail" data-accent={accent} open>
  <summary class="hdr"><span class="h3">{title}</span></summary>
  <div class="body">
    {#if editHref}
      <a href={editHref} target="_blank" rel="noopener" class="edit" title="Edit in App Styles">Edit ↗</a>
    {/if}
    {#if names.length === 0}
      <p class="muted">{editHref ? 'No app classes yet.' : 'No classes yet.'}</p>
    {:else}
      <ul class="chips">
        {#each names as name (name)}
          <li
            class="chip"
            draggable="true"
            ondragstart={(e) => onDragStart(e, name)}
            title={`Drag onto a widget to apply${classes?.[name] ? `\n\n${classes[name]}` : ''}`}
          >
            .{name}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</details>

<style lang="scss">
  .rail {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    overflow: hidden;
    &[data-accent="app"] .chip { border-color: color-mix(in srgb, var(--theme-text) 35%, transparent); }
    &[data-accent="component"] .chip { border-color: color-mix(in srgb, var(--theme-text) 25%, transparent); border-style: dashed; }
  }
  .hdr {
    list-style: none;
    cursor: pointer;
    padding: 0.5rem 0.75rem;
    user-select: none;
    display: flex;
    align-items: center;
    gap: 0.375rem;
    &::-webkit-details-marker { display: none; }
    &::before {
      content: '▸';
      font-size: 0.625rem;
      color: var(--theme-text-muted);
      transition: transform 0.12s ease;
    }
    .h3 { font-size: 0.6875rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--theme-text-muted); font-weight: 600; }
  }
  .rail[open] > .hdr::before { transform: rotate(90deg); }
  .body {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    padding: 0 0.75rem 0.625rem;
  }
  .edit { font-size: 0.6875rem; color: var(--theme-text-muted); text-decoration: none; &:hover { color: var(--theme-text); } }
  .muted { margin: 0; font-size: 0.6875rem; color: var(--theme-text-muted); }
  .chips { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 0.25rem; }
  .chip {
    display: inline-flex; align-items: center;
    padding: 0.125rem 0.5rem;
    border: 1px solid var(--theme-border);
    border-radius: 999px;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.6875rem;
    color: var(--theme-text);
    background: var(--theme-background);
    cursor: grab;
    user-select: none;
    &:active { cursor: grabbing; }
  }
</style>
