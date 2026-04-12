<script lang="ts">
  import { invalidateAll } from '$app/navigation';
  import { apiPost } from '$lib/api/client';
  import type {
    ControllerSubscription,
    ControllerTag,
    SubslotSubscription,
    SlotSubscription,
    NetworkInterface,
  } from '$lib/types/profinet';
  import { PROFINET_TYPES } from '$lib/types/profinet';

  interface Props {
    subscriptions: ControllerSubscription[];
    interfaces: NetworkInterface[];
    error: string | null;
  }

  let { subscriptions, interfaces, error }: Props = $props();

  // UI state
  let showForm = $state(false);
  let editingDeviceId: string | null = $state(null);
  let expandedDevices = $state(new Set<string>());
  let saving = $state(false);
  let deleteConfirm: string | null = $state(null);

  // Form state
  let form = $state(emptyForm());

  function emptyForm() {
    return {
      subscriberId: 'gateway',
      deviceId: '',
      stationName: '',
      ip: '',
      interfaceName: '',
      cycleTimeMs: 1,
      slots: [] as SlotSubscription[],
    };
  }

  function startAdd() {
    form = emptyForm();
    editingDeviceId = null;
    showForm = true;
  }

  function startEdit(sub: ControllerSubscription) {
    form = JSON.parse(JSON.stringify(sub));
    editingDeviceId = sub.deviceId;
    showForm = true;
  }

  function cancelForm() {
    showForm = false;
    editingDeviceId = null;
  }

  function toggleDevice(deviceId: string) {
    const next = new Set(expandedDevices);
    if (next.has(deviceId)) next.delete(deviceId);
    else next.add(deviceId);
    expandedDevices = next;
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
    subslot.tags = [
      ...subslot.tags,
      { tagId: '', byteOffset: 0, bitOffset: 0, datatype: 'uint16', direction: 'input' as const },
    ];
    form.slots = [...form.slots];
  }

  function removeTag(slotIdx: number, subIdx: number, tagIdx: number) {
    form.slots[slotIdx].subslots[subIdx].tags = form.slots[slotIdx].subslots[subIdx].tags.filter(
      (_, i) => i !== tagIdx,
    );
    form.slots = [...form.slots];
  }

  // Save
  async function save() {
    if (!form.deviceId || !form.interfaceName || (!form.stationName && !form.ip)) return;
    saving = true;
    try {
      const result = await apiPost('/scanner/profinetcontroller/subscribe', form);
      if (result.error) {
        error = result.error.error;
      } else {
        showForm = false;
        editingDeviceId = null;
        await invalidateAll();
      }
    } finally {
      saving = false;
    }
  }

  // Delete
  async function deleteDevice(sub: ControllerSubscription) {
    saving = true;
    try {
      const result = await apiPost('/scanner/profinetcontroller/unsubscribe', {
        subscriberId: sub.subscriberId,
        deviceId: sub.deviceId,
      });
      if (result.error) {
        error = result.error.error;
      } else {
        deleteConfirm = null;
        await invalidateAll();
      }
    } finally {
      saving = false;
    }
  }

  function tagCount(sub: ControllerSubscription): number {
    return sub.slots.reduce(
      (acc, s) => acc + s.subslots.reduce((a2, ss) => a2 + ss.tags.length, 0),
      0,
    );
  }
</script>

