<script lang="ts">
  import type { GatewayConfig, GatewayUdtTemplate, GatewayUdtVariable, DeadBandConfig } from '$lib/types/gateway';
  import { apiPut } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { slide, fly } from 'svelte/transition';
  import { state as saltState } from '@joyautomation/salt';
  import { ChevronRight } from '@joyautomation/salt/icons';

  let { gatewayConfig, error }: {
    gatewayConfig: GatewayConfig | null;
    error: string | null;
  } = $props();

  // ── Derived data ──
  const templates = $derived(gatewayConfig?.udtTemplates ?? []);
  const allInstances = $derived(gatewayConfig?.udtVariables ?? []);

  // ── State ──
  let activeTemplateKey = $state('');
  let activeTab: 'template' | 'instances' = $state('template');
  let selectedInstances: Set<string> = $state(new Set());
  let expandedInstances: Set<string> = $state(new Set());
  let batchPanelOpen = $state(false);
  let batchMember = $state('');
  let batchDeadband = $state(1.0);
  let saving = $state(false);
  let editingCell: string | null = $state(null);
  let editDraft = $state('');

  // Working copies for dirty tracking
  let workingTemplates: Map<string, GatewayUdtTemplate> = $state(new Map());
  let workingInstanceOverrides: Map<string, Record<string, DeadBandConfig>> = $state(new Map());

  // Auto-select first template
  $effect(() => {
    if (templates.length > 0 && (!activeTemplateKey || !templates.find(t => t.name === activeTemplateKey))) {
      activeTemplateKey = templates[0].name;
    }
    // Initialize working copies from config
    const tMap = new Map<string, GatewayUdtTemplate>();
    for (const t of templates) {
      tMap.set(t.name, JSON.parse(JSON.stringify(t)));
    }
    workingTemplates = tMap;

    const iMap = new Map<string, Record<string, DeadBandConfig>>();
    for (const inst of allInstances) {
      if (inst.memberDeadbands && Object.keys(inst.memberDeadbands).length > 0) {
        iMap.set(inst.id, JSON.parse(JSON.stringify(inst.memberDeadbands)));
      }
    }
    workingInstanceOverrides = iMap;
    selectedInstances = new Set();
    expandedInstances = new Set();
    batchPanelOpen = false;
  });

  // ── Derived from working state ──
  const activeTemplate = $derived(workingTemplates.get(activeTemplateKey));
  const activeInstances = $derived(allInstances.filter(i => i.templateName === activeTemplateKey));
  const analogMembers = $derived(
    (activeTemplate?.members ?? []).filter(m => m.datatype === 'number')
  );
  const enabledAnalogMembers = $derived(analogMembers);

  const allSelected = $derived(activeInstances.length > 0 && activeInstances.every(i => selectedInstances.has(i.id)));
  const someSelected = $derived(activeInstances.some(i => selectedInstances.has(i.id)));

  // Dirty tracking
  const isDirty = $derived(() => {
    for (const [name, working] of workingTemplates) {
      const original = templates.find(t => t.name === name);
      if (!original) continue;
      for (const wm of working.members) {
        const om = original.members.find(m => m.name === wm.name);
        if (!om) continue;
        const wdb = wm.defaultDeadband;
        const odb = om.defaultDeadband;
        if ((!wdb && odb) || (wdb && !odb)) return true;
        if (wdb && odb && (wdb.value !== odb.value || wdb.minTime !== odb.minTime || wdb.maxTime !== odb.maxTime)) return true;
      }
    }
    // Check instance overrides
    for (const inst of allInstances) {
      const origDb = inst.memberDeadbands ?? {};
      const workDb = workingInstanceOverrides.get(inst.id) ?? {};
      const origKeys = Object.keys(origDb);
      const workKeys = Object.keys(workDb);
      if (origKeys.length !== workKeys.length) return true;
      for (const k of workKeys) {
        if (!origDb[k]) return true;
        if (workDb[k].value !== origDb[k].value) return true;
      }
    }
    return false;
  });

  // ── Cascade helper ──
  function getEffectiveRbe(memberName: string, instanceId: string): { value: number; inherited: boolean } {
    const instOverrides = workingInstanceOverrides.get(instanceId);
    if (instOverrides?.[memberName]) {
      return { value: instOverrides[memberName].value, inherited: false };
    }
    const member = activeTemplate?.members.find(m => m.name === memberName);
    if (member?.defaultDeadband) {
      return { value: member.defaultDeadband.value, inherited: true };
    }
    return { value: 0, inherited: true };
  }

  function getOverrideCount(instanceId: string): number {
    return Object.keys(workingInstanceOverrides.get(instanceId) ?? {}).length;
  }

  // ── Template mutations ──
  function setTemplateMemberDeadband(memberName: string, value: number) {
    const tmpl = workingTemplates.get(activeTemplateKey);
    if (!tmpl) return;
    const member = tmpl.members.find(m => m.name === memberName);
    if (!member) return;
    member.defaultDeadband = { value };
    workingTemplates = new Map(workingTemplates);
  }

  function clearTemplateMemberDeadband(memberName: string) {
    const tmpl = workingTemplates.get(activeTemplateKey);
    if (!tmpl) return;
    const member = tmpl.members.find(m => m.name === memberName);
    if (!member) return;
    delete member.defaultDeadband;
    workingTemplates = new Map(workingTemplates);
  }

  // ── Instance mutations ──
  function setInstanceOverride(instanceId: string, memberName: string, value: number) {
    const overrides = { ...(workingInstanceOverrides.get(instanceId) ?? {}) };
    // Auto-clean: if matches template default, remove
    const member = activeTemplate?.members.find(m => m.name === memberName);
    if (member?.defaultDeadband && member.defaultDeadband.value === value) {
      delete overrides[memberName];
    } else {
      overrides[memberName] = { value };
    }
    const next = new Map(workingInstanceOverrides);
    if (Object.keys(overrides).length > 0) {
      next.set(instanceId, overrides);
    } else {
      next.delete(instanceId);
    }
    workingInstanceOverrides = next;
  }

  function clearInstanceOverride(instanceId: string, memberName: string) {
    const overrides = { ...(workingInstanceOverrides.get(instanceId) ?? {}) };
    delete overrides[memberName];
    const next = new Map(workingInstanceOverrides);
    if (Object.keys(overrides).length > 0) {
      next.set(instanceId, overrides);
    } else {
      next.delete(instanceId);
    }
    workingInstanceOverrides = next;
  }

  function clearAllOverrides(instanceId: string) {
    const next = new Map(workingInstanceOverrides);
    next.delete(instanceId);
    workingInstanceOverrides = next;
  }

  // ── Selection ──
  function toggleSelectAll() {
    if (allSelected) {
      const next = new Set(selectedInstances);
      for (const i of activeInstances) next.delete(i.id);
      selectedInstances = next;
    } else {
      const next = new Set(selectedInstances);
      for (const i of activeInstances) next.add(i.id);
      selectedInstances = next;
    }
  }

  function toggleSelect(id: string) {
    const next = new Set(selectedInstances);
    if (next.has(id)) next.delete(id); else next.add(id);
    selectedInstances = next;
  }

  function toggleExpand(id: string) {
    const next = new Set(expandedInstances);
    if (next.has(id)) next.delete(id); else next.add(id);
    expandedInstances = next;
  }

  // ── Batch ──
  const RBE_PRESETS = [
    { name: 'Tight', value: 0.1 },
    { name: 'Standard', value: 1.0 },
    { name: 'Loose', value: 5.0 },
    { name: 'Wide', value: 10.0 },
  ];

  function openBatchPanel() {
    batchMember = enabledAnalogMembers[0]?.name ?? '';
    const member = activeTemplate?.members.find(m => m.name === batchMember);
    batchDeadband = member?.defaultDeadband?.value ?? 1.0;
    batchPanelOpen = true;
  }

  function applyBatch() {
    if (!batchMember || selectedInstances.size === 0) return;
    for (const instId of selectedInstances) {
      const inst = activeInstances.find(i => i.id === instId);
      if (!inst) continue;
      setInstanceOverride(instId, batchMember, batchDeadband);
    }
    saltState.addNotification({ message: `Applied to ${selectedInstances.size} instances`, type: 'success' });
    batchPanelOpen = false;
  }

  function clearSelectedOverrides() {
    for (const instId of selectedInstances) {
      clearAllOverrides(instId);
    }
    saltState.addNotification({ message: `Cleared overrides on ${selectedInstances.size} instances`, type: 'success' });
  }

  // ── Inline editing ──
  function startEdit(key: string, currentValue: number) {
    editingCell = key;
    editDraft = String(currentValue);
  }

  function commitEdit(onCommit: (val: number) => void) {
    const val = parseFloat(editDraft);
    if (!isNaN(val) && val >= 0) onCommit(val);
    editingCell = null;
  }

  function cancelEdit() {
    editingCell = null;
  }

  // ── Save ──
  async function saveChanges() {
    saving = true;
    try {
      const tmpl = workingTemplates.get(activeTemplateKey);
      if (!tmpl) return;

      const memberUpdates: Record<string, unknown> = {};
      for (const m of tmpl.members.filter(m => m.datatype === 'number')) {
        memberUpdates[m.name] = m.defaultDeadband ? { value: m.defaultDeadband.value } : null;
      }

      const instanceUpdates: Record<string, unknown> = {};
      for (const inst of activeInstances) {
        instanceUpdates[inst.id] = workingInstanceOverrides.get(inst.id) ?? {};
      }

      const result = await apiPut(`/gateways/gateway/udt-config/${activeTemplateKey}`, { memberUpdates, instanceUpdates });

      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
        return;
      }

      saltState.addNotification({ message: `Saved tag config for ${activeTemplateKey}`, type: 'success' });
      await invalidateAll();
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Save failed', type: 'error' });
    } finally {
      saving = false;
    }
  }
