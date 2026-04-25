<script lang="ts">
  import type { PageData } from './$types';
  import { invalidateAll } from '$app/navigation';
  import { apiDelete } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import CollapsibleTree from '$lib/components/CollapsibleTree.svelte';

  let { data }: { data: PageData } = $props();

  type HierarchyNode = { name: string; children?: HierarchyNode[] };

  function buildTree(repos: { name: string; files: string[]; error?: string }[]): HierarchyNode {
    const root: HierarchyNode = { name: 'gitserver', children: [] };
    for (const repo of repos) {
      const repoNode: HierarchyNode = { name: repo.name, children: [] };
      const dirs = new Map<string, HierarchyNode>();
      dirs.set('', repoNode);
      for (const filePath of repo.files) {
        const parts = filePath.split('/');
        let parent = repoNode;
        let prefix = '';
        for (let i = 0; i < parts.length - 1; i++) {
          prefix = prefix ? `${prefix}/${parts[i]}` : parts[i];
          let dir = dirs.get(prefix);
          if (!dir) {
            dir = { name: parts[i], children: [] };
            parent.children!.push(dir);
            dirs.set(prefix, dir);
          }
          parent = dir;
        }
        parent.children!.push({ name: parts[parts.length - 1] });
      }
      if (repo.error) {
        repoNode.children!.push({ name: `(error: ${repo.error})` });
      } else if (repo.files.length === 0) {
        repoNode.children!.push({ name: '(empty)' });
      }
      root.children!.push(repoNode);
    }
    return root;
  }

  const tree = $derived(buildTree(data.repos));
  const totalFiles = $derived(data.repos.reduce((sum, r) => sum + r.files.length, 0));

  let deleteTarget: { name: string; fileCount: number } | null = $state(null);
  let deleteConfirmInput = $state('');
  let deleting = $state(false);

  function openDelete(name: string, fileCount: number) {
    deleteTarget = { name, fileCount };
    deleteConfirmInput = '';
  }

  function closeDelete() {
    if (deleting) return;
    deleteTarget = null;
    deleteConfirmInput = '';
  }

  async function confirmDelete() {
    if (!deleteTarget || deleteConfirmInput !== deleteTarget.name) return;
    deleting = true;
    const name = deleteTarget.name;
    const result = await apiDelete<void>(`/gitops/repos/${encodeURIComponent(name)}`);
    deleting = false;
    if (result.error) {
      saltState.addNotification({ message: `Failed to delete ${name}: ${result.error.error}`, type: 'error' });
      return;
    }
    saltState.addNotification({ message: `Deleted repo ${name}`, type: 'success' });
    deleteTarget = null;
    deleteConfirmInput = '';
    await invalidateAll();
  }
</script>

