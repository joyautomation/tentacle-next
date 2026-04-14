<script lang="ts">
  import GatewayTagConfig from '$lib/components/gateway-tag-config/GatewayTagConfig.svelte';
  import ModbusTagConfig from '$lib/components/modbus-tag-config/ModbusTagConfig.svelte';

  let { data } = $props();
</script>

{#if data.serviceType === 'gateway'}
  <GatewayTagConfig
    gatewayConfig={data.gatewayConfig}
    browseCaches={data.browseCaches}
    browseStates={data.browseStates}
    error={data.error}
  />
{:else if data.serviceType === 'modbus'}
  <ModbusTagConfig
    gatewayConfig={data.gatewayConfig}
    error={data.error}
  />
{:else}
  <div style="padding: 2rem;">
    <p>Tag configuration is only available for gateway and modbus services.</p>
  </div>
{/if}
