<script lang="ts">
  import type { PageData } from './$types';
  import LogViewer from '$lib/components/LogViewer.svelte';

  let { data }: { data: PageData } = $props();
</script>

<div class="logs-page">
  {#if data.error}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {/if}

  <LogViewer serviceType={data.serviceType} initialLogs={data.initialLogs} />
</div>

<style lang="scss">
  .logs-page {
    flex: 1;
    display: flex;
    flex-direction: column;
    padding: 1rem 2rem 2rem;
    min-height: 0;
  }

  .info-box {
    padding: 0.75rem 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    margin-bottom: 1rem;

    p {
      margin: 0;
      font-size: 0.875rem;
      color: var(--theme-text-muted);
    }

    &.error {
      border-color: var(--color-red-500, #ef4444);
      p { color: var(--color-red-500, #ef4444); }
    }
  }
</style>
