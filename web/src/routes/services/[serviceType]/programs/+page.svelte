<script lang="ts">
  import type { PageData } from './$types';
  import type { ProgramListItem } from './+page';
  import type { LadderProgram } from '$lib/components/ladder/types';
  import type { GoLadderProgram } from '$lib/utils/ladder-convert';
  import { api, apiPut, apiPost, apiDelete } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { slide } from 'svelte/transition';
  import { ChevronRight } from '@joyautomation/salt/icons';
  import { LadderEditor } from '$lib/components/ladder/index.js';
  import CodeEditor from '$lib/components/CodeEditor.svelte';
  import { goToTsProgram, tsToGoProgram } from '$lib/utils/ladder-convert';

  interface PlcProgramKV {
    name: string;
    language: string;
    source: string;
    stSource?: string;
    updatedAt: number;
    updatedBy?: string;
  }

  interface TranspileResponse {
    starlark: string;
    vars: { name: string; datatype: string }[];
  }

  let { data }: { data: PageData } = $props();

  let showAddForm = $state(false);
  let saving = $state(false);
  let expandedProgram: string | null = $state(null);
  let deleteTarget: string | null = $state(null);

  let newName = $state('');
  let newLanguage = $state<'starlark' | 'st' | 'ladder'>('starlark');

  let editSource = $state('');
  let editStSource = $state('');
  let ladderProgram = $state<LadderProgram | null>(null);
  let ladderLoading = $state(false);

  let transpileResult = $state<TranspileResponse | null>(null);
  let showTranspile = $state(false);
  let transpiling = $state(false);

  const programList = $derived(
    (data.programs ?? []).slice().sort((a: ProgramListItem, b: ProgramListItem) => a.name.localeCompare(b.name))
  );

  const canAdd = $derived(newName.trim() !== '' && !saving);

  function resetAddForm() {
    newName = '';
    newLanguage = 'starlark';
  }

  function formatTime(ts: number): string {
    if (!ts) return '';
    const d = new Date(ts * 1000);
    const now = Date.now();
    const diff = now - d.getTime();
    if (diff < 60_000) return 'just now';
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
    if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
    return d.toLocaleDateString();
  }

  async function addProgram() {
    if (!canAdd) return;
    saving = true;
    try {
      const name = newName.trim();
      let source = '';
      if (newLanguage === 'starlark') {
        source = 'def main():\n    pass\n';
      } else if (newLanguage === 'ladder') {
        source = 'def main():\n    pass\n';
      } else {
        source = '';
      }

      const body: Record<string, unknown> = {
        name,
        language: newLanguage,
        source,
        updatedBy: 'gui',
      };
      if (newLanguage === 'st') {
        body.stSource = '';
        body.source = '';
      }

      const result = await apiPut(`/plcs/plc/programs/${encodeURIComponent(name)}`, body);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Program "${name}" created`, type: 'success' });
        showAddForm = false;
        resetAddForm();
        await invalidateAll();
        expandProgram({ name, language: newLanguage, updatedAt: 0 } as ProgramListItem);
      }
    } finally {
      saving = false;
    }
  }

  async function expandProgram(prog: ProgramListItem) {
    if (expandedProgram === prog.name) {
      expandedProgram = null;
      ladderProgram = null;
      transpileResult = null;
      showTranspile = false;
      return;
    }

    expandedProgram = prog.name;
    editSource = '';
    editStSource = '';
    ladderProgram = null;
    transpileResult = null;
    showTranspile = false;

    const result = await api<PlcProgramKV>(`/plcs/plc/programs/${encodeURIComponent(prog.name)}`);
    if (result.error) {
      saltState.addNotification({ message: result.error.error, type: 'error' });
      return;
    }

    const full = result.data;
    editSource = full.source ?? '';
    editStSource = full.stSource ?? '';

    if (prog.language === 'ladder' && full.source) {
      ladderLoading = true;
      try {
        const parseResult = await apiPost<GoLadderProgram>(`/plcs/plc/programs/ladder/parse`, { source: full.source });
        if (parseResult.error) {
          saltState.addNotification({ message: `Ladder parse: ${parseResult.error.error}`, type: 'error' });
        } else {
          ladderProgram = goToTsProgram(parseResult.data, prog.name);
        }
      } finally {
        ladderLoading = false;
      }
    } else if (prog.language === 'ladder') {
      ladderProgram = { name: prog.name, rungs: [] };
    }
  }

  function handleLadderChange(updated: LadderProgram) {
    ladderProgram = updated;
  }

  async function saveProgram(prog: ProgramListItem) {
    saving = true;
    try {
      let source = editSource;
      let stSource: string | undefined;

      if (prog.language === 'ladder' && ladderProgram) {
        const goProgram = tsToGoProgram(ladderProgram);
        const genResult = await apiPost<{ source: string }>(`/plcs/plc/programs/ladder/generate`, goProgram);
        if (genResult.error) {
          saltState.addNotification({ message: `Ladder generate: ${genResult.error.error}`, type: 'error' });
          return;
        }
        source = genResult.data.source;
      } else if (prog.language === 'st') {
        stSource = editStSource;
        if (editStSource.trim()) {
          const transpileRes = await apiPost<TranspileResponse>(`/plcs/plc/programs/transpile`, { source: editStSource });
          if (transpileRes.error) {
            saltState.addNotification({ message: `Transpile: ${transpileRes.error.error}`, type: 'error' });
            return;
          }
          source = transpileRes.data.starlark;
        }
      }

      const body: Record<string, unknown> = {
        name: prog.name,
        language: prog.language,
        source,
        updatedBy: 'gui',
      };
      if (stSource !== undefined) {
        body.stSource = stSource;
      }

      const result = await apiPut(`/plcs/plc/programs/${encodeURIComponent(prog.name)}`, body);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Program "${prog.name}" saved`, type: 'success' });
        await invalidateAll();
      }
    } finally {
      saving = false;
    }
  }

  async function previewTranspile() {
    if (!editStSource.trim()) return;
    transpiling = true;
    try {
      const result = await apiPost<TranspileResponse>(`/plcs/plc/programs/transpile`, { source: editStSource });
      if (result.error) {
        saltState.addNotification({ message: `Transpile: ${result.error.error}`, type: 'error' });
      } else {
        transpileResult = result.data;
        showTranspile = true;
      }
    } finally {
      transpiling = false;
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    saving = true;
    try {
      const result = await apiDelete(`/plcs/plc/programs/${encodeURIComponent(deleteTarget)}`);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Program "${deleteTarget}" deleted`, type: 'success' });
        if (expandedProgram === deleteTarget) {
          expandedProgram = null;
          ladderProgram = null;
        }
        deleteTarget = null;
        await invalidateAll();
      }
    } finally {
      saving = false;
    }
  }

  function languageBadgeClass(lang: string): string {
    if (lang === 'ladder') return 'lang-badge ladder';
    if (lang === 'st') return 'lang-badge st';
    return 'lang-badge starlark';
  }

  function languageLabel(lang: string): string {
    if (lang === 'st') return 'Structured Text';
    if (lang === 'ladder') return 'Ladder';
    return 'Starlark';
  }
</script>

<div class="programs-page">
  {#if data.error}
    <div class="error-box"><p>{data.error}</p></div>
  {/if}

  <div class="section-header">
    <h2>Programs <span class="count-badge">{programList.length}</span></h2>
    <button class="add-btn" onclick={() => { showAddForm = !showAddForm; if (showAddForm) resetAddForm(); }}>
      {showAddForm ? 'Cancel' : '+ Add Program'}
    </button>
  </div>

  {#if showAddForm}
    <div class="add-form" transition:slide>
      <div class="form-row">
        <label for="new-name">Name</label>
        <input id="new-name" type="text" bind:value={newName} placeholder="e.g. MainProgram" />
      </div>
      <div class="form-row">
        <label for="new-lang">Language</label>
        <select id="new-lang" bind:value={newLanguage}>
          <option value="starlark">Starlark</option>
          <option value="st">Structured Text</option>
          <option value="ladder">Ladder</option>
        </select>
      </div>
      <div class="form-actions">
        <button class="save-btn" disabled={!canAdd} onclick={addProgram}>
          {saving ? 'Creating...' : 'Create Program'}
        </button>
      </div>
    </div>
  {/if}

  {#if programList.length === 0 && !showAddForm}
    <div class="empty-state">
      <p>No programs yet.</p>
      <p class="muted">Programs contain the logic executed by tasks.</p>
    </div>
  {:else}
    <div class="tree">
      {#each programList as prog (prog.name)}
        <div class="tree-node" class:expanded={expandedProgram === prog.name}>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="prog-row" onclick={() => expandProgram(prog)}>
            <span class="chevron" class:rotated={expandedProgram === prog.name}>
              <ChevronRight size={14} />
            </span>
            <span class="prog-name">{prog.name}</span>
            <span class={languageBadgeClass(prog.language)}>{languageLabel(prog.language)}</span>
            {#if prog.updatedAt}
              <span class="updated">{formatTime(prog.updatedAt)}</span>
            {/if}
            <button
              class="delete-btn"
              title="Delete program"
              onclick={(e) => { e.stopPropagation(); deleteTarget = prog.name; }}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M18 6L6 18M6 6l12 12"/>
              </svg>
            </button>
          </div>

          {#if expandedProgram === prog.name}
            <div class="prog-editor" transition:slide>
              {#if prog.language === 'starlark'}
                <div class="editor-pad">
                  <CodeEditor
                    value={editSource}
                    language="starlark"
                    onchange={(v) => (editSource = v)}
                    variableNames={data.variableNames}
                  />
                </div>
                <div class="editor-actions">
                  <button class="save-btn" disabled={saving} onclick={() => saveProgram(prog)}>
                    {saving ? 'Saving...' : 'Save'}
                  </button>
                </div>

              {:else if prog.language === 'st'}
                <div class="editor-pad">
                  <CodeEditor
                    value={editStSource}
                    language="st"
                    onchange={(v) => (editStSource = v)}
                    variableNames={data.variableNames}
                  />
                </div>
                <div class="editor-actions">
                  <button
                    class="transpile-btn"
                    disabled={transpiling || !editStSource.trim()}
                    onclick={previewTranspile}
                  >
                    {transpiling ? 'Transpiling...' : 'Preview Transpile'}
                  </button>
                  <button class="save-btn" disabled={saving} onclick={() => saveProgram(prog)}>
                    {saving ? 'Saving...' : 'Save'}
                  </button>
                </div>
                {#if showTranspile && transpileResult}
                  <div class="transpile-panel" transition:slide>
                    <div class="transpile-header">
                      <strong>Generated Starlark</strong>
                      <button class="close-transpile" onclick={() => (showTranspile = false)}>
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                          <path d="M18 6L6 18M6 6l12 12"/>
                        </svg>
                      </button>
                    </div>
                    {#if transpileResult.vars.length > 0}
                      <div class="transpile-vars">
                        <strong>Variables:</strong>
                        {#each transpileResult.vars as v}
                          <span class="var-chip">{v.name}: {v.datatype}</span>
                        {/each}
                      </div>
                    {/if}
                    <CodeEditor
                      value={transpileResult.starlark}
                      language="starlark"
                      readonly={true}
                    />
                  </div>
                {/if}

              {:else if prog.language === 'ladder'}
                {#if ladderLoading}
                  <div class="ladder-loading">Loading ladder program...</div>
                {:else if ladderProgram}
                  <div class="ladder-container">
                    <LadderEditor
                      program={ladderProgram}
                      onProgramChange={handleLadderChange}
                    />
                  </div>
                {:else}
                  <div class="ladder-loading">No ladder data available.</div>
                {/if}
                <div class="editor-actions">
                  <button class="save-btn" disabled={saving || ladderLoading} onclick={() => saveProgram(prog)}>
                    {saving ? 'Saving...' : 'Save'}
                  </button>
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if deleteTarget}
  <div class="modal-backdrop" onclick={() => (deleteTarget = null)}>
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Delete Program</h3>
      <p>Are you sure you want to delete <strong>{deleteTarget}</strong>?</p>
      <p class="modal-warning">Any tasks referencing this program will need to be updated.</p>
      <div class="modal-actions">
        <button class="cancel-btn" onclick={() => (deleteTarget = null)}>Cancel</button>
        <button class="confirm-delete-btn" disabled={saving} onclick={confirmDelete}>
          {saving ? 'Deleting...' : 'Delete'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .programs-page {
    padding: 1.5rem;
    overflow-x: hidden;
  }

  .error-box {
    padding: 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444);
    margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
    h2 {
      margin: 0;
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
    }
  }

  .count-badge {
    font-size: 0.75rem;
    font-weight: 500;
    padding: 0.125rem 0.5rem;
    border-radius: 9999px;
    background: var(--theme-surface);
    color: var(--theme-text-muted);
    margin-left: 0.5rem;
  }

  .add-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;
    &:hover { background: var(--theme-background); }
  }

  .add-form {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1rem;
    margin-bottom: 1rem;
    max-width: 500px;
  }

  .form-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 0.75rem;
    label {
      min-width: 80px;
      font-size: 0.8125rem;
      color: var(--theme-text-muted);
      flex-shrink: 0;
    }
    input[type="text"],
    select {
      flex: 1;
      padding: 0.375rem 0.5rem;
      font-size: 0.8125rem;
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded);
      background: var(--theme-background);
      color: var(--theme-text);
      font-family: inherit;
    }
    select { cursor: pointer; }
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 0.5rem;
  }

  .save-btn {
    padding: 0.375rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: var(--rounded-lg);
    background: var(--theme-primary);
    color: white;
    cursor: pointer;
    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .transpile-btn {
    padding: 0.375rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;
    &:hover:not(:disabled) { background: var(--theme-background); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .empty-state {
    padding: 3rem 2rem;
    text-align: center;
    color: var(--theme-text-muted);
    p { margin: 0.25rem 0; }
    .muted { font-size: 0.8125rem; opacity: 0.7; }
  }

  .tree {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .tree-node {
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
    &.expanded { border-color: var(--theme-primary); }
  }

  .prog-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.625rem 0.75rem;
    border: none;
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;
    font-size: 0.8125rem;
    text-align: left;
    &:hover { background: var(--theme-background); }
  }

  .chevron {
    display: flex;
    align-items: center;
    transition: transform 0.15s;
    color: var(--theme-text-muted);
    flex-shrink: 0;
    &.rotated { transform: rotate(90deg); }
  }

  .prog-name {
    font-family: 'IBM Plex Mono', monospace;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  .lang-badge {
    font-size: 0.6875rem;
    padding: 0.125rem 0.5rem;
    border-radius: 9999px;
    white-space: nowrap;
    flex-shrink: 0;
    font-weight: 500;

    &.starlark {
      background: color-mix(in srgb, var(--color-blue-500, #3b82f6) 15%, transparent);
      color: var(--color-blue-500, #3b82f6);
    }
    &.st {
      background: color-mix(in srgb, var(--color-purple-500, #a855f7) 15%, transparent);
      color: var(--color-purple-500, #a855f7);
    }
    &.ladder {
      background: color-mix(in srgb, var(--color-green-500, #22c55e) 15%, transparent);
      color: var(--color-green-500, #22c55e);
    }
  }

  .updated {
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    white-space: nowrap;
    flex-shrink: 0;
    margin-left: auto;
  }

  .delete-btn {
    display: flex;
    align-items: center;
    padding: 0.25rem;
    border: none;
    background: transparent;
    color: var(--theme-text-muted);
    cursor: pointer;
    border-radius: var(--rounded);
    flex-shrink: 0;
    &:hover { color: var(--color-red-500, #ef4444); background: color-mix(in srgb, var(--color-red-500, #ef4444) 10%, transparent); }
  }

  .prog-editor {
    border-top: 1px solid var(--theme-border);
    background: var(--theme-background);
  }

  .editor-pad {
    padding: 0.75rem;
  }

  .editor-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
  }

  .transpile-panel {
    border-top: 1px solid var(--theme-border);
    padding: 1rem;
    background: var(--theme-surface);
  }

  .transpile-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.75rem;
    strong { font-size: 0.8125rem; color: var(--theme-text); }
  }

  .close-transpile {
    display: flex;
    align-items: center;
    padding: 0.25rem;
    border: none;
    background: transparent;
    color: var(--theme-text-muted);
    cursor: pointer;
    border-radius: var(--rounded);
    &:hover { color: var(--theme-text); }
  }

  .transpile-vars {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.375rem;
    margin-bottom: 0.75rem;
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    strong { margin-right: 0.25rem; }
  }

  .var-chip {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    padding: 0.125rem 0.375rem;
    border-radius: var(--rounded);
    background: var(--theme-background);
    color: var(--theme-text);
  }

  .ladder-container {
    padding: 1rem;
    overflow-x: auto;
  }

  .ladder-loading {
    padding: 2rem;
    text-align: center;
    color: var(--theme-text-muted);
    font-size: 0.875rem;
  }

  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1.5rem;
    max-width: 400px;
    width: 90%;
    h3 { margin: 0 0 0.75rem; font-size: 1rem; color: var(--theme-text); }
    p { margin: 0 0 0.5rem; font-size: 0.875rem; color: var(--theme-text-muted); }
    strong { color: var(--theme-text); }
  }

  .modal-warning {
    font-size: 0.75rem !important;
    opacity: 0.7;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }

  .cancel-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;
    &:hover { background: var(--theme-background); }
  }

  .confirm-delete-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: var(--rounded-lg);
    background: var(--color-red-500, #ef4444);
    color: white;
    cursor: pointer;
    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  @media (max-width: 640px) {
    .prog-row {
      flex-wrap: wrap;
      gap: 0.375rem;
    }
    .updated { display: none; }
    .form-row {
      flex-direction: column;
      align-items: flex-start;
      gap: 0.25rem;
      label { min-width: unset; }
      input, select { width: 100%; }
    }
  }
</style>
