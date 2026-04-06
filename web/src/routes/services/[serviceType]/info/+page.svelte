<script lang="ts">
  import type { PageData } from './$types';
  import PlcVariables from '$lib/components/PlcVariables.svelte';

  let { data }: { data: PageData } = $props();
</script>

{#if data.serviceType === 'plc'}
  <PlcVariables variables={data.variables} error={data.error} />
{:else}
  <div style="padding: 2rem;">
    <p>This page is not available for this service type.</p>
  </div>
{/if}
