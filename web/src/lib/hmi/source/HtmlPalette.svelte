<script lang="ts">
  interface Snippet {
    label: string;
    snippet: string;
  }

  interface Props {
    /** Called with the markup snippet to insert at the editor's cursor. */
    onInsert: (snippet: string) => void;
  }

  let { onInsert }: Props = $props();

  // Each entry's `snippet` is the literal HTML pasted at the cursor. Keep
  // them small — control flow and reactivity is up to the user.
  const groups: { name: string; items: Snippet[] }[] = [
    {
      name: 'Layout',
      items: [
        { label: 'div', snippet: '<div></div>' },
        { label: 'section', snippet: '<section></section>' },
        { label: 'article', snippet: '<article></article>' },
        { label: 'header', snippet: '<header></header>' },
        { label: 'footer', snippet: '<footer></footer>' },
        { label: 'main', snippet: '<main></main>' },
        { label: 'aside', snippet: '<aside></aside>' },
        { label: 'nav', snippet: '<nav></nav>' },
      ],
    },
    {
      name: 'Text',
      items: [
        { label: 'h1', snippet: '<h1></h1>' },
        { label: 'h2', snippet: '<h2></h2>' },
        { label: 'h3', snippet: '<h3></h3>' },
        { label: 'p', snippet: '<p></p>' },
        { label: 'span', snippet: '<span></span>' },
        { label: 'strong', snippet: '<strong></strong>' },
        { label: 'em', snippet: '<em></em>' },
        { label: 'small', snippet: '<small></small>' },
        { label: 'code', snippet: '<code></code>' },
      ],
    },
    {
      name: 'Lists',
      items: [
        { label: 'ul', snippet: '<ul>\n  <li></li>\n</ul>' },
        { label: 'ol', snippet: '<ol>\n  <li></li>\n</ol>' },
        { label: 'li', snippet: '<li></li>' },
      ],
    },
    {
      name: 'Form',
      items: [
        { label: 'button', snippet: '<button></button>' },
        { label: 'input', snippet: '<input type="text" />' },
        { label: 'label', snippet: '<label></label>' },
        { label: 'select', snippet: '<select>\n  <option></option>\n</select>' },
        { label: 'textarea', snippet: '<textarea></textarea>' },
      ],
    },
    {
      name: 'Media',
      items: [
        { label: 'img', snippet: '<img src="" alt="" />' },
        { label: 'svg', snippet: '<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">\n  \n</svg>' },
        { label: 'a', snippet: '<a href=""></a>' },
      ],
    },
    {
      name: 'Svelte',
      items: [
        { label: '{expr}', snippet: '{}' },
        { label: '{#if}', snippet: '{#if }\n  \n{/if}' },
        { label: '{#each}', snippet: '{#each items as item (item.id)}\n  \n{/each}' },
        { label: 'class:', snippet: 'class:active={cond}' },
        { label: 'style:', snippet: 'style:color={value}' },
        { label: 'onclick', snippet: 'onclick={() => {}}' },
      ],
    },
  ];

  function onDragStart(e: DragEvent, snippet: string) {
    if (!e.dataTransfer) return;
    e.dataTransfer.setData('text/plain', snippet);
    e.dataTransfer.effectAllowed = 'copy';
  }
</script>

<aside class="palette">
  {#each groups as group (group.name)}
    <div class="group">
      <div class="group-name">{group.name}</div>
      <div class="items">
        {#each group.items as item (item.label)}
          <button
            type="button"
            class="item"
            onclick={() => onInsert(item.snippet)}
            ondragstart={(e) => onDragStart(e, item.snippet)}
            draggable="true"
            title={item.snippet}
          >
            {item.label}
          </button>
        {/each}
      </div>
    </div>
  {/each}
</aside>

<style lang="scss">
  .palette {
    width: 12rem;
    flex-shrink: 0;
    border-right: 1px solid var(--theme-border);
    background: var(--theme-surface);
    overflow-y: auto;
    padding: 0.5rem;
  }
  .group { margin-bottom: 0.75rem; }
  .group-name {
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    margin-bottom: 0.25rem;
    padding: 0 0.25rem;
  }
  .items {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  .item {
    flex: 0 0 auto;
    background: var(--theme-background);
    color: var(--theme-text);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    padding: 0.1875rem 0.5rem;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    cursor: grab;
    &:hover {
      border-color: var(--theme-text);
      background: var(--theme-surface);
    }
    &:active { cursor: grabbing; }
  }
</style>
