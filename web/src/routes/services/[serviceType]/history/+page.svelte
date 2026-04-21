<script lang="ts">
  import { onMount } from 'svelte';
  import { slide } from 'svelte/transition';
  import { api } from '$lib/api/client';
  import DiffVizSelector from '$lib/components/DiffVizSelector.svelte';
  import HistoryDiffSummary from '$lib/components/HistoryDiffSummary.svelte';
  import HistoryDiffTree from '$lib/components/HistoryDiffTree.svelte';
  import HistoryDiffTopology from '$lib/components/HistoryDiffTopology.svelte';
  import type { DiffVizMode } from '$lib/components/DiffVizSelector.svelte';
  import type { CommitEntry, HistoryDiffResult, HistoryDiffChange } from '$lib/types/gitops-history';

  let vizMode: DiffVizMode = $state('summary');
  let commits: CommitEntry[] = $state([]);
  let fromSha: string = $state('');
  let toSha: string = $state('');
  let diff: HistoryDiffResult | null = $state(null);
  let loading: boolean = $state(false);
  let loadingCommits: boolean = $state(true);
  let error: string | null = $state(null);
  let selectedChange: HistoryDiffChange | null = $state(null);

  function formatDate(iso: string): string {
    const d = new Date(iso);
    const now = new Date();
    const diffMs = now.getTime() - d.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    if (diffMin < 1) return 'just now';
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    const diffDay = Math.floor(diffHr / 24);
    if (diffDay < 30) return `${diffDay}d ago`;
    return d.toLocaleDateString();
  }

  function commitLabel(c: CommitEntry): string {
    const sha = c.sha.slice(0, 8);
    const msg = c.message.length > 50 ? c.message.slice(0, 47) + '...' : c.message;
    return `${sha} — ${msg} (${formatDate(c.date)})`;
  }

  async function loadCommits() {
    loadingCommits = true;
    const result = await api<CommitEntry[]>('/gitops/history?limit=50');
    if (result.data) {
      commits = result.data;
      if (commits.length >= 2) {
        fromSha = commits[1].sha;
        toSha = commits[0].sha;
      } else if (commits.length === 1) {
        fromSha = commits[0].sha;
        toSha = commits[0].sha;
      }
    } else {
      error = result.error?.error ?? 'Failed to load commit history';
    }
    loadingCommits = false;
  }

  async function loadDiff() {
    if (!fromSha || !toSha) return;
    if (fromSha === toSha) {
      diff = { fromSha, toSha, changes: [], summary: { added: 0, modified: 0, removed: 0, unchanged: 0 } };
      return;
    }
    loading = true;
    error = null;
    selectedChange = null;
    const result = await api<HistoryDiffResult>(`/gitops/history/diff?from=${fromSha}&to=${toSha}`);
    if (result.data) {
      diff = result.data;
    } else {
      error = result.error?.error ?? 'Failed to load diff';
      diff = null;
    }
    loading = false;
  }

  function handleSelect(change: HistoryDiffChange) {
    selectedChange = selectedChange?.kind === change.kind && selectedChange?.name === change.name ? null : change;
  }

  function formatFieldValue(val: unknown): string {
    if (val === null || val === undefined) return '';
    if (typeof val === 'string') return val;
    return JSON.stringify(val, null, 2);
  }

  // Load commits on mount.
  onMount(loadCommits);

  // Load diff when selection changes.
  $effect(() => {
    if (fromSha && toSha) loadDiff();
  });
</script>

