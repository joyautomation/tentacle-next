<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    description: string;
    selected: boolean;
    onclick: () => void;
    badge?: string;
    diagram: Snippet;
  }

  let { title, description, selected, onclick, badge, diagram }: Props = $props();
</script>

<button class="architecture-card" class:selected onclick={onclick}>
  {#if badge}
    <span class="card-badge">{badge}</span>
  {/if}
  <div class="card-diagram">
    {@render diagram()}
  </div>
  <div class="card-content">
    <h3>{title}</h3>
    <p>{description}</p>
  </div>
</button>

<style lang="scss">
  .architecture-card {
    position: relative;
    display: flex;
    flex-direction: column;
    background: var(--theme-surface);
    border: 2px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 0;
    cursor: pointer;
    transition: border-color 0.2s, box-shadow 0.2s;
    text-align: left;
    overflow: hidden;
    width: 100%;

    &:hover {
      border-color: var(--theme-primary);
    }

    &.selected {
      border-color: var(--theme-primary);
      box-shadow: 0 0 0 1px var(--theme-primary);
    }
  }

  .card-badge {
    position: absolute;
    top: 0.75rem;
    right: 0.75rem;
    font-size: 0.625rem;
    font-weight: 700;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-full);
    background: var(--theme-primary);
    color: white;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    z-index: 1;
  }

  .card-diagram {
    padding: 1rem 1rem 0;
  }

  .card-content {
    padding: 0.75rem 1.25rem 1.25rem;

    h3 {
      margin: 0 0 0.25rem;
      font-size: 1rem;
      font-weight: 700;
      color: var(--theme-text);
    }

    p {
      margin: 0;
      font-size: 0.8125rem;
      color: var(--theme-text-muted);
      line-height: 1.4;
    }
  }
</style>