</script>

<div class="tag-config">
  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  {#if templates.length === 0}
    <div class="empty-state">
      <p>No UDT templates configured. Go to Variables and browse a device to import UDT tags.</p>
    </div>
  {:else}
    <div class="tc-layout">
      <!-- Side nav (desktop) -->
      <nav class="tc-side-nav">
        <div class="tc-side-head">UDT Templates</div>
        {#each templates as tmpl}
          {@const instCount = allInstances.filter(i => i.templateName === tmpl.name).length}
          <button
            class="tc-side-item"
            class:active={activeTemplateKey === tmpl.name}
            onclick={() => { activeTemplateKey = tmpl.name; activeTab = 'template'; selectedInstances = new Set(); batchPanelOpen = false; }}
          >
            <span class="tc-side-icon">T</span>
            <span class="tc-side-name">{tmpl.name}</span>
            <span class="tc-side-count">{instCount}</span>
          </button>
        {/each}
      </nav>

      <div class="tc-main">
        <!-- Mobile/tablet template selector -->
        <div class="tc-top-bar">
          <select class="tc-tpl-select" bind:value={activeTemplateKey} onchange={() => { selectedInstances = new Set(); batchPanelOpen = false; }}>
            {#each templates as tmpl}
              <option value={tmpl.name}>{tmpl.name}</option>
            {/each}
          </select>
          <span class="tc-tpl-meta">{activeTemplate?.members.length ?? 0}m / {activeInstances.length}i</span>
        </div>

        <!-- Header + tabs -->
        <div class="tc-header">
          <span class="tc-title">{activeTemplateKey.replace(/_/g, ' ')}</span>
          <span class="tc-subtitle">{activeTemplate?.members.length ?? 0} members / {activeInstances.length} instances</span>
        </div>

        <div class="tc-tabs">
          <button class="tc-tab" class:active={activeTab === 'template'} onclick={() => activeTab = 'template'}>Template Defaults</button>
          <button class="tc-tab" class:active={activeTab === 'instances'} onclick={() => activeTab = 'instances'}>Instances</button>
        </div>

        <!-- Template Defaults Tab -->
        {#if activeTab === 'template'}
          <div class="tc-scroll">
            <table class="tpl-table">
              <thead>
                <tr>
                  <th style="width:35%">Member</th>
                  <th style="width:12%">Type</th>
                  <th style="width:20%">Deadband</th>
                  <th style="width:18%">Status</th>
                  <th style="width:15%"></th>
                </tr>
              </thead>
              <tbody>
                {#each activeTemplate?.members ?? [] as member}
                  {@const isAnalog = member.datatype === 'number'}
                  {@const cellKey = `tpl::${member.name}`}
                  <tr>
                    <td><span class="mono member-name">{member.name}</span></td>
                    <td>
                      <span class="type-badge" class:type-number={isAnalog} class:type-bool={member.datatype === 'boolean'}>
                        {member.datatype}
                      </span>
                    </td>
                    <td>
                      {#if isAnalog}
                        {#if editingCell === cellKey}
                          <input
                            class="il-input"
                            type="number"
                            step="any"
                            min="0"
                            bind:value={editDraft}
                            onblur={() => commitEdit((v) => setTemplateMemberDeadband(member.name, v))}
                            onkeydown={(e) => { if (e.key === 'Enter') commitEdit((v) => setTemplateMemberDeadband(member.name, v)); if (e.key === 'Escape') cancelEdit(); }}
                          />
                        {:else}
                          <button class="il-value" onclick={() => startEdit(cellKey, member.defaultDeadband?.value ?? 0)}>
                            {member.defaultDeadband?.value ?? '—'}
                          </button>
                        {/if}
                      {:else}
                        <span class="rbe-na">n/a</span>
                      {/if}
                    </td>
                    <td>
                      {#if isAnalog && member.defaultDeadband}
                        <span class="rbe-pill on">RBE {member.defaultDeadband.value}</span>
                      {:else if isAnalog}
                        <span class="rbe-pill off">no deadband</span>
                      {:else}
                        <span class="rbe-pill off">COV</span>
                      {/if}
                    </td>
                    <td>
                      {#if isAnalog && member.defaultDeadband}
                        <button class="clear-btn" onclick={() => clearTemplateMemberDeadband(member.name)}>clear</button>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}

        <!-- Instances Tab -->
        {#if activeTab === 'instances'}
          <div class="inst-toolbar">
            <label class="chk-wrap">
              <input type="checkbox" checked={allSelected && activeInstances.length > 0} indeterminate={someSelected && !allSelected} onchange={toggleSelectAll} />
            </label>
            <span class="inst-toolbar-count">{activeInstances.length} instances</span>
            <div class="inst-legend">
              <span class="legend-dot"></span>
              <span class="legend-text">= override</span>
            </div>
          </div>

          <div class="tc-scroll">
            {#each activeInstances as inst (inst.id)}
              {@const expanded = expandedInstances.has(inst.id)}
              {@const ovCount = getOverrideCount(inst.id)}
              <div class="inst-row" class:selected={selectedInstances.has(inst.id)}>
                <div class="inst-head">
                  <label class="chk-wrap" onclick={(e: MouseEvent) => e.stopPropagation()}>
                    <input type="checkbox" checked={selectedInstances.has(inst.id)} onchange={() => toggleSelect(inst.id)} />
                  </label>
                  <button class="inst-expand" class:open={expanded} onclick={() => toggleExpand(inst.id)}>
                    <ChevronRight size="0.75rem" />
                  </button>
                  <button class="inst-name mono" onclick={() => toggleExpand(inst.id)}>{inst.tag}</button>
                  <span class="inst-id mono">{inst.id}</span>
                  <span class="inst-ov-badge" class:has-overrides={ovCount > 0}>
                    {ovCount > 0 ? `${ovCount} override${ovCount > 1 ? 's' : ''}` : 'inherited'}
                  </span>
                </div>

                {#if expanded}
                  <div class="inst-detail" transition:slide|local={{ duration: 150 }}>
                    {#if enabledAnalogMembers.length === 0}
                      <p class="inst-empty">No analog members for RBE configuration.</p>
                    {:else}
                      <table class="inst-table">
                        <thead>
                          <tr>
                            <th style="width:30%">Member</th>
                            <th style="width:22%">Deadband</th>
                            <th style="width:18%">Status</th>
                            <th style="width:15%"></th>
                          </tr>
                        </thead>
                        <tbody>
                          {#each enabledAnalogMembers as member}
                            {@const eff = getEffectiveRbe(member.name, inst.id)}
                            {@const cellKey = `inst::${inst.id}::${member.name}`}
                            <tr>
                              <td><span class="mono member-name">{member.name}</span></td>
                              <td>
                                {#if editingCell === cellKey}
                                  <input
                                    class="il-input"
                                    type="number"
                                    step="any"
                                    min="0"
                                    bind:value={editDraft}
                                    onblur={() => commitEdit((v) => setInstanceOverride(inst.id, member.name, v))}
                                    onkeydown={(e) => { if (e.key === 'Enter') commitEdit((v) => setInstanceOverride(inst.id, member.name, v)); if (e.key === 'Escape') cancelEdit(); }}
                                  />
                                {:else}
                                  <button
                                    class="il-value"
                                    class:inherited={eff.inherited}
                                    class:overridden={!eff.inherited}
                                    onclick={() => startEdit(cellKey, eff.value)}
                                  >
                                    {eff.value}
                                  </button>
                                {/if}
                              </td>
                              <td>
                                {#if eff.inherited}
                                  <span class="status-inherited">inherited</span>
                                {:else}
                                  <span class="status-override">override</span>
                                {/if}
                              </td>
                              <td>
                                {#if !eff.inherited}
                                  <button class="revert-btn" onclick={() => clearInstanceOverride(inst.id, member.name)}>revert</button>
                                {/if}
                              </td>
                            </tr>
                          {/each}
                        </tbody>
                      </table>
                    {/if}
                  </div>
                {/if}
              </div>
            {/each}
          </div>

          <!-- Bulk bar -->
          {#if someSelected}
            <div class="bulk-bar" transition:fly|local={{ y: 40, duration: 150 }}>
              <span class="bulk-count">{selectedInstances.size} selected</span>
              <div class="bulk-sep"></div>
              <button class="bulk-btn accent" onclick={openBatchPanel}>Batch Set RBE</button>
              <button class="bulk-btn" onclick={clearSelectedOverrides}>Clear Overrides</button>
              <button class="bulk-clear" onclick={() => { selectedInstances = new Set(); batchPanelOpen = false; }}>Clear</button>

              {#if batchPanelOpen}
                <button class="bp-backdrop" aria-label="Close batch panel" onclick={() => batchPanelOpen = false}></button>
                <div class="batch-panel">
                  <div class="bp-title">Batch RBE — {selectedInstances.size} instances</div>
                  <div class="bp-row">
                    <label>Member</label>
                    <select bind:value={batchMember}>
                      {#each enabledAnalogMembers as m}
                        <option value={m.name}>{m.name}</option>
                      {/each}
                    </select>
                  </div>
                  <div class="bp-row">
                    <label>Deadband</label>
                    <input type="number" step="any" min="0" bind:value={batchDeadband} />
                  </div>
                  <div class="bp-presets">
                    {#each RBE_PRESETS as preset}
                      <button class="bp-preset" onclick={() => batchDeadband = preset.value}>
                        {preset.name} ({preset.value})
                      </button>
                    {/each}
                  </div>
                  <div class="bp-actions">
                    <button class="bp-cancel" onclick={() => batchPanelOpen = false}>Cancel</button>
                    <button class="bp-apply" onclick={applyBatch}>Apply to {selectedInstances.size}</button>
                  </div>
                </div>
              {/if}
            </div>
          {/if}
        {/if}
      </div>
    </div>

    <!-- Save bar -->
    {#if isDirty()}
      <div class="save-bar" transition:fly|local={{ y: 40, duration: 150 }}>
        <button class="save-btn" onclick={saveChanges} disabled={saving}>
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
        <button class="reset-btn" onclick={() => invalidateAll()} disabled={saving}>Discard</button>
      </div>
    {/if}
  {/if}
</div>

<style lang="scss">
  .tag-config { display: flex; flex-direction: column; height: 100%; }

  .error-box {
    padding: 1rem; border-radius: var(--rounded-lg); background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444); margin: 1.5rem 2rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .empty-state { padding: 3rem 2rem; text-align: center; p { color: var(--theme-text-muted); font-size: 0.875rem; } }

  .mono { font-family: 'IBM Plex Mono', monospace; }

  // ── Layout ──
  .tc-layout { display: flex; flex: 1; overflow: hidden; }

  .tc-side-nav {
    width: 230px; min-width: 230px;
    background: var(--theme-surface);
    border-right: 1px solid var(--theme-border);
    display: flex; flex-direction: column;
    overflow-y: auto;
  }

  .tc-side-head {
    padding: 1rem; font-size: 0.625rem; font-weight: 700;
    text-transform: uppercase; letter-spacing: 0.1em;
    color: var(--theme-text-muted); border-bottom: 1px solid var(--theme-border);
  }

  .tc-side-item {
    display: flex; align-items: center; gap: 0.5rem;
    padding: 0.5rem 1rem; cursor: pointer;
    border: none; background: none; color: var(--theme-text);
    font-size: 0.8125rem; font-family: inherit; text-align: left;
    border-left: 2px solid transparent;
    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
    &.active { background: color-mix(in srgb, var(--theme-primary) 10%, transparent); border-left-color: var(--theme-primary); }
  }

  .tc-side-icon {
    display: inline-flex; align-items: center; justify-content: center;
    width: 20px; height: 20px; border-radius: var(--rounded-sm);
    background: var(--badge-purple-bg); color: var(--badge-purple-text);
    font-size: 0.6875rem; font-weight: 700; flex-shrink: 0;
  }

  .tc-side-name { font-family: 'IBM Plex Mono', monospace; font-weight: 500; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tc-side-count { font-size: 0.6875rem; color: var(--theme-text-muted); font-family: 'IBM Plex Mono', monospace; }

  .tc-main { flex: 1; display: flex; flex-direction: column; overflow: hidden; min-width: 0; }

  // ── Top bar (tablet/mobile) ──
  .tc-top-bar {
    display: none; padding: 0.625rem 1rem; gap: 0.625rem; align-items: center;
    background: var(--theme-surface); border-bottom: 1px solid var(--theme-border);
  }

  .tc-tpl-select {
    font-family: 'IBM Plex Mono', monospace; font-size: 0.8125rem; font-weight: 600;
    background: var(--theme-input-bg); border: 1px solid var(--theme-border);
    color: var(--theme-text); padding: 0.4rem 0.75rem; border-radius: var(--rounded-md);
    flex: 1; max-width: 260px; outline: none;
    &:focus { border-color: var(--theme-primary); }
  }

  .tc-tpl-meta { margin-left: auto; font-size: 0.625rem; color: var(--theme-text-muted); font-family: 'IBM Plex Mono', monospace; }

  // ── Header + tabs ──
  .tc-header {
    padding: 0.875rem 1.25rem;
    background: var(--theme-surface);
    border-bottom: 1px solid var(--theme-border);
    display: flex; align-items: center; gap: 0.75rem;
  }

  .tc-title { font-size: 1rem; font-weight: 700; color: var(--theme-text); }

  .tc-subtitle {
    font-size: 0.6875rem; color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-text) 6%, transparent);
    padding: 0.15rem 0.5rem; border-radius: var(--rounded-sm);
    font-family: 'IBM Plex Mono', monospace;
  }

  .tc-tabs {
    display: flex; gap: 0;
    border-bottom: 1px solid var(--theme-border);
    background: var(--theme-surface);
    padding: 0 1.25rem;
  }

  .tc-tab {
    padding: 0.625rem 1rem;
    font-size: 0.8125rem; font-weight: 600;
    color: var(--theme-text-muted); cursor: pointer;
    border: none; background: none; font-family: inherit;
    border-bottom: 2px solid transparent; margin-bottom: -1px;
    &:hover { color: var(--theme-text); }
    &.active { color: var(--theme-primary); border-bottom-color: var(--theme-primary); }
  }

  .tc-scroll { flex: 1; overflow: auto; padding: 1rem 1.25rem; }

  // ── Template table ──
  .tpl-table { width: 100%; border-collapse: collapse; }
  .tpl-table th {
    text-align: left; font-size: 0.625rem; font-weight: 700;
    text-transform: uppercase; letter-spacing: 0.06em;
    color: var(--theme-text-muted); padding: 0.5rem 0.75rem;
    border-bottom: 2px solid var(--theme-border);
    background: var(--theme-surface); position: sticky; top: 0; z-index: 2;
  }
  .tpl-table td { padding: 0.5rem 0.75rem; border-bottom: 1px solid var(--theme-border); font-size: 0.8125rem; }
  .tpl-table tr:hover td { background: color-mix(in srgb, var(--theme-text) 3%, transparent); }

  .member-name { font-weight: 500; font-size: 0.8125rem; }

  .type-badge {
    font-size: 0.6875rem; padding: 0.1rem 0.4rem; border-radius: var(--rounded-sm);
    font-weight: 500; font-family: 'IBM Plex Mono', monospace;
  }
  .type-number { background: color-mix(in srgb, var(--theme-primary) 10%, transparent); color: var(--theme-primary); }
  .type-bool { background: var(--badge-amber-bg, #fef3c7); color: var(--badge-amber-text, #92400e); }

  .rbe-na { color: var(--theme-text-muted); font-size: 0.6875rem; }

  .rbe-pill {
    font-size: 0.6875rem; padding: 0.15rem 0.5rem; border-radius: var(--rounded-sm);
    font-family: 'IBM Plex Mono', monospace;
    &.on { background: color-mix(in srgb, var(--badge-teal-bg) 50%, transparent); color: var(--badge-teal-text); border: 1px solid var(--badge-teal-text); }
    &.off { color: var(--theme-text-muted); }
  }

  .clear-btn, .revert-btn {
    font-size: 0.6875rem; color: var(--theme-text-muted); cursor: pointer;
    padding: 0.15rem 0.4rem; border-radius: var(--rounded-sm);
    border: none; background: none; font-family: 'IBM Plex Mono', monospace;
    &:hover { color: #a78bfa; background: rgba(167, 139, 250, 0.08); }
  }

  // ── Inline editing ──
  .il-value {
    font-size: 0.8125rem; padding: 0.15rem 0.4rem; border-radius: var(--rounded-sm);
    cursor: pointer; border: 1px solid transparent; background: none;
    font-family: 'IBM Plex Mono', monospace; min-width: 2rem; text-align: left;
    color: var(--theme-text);
    &:hover { background: color-mix(in srgb, var(--theme-text) 6%, transparent); }
    &.inherited { color: var(--theme-text-muted); }
    &.overridden {
      color: #a78bfa;
      background: rgba(167, 139, 250, 0.08);
      border-color: rgba(167, 139, 250, 0.25);
    }
  }

  .il-input {
    font-size: 0.8125rem; background: var(--theme-input-bg);
    border: 1px solid var(--theme-primary); color: var(--theme-text);
    padding: 0.15rem 0.4rem; border-radius: var(--rounded-sm); outline: none;
    width: 5rem; font-family: 'IBM Plex Mono', monospace;
  }

  // ── Instances ──
  .inst-toolbar {
    display: flex; align-items: center; gap: 0.5rem;
    padding: 0.625rem 1.25rem; border-bottom: 1px solid var(--theme-border);
    background: var(--theme-surface);
  }

  .chk-wrap {
    display: flex; align-items: center; cursor: pointer; flex-shrink: 0;
    input[type="checkbox"] { cursor: pointer; }
  }

  .inst-toolbar-count { font-size: 0.6875rem; color: var(--theme-text-muted); }

  .inst-legend {
    margin-left: auto; font-size: 0.625rem; color: var(--theme-text-muted);
    display: flex; align-items: center; gap: 0.375rem;
  }

  .legend-dot { width: 8px; height: 8px; border-radius: 2px; background: #a78bfa; }
  .legend-text { font-family: 'IBM Plex Mono', monospace; }

  .inst-row {
    border-bottom: 1px solid var(--theme-border);
    &.selected { background: color-mix(in srgb, var(--theme-primary) 5%, transparent); }
  }

  .inst-head {
    display: flex; align-items: center; gap: 0.625rem;
    padding: 0.625rem 1.25rem; cursor: default;
    &:hover { background: color-mix(in srgb, var(--theme-text) 3%, transparent); }
  }

  .inst-expand {
    display: inline-flex; flex-shrink: 0; color: var(--theme-text-muted);
    transition: transform 0.15s ease; border: none; background: none; cursor: pointer; padding: 0;
    &.open { transform: rotate(90deg); }
  }

  .inst-name {
    font-weight: 600; font-size: 0.8125rem; border: none; background: none;
    color: var(--theme-text); cursor: pointer; padding: 0; text-align: left;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  .inst-id {
    font-size: 0.625rem; color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-text) 6%, transparent);
    padding: 0.1rem 0.5rem; border-radius: var(--rounded-sm);
  }

  .inst-ov-badge {
    margin-left: auto; font-size: 0.625rem; font-family: 'IBM Plex Mono', monospace;
    padding: 0.1rem 0.5rem; border-radius: var(--rounded-sm); white-space: nowrap;
    color: var(--theme-text-muted);
    &.has-overrides {
      background: rgba(167, 139, 250, 0.08);
      color: #a78bfa;
      border: 1px solid rgba(167, 139, 250, 0.25);
    }
  }

  .inst-detail { padding: 0 1.25rem 0.75rem 3.5rem; }

  .inst-table { width: 100%; border-collapse: collapse; }
  .inst-table th {
    text-align: left; font-size: 0.5625rem; font-weight: 700;
    text-transform: uppercase; letter-spacing: 0.06em;
    color: var(--theme-text-muted); padding: 0.375rem 0.625rem;
    border-bottom: 1px solid var(--theme-border);
  }
  .inst-table td { padding: 0.3rem 0.625rem; border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent); font-size: 0.8125rem; }
  .inst-table tr:hover td { background: color-mix(in srgb, var(--theme-text) 2%, transparent); }

  .inst-empty { font-size: 0.8125rem; color: var(--theme-text-muted); padding: 0.5rem 0; }

  .status-inherited { font-size: 0.625rem; color: var(--theme-text-muted); font-family: 'IBM Plex Mono', monospace; }
  .status-override { font-size: 0.625rem; color: #a78bfa; font-family: 'IBM Plex Mono', monospace; }

  // ── Bulk bar ──
  .bulk-bar {
    display: flex; align-items: center; gap: 0.625rem; flex-wrap: wrap;
    padding: 0.625rem 1.25rem;
    background: color-mix(in srgb, var(--theme-primary) 8%, transparent);
    border-top: 1px solid color-mix(in srgb, var(--theme-primary) 25%, transparent);
    position: relative;
  }

  .bulk-count { font-size: 0.8125rem; font-weight: 600; color: var(--theme-primary); white-space: nowrap; font-family: 'IBM Plex Mono', monospace; }
  .bulk-sep { width: 1px; height: 1.25rem; background: var(--theme-border); }

  .bulk-btn {
    font-size: 0.8125rem; font-weight: 600; font-family: inherit;
    padding: 0.375rem 0.875rem; border-radius: var(--rounded-md);
    border: 1px solid var(--theme-border); background: var(--theme-surface);
    color: var(--theme-text); cursor: pointer; white-space: nowrap;
    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface)); }
    &.accent { border-color: var(--theme-primary); color: var(--theme-primary); &:hover { background: color-mix(in srgb, var(--theme-primary) 8%, transparent); } }
  }

  .bulk-clear {
    margin-left: auto; font-size: 0.6875rem; color: var(--theme-text-muted);
    cursor: pointer; background: none; border: none; padding: 0.25rem 0.5rem; border-radius: var(--rounded-sm);
    &:hover { color: var(--theme-text); background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
  }

  // ── Batch panel ──
  .bp-backdrop { position: fixed; inset: 0; z-index: 49; background: rgba(0,0,0,0.3); border: none; cursor: default; }

  .batch-panel {
    position: absolute; bottom: 3rem; left: 1.25rem;
    background: var(--theme-surface); border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg); padding: 1rem 1.125rem; z-index: 50;
    min-width: 320px; box-shadow: 0 12px 40px rgba(0, 0, 0, 0.4);
  }

  .bp-title {
    font-size: 0.6875rem; font-weight: 700; text-transform: uppercase;
    letter-spacing: 0.06em; color: var(--theme-text-muted); margin-bottom: 0.75rem;
  }

  .bp-row {
    display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.625rem;
    label { font-size: 0.8125rem; color: var(--theme-text-muted); width: 70px; flex-shrink: 0; }
    select, input {
      flex: 1; font-size: 0.8125rem; background: var(--theme-input-bg);
      border: 1px solid var(--theme-border); color: var(--theme-text);
      padding: 0.5rem 0.625rem; border-radius: var(--rounded-md); outline: none;
      font-family: 'IBM Plex Mono', monospace;
      &:focus { border-color: var(--theme-primary); }
    }
  }

  .bp-presets {
    display: flex; gap: 0.375rem; margin: 0.625rem 0 0.875rem; flex-wrap: wrap;
  }

  .bp-preset {
    font-size: 0.6875rem; padding: 0.3rem 0.75rem; border-radius: var(--rounded-md);
    border: 1px solid var(--theme-border); background: var(--theme-surface);
    color: var(--theme-text-muted); cursor: pointer; font-family: inherit;
    &:hover { border-color: var(--theme-primary); color: var(--theme-primary); background: color-mix(in srgb, var(--theme-primary) 8%, transparent); }
  }

  .bp-actions { display: flex; gap: 0.5rem; }

  .bp-cancel {
    padding: 0.5rem 1rem; background: none; color: var(--theme-text-muted);
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    font-size: 0.8125rem; cursor: pointer; font-family: inherit;
    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
  }

  .bp-apply {
    flex: 1; padding: 0.5rem; background: var(--theme-primary); color: white;
    border: none; border-radius: var(--rounded-md); font-size: 0.8125rem; font-weight: 600;
    cursor: pointer; font-family: inherit;
    &:hover { opacity: 0.9; }
  }

  // ── Save bar ──
  .save-bar {
    position: sticky; bottom: 1rem; display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem;
    background: var(--theme-surface); border: 1px solid var(--badge-amber-text, #f59e0b);
    border-radius: var(--rounded-lg); box-shadow: 0 -2px 12px rgba(0, 0, 0, 0.3); justify-content: flex-end;
    margin: 0 1.25rem;
  }

  .save-btn {
    padding: 0.5rem 1.25rem; font-size: 0.8125rem; font-weight: 600; border: none;
    border-radius: var(--rounded-md); background: var(--theme-primary); color: white; cursor: pointer;
    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .reset-btn {
    padding: 0.5rem 1rem; font-size: 0.8125rem; font-weight: 500;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: none; color: var(--theme-text); cursor: pointer;
    &:hover:not(:disabled) { background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  // ── Responsive ──
  @media (max-width: 1024px) {
    .tc-side-nav { display: none; }
    .tc-top-bar { display: flex; }
  }

  @media (max-width: 640px) {
    .tc-header { padding: 0.625rem 0.875rem; }
    .tc-tabs { padding: 0 0.875rem; }
    .tc-tab { flex: 1; text-align: center; font-size: 0.75rem; padding: 0.625rem 0.5rem; }
    .tc-scroll { padding: 0.75rem; }
    .inst-head { padding: 0.625rem 0.875rem; gap: 0.5rem; }
    .inst-id { display: none; }
    .inst-detail { padding: 0.25rem 0.875rem 0.75rem; }
    .bulk-bar { padding: 0.625rem 0.875rem; }
    .bulk-sep { display: none; }
    .bulk-btn { flex: 1; text-align: center; font-size: 0.75rem; }
    .batch-panel {
      position: fixed; bottom: 0; left: 0; right: 0;
      border-radius: var(--rounded-lg) var(--rounded-lg) 0 0;
      min-width: auto;
    }
    .save-bar { margin: 0 0.75rem; }
    .tc-subtitle { display: none; }
  }
</style>
