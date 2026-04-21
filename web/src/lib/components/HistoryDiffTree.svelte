<script lang="ts">
  import { slide } from 'svelte/transition';
  import type { HistoryDiffChange } from '$lib/types/gitops-history';

  let {
    changes,
    onselect,
  }: {
    changes: HistoryDiffChange[];
    onselect: (change: HistoryDiffChange) => void;
  } = $props();

  // Group changes by kind.
  const kindGroups = $derived(() => {
    const groups = new Map<string, HistoryDiffChange[]>();
    for (const change of changes) {
      const existing = groups.get(change.kind) ?? [];
      existing.push(change);
      groups.set(change.kind, existing);
    }
    return groups;
  });

  // Track expanded state.
  let expandedKinds: Record<string, boolean> = $state({});
  let expandedResources: Record<string, boolean> = $state({});

  // Auto-expand kinds that have changes.
  $effect(() => {
    const groups = kindGroups();
    for (const [kind, items] of groups) {
      if (expandedKinds[kind] === undefined) {
        expandedKinds[kind] = items.some((c) => c.action !== 'unchanged');
      }
    }
  });

  function toggleKind(kind: string) {
    expandedKinds[kind] = !expandedKinds[kind];
  }

  function toggleResource(key: string) {
    expandedResources[key] = !expandedResources[key];
  }

  function actionColor(action: string): string {
    switch (action) {
      case 'added': return 'var(--green-500)';
      case 'modified': return 'var(--amber-500)';
      case 'removed': return 'var(--red-500)';
      default: return 'var(--theme-border)';
    }
  }

  function actionLabel(action: string): string {
    switch (action) {
      case 'added': return '+';
      case 'modified': return '~';
      case 'removed': return '-';
      default: return '';
    }
  }

  function formatValue(val: unknown): string {
    if (val === null || val === undefined) return '';
    if (typeof val === 'string') return val;
    return JSON.stringify(val);
  }
</script>

