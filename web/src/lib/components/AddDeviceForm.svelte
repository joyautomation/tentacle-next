<script lang="ts">
  import { slide } from "svelte/transition";
  import { apiPut } from "$lib/api/client";
  import { invalidateAll } from "$app/navigation";
  import { state as saltState } from "@joyautomation/salt";

  type Props = {
    availableProtocols: readonly string[];
    onSaved?: () => void;
    onCancel?: () => void;
  };

  let { availableProtocols, onSaved, onCancel }: Props = $props();

  const allProtocols = [
    { value: "ethernetip", label: "EtherNet/IP" },
    { value: "opcua", label: "OPC UA" },
    { value: "snmp", label: "SNMP" },
    { value: "modbus", label: "Modbus TCP" },
  ] as const;

  const protocolsForUi = $derived(
    availableProtocols.length
      ? allProtocols.filter((p) => availableProtocols.includes(p.value))
      : [],
  );
  const defaultProtocol = $derived(availableProtocols[0] ?? "ethernetip");

  let deviceId = $state("");
  let protocol = $state<string>("ethernetip");
  let host = $state("");
  let port = $state("");
  let slot = $state("");
  let endpointUrl = $state("");
  let version = $state("2c");
  let community = $state("public");
  let unitId = $state("1");
  let saving = $state(false);

  $effect(() => {
    if (defaultProtocol && !availableProtocols.includes(protocol)) {
      protocol = defaultProtocol;
    }
  });

  function reset() {
    deviceId = "";
    protocol = defaultProtocol;
    host = "";
    port = "";
    slot = "";
    endpointUrl = "";
    version = "2c";
    community = "public";
    unitId = "1";
  }

  async function submit() {
    if (!deviceId) return;
    saving = true;
    try {
      const body: Record<string, unknown> = {
        protocol,
        ...(protocol !== "opcua" && host ? { host } : {}),
        ...(port ? { port: parseInt(port) } : {}),
        ...(protocol === "ethernetip" && slot ? { slot: parseInt(slot) } : {}),
        ...(protocol === "opcua" && endpointUrl ? { endpointUrl } : {}),
        ...(protocol === "snmp" ? { version, community } : {}),
        ...(protocol === "modbus" && unitId ? { unitId: parseInt(unitId) } : {}),
      };
      const result = await apiPut(
        `/devices/${encodeURIComponent(deviceId)}`,
        body,
      );
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: "error" });
      } else {
        saltState.addNotification({
          message: `Device "${deviceId}" added`,
          type: "success",
        });
        const savedId = deviceId;
        reset();
        await invalidateAll();
        onSaved?.();
        // Keep savedId referenced to satisfy lint
        void savedId;
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

<div class="add-form" transition:slide={{ duration: 150 }}>
  <div class="form-row">
    <label for="add-device-id">Device ID</label>
    <input
      id="add-device-id"
      type="text"
      bind:value={deviceId}
      placeholder="e.g. plc-1"
    />
  </div>
  <div class="form-row">
    <label for="add-protocol">Protocol</label>
    {#if protocolsForUi.length === 0}
      <p class="no-protocols">
        No protocol modules connected. Start a protocol service (EtherNet/IP,
        OPC UA, SNMP, or Modbus) to add devices.
      </p>
    {:else}
      <select id="add-protocol" bind:value={protocol}>
        {#each protocolsForUi as proto}
          <option value={proto.value}>{proto.label}</option>
        {/each}
      </select>
    {/if}
  </div>
  {#if protocol === "opcua"}
    <div class="form-row">
      <label for="add-endpoint">Endpoint URL</label>
      <input
        id="add-endpoint"
        type="text"
        bind:value={endpointUrl}
        placeholder="opc.tcp://192.168.1.50:4840"
      />
    </div>
  {:else}
    <div class="form-row">
      <label for="add-host">Host</label>
      <input
        id="add-host"
        type="text"
        bind:value={host}
        placeholder="192.168.1.100"
      />
    </div>
    <div class="form-row">
      <label for="add-port">Port</label>
      <input
        id="add-port"
        type="text"
        bind:value={port}
        placeholder={protocol === "ethernetip"
          ? "44818"
          : protocol === "snmp"
            ? "161"
            : "502"}
      />
    </div>
    {#if protocol === "ethernetip"}
      <div class="form-row">
        <label for="add-slot">Slot</label>
        <input id="add-slot" type="text" bind:value={slot} placeholder="0" />
      </div>
    {/if}
  {/if}
  {#if protocol === "snmp"}
    <div class="form-row">
      <label for="add-version">SNMP Version</label>
      <select id="add-version" bind:value={version}>
        <option value="1">v1</option>
        <option value="2c">v2c</option>
        <option value="3">v3</option>
      </select>
    </div>
    <div class="form-row">
      <label for="add-community">Community</label>
      <input
        id="add-community"
        type="text"
        bind:value={community}
        placeholder="public"
      />
    </div>
  {/if}
  {#if protocol === "modbus"}
    <div class="form-row">
      <label for="add-unitid">Unit ID</label>
      <input id="add-unitid" type="text" bind:value={unitId} placeholder="1" />
    </div>
  {/if}
  <div class="form-actions">
    <button
      type="button"
      class="cancel-btn"
      onclick={() => {
        reset();
        onCancel?.();
      }}
      disabled={saving}
    >
      Cancel
    </button>
    <button
      type="button"
      class="save-btn"
      onclick={submit}
      disabled={saving || !deviceId || protocolsForUi.length === 0}
    >
      {saving ? "Saving…" : "Add Device"}
    </button>
  </div>
</div>

<style lang="scss">
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
</style>