<div class="pn-devices">
  {#if error}
    <div class="error-banner">{error}</div>
  {/if}

  <div class="header-row">
    <h2>PROFINET Devices</h2>
    <button class="add-btn" onclick={startAdd} disabled={showForm}>Add Device</button>
  </div>

  {#if showForm}
    <div class="form-card">
      <h3>{editingDeviceId ? 'Edit Device' : 'Add Device'}</h3>

      <div class="form-grid">
        <label>
          <span>Device ID</span>
          <input type="text" bind:value={form.deviceId} placeholder="e.g. profinet_plc_1" disabled={!!editingDeviceId} />
        </label>
        <label>
          <span>Station Name</span>
          <input type="text" bind:value={form.stationName} placeholder="PROFINET station name (DCP)" />
        </label>
        <label>
          <span>IP (optional)</span>
          <input type="text" bind:value={form.ip} placeholder="Skip DCP, connect directly" />
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
          <span>Cycle Time (ms)</span>
          <input type="number" bind:value={form.cycleTimeMs} min="1" />
        </label>
      </div>

      <!-- Slots -->
      <div class="slots-section">
        <div class="section-header">
          <h4>Slots</h4>
          <button class="small-btn" onclick={addSlot}>+ Slot</button>
        </div>

        {#each form.slots as slot, slotIdx}
          <div class="slot-card">
            <div class="slot-header">
              <span class="slot-label">Slot {slot.slotNumber}</span>
              <label class="inline-field">
                <span>Module ID</span>
                <input type="text" bind:value={slot.moduleIdentNo} placeholder="0"
                  oninput={(e) => { slot.moduleIdentNo = parseInt((e.target as HTMLInputElement).value) || 0; form.slots = [...form.slots]; }} />
              </label>
              <button class="remove-btn" onclick={() => removeSlot(slotIdx)}>Remove</button>
            </div>

            <!-- Subslots -->
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
                      <input type="text" bind:value={subslot.submoduleIdentNo} placeholder="0"
                        oninput={(e) => { subslot.submoduleIdentNo = parseInt((e.target as HTMLInputElement).value) || 0; form.slots = [...form.slots]; }} />
                    </label>
                    <label class="inline-field">
                      <span>Input Size</span>
                      <input type="number" bind:value={subslot.inputSize} min="0" />
                    </label>
                    <label class="inline-field">
                      <span>Output Size</span>
                      <input type="number" bind:value={subslot.outputSize} min="0" />
                    </label>
                    <button class="remove-btn" onclick={() => removeSubslot(slotIdx, subIdx)}>Remove</button>
                  </div>

                  <!-- Tags -->
                  <div class="tags-section">
                    <div class="section-header sub">
                      <span>Tags</span>
                      <button class="small-btn" onclick={() => addTag(slotIdx, subIdx)}>+ Tag</button>
                    </div>

                    {#each subslot.tags as tag, tagIdx}
                      <div class="tag-row">
                        <input type="text" bind:value={tag.tagId} placeholder="Tag ID" class="tag-id" />
                        <input type="number" bind:value={tag.byteOffset} min="0" placeholder="Byte" class="tag-num" title="Byte offset" />
                        <input type="number" bind:value={tag.bitOffset} min="0" max="7" placeholder="Bit" class="tag-bit" title="Bit offset (bool only)" />
                        <select bind:value={tag.datatype} class="tag-type">
                          {#each PROFINET_TYPES as t}
                            <option value={t}>{t}</option>
                          {/each}
                        </select>
                        <select bind:value={tag.direction} class="tag-dir">
                          <option value="input">Input</option>
                          <option value="output">Output</option>
                        </select>
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

      <div class="form-actions">
        <button class="save-btn" onclick={save} disabled={saving || !form.deviceId || !form.interfaceName || (!form.stationName && !form.ip)}>
          {saving ? 'Saving...' : 'Save'}
        </button>
        <button class="cancel-btn" onclick={cancelForm}>Cancel</button>
      </div>
    </div>
  {/if}

  <!-- Device list -->
  {#if subscriptions.length === 0 && !showForm}
    <div class="empty-state">
      <p>No PROFINET devices configured.</p>
      <p class="hint">Click "Add Device" to subscribe to a PROFINET IO Device.</p>
    </div>
  {:else}
    <div class="device-list">
      {#each subscriptions as sub}
        <div class="device-card">
          <button class="device-header" onclick={() => toggleDevice(sub.deviceId)}>
            <svg class="chevron" class:expanded={expandedDevices.has(sub.deviceId)} width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M9 18l6-6-6-6"/>
            </svg>
            <span class="device-name">{sub.deviceId}</span>
            <span class="device-meta">
              {sub.stationName || sub.ip} &middot; {sub.interfaceName} &middot; {sub.cycleTimeMs}ms
            </span>
            <span class="count-badge">{tagCount(sub)} tags</span>
            <span class="count-badge">{sub.slots.length} slots</span>
          </button>

          {#if expandedDevices.has(sub.deviceId)}
            <div class="device-details">
              <div class="detail-row">
                <span>Station Name:</span> <code>{sub.stationName || '—'}</code>
              </div>
              <div class="detail-row">
                <span>IP:</span> <code>{sub.ip || '(DCP discovery)'}</code>
              </div>
              <div class="detail-row">
                <span>Interface:</span> <code>{sub.interfaceName}</code>
              </div>
              <div class="detail-row">
                <span>Cycle Time:</span> <code>{sub.cycleTimeMs} ms</code>
              </div>

              {#each sub.slots as slot}
                <div class="detail-slot">
                  <span class="slot-badge">Slot {slot.slotNumber}</span>
                  <span class="id-badge">Module 0x{slot.moduleIdentNo.toString(16).toUpperCase()}</span>

                  {#each slot.subslots as subslot}
                    <div class="detail-subslot">
                      <span class="subslot-badge">Subslot {subslot.subslotNumber}</span>
                      <span class="id-badge">Submodule 0x{subslot.submoduleIdentNo.toString(16).toUpperCase()}</span>
                      <span class="size-badge">In: {subslot.inputSize}B</span>
                      <span class="size-badge">Out: {subslot.outputSize}B</span>

                      {#if subslot.tags.length > 0}
                        <table class="tag-table">
                          <thead>
                            <tr>
                              <th>Tag</th>
                              <th>Offset</th>
                              <th>Type</th>
                              <th>Dir</th>
                            </tr>
                          </thead>
                          <tbody>
                            {#each subslot.tags as tag}
                              <tr>
                                <td><code>{tag.tagId}</code></td>
                                <td>{tag.byteOffset}{tag.datatype === 'bool' ? `.${tag.bitOffset}` : ''}</td>
                                <td>{tag.datatype}</td>
                                <td class="dir-{tag.direction}">{tag.direction}</td>
                              </tr>
                            {/each}
                          </tbody>
                        </table>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/each}

              <div class="device-actions">
                <button class="edit-btn" onclick={() => startEdit(sub)}>Edit</button>
                {#if deleteConfirm === sub.deviceId}
                  <span class="confirm-text">Delete this device?</span>
                  <button class="delete-btn confirm" onclick={() => deleteDevice(sub)}>Yes, delete</button>
                  <button class="cancel-btn small" onclick={() => (deleteConfirm = null)}>No</button>
                {:else}
                  <button class="delete-btn" onclick={() => (deleteConfirm = sub.deviceId)}>Delete</button>
                {/if}
              </div>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style lang="scss">
  .pn-devices {
    padding: 1.5rem 2rem;
  }

  .error-banner {
    background: var(--color-red-500, #ef4444);
    color: white;
    padding: 0.75rem 1rem;
    border-radius: var(--rounded-lg);
    margin-bottom: 1rem;
    font-size: 0.875rem;
  }

  .header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }
  }

  .add-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-primary);
    background: transparent;
    border: 1px solid var(--theme-primary);
    border-radius: var(--rounded-md);
    cursor: pointer;
    transition: all 0.15s;

    &:hover:not(:disabled) {
      background: var(--theme-primary);
      color: white;
    }

    &:disabled {
      opacity: 0.4;
      cursor: default;
    }
  }

  /* Form */
  .form-card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1.5rem;
    margin-bottom: 1rem;

    h3 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 1rem;
    }
  }

  .form-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 0.75rem;
    margin-bottom: 1rem;

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

  /* Slots section */
  .slots-section {
    margin-top: 1rem;
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.5rem;

    h4 {
      font-size: 0.875rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }

    &.sub {
      margin-top: 0.5rem;

      span {
        font-size: 0.75rem;
        font-weight: 500;
        color: var(--theme-text-muted);
      }
    }
  }

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
    background: var(--theme-bg);
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

    input {
      width: 100px;
      padding: 0.25rem 0.375rem;
      font-size: 0.75rem;
      font-family: var(--font-mono);
      background: var(--theme-surface);
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-sm);
      color: var(--theme-text);
    }
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
    background: var(--theme-surface);
  }

  /* Tags */
  .tags-section {
    margin-top: 0.5rem;
    margin-left: 0.5rem;
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
      background: var(--theme-bg);
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-sm);
      color: var(--theme-text);
    }
  }

  .tag-id {
    flex: 1;
    min-width: 120px;
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

  .tag-dir {
    width: 75px;
  }

  /* Form actions */
  .form-actions {
    display: flex;
    gap: 0.5rem;
    margin-top: 1rem;
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

  .cancel-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    cursor: pointer;

    &.small {
      padding: 0.25rem 0.5rem;
      font-size: 0.75rem;
    }
  }

  /* Device list */
  .empty-state {
    text-align: center;
    padding: 3rem 1rem;
    color: var(--theme-text-muted);

    p {
      margin: 0.25rem 0;
    }

    .hint {
      font-size: 0.8125rem;
    }
  }

  .device-list {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .device-card {
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
  }

  .device-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.75rem 1rem;
    background: var(--theme-surface);
    border: none;
    cursor: pointer;
    text-align: left;
    color: var(--theme-text);
    font-size: 0.875rem;

    &:hover {
      background: var(--theme-bg);
    }
  }

  .chevron {
    flex-shrink: 0;
    transition: transform 0.15s;
    color: var(--theme-text-muted);

    &.expanded {
      transform: rotate(90deg);
    }
  }

  .device-name {
    font-weight: 600;
    font-family: var(--font-mono);
  }

  .device-meta {
    color: var(--theme-text-muted);
    font-size: 0.8125rem;
    margin-left: auto;
  }

  .count-badge {
    font-size: 0.6875rem;
    font-weight: 500;
    padding: 0.125rem 0.5rem;
    border-radius: 999px;
    background: var(--badge-green-bg, rgba(16, 185, 129, 0.1));
    color: var(--badge-green-text, #10b981);
    font-family: var(--font-mono);
  }

  /* Device details */
  .device-details {
    padding: 0.75rem 1rem 1rem;
    border-top: 1px solid var(--theme-border);
    background: var(--theme-bg);
  }

  .detail-row {
    display: flex;
    gap: 0.5rem;
    font-size: 0.8125rem;
    padding: 0.25rem 0;

    span {
      color: var(--theme-text-muted);
      min-width: 100px;
    }

    code {
      font-family: var(--font-mono);
      color: var(--theme-text);
    }
  }

  .detail-slot {
    margin-top: 0.75rem;
    padding: 0.5rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-surface);
  }

  .slot-badge,
  .subslot-badge {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--theme-primary);
  }

  .id-badge,
  .size-badge {
    font-size: 0.6875rem;
    font-family: var(--font-mono);
    color: var(--theme-text-muted);
    padding: 0.0625rem 0.375rem;
    background: var(--theme-bg);
    border-radius: var(--rounded-sm);
  }

  .detail-subslot {
    margin: 0.5rem 0 0 1rem;
    padding: 0.375rem 0.5rem;
    border-left: 2px solid var(--theme-border);
  }

  .tag-table {
    width: 100%;
    margin-top: 0.375rem;
    font-size: 0.75rem;
    border-collapse: collapse;

    th {
      text-align: left;
      font-weight: 500;
      color: var(--theme-text-muted);
      padding: 0.25rem 0.5rem;
      border-bottom: 1px solid var(--theme-border);
    }

    td {
      padding: 0.25rem 0.5rem;
      border-bottom: 1px solid var(--theme-border);
      color: var(--theme-text);
    }
  }

  .dir-input {
    color: var(--badge-green-text, #10b981);
  }

  .dir-output {
    color: var(--color-orange-400, #fb923c);
  }

  .device-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.75rem;
    padding-top: 0.5rem;
    border-top: 1px solid var(--theme-border);
  }

  .edit-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--theme-primary);
    background: transparent;
    border: 1px solid var(--theme-primary);
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover {
      background: var(--theme-primary);
      color: white;
    }
  }

  .delete-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--color-red-500, #ef4444);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover {
      border-color: var(--color-red-500, #ef4444);
    }

    &.confirm {
      border-color: var(--color-red-500, #ef4444);
      background: var(--color-red-500, #ef4444);
      color: white;
    }
  }

  .confirm-text {
    font-size: 0.75rem;
    color: var(--color-red-500, #ef4444);
  }
</style>
