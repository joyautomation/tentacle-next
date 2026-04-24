<script lang="ts">
  import { slide } from 'svelte/transition';

  interface Props {
    classes: Record<string, string>;
    onChange: (next: Record<string, string>) => void;
    /** Visible label for "what scope are these?" — e.g. "App classes". */
    title?: string;
    /** Tinted background for the chip list, used to distinguish app vs component. */
    accent?: 'app' | 'component';
  }

  let { classes, onChange, title = 'Classes', accent = 'app' }: Props = $props();

  let selected = $state<string | null>(null);
  let renameDraft = $state<string>('');
  let renaming = $state(false);
  let newName = $state('');
  let addOpen = $state(false);

  const names = $derived(Object.keys(classes).sort());
  const body = $derived(selected ? (classes[selected] ?? '') : '');

  // Auto-select the first class once one exists.
  $effect(() => {
    if (!selected && names.length > 0) selected = names[0];
    if (selected && !classes[selected]) selected = names[0] ?? null;
  });

  function setBody(value: string) {
    if (!selected) return;
    onChange({ ...classes, [selected]: value });
  }

  function addClass(e: Event) {
    e.preventDefault();
    const name = newName.trim();
    if (!name) return;
    if (classes[name] !== undefined) {
      selected = name;
      newName = '';
      addOpen = false;
      return;
    }
    onChange({ ...classes, [name]: '' });
    selected = name;
    newName = '';
    addOpen = false;
  }

  function deleteClass(name: string) {
    if (!confirm(`Delete class .${name}?`)) return;
    const next = { ...classes };
    delete next[name];
    onChange(next);
    if (selected === name) selected = null;
  }

  function startRename(name: string) {
    renaming = true;
    renameDraft = name;
  }

  function commitRename() {
    if (!selected || !renaming) return;
    const target = renameDraft.trim();
    if (!target || target === selected) {
      renaming = false;
      return;
    }
    if (classes[target] !== undefined) {
      alert(`Class .${target} already exists.`);
      return;
    }
    const next: Record<string, string> = {};
    for (const [k, v] of Object.entries(classes)) {
      if (k === selected) next[target] = v;
      else next[k] = v;
    }
    onChange(next);
    selected = target;
    renaming = false;
  }

  function onDragStart(e: DragEvent, name: string, scope: 'app' | 'component') {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'copy';
    e.dataTransfer.setData('application/x-hmi-class', JSON.stringify({ scope, name }));
    // Also allow plain-text drop (debug/inspection).
    e.dataTransfer.setData('text/plain', name);
  }
</script>

<div class="class-editor" data-accent={accent}>
  <header class="hdr">
    <h3>{title}</h3>
    <button class="add" onclick={() => (addOpen = !addOpen)}>{addOpen ? '×' : '+ Add'}</button>
  </header>

  {#if addOpen}
    <form class="add-form" onsubmit={addClass} transition:slide={{ duration: 120 }}>
      <input
        type="text"
        placeholder="className (e.g. card, btn-primary)"
        bind:value={newName}
        autofocus
      />
      <button type="submit">Add</button>
    </form>
  {/if}

  {#if names.length === 0}
    <p class="muted">No classes yet. Add one above.</p>
  {:else}
    <ul class="chips">
      {#each names as name (name)}
        <li
          class="chip"
          class:selected={name === selected}
          draggable="true"
          ondragstart={(e) => onDragStart(e, name, accent)}
          onclick={() => (selected = name)}
          title="Drag onto a widget to apply"
          role="button"
          tabindex="0"
          onkeydown={(e) => { if (e.key === 'Enter') selected = name; }}
        >
          .{name}
        </li>
      {/each}
    </ul>
  {/if}

  {#if selected}
    <section class="body" transition:slide={{ duration: 120 }}>
      <header class="body-hdr">
        {#if renaming}
          <input
            class="rename"
            type="text"
            bind:value={renameDraft}
            onblur={commitRename}
            onkeydown={(e) => { if (e.key === 'Enter') commitRename(); if (e.key === 'Escape') renaming = false; }}
          />
        {:else}
          <button class="name-btn" onclick={() => startRename(selected!)} title="Rename">.{selected}</button>
        {/if}
        <button class="del" onclick={() => deleteClass(selected!)} title="Delete">Delete</button>
      </header>
      <textarea
        class="css"
        spellcheck="false"
        placeholder={'/* CSS rules — applied as-is */\nbackground: var(--theme-surface);\npadding: 0.75rem;'}
        value={body}
        oninput={(e) => setBody(e.currentTarget.value)}
      ></textarea>
      <p class="hint">Plain CSS. Properties only — selectors are wrapped automatically.</p>
    </section>
  {/if}
</div>

<style lang="scss">
  .class-editor {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.75rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);

    &[data-accent="app"] .chip { border-color: color-mix(in srgb, var(--theme-text) 35%, transparent); }
    &[data-accent="component"] .chip { border-color: color-mix(in srgb, var(--theme-text) 25%, transparent); border-style: dashed; }
  }
  .hdr {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    h3 { margin: 0; font-size: 0.6875rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--theme-text-muted); }
  }
  .add {
    background: transparent;
    border: 1px solid var(--theme-border);
    color: var(--theme-text);
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-sm, 4px);
    font-size: 0.75rem;
    cursor: pointer;
    &:hover { border-color: var(--theme-text); }
  }
  .add-form {
    display: flex;
    gap: 0.25rem;
    input {
      flex: 1;
      background: var(--theme-background);
      border: 1px solid var(--theme-border);
      color: var(--theme-text);
      padding: 0.375rem 0.5rem;
      border-radius: var(--rounded-sm, 4px);
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
    }
    button {
      background: var(--theme-text);
      color: var(--theme-background);
      border: 1px solid var(--theme-text);
      padding: 0.375rem 0.625rem;
      border-radius: var(--rounded-sm, 4px);
      font-size: 0.75rem;
      cursor: pointer;
    }
  }
  .muted { color: var(--theme-text-muted); margin: 0; font-size: 0.75rem; }
  .chips {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  .chip {
    display: inline-flex;
    align-items: center;
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
    &.selected { background: var(--theme-text); color: var(--theme-background); border-color: var(--theme-text); }
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    padding-top: 0.5rem;
    border-top: 1px solid var(--theme-border);
  }
  .body-hdr {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.5rem;
    .name-btn {
      background: transparent;
      border: none;
      color: var(--theme-text);
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.8125rem;
      cursor: text;
      padding: 0;
      &:hover { text-decoration: underline; }
    }
    .rename {
      flex: 1;
      background: var(--theme-background);
      border: 1px solid var(--theme-text);
      color: var(--theme-text);
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.8125rem;
      padding: 0.125rem 0.375rem;
      border-radius: var(--rounded-sm, 4px);
    }
    .del {
      background: transparent;
      border: 1px solid var(--theme-border);
      color: var(--theme-text-muted);
      padding: 0.125rem 0.5rem;
      border-radius: var(--rounded-sm, 4px);
      font-size: 0.6875rem;
      cursor: pointer;
      &:hover { color: #ef4444; border-color: #ef4444; }
    }
  }
  .css {
    width: 100%;
    min-height: 8rem;
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    color: var(--theme-text);
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    padding: 0.5rem;
    resize: vertical;
    line-height: 1.5;
    &:focus { outline: none; border-color: var(--theme-text); }
  }
  .hint { margin: 0; font-size: 0.6875rem; color: var(--theme-text-muted); }
</style>
