<script lang="ts">
  import { slide } from "svelte/transition";
  import { untrack } from "svelte";
  import type { GatewayDevice } from "$lib/types/gateway";
  import { apiPut } from "$lib/api/client";
  import { invalidateAll } from "$app/navigation";
  import { state as saltState } from "@joyautomation/salt";

  type Props = {
    device: GatewayDevice;
    onClose: () => void;
    onSaved?: () => void;
  };

  let { device, onClose, onSaved }: Props = $props();

  const protocolDefaults: Record<string, number> = {
    ethernetip: 1000,
    opcua: 1000,
    snmp: 5000,
    modbus: 1000,
  };

  // Snapshot device fields once — the panel edits a specific instance until closed.
  let editHost = $state(untrack(() => device.host ?? ""));
  let editPort = $state(untrack(() => device.port?.toString() ?? ""));
  let editSlot = $state(untrack(() => device.slot?.toString() ?? ""));
  let editEndpointUrl = $state(untrack(() => device.endpointUrl ?? ""));
  let editVersion = $state(untrack(() => device.version ?? "2c"));
  let editCommunity = $state(untrack(() => device.community ?? "public"));
  let editUnitId = $state(untrack(() => device.unitId?.toString() ?? "1"));
  let editScanRate = $state(untrack(() => device.scanRate?.toString() ?? ""));
  let editDeadbandValue = $state(untrack(() => device.deadband?.value?.toString() ?? ""));
  let editDeadbandMinTime = $state(untrack(() => device.deadband?.minTime?.toString() ?? ""));
  let editDeadbandMaxTime = $state(untrack(() => device.deadband?.maxTime?.toString() ?? ""));
  let editDisableRBE = $state(untrack(() => device.disableRBE ?? false));
  let savingEdit = $state(false);

  async function saveDeviceSettings() {
    savingEdit = true;
    try {
      const input: Record<string, unknown> = {
        protocol: device.protocol,
        ...(!device.autoManaged && device.protocol !== "opcua" && editHost
          ? { host: editHost }
          : {}),
        ...(!device.autoManaged && editPort ? { port: parseInt(editPort) } : {}),
        ...(!device.autoManaged && device.protocol === "ethernetip" && editSlot
          ? { slot: parseInt(editSlot) }
          : {}),
        ...(!device.autoManaged &&
        device.protocol === "opcua" &&
        editEndpointUrl
          ? { endpointUrl: editEndpointUrl }
          : {}),
        ...(!device.autoManaged && device.protocol === "snmp"
          ? { version: editVersion, community: editCommunity }
          : {}),
        ...(!device.autoManaged && device.protocol === "modbus" && editUnitId
          ? { unitId: parseInt(editUnitId) }
          : {}),
      };

      if (!device.autoManaged && editScanRate)
        input.scanRate = parseInt(editScanRate);
      if (editDisableRBE) {
        input.disableRBE = true;
      } else if (editDeadbandValue) {
        input.deadband = {
          value: parseFloat(editDeadbandValue),
          ...(editDeadbandMinTime
            ? { minTime: parseInt(editDeadbandMinTime) }
            : {}),
          ...(editDeadbandMaxTime
            ? { maxTime: parseInt(editDeadbandMaxTime) }
            : {}),
        };
      }

      const result = await apiPut(
        `/devices/${encodeURIComponent(device.deviceId)}`,
        input,
      );
      if (result.error) {
        saltState.addNotification({
          message: result.error.error,
          type: "error",
        });
      } else {
        saltState.addNotification({
          message: `Device "${device.deviceId}" settings saved`,
          type: "success",
        });
        await invalidateAll();
        onSaved?.();
        onClose();
      }
    } catch (err) {
      saltState.addNotification({
        message: err instanceof Error ? err.message : "Failed",
        type: "error",
      });
    } finally {
      savingEdit = false;
    }
  }
</script>

