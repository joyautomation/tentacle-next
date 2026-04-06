<script lang="ts">
  import GatewayTagConfig from '$lib/components/gateway-tag-config/GatewayTagConfig.svelte';

  let { data } = $props();
</script>

{#if data.serviceType === 'gateway'}
  <GatewayTagConfig
    gatewayConfig={data.gatewayConfig}
    browseCaches={data.browseCaches}
    browseStates={data.browseStates}
    error={data.error}
  />
{:else}
  <div style="padding: 2rem;">
    <p>Tag configuration is only available for gateway services.</p>
  </div>
{/if}
