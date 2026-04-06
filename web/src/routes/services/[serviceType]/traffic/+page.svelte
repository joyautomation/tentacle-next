<script lang="ts">
  import NatsTrafficViewer from '$lib/components/NatsTrafficViewer.svelte';

  let { data } = $props();
</script>

<div class="traffic-page">
  <NatsTrafficViewer initialTraffic={data.initialTraffic} />
</div>

<style lang="scss">
  .traffic-page {
    flex: 1;
    display: flex;
    flex-direction: column;
    padding: 1.5rem 2rem;
    min-height: 0;
  }
</style>