<div class="device-settings" transition:slide={{ duration: 150 }}>
  <div class="settings-grid">
    {#if !device.autoManaged}
      <div class="setting-group">
        <h3>Connection</h3>
        {#if device.protocol === "opcua"}
          <div class="form-row">
            <label for="dsp-endpoint-{device.deviceId}">Endpoint URL</label>
            <input
              id="dsp-endpoint-{device.deviceId}"
              type="text"
              bind:value={editEndpointUrl}
              placeholder="opc.tcp://192.168.1.50:4840"
            />
          </div>
        {:else}
          <div class="form-row">
            <label for="dsp-host-{device.deviceId}">Host</label>
            <input
              id="dsp-host-{device.deviceId}"
              type="text"
              bind:value={editHost}
              placeholder="192.168.1.100"
            />
          </div>
          <div class="form-row">
            <label for="dsp-port-{device.deviceId}">Port</label>
            <input
              id="dsp-port-{device.deviceId}"
              type="text"
              bind:value={editPort}
              placeholder={device.protocol === "ethernetip"
                ? "44818"
                : device.protocol === "snmp"
                  ? "161"
                  : "502"}
            />
          </div>
          {#if device.protocol === "ethernetip"}
            <div class="form-row">
              <label for="dsp-slot-{device.deviceId}">Slot</label>
              <input
                id="dsp-slot-{device.deviceId}"
                type="text"
                bind:value={editSlot}
                placeholder="0"
              />
            </div>
          {/if}
          {#if device.protocol === "snmp"}
            <div class="form-row">
              <label for="dsp-version-{device.deviceId}">SNMP Version</label>
              <select
                id="dsp-version-{device.deviceId}"
                bind:value={editVersion}
              >
                <option value="1">v1</option>
                <option value="2c">v2c</option>
                <option value="3">v3</option>
              </select>
            </div>
            <div class="form-row">
              <label for="dsp-community-{device.deviceId}">Community</label>
              <input
                id="dsp-community-{device.deviceId}"
                type="text"
                bind:value={editCommunity}
                placeholder="public"
              />
            </div>
          {/if}
          {#if device.protocol === "modbus"}
            <div class="form-row">
              <label for="dsp-unitid-{device.deviceId}">Unit ID</label>
              <input
                id="dsp-unitid-{device.deviceId}"
                type="text"
                bind:value={editUnitId}
                placeholder="1"
              />
            </div>
          {/if}
        {/if}
      </div>
      <div class="setting-group">
        <h3>Polling</h3>
        <div class="form-row">
          <label for="dsp-sr-{device.deviceId}">Scan Rate (ms)</label>
          <input
            id="dsp-sr-{device.deviceId}"
            type="number"
            bind:value={editScanRate}
            placeholder={String(protocolDefaults[device.protocol] ?? 1000)}
            min="100"
            step="100"
          />
        </div>
      </div>
    {/if}
    <div class="setting-group">
      <h3>RBE / Deadband</h3>
      <label class="checkbox-label">
        <input type="checkbox" bind:checked={editDisableRBE} />
        <span>Disable RBE (publish every update)</span>
      </label>
      {#if !editDisableRBE}
        <div class="form-row">
          <label for="dsp-db-val-{device.deviceId}">Deadband</label>
          <input
            id="dsp-db-val-{device.deviceId}"
            type="number"
            bind:value={editDeadbandValue}
            placeholder="0"
            min="0"
            step="0.1"
          />
        </div>
        <div class="form-row">
          <label for="dsp-db-min-{device.deviceId}">Min Time (ms)</label>
          <input
            id="dsp-db-min-{device.deviceId}"
            type="number"
            bind:value={editDeadbandMinTime}
            placeholder="none"
            min="0"
            step="100"
          />
        </div>
        <div class="form-row">
          <label for="dsp-db-max-{device.deviceId}">Max Time (ms)</label>
          <input
            id="dsp-db-max-{device.deviceId}"
            type="number"
            bind:value={editDeadbandMaxTime}
            placeholder="none"
            min="0"
            step="1000"
          />
        </div>
      {/if}
    </div>
  </div>
  <div class="form-actions">
    <button
      type="button"
      class="cancel-btn"
      onclick={onClose}
      disabled={savingEdit}
    >
      Cancel
    </button>
    <button
      type="button"
      class="save-btn"
      onclick={saveDeviceSettings}
      disabled={savingEdit}
    >
      {savingEdit ? "Saving…" : "Save"}
    </button>
  </div>
</div>

<style lang="scss">
  .device-settings {
    border-top: 1px solid
      color-mix(in srgb, var(--theme-border) 50%, transparent);
    padding: 0.75rem;
    background: color-mix(in srgb, var(--theme-text) 2%, transparent);
  }

  .settings-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;

    @media (max-width: 640px) {
      grid-template-columns: 1fr;
    }
  }

  .setting-group {
    h3 {
      font-size: 0.6875rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--theme-text-muted);
      margin: 0 0 0.5rem;
    }
  }

  .form-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.5rem;

    label {
      font-size: 0.75rem;
      color: var(--theme-text-muted);
      min-width: 6rem;
      flex-shrink: 0;
    }

    input,
    select {
      flex: 1;
      padding: 0.25rem 0.375rem;
      font-size: 0.75rem;
      font-family: var(--font-mono, "IBM Plex Mono", monospace);
      border: 1px solid var(--theme-border);
      border-radius: 0.25rem;
      background: var(--theme-input-bg, var(--theme-background));
      color: var(--theme-text);
    }
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.75rem;
    color: var(--theme-text);
    cursor: pointer;
    margin-bottom: 0.5rem;

    input[type="checkbox"] {
      appearance: none;
      width: 14px;
      height: 14px;
      border: 1.5px solid var(--theme-border);
      border-radius: var(--rounded-sm, 3px);
      background: var(--theme-input-bg);
      cursor: pointer;
      flex-shrink: 0;
      position: relative;
      transition:
        background 0.15s ease,
        border-color 0.15s ease;

      &:checked {
        background: var(--theme-primary);
        border-color: var(--theme-primary);

        &::after {
          content: "";
          position: absolute;
          left: 3px;
          top: 0;
          width: 4px;
          height: 8px;
          border: solid white;
          border-width: 0 2px 2px 0;
          transform: rotate(45deg);
        }
      }
    }
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