<div class="repos-page">
  <header class="header">
    <div>
      <h1>Repositories</h1>
      <p class="subtitle">
        {data.repos.length} {data.repos.length === 1 ? 'repo' : 'repos'} · {totalFiles} {totalFiles === 1 ? 'file' : 'files'}
      </p>
    </div>
    <p class="hint">Click a tree node to expand or collapse. Hold Alt+click for slow transitions.</p>
  </header>

  {#if data.error}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {:else if data.repos.length === 0}
    <div class="info-box">
      <p>No repos yet. Create one via the fleet provisioning flow or push to <code>/git/&lt;name&gt;.git</code>.</p>
    </div>
  {:else}
    <div class="repo-list">
      {#each data.repos as repo (repo.name)}
        <div class="repo-row">
          <span class="repo-name mono">{repo.name}</span>
          <span class="file-count">{repo.files.length} {repo.files.length === 1 ? 'file' : 'files'}</span>
          {#if repo.error}
            <span class="error-tag">error</span>
          {/if}
          <button
            class="delete-btn"
            onclick={() => openDelete(repo.name, repo.files.length)}
            title="Delete repo"
            aria-label="Delete repo {repo.name}"
          >
            Delete
          </button>
        </div>
      {/each}
    </div>

    <div class="tree-card">
      <CollapsibleTree data={tree} />
    </div>
  {/if}
</div>

{#if deleteTarget}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onkeydown={(e) => { if (e.key === 'Escape') closeDelete(); }} onclick={closeDelete}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h2>Delete repository</h2>
      <p class="modal-warning">
        This permanently removes the bare repo <strong>{deleteTarget.name}.git</strong>
        {#if deleteTarget.fileCount > 0}
          and all <strong>{deleteTarget.fileCount}</strong> tracked file{deleteTarget.fileCount !== 1 ? 's' : ''}
        {/if}
        from this mantle. The edge tentacle will lose remote configuration history. This cannot be undone.
      </p>
      <p class="modal-confirm-label">Type <strong>{deleteTarget.name}</strong> to confirm:</p>
      <input
        class="modal-input"
        bind:value={deleteConfirmInput}
        placeholder={deleteTarget.name}
        autocomplete="off"
        spellcheck="false"
        onkeydown={(e) => { if (e.key === 'Enter' && deleteConfirmInput === deleteTarget?.name) confirmDelete(); }}
      />
      <div class="modal-actions">
        <button class="modal-cancel-btn" onclick={closeDelete} disabled={deleting}>Cancel</button>
        <button
          class="modal-delete-btn"
          disabled={deleteConfirmInput !== deleteTarget.name || deleting}
          onclick={confirmDelete}
        >{deleting ? 'Deleting...' : 'Delete repository'}</button>
      </div>
    </div>
  </div>
{/if}

<style lang="scss">
  .repos-page {
    padding: 2rem;
    max-width: 1100px;
  }

  .header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1.5rem;
    margin-bottom: 1.5rem;

    h1 {
      margin: 0;
      font-size: 1.5rem;
      font-weight: 600;
      color: var(--theme-text);
    }

    .subtitle {
      margin: 0.25rem 0 0;
      font-size: 0.875rem;
      color: var(--theme-text-muted);
    }

    .hint {
      margin: 0;
      font-size: 0.75rem;
      color: var(--theme-text-muted);
      max-width: 240px;
      text-align: right;
    }
  }

  .repo-list {
    margin-bottom: 1.5rem;
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    border-radius: var(--rounded-md, 0.5rem);
    overflow: hidden;
  }

  .repo-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.625rem 1rem;
    font-size: 0.8125rem;

    &:not(:last-child) {
      border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    }
  }

  .repo-name {
    color: var(--theme-text);
    font-weight: 500;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .file-count {
    font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace;
    padding: 0.1rem 0.4rem;
    border-radius: var(--rounded-sm, 0.25rem);
    background: var(--badge-muted-bg);
    color: var(--badge-muted-text);
  }

  .error-tag {
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.1rem 0.4rem;
    border-radius: var(--rounded-sm, 0.25rem);
    background: color-mix(in srgb, var(--red-500, #ef4444) 15%, transparent);
    color: var(--red-500, #ef4444);
  }

  .delete-btn {
    padding: 0.25rem 0.625rem;
    font-size: 0.75rem;
    font-weight: 500;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--theme-surface);
    color: var(--theme-text-muted);
    cursor: pointer;
    transition: all 0.15s ease;

    &:hover {
      color: var(--red-500, #ef4444);
      border-color: var(--red-500, #ef4444);
    }
  }

  .mono {
    font-family: 'IBM Plex Mono', monospace;
  }

  .tree-card {
    padding: 1rem 1.25rem;
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    border-radius: var(--rounded-md, 0.5rem);
  }

  .info-box {
    padding: 1rem;
    border-radius: var(--rounded-lg, 0.5rem);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);

    p {
      margin: 0;
      font-size: 0.875rem;
      color: var(--theme-text-muted);
    }

    code {
      font-family: var(--font-mono, monospace);
      color: var(--theme-text);
    }

    &.error {
      border-color: var(--red-500, #ef4444);
      p { color: var(--red-500, #ef4444); }
    }
  }

  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 1000;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
  }

  .modal {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg, 0.5rem);
    padding: 1.5rem;
    max-width: 480px;
    width: 100%;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 1rem;
    }
  }

  .modal-warning {
    font-size: 0.8125rem;
    color: var(--red-500, #ef4444);
    line-height: 1.5;
    margin: 0 0 1rem;
  }

  .modal-confirm-label {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 0.5rem;
  }

  .modal-input {
    width: 100%;
    padding: 0.375rem 0.5rem;
    font-size: 0.8125rem;
    font-family: 'IBM Plex Mono', monospace;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--theme-input-bg, var(--theme-surface));
    color: var(--theme-text);
    box-sizing: border-box;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }

  .modal-cancel-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;

    &:hover:not(:disabled) {
      background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface));
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .modal-delete-btn {
    padding: 0.375rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: var(--rounded-md, 0.375rem);
    background: var(--red-500, #ef4444);
    color: white;
    cursor: pointer;

    &:hover:not(:disabled) {
      opacity: 0.9;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }
</style>
