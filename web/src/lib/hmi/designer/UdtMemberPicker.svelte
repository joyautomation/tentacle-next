<script lang="ts">
  import type { HmiUdtMember } from '$lib/types/hmi';

  interface Props {
    open: boolean;
    members: HmiUdtMember[];
    onClose: () => void;
    onPick: (member: string) => void;
  }

  let { open, members, onClose, onPick }: Props = $props();
  let filter = $state('');

  const filtered = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return members;
    return members.filter((m) => m.name.toLowerCase().includes(q) || (m.datatype ?? '').toLowerCase().includes(q));
  });

  function onBackdropKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose();
  }
</script>

{#if open}
  <div class="backdrop" role="button" tabindex="-1" onclick={onClose} onkeydown={onBackdropKey}>
    <div class="dialog" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
      <header class="dialog-header">
        <h2>Pick a UDT member</h2>
        <button class="close" onclick={onClose} aria-label="Close">×</button>
      </header>
      <input class="filter" type="text" placeholder="Filter…" bind:value={filter} />
      {#if filtered.length === 0}
        <p class="muted">No members.</p>
      {:else}
        <ul class="member-list">
          {#each filtered as m (m.name)}
            <li>
              <button class="member-row" onclick={() => onPick(m.name)}>
                <span class="member-name">{m.name}</span>
                <span class="member-type">{m.datatype}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
{/if}

<style lang="scss">
  .backdrop {
    position: fixed; inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex; align-items: center; justify-content: center;
    z-index: 1000;
  }
  .dialog {
    width: min(32rem, calc(100% - 2rem));
    max-height: calc(100vh - 4rem);
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    display: flex; flex-direction: column; overflow: hidden;
  }
  .dialog-header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 0.875rem 1.125rem;
    border-bottom: 1px solid var(--theme-border);
    h2 { margin: 0; font-size: 1rem; color: var(--theme-text); }
  }
  .close {
    background: transparent; border: none; color: var(--theme-text-muted);
    font-size: 1.25rem; cursor: pointer;
    &:hover { color: var(--theme-text); }
  }
  .filter {
    margin: 0.875rem 1.125rem 0.5rem;
    padding: 0.5rem 0.75rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
    color: var(--theme-text);
    font-family: inherit;
  }
  .member-list { list-style: none; margin: 0; padding: 0 1.125rem 1.125rem; overflow-y: auto; display: flex; flex-direction: column; gap: 0.125rem; }
  .member-row {
    width: 100%;
    display: flex; justify-content: space-between; align-items: baseline;
    padding: 0.375rem 0.5rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    color: var(--theme-text);
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.8125rem;
    cursor: pointer;
    text-align: left;
    &:hover { border-color: var(--theme-text); }
  }
  .member-name { color: var(--theme-text); }
  .member-type { color: var(--theme-text-muted); font-size: 0.75rem; }
  .muted { color: var(--theme-text-muted); padding: 1rem 1.125rem; margin: 0; }
</style>
