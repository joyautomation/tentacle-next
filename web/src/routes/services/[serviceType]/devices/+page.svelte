<script lang="ts">
  import type { PageData } from "./$types";
  import GatewayDevices from "$lib/components/GatewayDevices.svelte";
  import PlcDevices from "$lib/components/PlcDevices.svelte";

  let { data }: { data: PageData } = $props();
</script>

{#if data.serviceType === 'gateway'}
  <GatewayDevices gatewayConfig={data.gatewayConfig} error={data.error} />
{:else}
  <PlcDevices variables={data.variables} deviceInfo={data.deviceInfo} error={data.error} />
{/if}
