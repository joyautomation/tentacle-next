<script lang="ts">
  import type { PageData } from "./$types";
  import { onMount } from "svelte";
  import { invalidate } from "$app/navigation";
  import GatewayDevices from "$lib/components/GatewayDevices.svelte";
  import PlcDevices from "$lib/components/PlcDevices.svelte";
  import ProfinetControllerDevices from "$lib/components/ProfinetControllerDevices.svelte";

  let { data }: { data: PageData } = $props();

  // Poll the gateway page so device comm status badges refresh.
  onMount(() => {
    if (data.serviceType !== 'gateway') return;
    const id = setInterval(() => {
      invalidate('app:gateway-devices');
    }, 2500);
    return () => clearInterval(id);
  });
</script>

{#if data.serviceType === 'gateway'}
  <GatewayDevices
    gatewayConfig={data.gatewayConfig}
    deviceStatuses={data.deviceStatuses ?? {}}
    error={data.error}
  />
{:else if data.serviceType === 'profinetcontroller'}
  <ProfinetControllerDevices
    subscriptions={data.profinetSubscriptions ?? []}
    interfaces={data.networkInterfaces ?? []}
    error={data.error}
  />
{:else}
  <PlcDevices variables={data.variables} deviceInfo={data.deviceInfo} error={data.error} />
{/if}
