<script lang="ts">
  import { invalidateAll } from '$app/navigation';
  import { fly, slide } from 'svelte/transition';
  import { state as saltState } from '@joyautomation/salt';
  import { apiPost, apiPut } from '$lib/api/client';
  import Tabs, { type TabItem } from '$lib/components/Tabs.svelte';
  import type {
    GatewayConfig,
    GatewayDevice,
    GatewayVariable,
    GatewayUdtTemplate,
    GatewayUdtVariable,
    GatewayUdtTemplateMember,
  } from '$lib/types/gateway';
  import {
    FUNCTION_CODES,
    MODBUS_DATATYPES,
    BYTE_ORDERS,
    registerWidth,
    functionCodeToInt,
    intToFunctionCode,
    gatewayDatatype,
    parseCsv,
    type ParsedCsvTag,
  } from './utils';

  let {
    gatewayConfig = null,
    error = null,
  }: {
    gatewayConfig: GatewayConfig | null;
    error: string | null;
  } = $props();

  // ── Derived data from config ──
  const modbusDevices = $derived(
    (gatewayConfig?.devices ?? []).filter((d) => d.protocol === 'modbus' && !d.autoManaged)
  );

  // ── Active view state ──
  let activeTab: 'templates' | 'instances' | 'atomic' = $state('templates');
  let activeTemplateName: string | null = $state(null);
  let activeDeviceId: string | null = $state(null);

  // ── Working copies ──
  type WorkingTemplate = {
    name: string;
    version: string;
    members: WorkingMember[];
  };
  type WorkingMember = {
    name: string;
    datatype: string;
    functionCode: string;
    modbusDatatype: string;
  };
  type WorkingInstance = {
    id: string;
    deviceId: string;
    tag: string;
    templateName: string;
    memberTags: Record<string, string>;
    memberAddresses: Record<string, number>;
    memberByteOrders: Record<string, string>;
  };
  type WorkingAtomicTag = {
    id: string;
    deviceId: string;
    tag: string;
    description: string;
    address: number;
    functionCode: string;
    modbusDatatype: string;
    byteOrder: string;
    bidirectional: boolean;
  };

  let templates: Map<string, WorkingTemplate> = $state(new Map());
  let instances: Map<string, WorkingInstance> = $state(new Map());
  let atomicTags: Map<string, WorkingAtomicTag> = $state(new Map());

  // Snapshots for dirty tracking
  let initialJson = $state('');

  let needsInit = $state(true);
  let saving = $state(false);

  // ── Initialize from config ──
  $effect(() => {
    if (!needsInit || !gatewayConfig) return;
    needsInit = false;

    // Determine which templates are used by Modbus device instances
    const modbusDeviceIds = new Set(modbusDevices.map((d) => d.deviceId));
    const modbusTemplateNames = new Set(
      (gatewayConfig.udtVariables ?? [])
        .filter((uv) => modbusDeviceIds.has(uv.deviceId))
        .map((uv) => uv.templateName)
    );

    const tpls = new Map<string, WorkingTemplate>();
    for (const t of gatewayConfig.udtTemplates ?? []) {
      // Only load templates that are used by Modbus instances or have Modbus member fields
      const hasModbusFields = t.members.some((m) => m.functionCode || m.modbusDatatype);
      if (!modbusTemplateNames.has(t.name) && !hasModbusFields) continue;
      tpls.set(t.name, {
        name: t.name,
        version: t.version ?? '1.0',
        members: t.members.map((m) => ({
          name: m.name,
          datatype: m.datatype,
          functionCode: m.functionCode ?? 'holding',
          modbusDatatype: m.modbusDatatype ?? 'uint16',
        })),
      });
    }
    templates = tpls;

    const insts = new Map<string, WorkingInstance>();
    for (const uv of gatewayConfig.udtVariables ?? []) {
      // Only include instances that belong to Modbus devices
      const dev = modbusDevices.find((d) => d.deviceId === uv.deviceId);
      if (!dev) continue;
      insts.set(uv.id, {
        id: uv.id,
        deviceId: uv.deviceId,
        tag: uv.tag,
        templateName: uv.templateName,
        memberTags: { ...(uv.memberTags ?? {}) },
        memberAddresses: { ...(uv.memberAddresses ?? {}) },
        memberByteOrders: { ...(uv.memberByteOrders ?? {}) },
      });
    }
    instances = insts;

    const tags = new Map<string, WorkingAtomicTag>();
    for (const v of gatewayConfig.variables ?? []) {
      const dev = modbusDevices.find((d) => d.deviceId === v.deviceId);
      if (!dev) continue;
      tags.set(v.id, {
        id: v.id,
        deviceId: v.deviceId,
        tag: v.tag,
        description: v.description ?? '',
        address: v.address ?? 0,
        functionCode: v.functionCode != null ? intToFunctionCode(v.functionCode) : 'holding',
        modbusDatatype: v.modbusDatatype ?? 'uint16',
        byteOrder: v.byteOrder ?? '',
        bidirectional: v.bidirectional ?? false,
      });
    }
    atomicTags = tags;

    initialJson = serializeState();

    // Set initial active view
    if (tpls.size > 0 && !activeTemplateName) {
      activeTemplateName = [...tpls.keys()][0];
    }
    if (modbusDevices.length > 0 && !activeDeviceId) {
      activeDeviceId = modbusDevices[0].deviceId;
    }
  });

  function serializeState(): string {
    return JSON.stringify({
      templates: [...templates.entries()],
      instances: [...instances.entries()],
      atomicTags: [...atomicTags.entries()],
    });
  }

  const isDirty = $derived.by(() => {
    if (!initialJson) return false;
    return serializeState() !== initialJson;
  });

  // ── Template operations ──
  let newTemplateName = $state('');

  function addTemplate() {
    const name = newTemplateName.trim();
    if (!name || templates.has(name)) return;
    templates.set(name, { name, version: '1.0', members: [] });
    templates = new Map(templates);
    activeTemplateName = name;
    activeTab = 'templates';
    newTemplateName = '';
  }

  function deleteTemplate(name: string) {
    templates.delete(name);
    templates = new Map(templates);
    // Remove instances using this template
    for (const [id, inst] of instances) {
      if (inst.templateName === name) instances.delete(id);
    }
    instances = new Map(instances);
    if (activeTemplateName === name) {
      activeTemplateName = templates.size > 0 ? [...templates.keys()][0] : null;
    }
  }

  function addMember(templateName: string) {
    const tpl = templates.get(templateName);
    if (!tpl) return;
    const idx = tpl.members.length + 1;
    tpl.members.push({
      name: `member_${idx}`,
      datatype: 'number',
      functionCode: 'holding',
      modbusDatatype: 'uint16',
    });
    templates = new Map(templates);
  }

  function removeMember(templateName: string, memberIdx: number) {
    const tpl = templates.get(templateName);
    if (!tpl) return;
    tpl.members.splice(memberIdx, 1);
    templates = new Map(templates);
  }

  // ── Instance operations ──
  let newInstanceTag = $state('');
  let newInstanceDevice = $state('');
  let newInstanceTemplate = $state('');

  function addInstance() {
    const tag = newInstanceTag.trim();
    const deviceId = newInstanceDevice;
    const tmplName = newInstanceTemplate;
    if (!tag || !deviceId || !tmplName) return;
    const tpl = templates.get(tmplName);
    if (!tpl) return;

    const id = `${deviceId}/${tag}`;
    if (instances.has(id)) return;

    const memberTags: Record<string, string> = {};
    const memberAddresses: Record<string, number> = {};
    for (const m of tpl.members) {
      memberTags[m.name] = `${tag}.${m.name}`;
      memberAddresses[m.name] = 0;
    }

    instances.set(id, {
      id,
      deviceId,
      tag,
      templateName: tmplName,
      memberTags,
      memberAddresses,
      memberByteOrders: {},
    });
    instances = new Map(instances);
    newInstanceTag = '';
  }

  function deleteInstance(id: string) {
    instances.delete(id);
    instances = new Map(instances);
  }

  function autoAssignAddresses(instanceId: string, baseAddress: number) {
    const inst = instances.get(instanceId);
    if (!inst) return;
    const tpl = templates.get(inst.templateName);
    if (!tpl) return;

    let addr = baseAddress;
    for (const m of tpl.members) {
      inst.memberAddresses[m.name] = addr;
      addr += registerWidth(m.modbusDatatype);
    }
    instances = new Map(instances);
  }

  // ── Atomic tag operations ──
  function addAtomicTag() {
    if (!activeDeviceId) return;
    const idx = atomicTags.size + 1;
    const id = `${activeDeviceId}/tag_${idx}`;
    atomicTags.set(id, {
      id,
      deviceId: activeDeviceId,
      tag: `tag_${idx}`,
      description: '',
      address: 0,
      functionCode: 'holding',
      modbusDatatype: 'uint16',
      byteOrder: '',
      bidirectional: false,
    });
    atomicTags = new Map(atomicTags);
  }

  function deleteAtomicTag(id: string) {
    atomicTags.delete(id);
    atomicTags = new Map(atomicTags);
  }

  // ── CSV import ──
  let showCsvDialog = $state(false);
  let csvText = $state('');
  let csvMode: 'atomic' | 'template' = $state('atomic');
  let csvTemplateName = $state('');

  const csvPreview = $derived.by(() => {
    if (!csvText.trim()) return { tags: [] as ParsedCsvTag[], errors: [] as string[] };
    return parseCsv(csvText);
  });

  function importCsv() {
    const { tags, errors } = csvPreview;
    if (tags.length === 0) return;

    if (csvMode === 'atomic' && activeDeviceId) {
      for (const t of tags) {
        const id = `${activeDeviceId}/${t.name}`;
        atomicTags.set(id, {
          id,
          deviceId: activeDeviceId,
          tag: t.name,
          description: t.description,
          address: t.address,
          functionCode: t.functionCode,
          modbusDatatype: t.datatype,
          byteOrder: t.byteOrder,
          bidirectional: false,
        });
      }
      atomicTags = new Map(atomicTags);
    } else if (csvMode === 'template') {
      const name = csvTemplateName.trim();
      if (!name) return;
      templates.set(name, {
        name,
        version: '1.0',
        members: tags.map((t) => ({
          name: t.name,
          datatype: gatewayDatatype(t.datatype),
          functionCode: t.functionCode,
          modbusDatatype: t.datatype,
        })),
      });
      templates = new Map(templates);
      activeTemplateName = name;
      activeTab = 'templates';
    }

    showCsvDialog = false;
    csvText = '';
  }

  // ── Filtered views ──
  const activeTemplate = $derived(activeTemplateName ? templates.get(activeTemplateName) : null);

  const deviceInstances = $derived.by(() => {
    if (!activeDeviceId) return [];
    return [...instances.values()].filter((i) => i.deviceId === activeDeviceId);
  });

  const deviceAtomicTags = $derived.by(() => {
    if (!activeDeviceId) return [];
    return [...atomicTags.values()].filter((t) => t.deviceId === activeDeviceId);
  });

  // ── Save ──
  async function saveChanges() {
    saving = true;
    try {
      // Group data by device
      const deviceIds = new Set<string>();
      for (const inst of instances.values()) deviceIds.add(inst.deviceId);
      for (const tag of atomicTags.values()) deviceIds.add(tag.deviceId);
      // Include devices that may have had their tags removed
      for (const dev of modbusDevices) deviceIds.add(dev.deviceId);

      const allTemplateNames = new Set<string>();
      for (const inst of instances.values()) allTemplateNames.add(inst.templateName);

      for (const deviceId of deviceIds) {
        const deviceAtomics = [...atomicTags.values()]
          .filter((t) => t.deviceId === deviceId)
          .map((t) => ({
            id: t.id,
            deviceId: t.deviceId,
            tag: t.tag,
            description: t.description || undefined,
            datatype: gatewayDatatype(t.modbusDatatype),
            default: t.modbusDatatype === 'boolean' ? false : 0,
            functionCode: functionCodeToInt(t.functionCode),
            modbusDatatype: t.modbusDatatype,
            byteOrder: t.byteOrder || undefined,
            address: t.address,
            bidirectional: t.bidirectional || undefined,
          }));

        const deviceInsts = [...instances.values()].filter((i) => i.deviceId === deviceId);
        const udtVariables = deviceInsts.map((inst) => ({
          id: inst.id,
          deviceId: inst.deviceId,
          tag: inst.tag,
          templateName: inst.templateName,
          memberTags: inst.memberTags,
          memberAddresses: inst.memberAddresses,
          memberByteOrders:
            Object.keys(inst.memberByteOrders).length > 0 ? inst.memberByteOrders : undefined,
        }));

        // Include templates used by this device's instances
        const deviceTemplateNames = new Set(deviceInsts.map((i) => i.templateName));
        const udtTemplates = [...templates.values()]
          .filter((t) => deviceTemplateNames.has(t.name))
          .map((t) => ({
            name: t.name,
            version: t.version,
            members: t.members.map((m) => ({
              name: m.name,
              datatype: gatewayDatatype(m.modbusDatatype),
              functionCode: m.functionCode,
              modbusDatatype: m.modbusDatatype,
            })),
          }));

        const result = await apiPost(`/gateways/gateway/devices/${deviceId}/sync`, {
          atomicVariables: deviceAtomics,
          udtTemplates,
          udtVariables,
        });

        if (result.error) {
          saltState.addNotification({ message: result.error.error, type: 'error' });
          return;
        }
      }

      saltState.addNotification({ message: 'Changes saved successfully', type: 'success' });
      await invalidateAll();
      needsInit = true;
    } catch (err) {
      saltState.addNotification({
        message: err instanceof Error ? err.message : 'Save failed',
        type: 'error',
      });
    } finally {
      saving = false;
    }
  }

  // ── Device management ──
  let showAddDevice = $state(false);
  let newDeviceId = $state('');
  let newDeviceHost = $state('');
  let newDevicePort = $state('502');
  let newDeviceUnitId = $state('1');

  async function addDevice() {
    const deviceId = newDeviceId.trim();
    const host = newDeviceHost.trim();
    if (!deviceId || !host) return;

    const result = await apiPut('/gateways/gateway/devices', {
      deviceId,
      protocol: 'modbus',
      host,
      port: parseInt(newDevicePort, 10) || 502,
      unitId: parseInt(newDeviceUnitId, 10) || 1,
    });

    if (result.error) {
      saltState.addNotification({ message: result.error.error, type: 'error' });
      return;
    }

    saltState.addNotification({ message: `Device "${deviceId}" added`, type: 'success' });
    newDeviceId = '';
    newDeviceHost = '';
    newDevicePort = '502';
    newDeviceUnitId = '1';
    showAddDevice = false;
    await invalidateAll();
    needsInit = true;
  }

  function discardChanges() {
    needsInit = true;
  }
