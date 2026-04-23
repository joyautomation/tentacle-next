<script lang="ts">
  import type { GatewayConfig, GatewayDevice } from '$lib/types/gateway';
  import { apiPut, apiDelete } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { slide } from 'svelte/transition';
  import { ChevronRight } from '@joyautomation/salt/icons';

  let { gatewayConfig, error }: {
    gatewayConfig: GatewayConfig | null;
    error: string | null;
  } = $props();

  let showAddDevice = $state(false);
  let saving = $state(false);
  let expandedDevice: string | null = $state(null);
  let deleteTarget: { deviceId: string; varCount: number } | null = $state(null);
  let deleteConfirmInput = $state('');
  let editHost = $state('');
  let editPort = $state('');
  let editSlot = $state('');
  let editEndpointUrl = $state('');
  let editVersion = $state('2c');
  let editCommunity = $state('public');
  let editUnitId = $state('1');
  let editScanRate = $state('');
  let editDeadbandValue = $state('');
  let editDeadbandMinTime = $state('');
  let editDeadbandMaxTime = $state('');
  let editDisableRBE = $state(false);

  const allProtocols = [
    { value: 'ethernetip', label: 'EtherNet/IP', defaultScanRate: 1000 },
    { value: 'opcua', label: 'OPC UA', defaultScanRate: 1000 },
    { value: 'snmp', label: 'SNMP', defaultScanRate: 5000 },
    { value: 'modbus', label: 'Modbus TCP', defaultScanRate: 1000 },
  ] as const;

  const moduleDevices = $derived(
    (gatewayConfig?.devices?.filter(d => d.autoManaged) ?? [])
      .sort((a, b) => a.deviceId.localeCompare(b.deviceId))
  );
  const externalDevices = $derived(
    (gatewayConfig?.devices?.filter(d => !d.autoManaged) ?? [])
      .sort((a, b) => a.protocol.localeCompare(b.protocol) || a.deviceId.localeCompare(b.deviceId))
  );

  const protocolLabels: Record<string, string> = {
    ...Object.fromEntries(allProtocols.map(p => [p.value, p.label])),
    network: 'Network',
    gateway: 'Gateway',
    mqtt: 'MQTT',
    orchestrator: 'Orchestrator',
    gitops: 'GitOps',
  };

  const protocolDefaults: Record<string, number> = Object.fromEntries(
    allProtocols.map(p => [p.value, p.defaultScanRate])
  );

  const defaultProtocol = $derived(gatewayConfig?.availableProtocols?.[0] ?? 'ethernetip');
  const availableProtocols = $derived(
    gatewayConfig?.availableProtocols?.length
      ? allProtocols.filter(p => gatewayConfig!.availableProtocols!.includes(p.value))
      : []
  );

  let newDevice = $state({
    deviceId: '',
    protocol: 'ethernetip' as string,
    host: '',
    port: '',
    slot: '',
    endpointUrl: '',
    version: '2c',
    community: 'public',
    unitId: '1',
  });

  $effect(() => {
    if (defaultProtocol && !gatewayConfig?.availableProtocols?.includes(newDevice.protocol)) {
      newDevice.protocol = defaultProtocol;
    }
  });

  function resetNewDevice() {
    newDevice = {
      deviceId: '', protocol: defaultProtocol, host: '', port: '', slot: '',
      endpointUrl: '', version: '2c', community: 'public', unitId: '1',
    };
  }

  function varCount(device: GatewayDevice): number {
    return (gatewayConfig?.variables?.filter(v => v.deviceId === device.deviceId).length ?? 0)
      + (gatewayConfig?.udtVariables?.filter(v => v.deviceId === device.deviceId).length ?? 0);
  }

  function toggleDeviceSettings(device: GatewayDevice) {
    if (expandedDevice === device.deviceId) {
      expandedDevice = null;
    } else {
      expandedDevice = device.deviceId;
      editHost = device.host ?? '';
      editPort = device.port?.toString() ?? '';
      editSlot = device.slot?.toString() ?? '';
      editEndpointUrl = device.endpointUrl ?? '';
      editVersion = device.version ?? '2c';
      editCommunity = device.community ?? 'public';
      editUnitId = device.unitId?.toString() ?? '1';
      editScanRate = device.scanRate?.toString() ?? '';
      editDeadbandValue = device.deadband?.value?.toString() ?? '';
      editDeadbandMinTime = device.deadband?.minTime?.toString() ?? '';
      editDeadbandMaxTime = device.deadband?.maxTime?.toString() ?? '';
      editDisableRBE = device.disableRBE ?? false;
    }
  }

  async function saveDeviceSettings(device: GatewayDevice) {
    saving = true;
    try {
      const input: Record<string, unknown> = {
        protocol: device.protocol,
        ...(device.protocol !== 'opcua' && editHost ? { host: editHost } : {}),
        ...(editPort ? { port: parseInt(editPort) } : {}),
        ...(device.protocol === 'ethernetip' && editSlot ? { slot: parseInt(editSlot) } : {}),
        ...(device.protocol === 'opcua' && editEndpointUrl ? { endpointUrl: editEndpointUrl } : {}),
        ...(device.protocol === 'snmp' ? { version: editVersion, community: editCommunity } : {}),
        ...(device.protocol === 'modbus' && editUnitId ? { unitId: parseInt(editUnitId) } : {}),
      };

      if (editScanRate) input.scanRate = parseInt(editScanRate);
      if (editDisableRBE) {
        input.disableRBE = true;
      } else if (editDeadbandValue) {
        input.deadband = {
          value: parseFloat(editDeadbandValue),
          ...(editDeadbandMinTime ? { minTime: parseInt(editDeadbandMinTime) } : {}),
          ...(editDeadbandMaxTime ? { maxTime: parseInt(editDeadbandMaxTime) } : {}),
        };
      }

      const result = await apiPut(`/devices/${encodeURIComponent(device.deviceId)}`, input);

      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Device "${device.deviceId}" settings saved`, type: 'success' });
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Failed', type: 'error' });
    } finally {
      saving = false;
    }
  }

  async function addDevice() {
    if (!newDevice.deviceId) return;
    saving = true;
    try {
      const deviceBody: Record<string, unknown> = {
        protocol: newDevice.protocol,
        ...(newDevice.protocol !== 'opcua' && newDevice.host ? { host: newDevice.host } : {}),
        ...(newDevice.port ? { port: parseInt(newDevice.port) } : {}),
        ...(newDevice.protocol === 'ethernetip' && newDevice.slot ? { slot: parseInt(newDevice.slot) } : {}),
        ...(newDevice.protocol === 'opcua' && newDevice.endpointUrl ? { endpointUrl: newDevice.endpointUrl } : {}),
        ...(newDevice.protocol === 'snmp' ? { version: newDevice.version, community: newDevice.community } : {}),
        ...(newDevice.protocol === 'modbus' && newDevice.unitId ? { unitId: parseInt(newDevice.unitId) } : {}),
      };
      const result = await apiPut(`/devices/${encodeURIComponent(newDevice.deviceId)}`, deviceBody);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Device "${newDevice.deviceId}" added`, type: 'success' });
        resetNewDevice();
        showAddDevice = false;
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Failed', type: 'error' });
    } finally {
      saving = false;
    }
  }

  async function removeDevice(deviceId: string) {
    saving = true;
    try {
      const result = await apiDelete(`/devices/${encodeURIComponent(deviceId)}`);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({ message: `Device "${deviceId}" removed`, type: 'success' });
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Failed', type: 'error' });
    } finally {
      saving = false;
    }
  }

  function formatDeviceInfo(device: GatewayDevice): string {
    switch (device.protocol) {
      case 'ethernetip': return `${device.host ?? ''}:${device.port ?? 44818}`;
      case 'opcua': return String(device.endpointUrl ?? '');
      case 'snmp': return `${device.host ?? ''}:${device.port ?? 161} (v${device.version ?? '2c'})`;
      case 'modbus': return `${device.host ?? ''}:${device.port ?? 502} (unit ${device.unitId ?? 1})`;
      case 'plc': return 'internal bus';
      default: return '';
    }
  }
