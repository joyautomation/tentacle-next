<script lang="ts">
  import type { PageData } from './$types';
  import PlcVariables from '$lib/components/PlcVariables.svelte';
  import PlcVariableConfig from '$lib/components/PlcVariableConfig.svelte';
  import Tabs, { type TabItem } from '$lib/components/Tabs.svelte';

  let { data }: { data: PageData } = $props();

  let activeTab: 'live' | 'config' = $state('live');

  const tabs: TabItem[] = [
    { id: 'live', label: 'Live Values' },
    { id: 'config', label: 'Configuration' }
  ];
</script>

{#if data.serviceType === 'plc'}
  <div class="plc-info-page">
    <div class="tab-switcher">
      <Tabs {tabs} active={activeTab} onChange={(id) => (activeTab = id as 'live' | 'config')} ariaLabel="Variables view" />
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
    padding: 0 2rem;
  }

  .config-container {
    padding: 2rem;
  }
</style>
