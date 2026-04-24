<script lang="ts">
  import { slide } from "svelte/transition";
  import type { GatewayConfig } from "$lib/types/gateway";
  import { ChevronRight, Plus } from "@joyautomation/salt/icons";
  import DeviceNavigator from "./DeviceNavigator.svelte";
  import AddDeviceForm from "./AddDeviceForm.svelte";
  import DeleteDeviceModal from "./DeleteDeviceModal.svelte";

  let {
    gatewayConfig,
    error,
  }: {
    gatewayConfig: GatewayConfig | null;
    error: string | null;
  } = $props();

  let filter = $state("");
  let sectionOpen = $state(true);
  let showAddDevice = $state(false);
  let deleteTarget: { deviceId: string; varCount: number } | null = $state(null);

  const availableProtocols = $derived(
    gatewayConfig?.availableProtocols ?? [],
  );

  const devices = $derived(gatewayConfig?.devices ?? []);

  const varCounts = $derived.by(() => {
    const counts: Record<string, number> = {};
    for (const v of gatewayConfig?.variables ?? []) {
      counts[v.deviceId] = (counts[v.deviceId] ?? 0) + 1;
    }
    for (const v of gatewayConfig?.udtVariables ?? []) {
      counts[v.deviceId] = (counts[v.deviceId] ?? 0) + 1;
    }
    return counts;
  });

  function varCountFor(deviceId: string): number {
    return varCounts[deviceId] ?? 0;
  }

  const deviceCount = $derived(
    filter
      ? devices.filter((d) =>
          d.deviceId.toLowerCase().includes(filter.toLowerCase()),
        ).length
      : devices.length,
  );

  function onRequestDelete(deviceId: string) {
    deleteTarget = { deviceId, varCount: varCountFor(deviceId) };
  }
</script>

<div class="devices-page">
  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  <div class="filter-wrap">
    <input
      type="text"
      class="filter-input"
      placeholder="Filter devices…"
      bind:value={filter}
      aria-label="Filter devices"
    />
  </div>

  <section class="section">
    <div class="section-header-row">
      <button
        type="button"
        class="section-header"
        onclick={() => (sectionOpen = !sectionOpen)}
        aria-expanded={sectionOpen}
      >
        <span class="chevron" class:open={sectionOpen}>
          <ChevronRight size="0.75rem" />
        </span>
        <span class="label">Devices</span>
        <span class="count">{deviceCount}</span>
      </button>
      <button
        type="button"
        class="add-btn"
        onclick={() => (showAddDevice = !showAddDevice)}
        title={showAddDevice ? "Cancel" : "New device"}
        aria-label={showAddDevice ? "Cancel" : "New device"}
        aria-expanded={showAddDevice}
      >
        <Plus size="0.875rem" />
      </button>
    </div>

    {#if showAddDevice}
      <AddDeviceForm
        {availableProtocols}
        onSaved={() => (showAddDevice = false)}
        onCancel={() => (showAddDevice = false)}
      />
    {/if}

    {#if sectionOpen}
      <div transition:slide={{ duration: 150 }}>
        <DeviceNavigator
          {gatewayConfig}
          {filter}
          enableEditing={true}
          {varCounts}
          storagePrefix="gateway-devices:"
          {onRequestDelete}
        />
      </div>
    {/if}
  </section>

  {#if deleteTarget}
    <DeleteDeviceModal
      deviceId={deleteTarget.deviceId}
      varCount={deleteTarget.varCount}
      onClose={() => (deleteTarget = null)}
    />
  {/if}
</div>

<style lang="scss">
  .devices-page {
    max-width: 64rem;
    margin: 0 auto;
    padding: 1.5rem 1rem;
  }

  .filter-wrap {
    margin-bottom: 1rem;
  }

  .filter-input {
    width: 100%;
    padding: 0.4375rem 0.625rem;
    font-size: 0.8125rem;
    background: var(--theme-background);
    color: var(--theme-text);
    border: 1px solid var(--theme-border);
    border-radius: 0.25rem;

    &:focus {
      outline: none;
      border-color: var(--theme-primary);
    }
  }

  .section {
    border: 1px solid var(--theme-border);
    border-radius: 0.375rem;
    background: var(--theme-background);
    overflow: hidden;
  }

  .section-header-row {
    display: flex;
    align-items: stretch;
    border-bottom: 1px solid var(--theme-border);
  }

  .section-header {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    flex: 1;
    min-width: 0;
    padding: 0.5rem 0.625rem;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--theme-text);
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    text-align: left;

    &:hover {
      background: var(--theme-surface);
    }
  }

  .chevron {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--theme-text-muted);
    transition: transform 0.15s ease;

    &.open {
      transform: rotate(90deg);
    }
  }

  .label {
    flex: 1;
  }

  .count {
    padding: 0.0625rem 0.375rem;
    font-size: 0.6875rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    background: var(--theme-surface);
    border-radius: 0.625rem;
  }

  .add-btn {
    aspect-ratio: 1;
    width: 2rem;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    line-height: 1;
    cursor: pointer;
    color: var(--theme-text-muted);
    opacity: 0.7;
    transition:
      opacity 0.12s ease,
      color 0.12s ease,
      background 0.12s ease;

    &:hover {
      opacity: 1;
      color: var(--theme-text);
      background: var(--theme-surface);
    }

    &[aria-expanded="true"] {
      opacity: 1;
      color: var(--theme-primary);
    }
  }

  .error-box {
    padding: 0.875rem;
    border-radius: 0.375rem;
    background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444);
    margin-bottom: 1rem;

    p {
      margin: 0;
      font-size: 0.8125rem;
      color: var(--color-red-500, #ef4444);
    }
  }
</style>
