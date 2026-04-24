<script lang="ts">
  import ExpansionPanel from '$lib/components/ExpansionPanel.svelte';

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

<div class="rail" data-accent={accent}>
  <ExpansionPanel title={title} count={names.length || undefined}>
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
  </ExpansionPanel>
</div>

<style lang="scss">
  .rail {
    &[data-accent="app"] :global(.chip) { border-color: color-mix(in srgb, var(--theme-text) 35%, transparent); }
    &[data-accent="component"] :global(.chip) { border-color: color-mix(in srgb, var(--theme-text) 25%, transparent); border-style: dashed; }
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
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
