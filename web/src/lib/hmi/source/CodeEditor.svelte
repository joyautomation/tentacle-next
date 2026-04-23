<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { EditorState, Compartment } from '@codemirror/state';
  import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter } from '@codemirror/view';
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
  import { html } from '@codemirror/lang-html';
  import { syntaxHighlighting, defaultHighlightStyle, bracketMatching, indentOnInput } from '@codemirror/language';
  import { closeBrackets } from '@codemirror/autocomplete';

  interface Props {
    value: string;
    onChange: (value: string) => void;
    placeholder?: string;
  }

  let { value, onChange, placeholder = '' }: Props = $props();

  let host: HTMLDivElement | undefined = $state();
  let view: EditorView | null = null;
  let updatingFromExternal = false;

  /** Insert text at the current cursor position. Exposed for the palette. */
  export function insertAtCursor(text: string) {
    if (!view) return;
    const { from, to } = view.state.selection.main;
    view.dispatch({
      changes: { from, to, insert: text },
      selection: { anchor: from + text.length },
    });
    view.focus();
  }

  onMount(() => {
    if (!host) return;
    const updateListener = EditorView.updateListener.of((u) => {
      if (!u.docChanged || updatingFromExternal) return;
      onChange(u.state.doc.toString());
    });
    const state = EditorState.create({
      doc: value,
      extensions: [
        lineNumbers(),
        highlightActiveLine(),
        highlightActiveLineGutter(),
        history(),
        bracketMatching(),
        closeBrackets(),
        indentOnInput(),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        html({ matchClosingTags: true, autoCloseTags: true }),
        keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap]),
        EditorView.theme({
          '&': { height: '100%', fontSize: '13px', backgroundColor: 'transparent' },
          '.cm-scroller': { fontFamily: "'IBM Plex Mono', monospace" },
          '.cm-content': { padding: '0.5rem 0' },
          '.cm-gutters': { backgroundColor: 'transparent', border: 'none' },
        }),
        updateListener,
      ],
    });
    view = new EditorView({ state, parent: host });
  });

  onDestroy(() => view?.destroy());

  $effect(() => {
    if (!view) return;
    if (value !== view.state.doc.toString()) {
      updatingFromExternal = true;
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: value },
      });
      updatingFromExternal = false;
    }
  });
</script>

<div class="editor" bind:this={host}>
  {#if !value && placeholder}
    <div class="placeholder">{placeholder}</div>
  {/if}
</div>

<style lang="scss">
  .editor {
    position: relative;
    height: 100%;
    width: 100%;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    overflow: hidden;
    :global(.cm-editor) { outline: none; }
    :global(.cm-editor.cm-focused) { outline: none; }
  }
  .placeholder {
    position: absolute;
    inset: 0.5rem 0.5rem auto 3rem;
    color: var(--theme-text-muted);
    font-size: 0.8125rem;
    font-family: 'IBM Plex Mono', monospace;
    pointer-events: none;
  }
</style>
