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
    enableVariableDrop?: boolean;
    flush?: boolean;
  }

  let { value = '', language = 'starlark', readonly = false, onchange, variableNames = [], enableVariableDrop = false, flush = false }: Props = $props();

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

  const VARIABLE_MIME = 'application/x-plc-variable';

  function formatVariableInsert(name: string, datatype: string | undefined): string {
    if (language === 'st') return name;
    const numericTypes = new Set(['int', 'int16', 'int32', 'uint16', 'uint32', 'float', 'float32', 'float64', 'double', 'number']);
    if (datatype && numericTypes.has(datatype.toLowerCase())) {
      return `get_num("${name}")`;
    }
    if (datatype?.toLowerCase() === 'bool' || datatype?.toLowerCase() === 'boolean') {
      return `get_bool("${name}")`;
    }
    return `get_var("${name}")`;
  }

  function getDropExtension() {
    return EditorView.domEventHandlers({
      dragover(event) {
        if (event.dataTransfer?.types.includes(VARIABLE_MIME)) {
          event.preventDefault();
          if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
        }
        return false;
      },
      drop(event, view) {
        const raw = event.dataTransfer?.getData(VARIABLE_MIME);
        if (!raw) return false;
        event.preventDefault();
        try {
          const payload = JSON.parse(raw) as { name: string; datatype?: string };
          if (!payload.name) return false;
          const insert = formatVariableInsert(payload.name, payload.datatype);
          const pos = view.posAtCoords({ x: event.clientX, y: event.clientY }) ?? view.state.selection.main.head;
          view.dispatch({
            changes: { from: pos, to: pos, insert },
            selection: { anchor: pos + insert.length }
          });
          view.focus();
        } catch {
          /* ignore malformed payload */
        }
        return true;
      }
    });
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
        ...(enableVariableDrop ? [getDropExtension()] : []),
        EditorView.updateListener.of((update) => {
          if (update.docChanged && !updating) {
            onchange?.(update.state.doc.toString());
          }
        }),
        EditorView.theme({
          '&': {
            fontSize: '13px',
            height: flush ? '100%' : 'auto',
            border: flush ? 'none' : '1px solid var(--theme-border)',
            borderRadius: flush ? '0' : 'var(--rounded-lg)',
            overflow: 'hidden',
          },
          '&.cm-focused': {
            outline: 'none',
            boxShadow: flush ? 'none' : 'inset 0 0 0 2px var(--theme-primary)',
          },
          '.cm-scroller': {
            fontFamily: "'IBM Plex Mono', monospace",
          },
          '.cm-gutters': {
            backgroundColor: 'var(--theme-surface)',
            borderRight: '1px solid var(--theme-border)',
          },
          '.cm-content': {
            minHeight: flush ? '0' : '300px',
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
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .code-editor-wrapper :global(.cm-editor) {
    flex: 1;
    min-height: 0;
  }
</style>
