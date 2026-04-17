<script lang="ts">
  import type { PageData } from './$types';
  import type { PlcTaskConfigKV } from './+page';
  import { apiPut, apiDelete } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { slide } from 'svelte/transition';
  import { ChevronRight } from '@joyautomation/salt/icons';

  let { data }: { data: PageData } = $props();

  let showAddForm = $state(false);
  let saving = $state(false);
  let expandedTask: string | null = $state(null);
  let deleteTarget: string | null = $state(null);

  let newName = $state('');
  let newDescription = $state('');
  let newScanRateMs = $state(100);
  let newProgramRef = $state('');
  let newEnabled = $state(true);

  let editDescription = $state('');
  let editScanRateMs = $state(100);
  let editProgramRef = $state('');
  let editEnabled = $state(true);

  const taskList = $derived(
    Object.values(data.tasks).sort((a, b) => a.name.localeCompare(b.name))
  );

  const canAdd = $derived(
    newName.trim() !== '' && newScanRateMs > 0 && newProgramRef !== '' && !saving
  );

  const canSaveEdit = $derived(
    editScanRateMs > 0 && editProgramRef !== '' && !saving
  );

  function expandTask(task: PlcTaskConfigKV) {
    if (expandedTask === task.name) {
      expandedTask = null;
      return;
    }
    expandedTask = task.name;
    editDescription = task.description ?? '';
    editScanRateMs = task.scanRateMs;
    editProgramRef = task.programRef;
    editEnabled = task.enabled;
  }

  function resetAddForm() {
    newName = '';
    newDescription = '';
    newScanRateMs = 100;
    newProgramRef = data.programs[0]?.name ?? '';
    newEnabled = true;
  }

  async function addTask() {
    if (!canAdd) return;
    saving = true;
    try {
      const name = newName.trim();
      const result = await apiPut(`/plcs/plc/tasks/${encodeURIComponent(name)}`, {
        name,
        description: newDescription.trim() || undefined,
        scanRateMs: newScanRateMs,
        programRef: newProgramRef,
        enabled: newEnabled,
      });
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Task "${name}" created`, type: 'success' });
        showAddForm = false;
        resetAddForm();
        await invalidateAll();
      }
    } finally {
      saving = false;
    }
  }

  async function saveEdit(taskName: string) {
    if (!canSaveEdit) return;
    saving = true;
    try {
      const result = await apiPut(`/plcs/plc/tasks/${encodeURIComponent(taskName)}`, {
        name: taskName,
        description: editDescription.trim() || undefined,
        scanRateMs: editScanRateMs,
        programRef: editProgramRef,
        enabled: editEnabled,
      });
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Task "${taskName}" updated`, type: 'success' });
        expandedTask = null;
        await invalidateAll();
      }
    } finally {
      saving = false;
    }
  }

  async function toggleEnabled(task: PlcTaskConfigKV) {
    saving = true;
    try {
      const result = await apiPut(`/plcs/plc/tasks/${encodeURIComponent(task.name)}`, {
        ...task,
        enabled: !task.enabled,
      });
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        await invalidateAll();
      }
    } finally {
      saving = false;
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    saving = true;
    try {
      const result = await apiDelete(`/plcs/plc/tasks/${encodeURIComponent(deleteTarget)}`);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Task "${deleteTarget}" deleted`, type: 'success' });
        if (expandedTask === deleteTarget) expandedTask = null;
        deleteTarget = null;
        await invalidateAll();
      }
    } finally {
      saving = false;
    }
  }
</script>

<div class="tasks-page">
  {#if data.error}
    <div class="error-box"><p>{data.error}</p></div>
  {/if}

  <div class="section-header">
    <h2>Tasks <span class="count-badge">{taskList.length}</span></h2>
    <button class="add-btn" onclick={() => { showAddForm = !showAddForm; if (showAddForm) resetAddForm(); }}>
      {showAddForm ? 'Cancel' : '+ Add Task'}
    </button>
  </div>

  {#if showAddForm}
    <div class="add-form" transition:slide>
      {#if data.programs.length === 0}
        <p class="no-programs">No programs available. Create a program first.</p>
      {:else}
        <div class="form-row">
          <label for="new-name">Name</label>
          <input id="new-name" type="text" bind:value={newName} placeholder="e.g. MainTask" />
        </div>
        <div class="form-row">
          <label for="new-desc">Description</label>
          <input id="new-desc" type="text" bind:value={newDescription} placeholder="optional" />
        </div>
        <div class="form-row">
          <label for="new-rate">Scan Rate (ms)</label>
          <input id="new-rate" type="number" bind:value={newScanRateMs} min="1" />
        </div>
        <div class="form-row">
          <label for="new-prog">Program</label>
          <select id="new-prog" bind:value={newProgramRef}>
            <option value="" disabled>Select a program</option>
            {#each data.programs as prog}
              <option value={prog.name}>{prog.name} ({prog.language})</option>
            {/each}
          </select>
        </div>
        <div class="form-row">
          <label for="new-enabled">Enabled</label>
          <label class="toggle">
            <input type="checkbox" bind:checked={newEnabled} />
            <span class="toggle-slider"></span>
          </label>
        </div>
        <div class="form-actions">
          <button class="save-btn" disabled={!canAdd} onclick={addTask}>
            {saving ? 'Saving...' : 'Add Task'}
          </button>
        </div>
      {/if}
    </div>
  {/if}

  {#if taskList.length === 0 && !showAddForm}
    <div class="empty-state">
      <p>No tasks configured.</p>
      <p class="muted">Tasks run programs on a fixed scan interval.</p>
    </div>
  {:else}
    <div class="tree">
      {#each taskList as task (task.name)}
        <div class="tree-node" class:expanded={expandedTask === task.name}>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="task-row" onclick={() => expandTask(task)}>
            <span class="chevron" class:rotated={expandedTask === task.name}>
              <ChevronRight size={14} />
            </span>
            <span class="task-name">{task.name}</span>
            {#if task.description}
              <span class="task-desc">{task.description}</span>
            {/if}
            <span class="badge">{task.scanRateMs}ms</span>
            <span class="badge prog">{task.programRef}</span>
            <label class="toggle" onclick={(e) => e.stopPropagation()}>
              <input
                type="checkbox"
                checked={task.enabled}
                disabled={saving}
                onchange={() => toggleEnabled(task)}
              />
              <span class="toggle-slider"></span>
            </label>
            <button
              class="delete-btn"
              title="Delete task"
              onclick={(e) => { e.stopPropagation(); deleteTarget = task.name; }}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M18 6L6 18M6 6l12 12"/>
              </svg>
            </button>
          </div>

          {#if expandedTask === task.name}
            <div class="task-settings" transition:slide>
              <div class="form-row">
                <label for="edit-desc">Description</label>
                <input id="edit-desc" type="text" bind:value={editDescription} placeholder="optional" />
              </div>
              <div class="form-row">
                <label for="edit-rate">Scan Rate (ms)</label>
                <input id="edit-rate" type="number" bind:value={editScanRateMs} min="1" />
              </div>
              <div class="form-row">
                <label for="edit-prog">Program</label>
                <select id="edit-prog" bind:value={editProgramRef}>
                  {#each data.programs as prog}
                    <option value={prog.name}>{prog.name} ({prog.language})</option>
                  {/each}
                </select>
              </div>
              <div class="form-row">
                <label for="edit-enabled">Enabled</label>
                <label class="toggle">
                  <input type="checkbox" bind:checked={editEnabled} />
                  <span class="toggle-slider"></span>
                </label>
              </div>
              <div class="form-actions">
                <button class="save-btn" disabled={!canSaveEdit} onclick={() => saveEdit(task.name)}>
                  {saving ? 'Saving...' : 'Save'}
                </button>
              </div>
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
      <h3>Delete Task</h3>
      <p>Are you sure you want to delete <strong>{deleteTarget}</strong>?</p>
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
  .tasks-page {
    padding: 1.5rem;
    max-width: 900px;
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
  }

  .no-programs {
    color: var(--theme-text-muted);
    font-size: 0.875rem;
    margin: 0;
  }

  .form-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 0.75rem;
    label {
      min-width: 110px;
      font-size: 0.8125rem;
      color: var(--theme-text-muted);
      flex-shrink: 0;
    }
    input[type="text"],
    input[type="number"],
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
    select {
      cursor: pointer;
    }
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
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

  .task-row {
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

  .task-name {
    font-family: 'IBM Plex Mono', monospace;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .task-desc {
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
    min-width: 0;
  }

  .badge {
    font-size: 0.6875rem;
    padding: 0.125rem 0.375rem;
    border-radius: var(--rounded);
    background: var(--theme-background);
    color: var(--theme-text-muted);
    white-space: nowrap;
    flex-shrink: 0;
    &.prog {
      font-family: 'IBM Plex Mono', monospace;
    }
  }

  .toggle {
    position: relative;
    display: inline-block;
    width: 36px;
    height: 20px;
    cursor: pointer;
    flex-shrink: 0;
    input { opacity: 0; width: 0; height: 0; }
  }

  .toggle-slider {
    position: absolute;
    inset: 0;
    background: var(--theme-border);
    border-radius: 20px;
    transition: background 0.2s;
    &::before {
      content: '';
      position: absolute;
      width: 14px;
      height: 14px;
      left: 3px;
      bottom: 3px;
      background: var(--theme-text);
      border-radius: 50%;
      transition: transform 0.2s;
    }
  }

  .toggle input:checked + .toggle-slider { background: var(--color-green-500, #22c55e); }
  .toggle input:checked + .toggle-slider::before { transform: translateX(16px); }
  .toggle input:disabled + .toggle-slider { opacity: 0.5; cursor: not-allowed; }

  .delete-btn {
    display: flex;
    align-items: center;
    padding: 0.25rem;
    border: none;
    background: transparent;
    color: var(--theme-text-muted);
    cursor: pointer;
    border-radius: var(--rounded);
    margin-left: auto;
    flex-shrink: 0;
    &:hover { color: var(--color-red-500, #ef4444); background: color-mix(in srgb, var(--color-red-500, #ef4444) 10%, transparent); }
  }

  .task-settings {
    padding: 1rem;
    border-top: 1px solid var(--theme-border);
    background: var(--theme-background);
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
    p { margin: 0 0 1rem; font-size: 0.875rem; color: var(--theme-text-muted); }
    strong { color: var(--theme-text); }
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
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
    .task-row {
      flex-wrap: wrap;
      gap: 0.375rem;
    }
    .task-desc { display: none; }
    .form-row {
      flex-direction: column;
      align-items: flex-start;
      gap: 0.25rem;
      label { min-width: unset; }
      input, select { width: 100%; }
    }
  }
</style>
