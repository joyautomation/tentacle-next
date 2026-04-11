<script lang="ts">
  import type { HistoryDiffChange } from '$lib/types/gitops-history';

  let {
    changes,
    onselect,
  }: {
    changes: HistoryDiffChange[];
    onselect: (change: HistoryDiffChange) => void;
  } = $props();

  const grouped = $derived(() => {
    const added = changes.filter((c) => c.action === 'added');
    const modified = changes.filter((c) => c.action === 'modified');
    const removed = changes.filter((c) => c.action === 'removed');
    const unchanged = changes.filter((c) => c.action === 'unchanged');
    return { added, modified, removed, unchanged };
  });
</script>

<div class="summary">
  {#if grouped().added.length > 0}
    <div class="group">
      <h3 class="group-header added">
        <span class="badge added">+{grouped().added.length}</span> Added
      </h3>
      <div class="cards">
        {#each grouped().added as change}
          <button class="card added" onclick={() => onselect(change)}>
            <span class="kind-badge">{change.kind}</span>
            <span class="name">{change.name}</span>
          </button>
        {/each}
      </div>
    </div>
  {/if}

  {#if grouped().modified.length > 0}
    <div class="group">
      <h3 class="group-header modified">
        <span class="badge modified">~{grouped().modified.length}</span> Modified
      </h3>
      <div class="cards">
        {#each grouped().modified as change}
          <button class="card modified" onclick={() => onselect(change)}>
            <span class="kind-badge">{change.kind}</span>
            <span class="name">{change.name}</span>
            {#if change.fields}
              <span class="field-count">{change.fields.length} field{change.fields.length !== 1 ? 's' : ''}</span>
            {/if}
          </button>
        {/each}
      </div>
    </div>
  {/if}

  {#if grouped().removed.length > 0}
    <div class="group">
      <h3 class="group-header removed">
        <span class="badge removed">-{grouped().removed.length}</span> Removed
      </h3>
      <div class="cards">
        {#each grouped().removed as change}
          <button class="card removed" onclick={() => onselect(change)}>
            <span class="kind-badge">{change.kind}</span>
            <span class="name">{change.name}</span>
          </button>
        {/each}
      </div>
    </div>
  {/if}

  {#if grouped().unchanged.length > 0}
    <div class="group">
      <h3 class="group-header unchanged">
        <span class="badge unchanged">{grouped().unchanged.length}</span> Unchanged
      </h3>
      <div class="cards">
        {#each grouped().unchanged as change}
          <button class="card unchanged" onclick={() => onselect(change)}>
            <span class="kind-badge">{change.kind}</span>
            <span class="name">{change.name}</span>
          </button>
        {/each}
      </div>
    </div>
  {/if}

  {#if changes.length === 0}
    <div class="empty">No changes between selected commits.</div>
  {/if}
</div>

<style lang="scss">
  .summary {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
    max-width: 900px;
  }

  .group-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--theme-text);
    margin: 0 0 0.5rem 0;
  }

  .badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0.1rem 0.4rem;
    border-radius: var(--rounded-md);
    font-size: 0.7rem;
    font-weight: 700;
    min-width: 1.4rem;

    &.added {
      background: color-mix(in srgb, var(--color-green-500) 20%, transparent);
      color: var(--color-green-500);
    }
    &.modified {
      background: color-mix(in srgb, var(--color-amber-500) 20%, transparent);
      color: var(--color-amber-500);
    }
    &.removed {
      background: color-mix(in srgb, var(--color-red-500) 20%, transparent);
      color: var(--color-red-500);
    }
    &.unchanged {
      background: color-mix(in srgb, var(--theme-text-muted) 20%, transparent);
      color: var(--theme-text-muted);
    }
  }

  .cards {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .card {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.75rem;
    border-radius: var(--rounded-md);
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    cursor: pointer;
    text-align: left;
    font-family: inherit;
    font-size: 0.8125rem;
    color: var(--theme-text);
    transition: all 0.15s ease;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface));
    }

    &.added {
      border-left: 3px solid var(--color-green-500);
    }
    &.modified {
      border-left: 3px solid var(--color-amber-500);
    }
    &.removed {
      border-left: 3px solid var(--color-red-500);
    }
    &.unchanged {
      border-left: 3px solid var(--theme-border);
      opacity: 0.6;
    }
  }

  .kind-badge {
    padding: 0.1rem 0.35rem;
    border-radius: var(--rounded-sm, 2px);
    background: color-mix(in srgb, var(--theme-text) 10%, transparent);
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--theme-text-muted);
    white-space: nowrap;
  }

  .name {
    font-weight: 500;
    flex: 1;
  }

  .field-count {
    font-size: 0.7rem;
    color: var(--theme-text-muted);
    white-space: nowrap;
  }

  .empty {
    color: var(--theme-text-muted);
    font-size: 0.875rem;
    padding: 2rem;
    text-align: center;
  }
</style>
