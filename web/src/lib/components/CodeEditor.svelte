<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection, rectangularSelection, crosshairCursor, dropCursor } from '@codemirror/view';
  import { EditorState, Compartment } from '@codemirror/state';
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
  import { syntaxHighlighting, defaultHighlightStyle, indentOnInput, bracketMatching, foldGutter, foldKeymap } from '@codemirror/language';
  import { closeBrackets, closeBracketsKeymap, autocompletion, completionKeymap } from '@codemirror/autocomplete';
  import { highlightSelectionMatches, searchKeymap } from '@codemirror/search';
  import { python } from '@codemirror/lang-python';
  import { oneDark } from '@codemirror/theme-one-dark';
  import { structuredText } from '$lib/lang/structured-text';
  import { createVarCompletion } from '$lib/editor/var-completion';
  import { getEffectiveTheme } from '../../routes/theme.svelte';

  interface Props {
    value: string;
    language?: 'python' | 'starlark' | 'st';
    readonly?: boolean;
    onchange?: (value: string) => void;
    variableNames?: string[];
  }

  let { value = '', language = 'starlark', readonly = false, onchange, variableNames = [] }: Props = $props();

  let container: HTMLDivElement;
  let view: EditorView | undefined;
  let themeCompartment = new Compartment();
  let readonlyCompartment = new Compartment();
  let autocompleteCompartment = new Compartment();
  let updating = false;

  function getThemeExtension() {
    const effective = getEffectiveTheme();
    return effective === 'themeDark' ? oneDark : [];
  }

  function getLanguageExtension() {
    if (language === 'st') return structuredText();
    return python();
  }

  function getAutocompleteExtension() {
    if (variableNames.length > 0) {
      return autocompletion({ override: [createVarCompletion(variableNames)] });
    }
    return autocompletion();
  }

  onMount(() => {
    const state = EditorState.create({
      doc: value,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        history(),
        foldGutter(),
        drawSelection(),
        dropCursor(),
        EditorState.allowMultipleSelections.of(true),
        indentOnInput(),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        bracketMatching(),
        closeBrackets(),
        rectangularSelection(),
        crosshairCursor(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        keymap.of([
          ...closeBracketsKeymap,
          ...defaultKeymap,
          ...searchKeymap,
          ...historyKeymap,
          ...foldKeymap,
          ...completionKeymap,
          indentWithTab,
        ]),
        getLanguageExtension(),
        themeCompartment.of(getThemeExtension()),
        readonlyCompartment.of(EditorState.readOnly.of(readonly)),
        autocompleteCompartment.of(getAutocompleteExtension()),
        EditorView.updateListener.of((update) => {
          if (update.docChanged && !updating) {
            onchange?.(update.state.doc.toString());
          }
        }),
        EditorView.theme({
          '&': {
            fontSize: '13px',
            border: '1px solid var(--theme-border)',
            borderRadius: 'var(--rounded-lg)',
            overflow: 'hidden',
          },
          '&.cm-focused': {
            outline: 'none',
            boxShadow: 'inset 0 0 0 2px var(--theme-primary)',
          },
          '.cm-scroller': {
            fontFamily: "'IBM Plex Mono', monospace",
          },
          '.cm-gutters': {
            backgroundColor: 'var(--theme-surface)',
            borderRight: '1px solid var(--theme-border)',
          },
          '.cm-content': {
            minHeight: '300px',
          },
        }),
      ],
    });

    view = new EditorView({ state, parent: container });

    const mediaQuery = globalThis.matchMedia?.('(prefers-color-scheme: dark)');
    mediaQuery?.addEventListener('change', updateTheme);

    return () => {
      mediaQuery?.removeEventListener('change', updateTheme);
    };
  });

  onDestroy(() => {
    view?.destroy();
  });

  function updateTheme() {
    if (!view) return;
    view.dispatch({
      effects: themeCompartment.reconfigure(getThemeExtension()),
    });
  }

  $effect(() => {
    if (!view) return;
    const current = view.state.doc.toString();
    if (value !== current) {
      updating = true;
      view.dispatch({
        changes: { from: 0, to: current.length, insert: value },
      });
      updating = false;
    }
  });

  $effect(() => {
    if (!view) return;
    view.dispatch({
      effects: readonlyCompartment.reconfigure(EditorState.readOnly.of(readonly)),
    });
  });

  $effect(() => {
    if (!view) return;
    void variableNames;
    view.dispatch({
      effects: autocompleteCompartment.reconfigure(getAutocompleteExtension()),
    });
  });

  $effect(() => {
    void getEffectiveTheme();
    updateTheme();
  });
</script>

<div class="code-editor-wrapper" bind:this={container}></div>

<style>
  .code-editor-wrapper {
    width: 100%;
  }
</style>
