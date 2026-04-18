<script lang="ts">
  import type { PageData } from './$types';
  import PlcVariables from '$lib/components/PlcVariables.svelte';
  import PlcVariableConfig from '$lib/components/PlcVariableConfig.svelte';

  let { data }: { data: PageData } = $props();

  let activeTab: 'live' | 'config' = $state('live');
</script>

{#if data.serviceType === 'plc'}
  <div class="plc-info-page">
    <div class="tab-switcher">
      <button class="tab-btn" class:active={activeTab === 'live'} onclick={() => activeTab = 'live'}>Live Values</button>
      <button class="tab-btn" class:active={activeTab === 'config'} onclick={() => activeTab = 'config'}>Configuration</button>
    </div>

    {#if activeTab === 'live'}
      <PlcVariables variables={data.variables} error={data.error} />
    {:else}
      <div class="config-container">
        <PlcVariableConfig
          plcConfig={data.plcConfig}
          gatewayConfig={data.gatewayConfig}
          browseCaches={data.browseCaches}
          browseStates={data.browseStates}
          error={data.error}
        />
      </div>
    {/if}
  </div>
{:else}
  <div style="padding: 2rem;">
    <p>This page is not available for this service type.</p>
  </div>
{/if}

<style lang="scss">
  .plc-info-page {
    overflow-x: hidden;
  }

  .tab-switcher {
    display: flex; gap: 0; padding: 0 2rem; border-bottom: 1px solid var(--theme-border);
  }

  .tab-btn {
    padding: 0.75rem 1.25rem; font-size: 0.8125rem; font-weight: 500;
    border: none; border-bottom: 2px solid transparent;
    background: none; color: var(--theme-text-muted); cursor: pointer;
    transition: color 0.15s ease, border-color 0.15s ease;

    &:hover { color: var(--theme-text); }
    &.active {
      color: var(--theme-primary);
      border-bottom-color: var(--theme-primary);
      font-weight: 600;
    }
  }

  .config-container {
    padding: 2rem;
  }
</style>
