<script lang="ts">
  import { invalidateAll } from '$app/navigation';
  import { api, apiPut } from '$lib/api/client';
  import type {
    ProfinetConfig,
    ProfinetStatus,
    SlotConfig,
    SubslotConfig,
    TagMapping,
    NetworkInterface,
    ProfinetDirection,
    ProfinetType,
  } from '$lib/types/profinet';
  import { PROFINET_TYPES, typeSize } from '$lib/types/profinet';
  import type { Variable } from '$lib/types/gateway';
  import HexInput from './HexInput.svelte';

  interface Props {
    config: ProfinetConfig | null;
    status: ProfinetStatus | null;
    interfaces: NetworkInterface[];
    variables: Variable[];
    error: string | null;
  }

  let { config, status, interfaces, variables, error }: Props = $props();

  // Form state — initialized from existing config or defaults
  let form = $state(configToForm(config));
  let saving = $state(false);
  let saveError: string | null = $state(null);
  let saveSuccess = $state(false);
  let downloadingGsdml = $state(false);

  // Variable picker state
  let pickerTarget: { slotIdx: number; subIdx: number } | null = $state(null);
  let pickerSearch = $state('');

  function configToForm(cfg: ProfinetConfig | null) {
    if (cfg) return JSON.parse(JSON.stringify(cfg)) as ProfinetConfig;
    return {
      stationName: '',
      interfaceName: '',
      vendorId: 0,
      deviceId: 0,
      deviceName: '',
      cycleTimeUs: 1000,
      slots: [] as SlotConfig[],
    };
  }

  // Slot management
  function addSlot() {
    form.slots = [
      ...form.slots,
      {
        slotNumber: form.slots.length > 0 ? Math.max(...form.slots.map((s) => s.slotNumber)) + 1 : 1,
        moduleIdentNo: 0,
        subslots: [],
      },
    ];
  }

  function removeSlot(idx: number) {
    form.slots = form.slots.filter((_, i) => i !== idx);
  }

  // Subslot management
  function addSubslot(slotIdx: number) {
    const slot = form.slots[slotIdx];
    slot.subslots = [
      ...slot.subslots,
      {
        subslotNumber:
          slot.subslots.length > 0 ? Math.max(...slot.subslots.map((s) => s.subslotNumber)) + 1 : 1,
        submoduleIdentNo: 0,
        direction: 'inputOutput' as ProfinetDirection,
        inputSize: 0,
        outputSize: 0,
        tags: [],
      },
    ];
    form.slots = [...form.slots];
  }

  function removeSubslot(slotIdx: number, subIdx: number) {
    form.slots[slotIdx].subslots = form.slots[slotIdx].subslots.filter((_, i) => i !== subIdx);
    form.slots = [...form.slots];
  }

  // Tag management
  function addTag(slotIdx: number, subIdx: number) {
    const subslot = form.slots[slotIdx].subslots[subIdx];
    const nextOffset = subslot.tags.length > 0
      ? Math.max(...subslot.tags.map((t) => t.byteOffset + typeSize(t.datatype as ProfinetType)))
      : 0;
    subslot.tags = [
      ...subslot.tags,
      {
        tagId: '',
        byteOffset: nextOffset,
        bitOffset: 0,
        datatype: 'uint16' as ProfinetType,
        source: '',
      },
    ];
    form.slots = [...form.slots];
  }

  function removeTag(slotIdx: number, subIdx: number, tagIdx: number) {
    form.slots[slotIdx].subslots[subIdx].tags = form.slots[slotIdx].subslots[subIdx].tags.filter(
      (_, i) => i !== tagIdx,
    );
    form.slots = [...form.slots];
  }

  // Variable picker
  function openPicker(slotIdx: number, subIdx: number) {
    pickerTarget = { slotIdx, subIdx };
    pickerSearch = '';
  }

  function closePicker() {
    pickerTarget = null;
  }

  function selectVariable(v: Variable) {
    if (!pickerTarget) return;
    const subslot = form.slots[pickerTarget.slotIdx].subslots[pickerTarget.subIdx];
    const nextOffset = subslot.tags.length > 0
      ? Math.max(...subslot.tags.map((t) => t.byteOffset + typeSize(t.datatype as ProfinetType)))
      : 0;

    // Map variable datatype to PROFINET type
    const dt = mapDatatype(v.datatype);

    subslot.tags = [
      ...subslot.tags,
      {
        tagId: v.variableId,
        byteOffset: nextOffset,
        bitOffset: 0,
        datatype: dt,
        source: `*.data.*.${v.variableId}`,
      },
    ];
    form.slots = [...form.slots];
  }

  function mapDatatype(dt: string): ProfinetType {
    const lower = dt.toLowerCase();
    if (PROFINET_TYPES.includes(lower as ProfinetType)) return lower as ProfinetType;
    if (lower === 'boolean' || lower === 'bit') return 'bool';
    if (lower === 'float' || lower === 'real') return 'float32';
    if (lower === 'double' || lower === 'lreal') return 'float64';
    if (lower === 'int' || lower === 'dint') return 'int32';
    if (lower === 'word' || lower === 'uint') return 'uint16';
    if (lower === 'dword' || lower === 'udint') return 'uint32';
    return 'uint16';
  }

  const filteredVariables = $derived(
    pickerSearch
      ? variables.filter(
          (v) =>
            v.variableId.toLowerCase().includes(pickerSearch.toLowerCase()) ||
            v.moduleId.toLowerCase().includes(pickerSearch.toLowerCase()),
        )
      : variables,
  );

  // Save
  async function save() {
    saving = true;
    saveError = null;
    saveSuccess = false;
    try {
      const result = await apiPut('/profinet/config', form);
      if (result.error) {
        saveError = result.error.error;
      } else {
        saveSuccess = true;
        setTimeout(() => (saveSuccess = false), 3000);
        await invalidateAll();
      }
    } catch (e) {
      saveError = e instanceof Error ? e.message : 'Save failed';
    } finally {
      saving = false;
    }
  }

  // GSDML download
  async function downloadGsdml() {
    downloadingGsdml = true;
    try {
      const result = await api<{ filename: string; xml: string }>('/profinet/gsdml');
      if (result.error) {
        saveError = result.error.error;
        return;
      }
      if (!result.data) return;

      const blob = new Blob([result.data.xml], { type: 'application/xml' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = result.data.filename;
      a.click();
      URL.revokeObjectURL(url);
    } finally {
      downloadingGsdml = false;
    }
  }

</script>

<div class="pn-config">
  {#if error}
    <div class="error-banner">{error}</div>
  {/if}

  <!-- Status bar -->
  {#if status}
    <div class="status-bar" class:connected={status.connected}>
      <span class="status-dot"></span>
      <span class="status-text">{status.connected ? 'Connected' : 'Disconnected'}</span>
      {#if status.connected}
        <span class="status-detail">Controller: {status.controllerIp}</span>
        <span class="status-detail">AREP: {status.arep}</span>
        <span class="status-detail">In: {status.inputSlots} / Out: {status.outputSlots} slots</span>
      {/if}
    </div>
  {/if}

  <div class="config-header">
    <h2>PROFINET IO Device Configuration</h2>
    <div class="header-actions">
      <button class="gsdml-btn" onclick={downloadGsdml} disabled={downloadingGsdml || !config}>
        {downloadingGsdml ? 'Generating...' : 'Download GSDML'}
      </button>
    </div>
  </div>

  <!-- Device identity -->
  <div class="section">
    <h3>Device Identity</h3>
    <div class="form-grid">
      <label>
        <span>Station Name</span>
        <input type="text" bind:value={form.stationName} placeholder="e.g. tentacle_io_1" />
      </label>
      <label>
        <span>Interface</span>
        <select bind:value={form.interfaceName}>
          <option value="">Select interface...</option>
          {#each interfaces as iface}
            <option value={iface.name}>{iface.name} ({iface.mac})</option>
          {/each}
        </select>
      </label>
      <label>
        <span>Vendor ID</span>
        <HexInput value={form.vendorId} placeholder="0x0000"
          onchange={(v) => { form.vendorId = v; }} />
      </label>
      <label>
        <span>Device ID</span>
        <HexInput value={form.deviceId} placeholder="0x0000"
          onchange={(v) => { form.deviceId = v; }} />
      </label>
      <label>
        <span>Device Name</span>
        <input type="text" bind:value={form.deviceName} placeholder="Human-readable name" />
      </label>
      <label>
        <span>Cycle Time (us)</span>
        <input type="number" bind:value={form.cycleTimeUs} min="250" step="250" />
      </label>
    </div>
  </div>

  <!-- Slot/Subslot editor -->
  <div class="section">
    <div class="section-header">
      <h3>I/O Modules (Slots)</h3>
      <button class="small-btn" onclick={addSlot}>+ Add Slot</button>
    </div>

    {#if form.slots.length === 0}
      <p class="hint">No slots configured. Slot 0 (DAP) is auto-generated. Add slots for your I/O modules.</p>
    {/if}

    {#each form.slots as slot, slotIdx}
      <div class="slot-card">
        <div class="slot-header">
          <span class="slot-label">Slot {slot.slotNumber}</span>
          <label class="inline-field">
            <span>Module ID</span>
            <HexInput value={slot.moduleIdentNo} placeholder="0x00000000"
              onchange={(v) => { slot.moduleIdentNo = v; form.slots = [...form.slots]; }} />
          </label>
          <button class="remove-btn" onclick={() => removeSlot(slotIdx)}>Remove Slot</button>
        </div>

        <div class="subslots">
          <div class="section-header sub">
            <span>Subslots</span>
            <button class="small-btn" onclick={() => addSubslot(slotIdx)}>+ Subslot</button>
          </div>

          {#each slot.subslots as subslot, subIdx}
            <div class="subslot-card">
              <div class="subslot-header">
                <span class="subslot-label">Subslot {subslot.subslotNumber}</span>
                <label class="inline-field">
                  <span>Submodule ID</span>
                  <HexInput value={subslot.submoduleIdentNo} placeholder="0x00000000"
                    onchange={(v) => { subslot.submoduleIdentNo = v; form.slots = [...form.slots]; }} />
                </label>
                <label class="inline-field">
                  <span>Direction</span>
                  <select bind:value={subslot.direction} onchange={() => form.slots = [...form.slots]}>
                    <option value="input">Input</option>
                    <option value="output">Output</option>
                    <option value="inputOutput">Input/Output</option>
                  </select>
                </label>
                {#if subslot.direction === 'input' || subslot.direction === 'inputOutput'}
                  <label class="inline-field">
                    <span>Input Size</span>
                    <input type="number" bind:value={subslot.inputSize} min="0" class="size-input" />
                  </label>
                {/if}
                {#if subslot.direction === 'output' || subslot.direction === 'inputOutput'}
                  <label class="inline-field">
                    <span>Output Size</span>
                    <input type="number" bind:value={subslot.outputSize} min="0" class="size-input" />
                  </label>
                {/if}
                <button class="remove-btn" onclick={() => removeSubslot(slotIdx, subIdx)}>Remove</button>
              </div>

              <!-- Tags -->
              <div class="tags-section">
                <div class="section-header sub">
                  <span>Tags</span>
                  <div class="tag-actions">
                    <button class="small-btn" onclick={() => openPicker(slotIdx, subIdx)}>Pick Variable</button>
                    <button class="small-btn" onclick={() => addTag(slotIdx, subIdx)}>+ Manual Tag</button>
                  </div>
                </div>

                {#each subslot.tags as tag, tagIdx}
                  <div class="tag-row">
                    <input type="text" bind:value={tag.tagId} placeholder="Tag ID" class="tag-id" />
                    <input type="number" bind:value={tag.byteOffset} min="0" class="tag-num" title="Byte offset" />
                    {#if tag.datatype === 'bool'}
                      <input type="number" bind:value={tag.bitOffset} min="0" max="7" class="tag-bit" title="Bit offset" />
                    {/if}
                    <select bind:value={tag.datatype} class="tag-type" onchange={() => form.slots = [...form.slots]}>
                      {#each PROFINET_TYPES as t}
                        <option value={t}>{t}</option>
                      {/each}
                    </select>
                    <input type="text" bind:value={tag.source} placeholder="NATS source subject" class="tag-source" title="Bus subject for value updates" />
                    <button class="remove-btn small" onclick={() => removeTag(slotIdx, subIdx, tagIdx)}>x</button>
                  </div>
                {/each}
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/each}
  </div>

  <!-- Save -->
  <div class="form-actions">
    <button class="save-btn" onclick={save} disabled={saving || !form.stationName || !form.interfaceName}>
      {saving ? 'Saving...' : 'Save Configuration'}
    </button>
    {#if saveSuccess}
      <span class="save-success">Saved</span>
    {/if}
    {#if saveError}
      <span class="save-error">{saveError}</span>
    {/if}
  </div>
</div>

<!-- Variable picker modal -->
{#if pickerTarget}
  <div class="picker-overlay" onclick={closePicker} role="presentation">
    <div class="picker-modal" onclick={(e) => e.stopPropagation()} role="dialog">
      <div class="picker-header">
        <h3>Pick Variable</h3>
        <input type="text" bind:value={pickerSearch} placeholder="Search variables..." class="picker-search" />
        <button class="close-btn" onclick={closePicker}>x</button>
      </div>
      <div class="picker-list">
        {#if filteredVariables.length === 0}
          <p class="hint">No variables available. Make sure scanner modules are running.</p>
        {:else}
          {#each filteredVariables as v}
            <button class="picker-item" onclick={() => selectVariable(v)}>
              <span class="picker-var-id">{v.variableId}</span>
              <span class="picker-var-meta">{v.moduleId} &middot; {v.datatype}</span>
              <span class="picker-var-value">{v.value != null ? String(v.value) : '—'}</span>
            </button>
          {/each}
        {/if}
      </div>
    </div>
  </div>
{/if}

<style lang="scss">
  .pn-config {
    padding: 1.5rem 2rem;
    max-width: 960px;
  }

  .error-banner {
    background: var(--color-red-500, #ef4444);
    color: white;
    padding: 0.75rem 1rem;
    border-radius: var(--rounded-lg);
    margin-bottom: 1rem;
    font-size: 0.875rem;
  }

  /* Status bar */
  .status-bar {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.625rem 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    margin-bottom: 1rem;
    font-size: 0.8125rem;

    &.connected {
      border-color: var(--badge-green-text, #10b981);
    }
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-red-500, #ef4444);
    flex-shrink: 0;

    .connected & {
      background: var(--badge-green-text, #10b981);
    }
  }

  .status-text {
    font-weight: 600;
    color: var(--theme-text);
  }

  .status-detail {
    color: var(--theme-text-muted);
    font-family: var(--font-mono);
    font-size: 0.75rem;

    &::before {
      content: '|';
      margin-right: 0.5rem;
      color: var(--theme-border);
    }
  }

  /* Header */
  .config-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1.25rem;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }
  }

  .gsdml-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-primary);
    background: transparent;
    border: 1px solid var(--theme-primary);
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover:not(:disabled) {
      background: var(--theme-primary);
      color: white;
    }

    &:disabled {
      opacity: 0.4;
      cursor: default;
    }
  }

  /* Sections */
  .section {
    margin-bottom: 1.5rem;

    h3 {
      font-size: 0.9375rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 0.75rem;
    }
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.5rem;

    &.sub {
      margin-top: 0.5rem;

      span {
        font-size: 0.75rem;
        font-weight: 500;
        color: var(--theme-text-muted);
      }
    }
  }

  .hint {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0.5rem 0;
  }

  /* Form grid */
  .form-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 0.75rem;

    label {
      display: flex;
      flex-direction: column;
      gap: 0.25rem;

      span {
        font-size: 0.75rem;
        font-weight: 500;
        color: var(--theme-text-muted);
      }

      input,
      select {
        padding: 0.5rem;
        font-size: 0.8125rem;
        font-family: var(--font-mono);
        background: var(--theme-bg);
        border: 1px solid var(--theme-border);
        border-radius: var(--rounded-md);
        color: var(--theme-text);
      }
    }
  }

  /* Slots */
  .small-btn {
    padding: 0.25rem 0.5rem;
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--theme-primary);
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover {
      border-color: var(--theme-primary);
    }
  }

  .slot-card {
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    padding: 0.75rem;
    margin-bottom: 0.5rem;
    background: var(--theme-surface);
  }

  .slot-header,
  .subslot-header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex-wrap: wrap;
  }

  .slot-label,
  .subslot-label {
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--theme-text);
    min-width: 60px;
  }

  .inline-field {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    font-size: 0.75rem;

    span {
      color: var(--theme-text-muted);
      white-space: nowrap;
    }

    input,
    select {
      padding: 0.25rem 0.375rem;
      font-size: 0.75rem;
      font-family: var(--font-mono);
      background: var(--theme-bg);
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-sm);
      color: var(--theme-text);
    }

    input {
      width: 110px;
    }
  }

  .size-input {
    width: 60px !important;
  }

  .remove-btn {
    padding: 0.25rem 0.5rem;
    font-size: 0.6875rem;
    color: var(--color-red-500, #ef4444);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--rounded-sm);
    cursor: pointer;
    margin-left: auto;

    &:hover {
      border-color: var(--color-red-500, #ef4444);
    }

    &.small {
      padding: 0.125rem 0.375rem;
    }
  }

  .subslots {
    margin-left: 1rem;
    margin-top: 0.5rem;
  }

  .subslot-card {
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm);
    padding: 0.5rem 0.75rem;
    margin-bottom: 0.375rem;
    background: var(--theme-bg);
  }

  /* Tags */
  .tags-section {
    margin-top: 0.5rem;
    margin-left: 0.5rem;
  }

  .tag-actions {
    display: flex;
    gap: 0.375rem;
  }

  .tag-row {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    margin-bottom: 0.25rem;

    input,
    select {
      padding: 0.25rem 0.375rem;
      font-size: 0.75rem;
      font-family: var(--font-mono);
      background: var(--theme-surface);
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-sm);
      color: var(--theme-text);
    }
  }

  .tag-id {
    width: 120px;
  }

  .tag-num {
    width: 55px;
  }

  .tag-bit {
    width: 45px;
  }

  .tag-type {
    width: 80px;
  }

  .tag-source {
    flex: 1;
    min-width: 160px;
  }

  /* Form actions */
  .form-actions {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding-top: 1rem;
    border-top: 1px solid var(--theme-border);
  }

  .save-btn {
    padding: 0.5rem 1.25rem;
    font-size: 0.8125rem;
    font-weight: 500;
    color: white;
    background: var(--theme-primary);
    border: none;
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:disabled {
      opacity: 0.4;
      cursor: default;
    }
  }

  .save-success {
    font-size: 0.8125rem;
    color: var(--badge-green-text, #10b981);
    font-weight: 500;
  }

  .save-error {
    font-size: 0.8125rem;
    color: var(--color-red-500, #ef4444);
  }

  /* Variable picker modal */
  .picker-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .picker-modal {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    width: min(600px, 90vw);
    max-height: 70vh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .picker-header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 1rem;
    border-bottom: 1px solid var(--theme-border);

    h3 {
      font-size: 0.9375rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
      white-space: nowrap;
    }
  }

  .picker-search {
    flex: 1;
    padding: 0.375rem 0.5rem;
    font-size: 0.8125rem;
    font-family: var(--font-mono);
    background: var(--theme-bg);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text);
  }

  .close-btn {
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text-muted);
    cursor: pointer;
    font-size: 0.875rem;

    &:hover {
      color: var(--theme-text);
      border-color: var(--theme-text-muted);
    }
  }

  .picker-list {
    overflow-y: auto;
    padding: 0.5rem;
  }

  .picker-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.5rem 0.75rem;
    background: transparent;
    border: none;
    border-radius: var(--rounded-md);
    cursor: pointer;
    text-align: left;
    color: var(--theme-text);
    font-size: 0.8125rem;

    &:hover {
      background: var(--theme-bg);
    }
  }

  .picker-var-id {
    font-family: var(--font-mono);
    font-weight: 500;
    min-width: 0;
    flex: 1;
  }

  .picker-var-meta {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    white-space: nowrap;
  }

  .picker-var-value {
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    max-width: 100px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
