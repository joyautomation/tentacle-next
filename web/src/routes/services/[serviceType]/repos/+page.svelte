<script lang="ts">
  import type { PageData } from './$types';
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
</script>

<div class="repos-page">
  <header class="header">
    <div>
      <h1>Repositories</h1>
      <p class="subtitle">
        {data.repos.length} {data.repos.length === 1 ? 'repo' : 'repos'} · {totalFiles} {totalFiles === 1 ? 'file' : 'files'}
      </p>
    </div>
    <p class="hint">Click a node to expand or collapse. Hold Alt+click for slow transitions.</p>
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
    <div class="tree-card">
      <CollapsibleTree data={tree} />
    </div>
  {/if}
</div>

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
</style>