</script>

<div class="devices-page">
  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  <div class="devices-header">
    <h1>Devices</h1>
  </div>

  <!-- Modules Section -->
  {#if moduleDevices.length > 0}
    <div class="section">
      <div class="section-header">
        <h2>Modules</h2>
        <span class="count-badge">{moduleDevices.length}</span>
      </div>
      <div class="tree">
        {#each moduleDevices as device}
          <div class="tree-node">
            <div class="device-row" role="button" tabindex="0" onclick={() => toggleDeviceSettings(device)} onkeydown={(e) => e.key === 'Enter' && toggleDeviceSettings(device)}>
              <span class="chevron" class:expanded={expandedDevice === device.deviceId}><ChevronRight size="0.875rem" /></span>
              <span class="leaf-name">{protocolLabels[device.protocol] ?? device.protocol}</span>
              {#if device.disableRBE}
                <span class="setting-badge warn">RBE off</span>
              {:else if device.deadband}
                <span class="setting-badge">DB {device.deadband.value}</span>
              {/if}
              <span class="var-count">{varCount(device)} vars</span>
            </div>
            {#if expandedDevice === device.deviceId}
              <div class="device-settings" transition:slide|local={{ duration: 200 }}>
                <div class="settings-grid">
                  <div class="setting-group">
                    <h3>RBE / Deadband</h3>
                    <label class="checkbox-label">
                      <input type="checkbox" bind:checked={editDisableRBE} />
                      <span>Disable RBE (publish every update)</span>
                    </label>
                    {#if !editDisableRBE}
                      <div class="form-row">
                        <label for="db-val-{device.deviceId}">Deadband</label>
                        <input id="db-val-{device.deviceId}" type="number" bind:value={editDeadbandValue} placeholder="0" min="0" step="0.1" />
                      </div>
                      <div class="form-row">
                        <label for="db-min-{device.deviceId}">Min Time (ms)</label>
                        <input id="db-min-{device.deviceId}" type="number" bind:value={editDeadbandMinTime} placeholder="none" min="0" step="100" />
                      </div>
                      <div class="form-row">
                        <label for="db-max-{device.deviceId}">Max Time (ms)</label>
                        <input id="db-max-{device.deviceId}" type="number" bind:value={editDeadbandMaxTime} placeholder="none" min="0" step="1000" />
                      </div>
                    {/if}
                  </div>
                </div>
                <div class="form-actions">
                  <button class="save-btn" onclick={() => saveDeviceSettings(device)} disabled={saving}>
                    {saving ? 'Saving...' : 'Save Settings'}
                  </button>
                </div>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Devices Section -->
  <div class="section">
    <div class="section-header">
      <h2>Devices</h2>
      <span class="count-badge">{externalDevices.length}</span>
      <button class="add-btn" onclick={() => { showAddDevice = !showAddDevice; }} disabled={saving}>
        {showAddDevice ? 'Cancel' : '+ Add Device'}
      </button>
    </div>

    {#if showAddDevice}
      <div class="add-form">
        <div class="form-row">
          <label for="gw-device-id">Device ID</label>
          <input id="gw-device-id" type="text" bind:value={newDevice.deviceId} placeholder="e.g. plc-1" />
        </div>
        <div class="form-row">
          <label for="gw-protocol">Protocol</label>
          {#if availableProtocols.length === 0}
            <p class="no-protocols">No protocol modules connected. Start a protocol service (EtherNet/IP, OPC UA, SNMP, or Modbus) to add devices.</p>
          {:else}
            <select id="gw-protocol" bind:value={newDevice.protocol}>
              {#each availableProtocols as proto}
                <option value={proto.value}>{proto.label}</option>
              {/each}
            </select>
          {/if}
        </div>
        {#if newDevice.protocol === 'opcua'}
          <div class="form-row">
            <label for="gw-endpoint">Endpoint URL</label>
            <input id="gw-endpoint" type="text" bind:value={newDevice.endpointUrl} placeholder="opc.tcp://192.168.1.50:4840" />
          </div>
        {:else}
          <div class="form-row">
            <label for="gw-host">Host</label>
            <input id="gw-host" type="text" bind:value={newDevice.host} placeholder="192.168.1.100" />
          </div>
          <div class="form-row">
            <label for="gw-port">Port</label>
            <input id="gw-port" type="text" bind:value={newDevice.port} placeholder={newDevice.protocol === 'ethernetip' ? '44818' : newDevice.protocol === 'snmp' ? '161' : '502'} />
          </div>
          {#if newDevice.protocol === 'ethernetip'}
            <div class="form-row">
              <label for="gw-slot">Slot</label>
              <input id="gw-slot" type="text" bind:value={newDevice.slot} placeholder="0" />
            </div>
          {/if}
        {/if}
        {#if newDevice.protocol === 'snmp'}
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
            <input id="gw-community" type="text" bind:value={newDevice.community} placeholder="public" />
          </div>
        {/if}
        {#if newDevice.protocol === 'modbus'}
          <div class="form-row">
            <label for="gw-unitid">Unit ID</label>
            <input id="gw-unitid" type="text" bind:value={newDevice.unitId} placeholder="1" />
          </div>
        {/if}
        <div class="form-actions">
          <button class="save-btn" onclick={addDevice} disabled={saving || !newDevice.deviceId || availableProtocols.length === 0}>
            {saving ? 'Saving...' : 'Add Device'}
          </button>
        </div>
      </div>
    {/if}

    {#if externalDevices.length > 0}
      <div class="tree">
        {#each externalDevices as device}
          <div class="tree-node">
            <div class="device-row" role="button" tabindex="0" onclick={() => toggleDeviceSettings(device)} onkeydown={(e) => e.key === 'Enter' && toggleDeviceSettings(device)}>
              <span class="chevron" class:expanded={expandedDevice === device.deviceId}><ChevronRight size="0.875rem" /></span>
              <span class="protocol-badge">{protocolLabels[device.protocol] ?? device.protocol}</span>
              <span class="leaf-name">{device.deviceId}</span>
              <span class="device-host">{formatDeviceInfo(device)}</span>
              {#if device.scanRate}
                <span class="setting-badge">{device.scanRate}ms</span>
              {/if}
              {#if device.disableRBE}
                <span class="setting-badge warn">RBE off</span>
              {:else if device.deadband}
                <span class="setting-badge">DB {device.deadband.value}</span>
              {/if}
              <span class="var-count">{varCount(device)} vars</span>
              <button class="delete-btn" onclick={(e: MouseEvent) => {
                e.stopPropagation();
                deleteTarget = { deviceId: device.deviceId, varCount: varCount(device) };
                deleteConfirmInput = '';
              }} disabled={saving} title="Remove device">&times;</button>
            </div>
            {#if expandedDevice === device.deviceId}
              <div class="device-settings" transition:slide|local={{ duration: 200 }}>
                <div class="settings-grid">
                  <div class="setting-group">
                    <h3>Connection</h3>
                    {#if device.protocol === 'opcua'}
                      <div class="form-row">
                        <label for="edit-endpoint-{device.deviceId}">Endpoint URL</label>
                        <input id="edit-endpoint-{device.deviceId}" type="text" bind:value={editEndpointUrl} placeholder="opc.tcp://192.168.1.50:4840" />
                      </div>
                    {:else}
                      <div class="form-row">
                        <label for="edit-host-{device.deviceId}">Host</label>
                        <input id="edit-host-{device.deviceId}" type="text" bind:value={editHost} placeholder="192.168.1.100" />
                      </div>
                      <div class="form-row">
                        <label for="edit-port-{device.deviceId}">Port</label>
                        <input id="edit-port-{device.deviceId}" type="text" bind:value={editPort} placeholder={device.protocol === 'ethernetip' ? '44818' : device.protocol === 'snmp' ? '161' : '502'} />
                      </div>
                      {#if device.protocol === 'ethernetip'}
                        <div class="form-row">
                          <label for="edit-slot-{device.deviceId}">Slot</label>
                          <input id="edit-slot-{device.deviceId}" type="text" bind:value={editSlot} placeholder="0" />
                        </div>
                      {/if}
                      {#if device.protocol === 'snmp'}
                        <div class="form-row">
                          <label for="edit-version-{device.deviceId}">SNMP Version</label>
                          <select id="edit-version-{device.deviceId}" bind:value={editVersion}>
                            <option value="1">v1</option>
                            <option value="2c">v2c</option>
                            <option value="3">v3</option>
                          </select>
                        </div>
                        <div class="form-row">
                          <label for="edit-community-{device.deviceId}">Community</label>
                          <input id="edit-community-{device.deviceId}" type="text" bind:value={editCommunity} placeholder="public" />
                        </div>
                      {/if}
                      {#if device.protocol === 'modbus'}
                        <div class="form-row">
                          <label for="edit-unitid-{device.deviceId}">Unit ID</label>
                          <input id="edit-unitid-{device.deviceId}" type="text" bind:value={editUnitId} placeholder="1" />
                        </div>
                      {/if}
                    {/if}
                  </div>
                  <div class="setting-group">
                    <h3>Polling</h3>
                    <div class="form-row">
                      <label for="sr-{device.deviceId}">Scan Rate (ms)</label>
                      <input id="sr-{device.deviceId}" type="number" bind:value={editScanRate} placeholder={String(protocolDefaults[device.protocol] ?? 1000)} min="100" step="100" />
                    </div>
                  </div>
                  <div class="setting-group">
                    <h3>RBE / Deadband</h3>
                    <label class="checkbox-label">
                      <input type="checkbox" bind:checked={editDisableRBE} />
                      <span>Disable RBE (publish every scan)</span>
                    </label>
                    {#if !editDisableRBE}
                      <div class="form-row">
                        <label for="db-val-{device.deviceId}">Deadband</label>
                        <input id="db-val-{device.deviceId}" type="number" bind:value={editDeadbandValue} placeholder="0" min="0" step="0.1" />
                      </div>
                      <div class="form-row">
                        <label for="db-min-{device.deviceId}">Min Time (ms)</label>
                        <input id="db-min-{device.deviceId}" type="number" bind:value={editDeadbandMinTime} placeholder="none" min="0" step="100" />
                      </div>
                      <div class="form-row">
                        <label for="db-max-{device.deviceId}">Max Time (ms)</label>
                        <input id="db-max-{device.deviceId}" type="number" bind:value={editDeadbandMaxTime} placeholder="none" min="0" step="1000" />
                      </div>
                    {/if}
                  </div>
                </div>
                <div class="form-actions">
                  <button class="save-btn" onclick={() => saveDeviceSettings(device)} disabled={saving}>
                    {saving ? 'Saving...' : 'Save Settings'}
                  </button>
                </div>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {:else if !error && !showAddDevice}
      <div class="empty-state">
        <p>No devices configured. Click "Add Device" to connect to a scanner.</p>
      </div>
    {/if}
  </div>

  {#if deleteTarget}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onkeydown={(e) => { if (e.key === 'Escape') deleteTarget = null; }} onclick={() => deleteTarget = null}>
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal" onclick={(e) => e.stopPropagation()}>
        <h2>Delete Device</h2>
        <p class="modal-warning">This will permanently remove <strong>{deleteTarget.deviceId}</strong> and all <strong>{deleteTarget.varCount}</strong> variable{deleteTarget.varCount !== 1 ? 's' : ''} configured on it. Template name overrides, deadband settings, and browse data for this device will also be lost.</p>
        <p class="modal-confirm-label">Type <strong>{deleteTarget.deviceId}</strong> to confirm:</p>
        <input
          class="modal-input"
          bind:value={deleteConfirmInput}
          placeholder={deleteTarget.deviceId}
          onkeydown={(e) => { if (e.key === 'Enter' && deleteConfirmInput === deleteTarget?.deviceId) { removeDevice(deleteTarget.deviceId); deleteTarget = null; } }}
        />
        <div class="modal-actions">
          <button class="modal-cancel-btn" onclick={() => deleteTarget = null}>Cancel</button>
          <button
            class="modal-delete-btn"
            disabled={deleteConfirmInput !== deleteTarget.deviceId || saving}
            onclick={() => { if (deleteTarget) { removeDevice(deleteTarget.deviceId); deleteTarget = null; } }}
          >{saving ? 'Deleting...' : 'Delete Device'}</button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style lang="scss">
  .devices-page { padding: 2rem; }

  .devices-header {
    display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1.5rem;
    h1 { font-size: 1.5rem; font-weight: 600; color: var(--theme-text); margin: 0; }
  }

  .section { margin-bottom: 1.5rem; }

  .section-header {
    display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.75rem;
    h2 { font-size: 0.875rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; color: var(--theme-text-muted); margin: 0; }
  }

  .count-badge {
    padding: 0.2rem 0.5rem; border-radius: var(--rounded-md); font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace; background: var(--badge-teal-bg); color: var(--badge-teal-text);
  }

  .tree { background: var(--theme-surface); border: 1px solid var(--theme-border); border-radius: var(--rounded-lg); overflow: hidden; }

  .tree-node {
    &:not(:last-child) { border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent); }
  }

  .device-row {
    display: flex; align-items: center; gap: 0.5rem; padding: 0.625rem 1rem;
    font-size: 0.8125rem; cursor: pointer;
    &:hover { background: color-mix(in srgb, var(--theme-text) 3%, transparent); }
  }

  .chevron { display: inline-flex; flex-shrink: 0; color: var(--theme-text-muted); transition: transform 0.15s ease; &.expanded { transform: rotate(90deg); } }

  .leaf-name { font-family: 'IBM Plex Mono', monospace; color: var(--theme-text); font-weight: 500; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .device-host {
    font-size: 0.75rem; font-family: 'IBM Plex Mono', monospace; color: var(--badge-muted-text);
    padding: 0.1rem 0.35rem; border-radius: var(--rounded-sm); background: var(--badge-muted-bg);
  }

  .setting-badge {
    font-size: 0.75rem; font-family: 'IBM Plex Mono', monospace; color: var(--badge-teal-text);
    padding: 0.1rem 0.3rem; border-radius: var(--rounded-sm); background: var(--badge-teal-bg);
    &.warn { background: var(--badge-amber-bg, #fef3c7); color: var(--badge-amber-text, #92400e); }
  }

  .var-count { font-size: 0.75rem; color: var(--theme-text-muted); margin-left: auto; flex-shrink: 0; }

  .protocol-badge {
    padding: 0.15rem 0.4rem; border-radius: var(--rounded-sm); font-size: 0.6875rem;
    font-weight: 600; background: var(--badge-teal-bg); color: var(--badge-teal-text); flex-shrink: 0;
  }

  .device-settings {
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    padding: 1rem 1rem 1rem 2.5rem;
    background: color-mix(in srgb, var(--theme-text) 2%, transparent);
  }

  .settings-grid {
    display: grid; grid-template-columns: 1fr 1fr; gap: 1.5rem;
    @media (max-width: 640px) { grid-template-columns: 1fr; }
  }

  .setting-group {
    h3 {
      font-size: 0.6875rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em;
      color: var(--theme-text-muted); margin: 0 0 0.75rem;
    }
  }

  .checkbox-label {
    display: flex; align-items: center; gap: 0.5rem; font-size: 0.8125rem;
    color: var(--theme-text); cursor: pointer; margin-bottom: 0.75rem;
    input[type="checkbox"] {
      appearance: none;
      width: 16px; height: 16px;
      border: 1.5px solid var(--theme-border);
      border-radius: var(--rounded-sm, 3px);
      background: var(--theme-input-bg);
      cursor: pointer; flex-shrink: 0; position: relative;
      transition: background 0.15s ease, border-color 0.15s ease;
      &:checked {
        background: var(--theme-primary);
        border-color: var(--theme-primary);
        &::after {
          content: ''; position: absolute; left: 4px; top: 1px;
          width: 5px; height: 9px; border: solid white; border-width: 0 2px 2px 0;
          transform: rotate(45deg);
        }
      }
      &:hover { border-color: var(--theme-primary); }
    }
  }

  .add-btn {
    margin-left: auto; padding: 0.375rem 0.75rem; font-size: 0.8125rem; font-weight: 500;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: var(--theme-surface); color: var(--theme-text); cursor: pointer;
    &:hover:not(:disabled) { background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface)); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .add-form {
    background: var(--theme-surface); border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg); padding: 1.25rem; margin-bottom: 1rem;
  }

  .form-row {
    display: flex; align-items: center; gap: 1rem; margin-bottom: 0.75rem;
    label { font-size: 0.8125rem; color: var(--theme-text-muted); min-width: 100px; flex-shrink: 0; }
    input, select {
      flex: 1; padding: 0.375rem 0.5rem; font-size: 0.8125rem; font-family: 'IBM Plex Mono', monospace;
      border: 1px solid var(--theme-border); border-radius: var(--rounded-md); background: var(--theme-input-bg); color: var(--theme-text);
    }
  }

  .form-actions { display: flex; justify-content: flex-end; margin-top: 1rem; }

  .save-btn {
    padding: 0.375rem 1rem; font-size: 0.8125rem; font-weight: 500; border: none;
    border-radius: var(--rounded-md); background: var(--theme-primary); color: white; cursor: pointer;
    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .delete-btn {
    display: flex; align-items: center; justify-content: center; width: 28px; height: 28px;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-sm);
    background: var(--theme-surface); color: var(--theme-text-muted); font-size: 1rem;
    line-height: 1; cursor: pointer; flex-shrink: 0;
    &:hover:not(:disabled) { color: var(--color-red-500, #ef4444); border-color: var(--color-red-500, #ef4444); background: color-mix(in srgb, var(--color-red-500, #ef4444) 10%, transparent); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .error-box {
    padding: 1rem; border-radius: var(--rounded-lg); background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444); margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .empty-state { padding: 3rem 2rem; text-align: center; p { color: var(--theme-text-muted); font-size: 0.875rem; } }

  .modal-backdrop {
    position: fixed; inset: 0; z-index: 1000;
    background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center;
    padding: 1rem;
  }
  .modal {
    background: var(--theme-surface); border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg); padding: 1.5rem; max-width: 420px; width: 100%;
    h2 { font-size: 1.125rem; font-weight: 600; color: var(--theme-text); margin: 0 0 1rem; }
  }
  .modal-warning {
    font-size: 0.8125rem; color: var(--color-red-500, #ef4444); line-height: 1.5; margin: 0 0 1rem;
  }
  .modal-confirm-label { font-size: 0.8125rem; color: var(--theme-text-muted); margin: 0 0 0.5rem; }
  .modal-input {
    width: 100%; padding: 0.375rem 0.5rem; font-size: 0.8125rem;
    font-family: 'IBM Plex Mono', monospace; border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md); background: var(--theme-input-bg); color: var(--theme-text);
    box-sizing: border-box;
  }
  .modal-actions { display: flex; justify-content: flex-end; gap: 0.5rem; margin-top: 1rem; }
  .modal-cancel-btn {
    padding: 0.375rem 0.75rem; font-size: 0.8125rem; font-weight: 500;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: var(--theme-surface); color: var(--theme-text); cursor: pointer;
    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface)); }
  }
  .modal-delete-btn {
    padding: 0.375rem 1rem; font-size: 0.8125rem; font-weight: 500;
    border: none; border-radius: var(--rounded-md);
    background: var(--color-red-500, #ef4444); color: white; cursor: pointer;
    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  // ── Responsive ──
  @media (max-width: 640px) {
    .devices-page { padding: 1rem 0.75rem; }
    .devices-header { margin-bottom: 1rem; h1 { font-size: 1.125rem; } }
    .add-form { padding: 0.875rem; }
    .form-row {
      flex-direction: column; align-items: stretch; gap: 0.25rem;
      label { min-width: unset; }
    }
    .device-row {
      flex-wrap: wrap; gap: 0.375rem 0.5rem; padding: 0.75rem 0.75rem;
    }
    .device-host { order: 10; width: 100%; margin-top: 0.125rem; }
    .var-count { margin-left: 0; }
    .delete-btn { margin-left: auto; }
    .device-settings { padding: 0.75rem; }
    .tree { border-radius: var(--rounded-md); }
  }
</style>