</script>

<div class="tc-layout">
  <!-- Sidebar -->
  <nav class="tc-side-nav">
    <div class="tc-side-head">
      <span class="protocol-badge modbus">MODBUS</span>
    </div>

    <div class="tc-side-section">
      <button
        class="tc-side-section-header"
        onclick={() => { activeTab = 'templates'; }}
      >
        Templates
        <span class="count">{templates.size}</span>
      </button>
      {#each [...templates.keys()] as name}
        <button
          class="tc-side-item"
          class:active={activeTab === 'templates' && activeTemplateName === name}
          onclick={() => { activeTab = 'templates'; activeTemplateName = name; }}
        >
          <span class="tpl-icon">T</span>
          <span class="item-label">{name}</span>
          <span class="member-count">{templates.get(name)?.members.length ?? 0}</span>
        </button>
      {/each}
      <div class="tc-side-add">
        <input
          type="text"
          class="add-input"
          placeholder="New template..."
          bind:value={newTemplateName}
          onkeydown={(e) => { if (e.key === 'Enter') addTemplate(); }}
        />
        <button class="add-btn" onclick={addTemplate} disabled={!newTemplateName.trim()}>+</button>
      </div>
    </div>

    <div class="tc-side-section">
      <div class="tc-side-section-header">Devices</div>
      {#each modbusDevices as dev}
        <button
          class="tc-side-item"
          class:active={(activeTab === 'instances' || activeTab === 'atomic') && activeDeviceId === dev.deviceId}
          onclick={() => { activeDeviceId = dev.deviceId; if (activeTab === 'templates') activeTab = 'instances'; }}
        >
          <span class="item-label">{dev.deviceId}</span>
          <span class="member-count">{dev.host}{dev.port ? `:${dev.port}` : ''}</span>
        </button>
      {/each}
      {#if showAddDevice}
        <div class="tc-side-add-device" transition:slide|local={{ duration: 150 }}>
          <input type="text" class="add-input" placeholder="Device ID" bind:value={newDeviceId} />
          <input type="text" class="add-input" placeholder="Host / IP" bind:value={newDeviceHost} />
          <div class="add-device-row">
            <input type="text" class="add-input" placeholder="Port" bind:value={newDevicePort} />
            <input type="text" class="add-input" placeholder="Unit" bind:value={newDeviceUnitId} />
          </div>
          <div class="add-device-row">
            <button class="add-row-btn" onclick={addDevice} disabled={!newDeviceId.trim() || !newDeviceHost.trim()}>Add</button>
            <button class="action-btn" onclick={() => showAddDevice = false}>Cancel</button>
          </div>
        </div>
      {:else}
        <div class="tc-side-add">
          <button class="add-row-btn" style="width:100%" onclick={() => showAddDevice = true}>+ Add Device</button>
        </div>
      {/if}
    </div>
  </nav>

  <!-- Main area -->
  <div class="tc-main">
    {#if error}
      <div class="error-banner">{error}</div>
    {:else}
      <!-- Tabs -->
      <div class="tc-tabs">
        <Tabs
          tabs={[
            { id: 'templates', label: 'Templates' },
            { id: 'instances', label: 'Instances' },
            { id: 'atomic', label: 'Atomic Tags' }
          ] satisfies TabItem[]}
          active={activeTab}
          onChange={(id) => (activeTab = id as 'templates' | 'instances' | 'atomic')}
          ariaLabel="Tag config section"
        />
      </div>

      <div class="tc-content">
        <!-- Templates Tab -->
        {#if activeTab === 'templates'}
          {#if activeTemplate}
            <div class="section-header">
              <h3>{activeTemplate.name}</h3>
              <div class="section-actions">
                <button class="action-btn danger" onclick={() => deleteTemplate(activeTemplate!.name)}>Delete Template</button>
              </div>
            </div>
            <div class="table-wrap">
              <table class="tpl-table">
                <thead>
                  <tr>
                    <th>Member Name</th>
                    <th>Function Code</th>
                    <th>Modbus Datatype</th>
                    <th>Gateway Type</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {#each activeTemplate.members as member, idx}
                    <tr>
                      <td data-label="Name">
                        <input
                          type="text"
                          class="cell-input"
                          value={member.name}
                          onchange={(e) => { member.name = (e.target as HTMLInputElement).value; templates = new Map(templates); }}
                        />
                      </td>
                      <td data-label="FC">
                        <select
                          class="cell-select"
                          value={member.functionCode}
                          onchange={(e) => { member.functionCode = (e.target as HTMLSelectElement).value; templates = new Map(templates); }}
                        >
                          {#each FUNCTION_CODES as fc}
                            <option value={fc}>{fc}</option>
                          {/each}
                        </select>
                      </td>
                      <td data-label="Datatype">
                        <select
                          class="cell-select"
                          value={member.modbusDatatype}
                          onchange={(e) => {
                            member.modbusDatatype = (e.target as HTMLSelectElement).value;
                            member.datatype = gatewayDatatype(member.modbusDatatype);
                            templates = new Map(templates);
                          }}
                        >
                          {#each MODBUS_DATATYPES as dt}
                            <option value={dt}>{dt}</option>
                          {/each}
                        </select>
                      </td>
                      <td data-label="Type">
                        <span class="type-badge" class:type-number={member.datatype === 'number'} class:type-bool={member.datatype === 'boolean'}>
                          {member.datatype}
                        </span>
                      </td>
                      <td>
                        <button class="action-btn danger" onclick={() => removeMember(activeTemplate!.name, idx)}>
                          &times;
                        </button>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
              {#if activeTemplate.members.length === 0}
                <div class="empty-state"><p>No members. Add one below.</p></div>
              {/if}
            </div>
            <div class="table-actions">
              <button class="add-row-btn" onclick={() => addMember(activeTemplate!.name)}>+ Add Member</button>
            </div>
          {:else}
            <div class="empty-state">
              <p>Select a template from the sidebar or create a new one.</p>
            </div>
          {/if}

        <!-- Instances Tab -->
        {:else if activeTab === 'instances'}
          <div class="section-header">
            <h3>Instances{activeDeviceId ? ` — ${activeDeviceId}` : ''}</h3>
          </div>

          {#if deviceInstances.length > 0}
            {#each deviceInstances as inst (inst.id)}
              {@const tpl = templates.get(inst.templateName)}
              <div class="instance-card">
                <div class="instance-header">
                  <div class="instance-title">
                    <span class="tpl-icon">I</span>
                    <input
                      type="text"
                      class="cell-input tag-input"
                      value={inst.tag}
                      onchange={(e) => { inst.tag = (e.target as HTMLInputElement).value; instances = new Map(instances); }}
                    />
                    <span class="instance-template">({inst.templateName})</span>
                  </div>
                  <div class="instance-actions">
                    <label class="base-addr-label">
                      Base:
                      <input
                        type="number"
                        class="cell-input addr-input"
                        placeholder="0"
                        onchange={(e) => {
                          const base = parseInt((e.target as HTMLInputElement).value, 10);
                          if (!isNaN(base)) autoAssignAddresses(inst.id, base);
                        }}
                      />
                    </label>
                    <button class="action-btn danger" onclick={() => deleteInstance(inst.id)}>&times;</button>
                  </div>
                </div>
                {#if tpl}
                  <table class="tpl-table">
                    <thead>
                      <tr>
                        <th>Member</th>
                        <th>FC</th>
                        <th>Datatype</th>
                        <th>Address</th>
                        <th>Byte Order</th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each tpl.members as member}
                        <tr>
                          <td data-label="Member">
                            <span class="item-name">{member.name}</span>
                          </td>
                          <td data-label="FC">
                            <span class="type-badge">{member.functionCode}</span>
                          </td>
                          <td data-label="Type">
                            <span class="type-badge">{member.modbusDatatype}</span>
                          </td>
                          <td data-label="Addr">
                            <input
                              type="number"
                              class="cell-input addr-input"
                              value={inst.memberAddresses[member.name] ?? 0}
                              onchange={(e) => {
                                inst.memberAddresses[member.name] = parseInt((e.target as HTMLInputElement).value, 10) || 0;
                                instances = new Map(instances);
                              }}
                            />
                          </td>
                          <td data-label="Byte Order">
                            <select
                              class="cell-select"
                              value={inst.memberByteOrders[member.name] ?? ''}
                              onchange={(e) => {
                                const val = (e.target as HTMLSelectElement).value;
                                if (val) inst.memberByteOrders[member.name] = val;
                                else delete inst.memberByteOrders[member.name];
                                instances = new Map(instances);
                              }}
                            >
                              <option value="">device default</option>
                              {#each BYTE_ORDERS as bo}
                                <option value={bo}>{bo}</option>
                              {/each}
                            </select>
                          </td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                {:else}
                  <div class="empty-state"><p>Template "{inst.templateName}" not found.</p></div>
                {/if}
              </div>
            {/each}
          {:else}
            <div class="empty-state"><p>No instances for this device.</p></div>
          {/if}

          <div class="add-instance-form">
            <select class="cell-select" bind:value={newInstanceTemplate}>
              <option value="">Select template...</option>
              {#each [...templates.keys()] as name}
                <option value={name}>{name}</option>
              {/each}
            </select>
            <select class="cell-select" bind:value={newInstanceDevice}>
              <option value="">Select device...</option>
              {#each modbusDevices as dev}
                <option value={dev.deviceId}>{dev.deviceId}</option>
              {/each}
            </select>
            <input type="text" class="cell-input" placeholder="Instance tag..." bind:value={newInstanceTag} onkeydown={(e) => { if (e.key === 'Enter') addInstance(); }} />
            <button class="add-row-btn" onclick={addInstance} disabled={!newInstanceTag.trim() || !newInstanceDevice || !newInstanceTemplate}>+ Add Instance</button>
          </div>

        <!-- Atomic Tags Tab -->
        {:else if activeTab === 'atomic'}
          <div class="section-header">
            <h3>Atomic Tags{activeDeviceId ? ` — ${activeDeviceId}` : ''}</h3>
            <div class="section-actions">
              <button class="add-row-btn" onclick={() => { showCsvDialog = true; csvMode = 'atomic'; }}>Import CSV</button>
              <button class="add-row-btn" onclick={addAtomicTag}>+ Add Tag</button>
            </div>
          </div>

          {#if deviceAtomicTags.length > 0}
            <div class="table-wrap">
              <table class="tpl-table">
                <thead>
                  <tr>
                    <th>Tag Name</th>
                    <th>Address</th>
                    <th>Function Code</th>
                    <th>Datatype</th>
                    <th>Byte Order</th>
                    <th>Description</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {#each deviceAtomicTags as tag (tag.id)}
                    <tr>
                      <td data-label="Name">
                        <input
                          type="text"
                          class="cell-input"
                          value={tag.tag}
                          onchange={(e) => { tag.tag = (e.target as HTMLInputElement).value; tag.id = `${tag.deviceId}/${tag.tag}`; atomicTags = new Map(atomicTags); }}
                        />
                      </td>
                      <td data-label="Address">
                        <input
                          type="number"
                          class="cell-input addr-input"
                          value={tag.address}
                          onchange={(e) => { tag.address = parseInt((e.target as HTMLInputElement).value, 10) || 0; atomicTags = new Map(atomicTags); }}
                        />
                      </td>
                      <td data-label="FC">
                        <select
                          class="cell-select"
                          value={tag.functionCode}
                          onchange={(e) => { tag.functionCode = (e.target as HTMLSelectElement).value; atomicTags = new Map(atomicTags); }}
                        >
                          {#each FUNCTION_CODES as fc}
                            <option value={fc}>{fc}</option>
                          {/each}
                        </select>
                      </td>
                      <td data-label="Datatype">
                        <select
                          class="cell-select"
                          value={tag.modbusDatatype}
                          onchange={(e) => { tag.modbusDatatype = (e.target as HTMLSelectElement).value; atomicTags = new Map(atomicTags); }}
                        >
                          {#each MODBUS_DATATYPES as dt}
                            <option value={dt}>{dt}</option>
                          {/each}
                        </select>
                      </td>
                      <td data-label="Byte Order">
                        <select
                          class="cell-select"
                          value={tag.byteOrder}
                          onchange={(e) => { tag.byteOrder = (e.target as HTMLSelectElement).value; atomicTags = new Map(atomicTags); }}
                        >
                          <option value="">device default</option>
                          {#each BYTE_ORDERS as bo}
                            <option value={bo}>{bo}</option>
                          {/each}
                        </select>
                      </td>
                      <td data-label="Desc">
                        <input
                          type="text"
                          class="cell-input desc-input"
                          value={tag.description}
                          onchange={(e) => { tag.description = (e.target as HTMLInputElement).value; atomicTags = new Map(atomicTags); }}
                        />
                      </td>
                      <td>
                        <button class="action-btn danger" onclick={() => deleteAtomicTag(tag.id)}>&times;</button>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {:else}
            <div class="empty-state"><p>No atomic tags. Add one or import from CSV.</p></div>
          {/if}
        {/if}
      </div>

      <!-- Save bar -->
      {#if isDirty}
        <div class="save-bar" transition:fly|local={{ y: 40, duration: 150 }}>
          <button class="save-btn" onclick={saveChanges} disabled={saving}>
            {saving ? 'Saving...' : 'Save Changes'}
          </button>
          <button class="reset-btn" onclick={discardChanges} disabled={saving}>Discard</button>
        </div>
      {/if}
    {/if}
  </div>
</div>

<!-- CSV Import Dialog -->
{#if showCsvDialog}
  <div class="csv-overlay" transition:fly|local={{ y: 20, duration: 150 }}>
    <div class="csv-dialog">
      <div class="csv-header">
        <h3>Import CSV</h3>
        <button class="action-btn" onclick={() => showCsvDialog = false}>&times;</button>
      </div>

      <div class="csv-mode">
        <label>
          <input type="radio" bind:group={csvMode} value="atomic" /> Atomic Tags
        </label>
        <label>
          <input type="radio" bind:group={csvMode} value="template" /> Template Members
        </label>
        {#if csvMode === 'template'}
          <input type="text" class="cell-input" placeholder="Template name..." bind:value={csvTemplateName} />
        {/if}
      </div>

      <div class="csv-format">
        <code>name,address,functionCode,datatype,byteOrder,description</code>
      </div>

      <textarea
        class="csv-textarea"
        placeholder="Paste CSV here..."
        bind:value={csvText}
        rows="10"
      ></textarea>

      {#if csvPreview.errors.length > 0}
        <div class="csv-errors">
          {#each csvPreview.errors as err}
            <div class="csv-error">{err}</div>
          {/each}
        </div>
      {/if}

      {#if csvPreview.tags.length > 0}
        <div class="csv-preview-label">{csvPreview.tags.length} tags parsed</div>
        <div class="csv-preview-table">
          <table class="tpl-table">
            <thead>
              <tr><th>Name</th><th>Address</th><th>FC</th><th>Type</th></tr>
            </thead>
            <tbody>
              {#each csvPreview.tags.slice(0, 10) as tag}
                <tr>
                  <td>{tag.name}</td>
                  <td>{tag.address}</td>
                  <td>{tag.functionCode}</td>
                  <td>{tag.datatype}</td>
                </tr>
              {/each}
              {#if csvPreview.tags.length > 10}
                <tr><td colspan="4" style="text-align:center; color: var(--theme-text-muted)">...and {csvPreview.tags.length - 10} more</td></tr>
              {/if}
            </tbody>
          </table>
        </div>
      {/if}

      <div class="csv-actions">
        <button
          class="save-btn"
          onclick={importCsv}
          disabled={csvPreview.tags.length === 0 || (csvMode === 'template' && !csvTemplateName.trim())}
        >
          Import {csvPreview.tags.length} Tags
        </button>
        <button class="reset-btn" onclick={() => showCsvDialog = false}>Cancel</button>
      </div>
    </div>
  </div>
{/if}

<style lang="scss">
  @use '../gateway-tag-config/tag-table';

  .tc-layout {
    display: flex;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  // ── Sidebar ──
  .tc-side-nav {
    width: 260px;
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--theme-border);
    background: var(--theme-surface);
    overflow-y: auto;
    flex-shrink: 0;
    min-height: calc(100vh - var(--header-height, 60px) - 7rem);
  }

  .tc-side-head {
    padding: 1rem;
    border-bottom: 1px solid var(--theme-border);
  }

  .protocol-badge {
    font-size: 0.625rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding: 0.2rem 0.5rem;

    &.modbus {
      background: var(--badge-teal-bg);
      color: var(--badge-teal-text);
    }
  }

  .tc-side-section {
    border-bottom: 1px solid var(--theme-border);
  }

  .tc-side-section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.625rem 1rem;
    font-size: 0.6875rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--theme-text-muted);
    background: none;
    border: none;
    width: 100%;
    text-align: left;
    cursor: pointer;

    &:hover { color: var(--theme-text); }
  }

  .count {
    font-size: 0.625rem;
    color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-text) 8%, transparent);
    padding: 0.1rem 0.4rem;
    border-radius: 0;
  }

  .tc-side-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 1rem;
    border-left: 2px solid transparent;
    cursor: pointer;
    font-size: 0.8125rem;
    width: 100%;
    text-align: left;
    background: none;
    border-top: none;
    border-right: none;
    border-bottom: none;
    color: var(--theme-text);

    &:hover { background: color-mix(in srgb, var(--theme-text) 4%, transparent); }
    &.active {
      border-left-color: var(--theme-primary);
      background: color-mix(in srgb, var(--theme-primary) 6%, transparent);
    }
  }

  .tpl-icon {
    width: 1.25rem;
    height: 1.25rem;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.625rem;
    font-weight: 700;
    background: var(--badge-purple-bg);
    color: var(--badge-purple-text);
    flex-shrink: 0;
  }

  .item-label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .member-count {
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .tc-side-add {
    display: flex;
    gap: 0.25rem;
    padding: 0.5rem 1rem;
  }

  .tc-side-add-device {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    padding: 0.5rem 1rem;
    border-top: 1px solid var(--theme-border);
  }

  .add-device-row {
    display: flex;
    gap: 0.25rem;
  }

  .add-input {
    flex: 1;
    font-size: 0.75rem;
    padding: 0.25rem 0.5rem;
    background: var(--theme-input-bg);
    border: 1px solid var(--theme-border);
    color: var(--theme-text);
    font-family: 'IBM Plex Mono', monospace;

    &::placeholder { color: var(--theme-text-muted); }
  }

  .add-btn {
    font-size: 0.875rem;
    padding: 0.25rem 0.5rem;
    background: var(--theme-primary);
    color: white;
    border: none;
    cursor: pointer;
    font-weight: 700;

    &:disabled { opacity: 0.4; cursor: not-allowed; }
  }

  // ── Main area ──
  .tc-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    position: relative;
  }

  .error-banner {
    padding: 1rem;
    background: color-mix(in srgb, var(--color-red-500, #ef4444) 10%, transparent);
    color: var(--color-red-500, #ef4444);
    border-bottom: 1px solid var(--color-red-500, #ef4444);
    font-size: 0.875rem;
  }

  .tc-tabs {
    flex-shrink: 0;
  }

  .tc-content {
    flex: 1;
    overflow-y: auto;
    padding: 1rem;
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.75rem;

    h3 {
      font-size: 0.9375rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }
  }

  .section-actions {
    display: flex;
    gap: 0.5rem;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .table-actions {
    padding: 0.5rem 0;
  }

  // ── Form inputs ──
  .cell-input {
    font-size: 0.8125rem;
    padding: 0.25rem 0.5rem;
    background: var(--theme-input-bg);
    border: 1px solid var(--theme-border);
    color: var(--theme-text);
    font-family: 'IBM Plex Mono', monospace;
    height: 1.75rem;

    &:focus {
      border-color: var(--theme-primary);
      outline: none;
    }
  }

  .addr-input { width: 5rem; }
  .tag-input { width: 10rem; }
  .desc-input { width: 12rem; }

  .cell-select {
    font-size: 0.8125rem;
    padding: 0.25rem 0.375rem;
    background: var(--theme-input-bg);
    border: 1px solid var(--theme-border);
    color: var(--theme-text);
    font-family: 'IBM Plex Mono', monospace;
    height: 1.75rem;

    &:focus {
      border-color: var(--theme-primary);
      outline: none;
    }
  }

  .action-btn {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    cursor: pointer;
    padding: 0.2rem 0.5rem;
    border: none;
    background: none;

    &:hover { color: var(--theme-text); }
    &.danger:hover { color: var(--color-red-500, #ef4444); }
  }

  .add-row-btn {
    font-size: 0.75rem;
    padding: 0.375rem 0.75rem;
    background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    color: var(--theme-primary);
    border: 1px solid color-mix(in srgb, var(--theme-primary) 30%, transparent);
    cursor: pointer;
    font-family: 'IBM Plex Mono', monospace;

    &:hover { background: color-mix(in srgb, var(--theme-primary) 18%, transparent); }
    &:disabled { opacity: 0.4; cursor: not-allowed; }
  }

  // ── Instance cards ──
  .instance-card {
    border: 1px solid var(--theme-border);
    margin-bottom: 1rem;
    background: var(--theme-surface);
  }

  .instance-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.625rem 0.75rem;
    border-bottom: 1px solid var(--theme-border);
    background: color-mix(in srgb, var(--theme-text) 2%, transparent);
  }

  .instance-title {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .instance-template {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
  }

  .instance-actions {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .base-addr-label {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    display: flex;
    align-items: center;
    gap: 0.375rem;
  }

  // ── Add instance form ──
  .add-instance-form {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.75rem 0;
    flex-wrap: wrap;
  }

  // ── Save bar ──
  .save-bar {
    position: sticky;
    bottom: 1rem;
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1.25rem;
    margin: 0 1rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    box-shadow: 0 -2px 12px rgba(0, 0, 0, 0.15);
    z-index: 10;
  }

  .save-btn {
    padding: 0.5rem 1.25rem;
    font-size: 0.8125rem;
    font-weight: 600;
    background: var(--theme-primary);
    color: white;
    border: none;
    cursor: pointer;

    &:hover { filter: brightness(1.1); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .reset-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    background: none;
    border: 1px solid var(--theme-border);
    color: var(--theme-text-muted);
    cursor: pointer;

    &:hover { color: var(--theme-text); border-color: var(--theme-text-muted); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  // ── CSV dialog ──
  .csv-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }

  .csv-dialog {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    width: min(600px, 90vw);
    max-height: 80vh;
    overflow-y: auto;
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .csv-header {
    display: flex;
    align-items: center;
    justify-content: space-between;

    h3 { margin: 0; font-size: 1rem; }
  }

  .csv-mode {
    display: flex;
    align-items: center;
    gap: 1rem;
    font-size: 0.8125rem;

    label {
      display: flex;
      align-items: center;
      gap: 0.25rem;
      cursor: pointer;
    }
  }

  .csv-format {
    font-size: 0.75rem;
    color: var(--theme-text-muted);

    code {
      background: color-mix(in srgb, var(--theme-text) 6%, transparent);
      padding: 0.2rem 0.4rem;
      font-family: 'IBM Plex Mono', monospace;
    }
  }

  .csv-textarea {
    width: 100%;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    background: var(--theme-input-bg);
    border: 1px solid var(--theme-border);
    color: var(--theme-text);
    padding: 0.5rem;
    resize: vertical;

    &:focus { border-color: var(--theme-primary); outline: none; }
  }

  .csv-errors {
    font-size: 0.75rem;
    color: var(--color-red-500, #ef4444);
  }

  .csv-error {
    padding: 0.15rem 0;
  }

  .csv-preview-label {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
  }

  .csv-preview-table {
    max-height: 200px;
    overflow-y: auto;
  }

  .csv-actions {
    display: flex;
    gap: 0.75rem;
    justify-content: flex-end;
  }

  // ── Responsive ──
  @media (max-width: 768px) {
    .tc-side-nav { display: none; }
    .add-instance-form { flex-direction: column; align-items: stretch; }
  }
</style>
