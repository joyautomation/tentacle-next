<script lang="ts">
  import { slide } from 'svelte/transition';
  import { ChevronRight } from '@joyautomation/salt/icons';
  import type { Snippet } from 'svelte';

  let {
    title,
    count,
    open = $bindable(true),
    onAction,
    actionTitle = 'Add',
    action,
    children,
  }: {
    title: string;
    count?: number | string;
    open?: boolean;
    onAction?: () => void;
    actionTitle?: string;
    action?: Snippet;
    children: Snippet;
  } = $props();

  function toggle() {
    open = !open;
  }
</script>

<section class="xp-section">
  <div class="xp-header-row">
    <button class="xp-header" onclick={toggle} aria-expanded={open} type="button">
      <span class="xp-chevron" class:open><ChevronRight size="0.75rem" /></span>
      <span class="xp-label">{title}</span>
      {#if count !== undefined}<span class="xp-count">{count}</span>{/if}
    </button>
    {#if action}
      <div class="xp-action-wrap">{@render action()}</div>
    {:else if onAction}
      <button class="xp-action" onclick={onAction} title={actionTitle} type="button" aria-label={actionTitle}>
        +
      </button>
    {/if}
  </div>
  {#if open}
    <div class="xp-body" transition:slide={{ duration: 150 }}>
      {@render children()}
    </div>
  {/if}
</section>

<style lang="scss">
  .xp-section {
    display: flex;
    flex-direction: column;
  }
  .xp-header-row {
    display: flex;
    align-items: stretch;
  }
  .xp-header {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    flex: 1;
    padding: 0.375rem 0.5rem;
    background: transparent;
    border: none;
    border-radius: 0;
    color: var(--theme-text);
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    cursor: pointer;
    text-align: left;
    &:hover {
      background: var(--theme-surface);
    }
  }
  .xp-chevron {
    display: inline-flex;
    color: var(--theme-text-muted);
    transition: transform 0.15s ease;
    &.open {
      transform: rotate(90deg);
    }
  }
  .xp-label {
    flex: 1;
  }
  .xp-count {
    padding: 0.0625rem 0.375rem;
    font-size: 0.6875rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    background: var(--theme-surface);
    border-radius: 0.625rem;
  }
  .xp-action {
    width: 1.75rem;
    aspect-ratio: 1 / 1;
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    opacity: 0.7;
    background: transparent;
    border: none;
    color: var(--theme-text-muted);
    cursor: pointer;
    font-size: 1rem;
    line-height: 1;
    transition: opacity 0.12s ease, color 0.12s ease, background 0.12s ease;
    &:hover {
      opacity: 1;
      background: var(--theme-surface);
      color: var(--theme-text);
    }
  }
  .xp-action-wrap {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    padding-right: 0.25rem;
  }
  .xp-body {
    padding: 0.5rem 0.625rem 0.625rem;
  }
</style>