<div class="tree">
  {#each [...kindGroups().entries()] as [kind, items]}
    <div class="kind-group">
      <button class="kind-header" onclick={() => toggleKind(kind)}>
        <svg
          class="chevron"
          class:expanded={expandedKinds[kind]}
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M9 18l6-6-6-6" />
        </svg>
        <span class="kind-name">{kind}</span>
        <span class="kind-count">{items.length}</span>
        {#if items.some((c) => c.action !== 'unchanged')}
          <span class="kind-changes">
            {#if items.filter((c) => c.action === 'added').length > 0}
              <span class="mini-badge added">+{items.filter((c) => c.action === 'added').length}</span>
            {/if}
            {#if items.filter((c) => c.action === 'modified').length > 0}
              <span class="mini-badge modified">~{items.filter((c) => c.action === 'modified').length}</span>
            {/if}
            {#if items.filter((c) => c.action === 'removed').length > 0}
              <span class="mini-badge removed">-{items.filter((c) => c.action === 'removed').length}</span>
            {/if}
          </span>
        {/if}
      </button>

      {#if expandedKinds[kind]}
        <div class="kind-children" transition:slide={{ duration: 150 }}>
          {#each items as change}
            {@const resKey = `${change.kind}/${change.name}`}
            {@const hasFields = change.fields && change.fields.length > 0}
            <div class="resource" style="--action-color: {actionColor(change.action)}">
              <button
                class="resource-header"
                onclick={() => {
                  if (hasFields) toggleResource(resKey);
                  onselect(change);
                }}
              >
                {#if hasFields}
                  <svg
                    class="chevron"
                    class:expanded={expandedResources[resKey]}
                    width="12"
                    height="12"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path d="M9 18l6-6-6-6" />
                  </svg>
                {:else}
                  <span class="chevron-spacer"></span>
                {/if}
                <span class="action-indicator" style="color: {actionColor(change.action)}">{actionLabel(change.action)}</span>
                <span class="resource-name">{change.name}</span>
                {#if hasFields}
                  <span class="field-count">{change.fields?.length} field{change.fields && change.fields.length !== 1 ? 's' : ''}</span>
                {/if}
              </button>

              {#if hasFields && expandedResources[resKey]}
                <div class="field-list" transition:slide={{ duration: 150 }}>
                  {#each change.fields ?? [] as field}
                    <div class="field-row {field.action}">
                      <span class="field-path">{field.path}</span>
                      {#if field.action === 'added'}
                        <span class="field-value new">{formatValue(field.newValue)}</span>
                      {:else if field.action === 'removed'}
                        <span class="field-value old">{formatValue(field.oldValue)}</span>
                      {:else}
                        <span class="field-value old">{formatValue(field.oldValue)}</span>
                        <span class="field-arrow">&rarr;</span>
                        <span class="field-value new">{formatValue(field.newValue)}</span>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/each}

  {#if changes.length === 0}
    <div class="empty">No changes between selected commits.</div>
  {/if}
</div>

<style lang="scss">
  .tree {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    max-width: 900px;
  }

  .kind-group {
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    overflow: hidden;
  }

  .kind-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.625rem 0.75rem;
    border: none;
    background: var(--theme-surface);
    color: var(--theme-text);
    font-family: inherit;
    font-size: 0.8125rem;
    font-weight: 600;
    cursor: pointer;
    text-align: left;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface));
    }
  }

  .chevron {
    transition: transform 0.15s ease;
    color: var(--theme-text-muted);
    flex-shrink: 0;

    &.expanded {
      transform: rotate(90deg);
    }
  }

  .chevron-spacer {
    width: 12px;
    flex-shrink: 0;
  }

  .kind-name {
    flex: 1;
  }

  .kind-count {
    font-size: 0.7rem;
    color: var(--theme-text-muted);
    font-weight: 400;
  }

  .kind-changes {
    display: flex;
    gap: 0.25rem;
  }

  .mini-badge {
    font-size: 0.65rem;
    font-weight: 700;
    padding: 0 0.25rem;
    border-radius: 2px;

    &.added { color: var(--green-500); }
    &.modified { color: var(--amber-500); }
    &.removed { color: var(--red-500); }
  }

  .kind-children {
    border-top: 1px solid var(--theme-border);
  }

  .resource {
    border-left: 3px solid var(--action-color);

    & + .resource {
      border-top: 1px solid var(--theme-border);
    }
  }

  .resource-header {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    width: 100%;
    padding: 0.4rem 0.75rem;
    border: none;
    background: none;
    color: var(--theme-text);
    font-family: inherit;
    font-size: 0.8125rem;
    cursor: pointer;
    text-align: left;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 3%, transparent);
    }
  }

  .action-indicator {
    font-weight: 700;
    font-size: 0.8125rem;
    width: 0.75rem;
    text-align: center;
    flex-shrink: 0;
  }

  .resource-name {
    font-weight: 500;
    flex: 1;
  }

  .field-count {
    font-size: 0.7rem;
    color: var(--theme-text-muted);
  }

  .field-list {
    padding: 0.25rem 0.75rem 0.5rem 2.5rem;
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }

  .field-row {
    display: flex;
    align-items: baseline;
    gap: 0.375rem;
    font-size: 0.75rem;
    font-family: 'SF Mono', 'Fira Code', monospace;
    padding: 0.15rem 0.35rem;
    border-radius: 2px;

    &.added {
      background: color-mix(in srgb, var(--green-500) 8%, transparent);
    }
    &.removed {
      background: color-mix(in srgb, var(--red-500) 8%, transparent);
    }
    &.modified {
      background: color-mix(in srgb, var(--amber-500) 8%, transparent);
    }
  }

  .field-path {
    color: var(--theme-text-muted);
    min-width: 8rem;
    flex-shrink: 0;
  }

  .field-arrow {
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .field-value {
    word-break: break-all;

    &.old {
      color: var(--red-500);
      text-decoration: line-through;
    }
    &.new {
      color: var(--green-500);
    }
  }

  .empty {
    color: var(--theme-text-muted);
    font-size: 0.875rem;
    padding: 2rem;
    text-align: center;
  }
</style>