<div class="history-page">
  <div class="history-header">
    <h1>History</h1>
    {#if diff}
      <div class="summary-badges">
        {#if diff.summary.added > 0}
          <span class="badge added">+{diff.summary.added}</span>
        {/if}
        {#if diff.summary.modified > 0}
          <span class="badge modified">~{diff.summary.modified}</span>
        {/if}
        {#if diff.summary.removed > 0}
          <span class="badge removed">-{diff.summary.removed}</span>
        {/if}
      </div>
    {/if}
    <DiffVizSelector bind:mode={vizMode} />
  </div>

  {#if loadingCommits}
    <div class="loading">Loading commit history...</div>
  {:else if commits.length === 0}
    <div class="empty-state">
      <p>No commit history available.</p>
      <p class="hint">Configure GitOps and make changes to see history here.</p>
    </div>
  {:else}
    <div class="commit-picker">
      <div class="picker-field">
        <label for="from-commit">From</label>
        <select id="from-commit" bind:value={fromSha}>
          {#each commits as commit}
            <option value={commit.sha}>{commitLabel(commit)}</option>
          {/each}
        </select>
      </div>
      <div class="picker-field">
        <label for="to-commit">To</label>
        <select id="to-commit" bind:value={toSha}>
          {#each commits as commit}
            <option value={commit.sha}>{commitLabel(commit)}</option>
          {/each}
        </select>
      </div>
    </div>

    {#if error}
      <div class="error">{error}</div>
    {/if}

    {#if loading}
      <div class="loading">Computing diff...</div>
    {:else if diff}
      {#if fromSha === toSha}
        <div class="same-commit">Same commit selected. Choose two different commits to compare.</div>
      {:else}
        <div class="viz-area">
          {#if vizMode === 'summary'}
            <HistoryDiffSummary changes={diff.changes} onselect={handleSelect} />
          {:else if vizMode === 'tree'}
            <HistoryDiffTree changes={diff.changes} onselect={handleSelect} />
          {:else if vizMode === 'topology'}
            <HistoryDiffTopology changes={diff.changes} onselect={handleSelect} />
          {/if}
        </div>

        {#if selectedChange}
          <div class="detail-panel" transition:slide={{ duration: 200 }}>
            <div class="detail-header">
              <span class="detail-kind">{selectedChange.kind}</span>
              <span class="detail-sep">/</span>
              <span class="detail-name">{selectedChange.name}</span>
              <span class="detail-action {selectedChange.action}">{selectedChange.action}</span>
              <button class="detail-close" aria-label="Close detail panel" onclick={() => (selectedChange = null)}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M18 6L6 18M6 6l12 12" />
                </svg>
              </button>
            </div>
            {#if selectedChange.fields && selectedChange.fields.length > 0}
              <div class="detail-fields">
                {#each selectedChange.fields as field}
                  <div class="detail-field {field.action}">
                    <span class="df-path">{field.path}</span>
                    <div class="df-values">
                      {#if field.action === 'added'}
                        <span class="df-new">{formatFieldValue(field.newValue)}</span>
                      {:else if field.action === 'removed'}
                        <span class="df-old">{formatFieldValue(field.oldValue)}</span>
                      {:else}
                        <span class="df-old">{formatFieldValue(field.oldValue)}</span>
                        <span class="df-arrow">&rarr;</span>
                        <span class="df-new">{formatFieldValue(field.newValue)}</span>
                      {/if}
                    </div>
                  </div>
                {/each}
              </div>
            {:else if selectedChange.action === 'added'}
              <div class="detail-empty">New resource (no field-level diff available)</div>
            {:else if selectedChange.action === 'removed'}
              <div class="detail-empty">Resource was removed</div>
            {:else}
              <div class="detail-empty">No field-level changes</div>
            {/if}
          </div>
        {/if}
      {/if}
    {/if}
  {/if}
</div>

<style lang="scss">
  .history-page {
    padding: 1.5rem 2rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .history-header {
    display: flex;
    align-items: center;
    gap: 0.75rem;

    h1 {
      font-size: 1.25rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }
  }

  .summary-badges {
    display: flex;
    gap: 0.35rem;
  }

  .badge {
    display: inline-flex;
    align-items: center;
    padding: 0.1rem 0.4rem;
    border-radius: var(--rounded-md);
    font-size: 0.7rem;
    font-weight: 700;

    &.added {
      background: color-mix(in srgb, var(--green-500) 20%, transparent);
      color: var(--green-500);
    }
    &.modified {
      background: color-mix(in srgb, var(--amber-500) 20%, transparent);
      color: var(--amber-500);
    }
    &.removed {
      background: color-mix(in srgb, var(--red-500) 20%, transparent);
      color: var(--red-500);
    }
  }

  .commit-picker {
    display: flex;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .picker-field {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    flex: 1;
    min-width: 250px;

    label {
      font-size: 0.75rem;
      font-weight: 600;
      color: var(--theme-text-muted);
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }

    select {
      padding: 0.5rem 0.75rem;
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-md);
      background: var(--theme-surface);
      color: var(--theme-text);
      font-family: 'SF Mono', 'Fira Code', monospace;
      font-size: 0.8125rem;
      cursor: pointer;

      &:focus {
        outline: none;
        border-color: var(--theme-primary);
      }
    }
  }

  .loading, .same-commit {
    color: var(--theme-text-muted);
    font-size: 0.875rem;
    padding: 1rem 0;
  }

  .error {
    color: var(--red-500);
    font-size: 0.875rem;
    padding: 0.5rem 0.75rem;
    background: color-mix(in srgb, var(--red-500) 10%, transparent);
    border-radius: var(--rounded-md);
  }

  .empty-state {
    text-align: center;
    padding: 3rem 1rem;
    color: var(--theme-text-muted);

    p { margin: 0.25rem 0; }
    .hint { font-size: 0.8125rem; }
  }

  .viz-area {
    min-height: 200px;
  }

  // Detail panel
  .detail-panel {
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
    overflow: hidden;
  }

  .detail-header {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.625rem 0.75rem;
    border-bottom: 1px solid var(--theme-border);
    font-size: 0.8125rem;
  }

  .detail-kind {
    font-weight: 600;
    color: var(--theme-text-muted);
  }

  .detail-sep {
    color: var(--theme-border);
  }

  .detail-name {
    font-weight: 600;
    color: var(--theme-text);
    flex: 1;
  }

  .detail-action {
    padding: 0.1rem 0.35rem;
    border-radius: 2px;
    font-size: 0.7rem;
    font-weight: 700;
    text-transform: uppercase;

    &.added {
      background: color-mix(in srgb, var(--green-500) 20%, transparent);
      color: var(--green-500);
    }
    &.modified {
      background: color-mix(in srgb, var(--amber-500) 20%, transparent);
      color: var(--amber-500);
    }
    &.removed {
      background: color-mix(in srgb, var(--red-500) 20%, transparent);
      color: var(--red-500);
    }
    &.unchanged {
      background: color-mix(in srgb, var(--theme-text-muted) 20%, transparent);
      color: var(--theme-text-muted);
    }
  }

  .detail-close {
    padding: 0.25rem;
    border: none;
    background: none;
    color: var(--theme-text-muted);
    cursor: pointer;
    border-radius: 2px;
    display: flex;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 10%, transparent);
      color: var(--theme-text);
    }
  }

  .detail-fields {
    display: flex;
    flex-direction: column;
    padding: 0.5rem;
    gap: 0.15rem;
  }

  .detail-field {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    padding: 0.25rem 0.5rem;
    border-radius: 2px;
    font-size: 0.75rem;
    font-family: 'SF Mono', 'Fira Code', monospace;

    &.added { background: color-mix(in srgb, var(--green-500) 8%, transparent); }
    &.removed { background: color-mix(in srgb, var(--red-500) 8%, transparent); }
    &.modified { background: color-mix(in srgb, var(--amber-500) 8%, transparent); }
  }

  .df-path {
    color: var(--theme-text-muted);
    min-width: 10rem;
    flex-shrink: 0;
  }

  .df-values {
    display: flex;
    align-items: baseline;
    gap: 0.375rem;
    flex: 1;
    word-break: break-all;
  }

  .df-old {
    color: var(--red-500);
    text-decoration: line-through;
  }

  .df-new {
    color: var(--green-500);
  }

  .df-arrow {
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .detail-empty {
    padding: 0.75rem;
    color: var(--theme-text-muted);
    font-size: 0.8125rem;
  }
</style>
