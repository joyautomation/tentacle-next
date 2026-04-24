<script lang="ts">
  import { slide } from "svelte/transition";
  import type { GatewayConfig, GatewayDevice } from "$lib/types/gateway";
  import { apiPut, apiDelete } from "$lib/api/client";
  import { invalidateAll } from "$app/navigation";
  import { state as saltState } from "@joyautomation/salt";
  import { ChevronRight, Plus } from "@joyautomation/salt/icons";
  import DeviceNavigator from "./DeviceNavigator.svelte";

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
  let saving = $state(false);
  let deleteTarget: { deviceId: string; varCount: number } | null = $state(null);
  let deleteConfirmInput = $state("");

  const allProtocols = [
    { value: "ethernetip", label: "EtherNet/IP" },
    { value: "opcua", label: "OPC UA" },
    { value: "snmp", label: "SNMP" },
    { value: "modbus", label: "Modbus TCP" },
  ] as const;

  const availableProtocols = $derived(
    gatewayConfig?.availableProtocols?.length
      ? allProtocols.filter((p) =>
          gatewayConfig!.availableProtocols!.includes(p.value),
        )
      : [],
  );
  const defaultProtocol = $derived(
    gatewayConfig?.availableProtocols?.[0] ?? "ethernetip",
  );

  let newDevice = $state({
    deviceId: "",
    protocol: "ethernetip" as string,
    host: "",
    port: "",
    slot: "",
    endpointUrl: "",
    version: "2c",
    community: "public",
    unitId: "1",
  });

  $effect(() => {
    if (
      defaultProtocol &&
      !gatewayConfig?.availableProtocols?.includes(newDevice.protocol)
    ) {
      newDevice.protocol = defaultProtocol;
    }
  });

  function resetNewDevice() {
    newDevice = {
      deviceId: "",
      protocol: defaultProtocol,
      host: "",
      port: "",
      slot: "",
      endpointUrl: "",
      version: "2c",
      community: "public",
      unitId: "1",
    };
  }

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

  async function addDevice() {
    if (!newDevice.deviceId) return;
    saving = true;
    try {
      const deviceBody: Record<string, unknown> = {
        protocol: newDevice.protocol,
        ...(newDevice.protocol !== "opcua" && newDevice.host
          ? { host: newDevice.host }
          : {}),
        ...(newDevice.port ? { port: parseInt(newDevice.port) } : {}),
        ...(newDevice.protocol === "ethernetip" && newDevice.slot
          ? { slot: parseInt(newDevice.slot) }
          : {}),
        ...(newDevice.protocol === "opcua" && newDevice.endpointUrl
          ? { endpointUrl: newDevice.endpointUrl }
          : {}),
        ...(newDevice.protocol === "snmp"
          ? { version: newDevice.version, community: newDevice.community }
          : {}),
        ...(newDevice.protocol === "modbus" && newDevice.unitId
          ? { unitId: parseInt(newDevice.unitId) }
          : {}),
      };
      const result = await apiPut(
        `/devices/${encodeURIComponent(newDevice.deviceId)}`,
        deviceBody,
      );
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: "error" });
      } else {
        saltState.addNotification({
          message: `Device "${newDevice.deviceId}" added`,
          type: "success",
        });
        resetNewDevice();
        showAddDevice = false;
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({
        message: err instanceof Error ? err.message : "Failed",
        type: "error",
      });
    } finally {
      saving = false;
    }
  }

  function onRequestDelete(deviceId: string) {
    deleteTarget = { deviceId, varCount: varCountFor(deviceId) };
    deleteConfirmInput = "";
  }

  async function removeDevice(deviceId: string) {
    saving = true;
    try {
      const result = await apiDelete(
        `/devices/${encodeURIComponent(deviceId)}`,
      );
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: "error" });
      } else {
        saltState.addNotification({
          message: `Device "${deviceId}" removed`,
          type: "success",
        });
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({
        message: err instanceof Error ? err.message : "Failed",
        type: "error",
      });
    } finally {
      saving = false;
    }
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
      <div class="add-form" transition:slide={{ duration: 150 }}>
        <div class="form-row">
          <label for="gw-device-id">Device ID</label>
          <input
            id="gw-device-id"
            type="text"
            bind:value={newDevice.deviceId}
            placeholder="e.g. plc-1"
          />
        </div>
        <div class="form-row">
          <label for="gw-protocol">Protocol</label>
          {#if availableProtocols.length === 0}
            <p class="no-protocols">
              No protocol modules connected. Start a protocol service
              (EtherNet/IP, OPC UA, SNMP, or Modbus) to add devices.
            </p>
          {:else}
            <select id="gw-protocol" bind:value={newDevice.protocol}>
              {#each availableProtocols as proto}
                <option value={proto.value}>{proto.label}</option>
              {/each}
            </select>
          {/if}
        </div>
        {#if newDevice.protocol === "opcua"}
          <div class="form-row">
            <label for="gw-endpoint">Endpoint URL</label>
            <input
              id="gw-endpoint"
              type="text"
              bind:value={newDevice.endpointUrl}
              placeholder="opc.tcp://192.168.1.50:4840"
            />
          </div>
        {:else}
          <div class="form-row">
            <label for="gw-host">Host</label>
            <input
              id="gw-host"
              type="text"
              bind:value={newDevice.host}
              placeholder="192.168.1.100"
            />
          </div>
          <div class="form-row">
            <label for="gw-port">Port</label>
            <input
              id="gw-port"
              type="text"
              bind:value={newDevice.port}
              placeholder={newDevice.protocol === "ethernetip"
                ? "44818"
                : newDevice.protocol === "snmp"
                  ? "161"
                  : "502"}
            />
          </div>
          {#if newDevice.protocol === "ethernetip"}
            <div class="form-row">
              <label for="gw-slot">Slot</label>
              <input
                id="gw-slot"
                type="text"
                bind:value={newDevice.slot}
                placeholder="0"
              />
            </div>
          {/if}
        {/if}
        {#if newDevice.protocol === "snmp"}
          <div class="form-row">
            <label for="gw-version">SNMP Version</label>
            <select id="gw-version" bind:value={newDevice.version}>
              <option value="1">v1</option>
              <option value="2c">v2c</option>
              <option value="3">v3</option>
            </select>
          </div>
          <div class="form-row">
            <label for="gw-community">Community</label>
            <input
              id="gw-community"
              type="text"
              bind:value={newDevice.community}
              placeholder="public"
            />
          </div>
        {/if}
        {#if newDevice.protocol === "modbus"}
          <div class="form-row">
            <label for="gw-unitid">Unit ID</label>
            <input
              id="gw-unitid"
              type="text"
              bind:value={newDevice.unitId}
              placeholder="1"
            />
          </div>
        {/if}
        <div class="form-actions">
          <button
            type="button"
            class="cancel-btn"
            onclick={() => {
              showAddDevice = false;
              resetNewDevice();
            }}
            disabled={saving}
          >
            Cancel
          </button>
          <button
            type="button"
            class="save-btn"
            onclick={addDevice}
            disabled={saving ||
              !newDevice.deviceId ||
              availableProtocols.length === 0}
          >
            {saving ? "Saving…" : "Add Device"}
          </button>
        </div>
      </div>
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
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="modal-backdrop"
      onkeydown={(e) => {
        if (e.key === "Escape") deleteTarget = null;
      }}
      onclick={() => (deleteTarget = null)}
    >
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <div class="modal" onclick={(e) => e.stopPropagation()}>
        <h2>Delete Device</h2>
        <p class="modal-warning">
          This will permanently remove <strong>{deleteTarget.deviceId}</strong>
          and all <strong>{deleteTarget.varCount}</strong> variable{deleteTarget.varCount !==
          1
            ? "s"
            : ""} configured on it. Template name overrides, deadband
          settings, and browse data for this device will also be lost.
        </p>
        <p class="modal-confirm-label">
          Type <strong>{deleteTarget.deviceId}</strong> to confirm:
        </p>
        <input
          class="modal-input"
          bind:value={deleteConfirmInput}
          placeholder={deleteTarget.deviceId}
          onkeydown={(e) => {
            if (
              e.key === "Enter" &&
              deleteConfirmInput === deleteTarget?.deviceId
            ) {
              removeDevice(deleteTarget.deviceId);
              deleteTarget = null;
            }
          }}
        />
        <div class="modal-actions">
          <button
            class="modal-cancel-btn"
            onclick={() => (deleteTarget = null)}
          >
            Cancel
          </button>
          <button
            class="modal-delete-btn"
            disabled={deleteConfirmInput !== deleteTarget.deviceId || saving}
            onclick={() => {
              if (deleteTarget) {
                removeDevice(deleteTarget.deviceId);
                deleteTarget = null;
              }
            }}
          >
            {saving ? "Deleting…" : "Delete Device"}
          </button>
        </div>
      </div>
    </div>
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

  .add-form {
    padding: 1rem;
    background: color-mix(in srgb, var(--theme-text) 2%, transparent);
    border-bottom: 1px solid var(--theme-border);
  }

  .form-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 0.625rem;

    label {
      font-size: 0.75rem;
      color: var(--theme-text-muted);
      min-width: 6.5rem;
      flex-shrink: 0;
    }

    input,
    select {
      flex: 1;
      padding: 0.3125rem 0.5rem;
      font-size: 0.8125rem;
      font-family: var(--font-mono, "IBM Plex Mono", monospace);
      border: 1px solid var(--theme-border);
      border-radius: 0.25rem;
      background: var(--theme-input-bg, var(--theme-background));
      color: var(--theme-text);
    }
  }

  .no-protocols {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    margin: 0;
    font-style: italic;
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 0.75rem;
  }

  .cancel-btn,
  .save-btn {
    padding: 0.375rem 0.875rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border-radius: 0.25rem;
    cursor: pointer;

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .cancel-btn {
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    color: var(--theme-text);

    &:hover:not(:disabled) {
      background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface));
    }
  }

  .save-btn {
    border: none;
    background: var(--theme-primary);
    color: var(--theme-on-primary, white);

    &:hover:not(:disabled) {
      opacity: 0.9;
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

  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 1000;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
  }

  .modal {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: 0.5rem;
    padding: 1.5rem;
    max-width: 26rem;
    width: 100%;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 1rem;
    }
  }

  .modal-warning {
    font-size: 0.8125rem;
    color: var(--color-red-500, #ef4444);
    line-height: 1.5;
    margin: 0 0 1rem;
  }

  .modal-confirm-label {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 0.5rem;
  }

  .modal-input {
    width: 100%;
    padding: 0.375rem 0.5rem;
    font-size: 0.8125rem;
    font-family: var(--font-mono, "IBM Plex Mono", monospace);
    border: 1px solid var(--theme-border);
    border-radius: 0.25rem;
    background: var(--theme-input-bg);
    color: var(--theme-text);
    box-sizing: border-box;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }

  .modal-cancel-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: 1px solid var(--theme-border);
    border-radius: 0.25rem;
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface));
    }
  }

  .modal-delete-btn {
    padding: 0.375rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: 0.25rem;
    background: var(--color-red-500, #ef4444);
    color: white;
    cursor: pointer;

    &:hover:not(:disabled) {
      opacity: 0.9;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }
</style>
