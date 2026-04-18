<script lang="ts">
  import type { GatewayConfig, BrowseCache, GatewayBrowseState } from '$lib/types/gateway';
  import type { PlcConfig, PlcVariableConfig as PlcVarCfg } from '$lib/types/plc';
  import { apiPost, apiDelete, apiPut } from '$lib/api/client';
  import { subscribe } from '$lib/api/subscribe';
  import { invalidateAll } from '$app/navigation';
  import { slide } from 'svelte/transition';
  import { state as saltState } from '@joyautomation/salt';
  import { ChevronRight, Plus, Trash } from '@joyautomation/salt/icons';

  let { plcConfig, gatewayConfig, browseCaches, browseStates, error }: {
    plcConfig: PlcConfig | null;
    gatewayConfig: GatewayConfig | null;
    browseCaches: BrowseCache[];
    browseStates: GatewayBrowseState[];
    error: string | null;
  } = $props();

  // Build set of already-configured PLC input variables (from browse)
  const configuredTags = $derived((): Set<string> => {
    const s = new Set<string>();
    if (!plcConfig?.variables) return s;
    for (const v of Object.values(plcConfig.variables)) {
      if (v.source) {
        s.add(`${v.source.deviceId}::${v.source.tag}`);
      }
    }
    return s;
  });

  // Working selection state
  let checkedTags: Set<string> = $state(new Set());
  let expandedTypes: Record<string, boolean> = $state({});
  let filter = $state('');
  let saving = $state(false);
  let showAddManual = $state(false);

  // Manual variable form
  let manualName = $state('');
  let manualDatatype = $state('number');
  let manualDirection = $state('internal');
  let manualDefault = $state('0');
  let manualSaving = $state(false);

  // Browse state tracking
  let liveProgress: Map<string, GatewayBrowseState> = $state(new Map());

  const activeBrowseStates = $derived((): Map<string, GatewayBrowseState> => {
    const merged = new Map<string, GatewayBrowseState>();
    for (const s of browseStates ?? []) merged.set(s.deviceId, s);
    for (const [k, v] of liveProgress) merged.set(k, v);
    return merged;
  });

  function isDeviceBrowsing(deviceId: string): boolean {
    return activeBrowseStates().get(deviceId)?.status === 'browsing';
  }

  function getDeviceBrowseState(deviceId: string): GatewayBrowseState | undefined {
    return activeBrowseStates().get(deviceId);
  }

  $effect(() => {
    const cleanups: (() => void)[] = [];
    for (const [deviceId, state] of activeBrowseStates()) {
      if (state.status !== 'browsing') continue;
      const cleanup = subscribe<{
        browseId: string; deviceId: string; phase: string;
        discoveredCount: number; totalCount: number; message: string; timestamp: string;
      }>(
        `/gateways/gateway/browse/${state.browseId}/progress`,
        (p) => {
          const isTerminal = p.phase === 'completed' || p.phase === 'failed';
          const updated = new Map(liveProgress);
          updated.set(deviceId, {
            ...state,
            phase: p.phase,
            discoveredCount: p.discoveredCount,
            totalCount: p.totalCount,
            message: p.message,
            updatedAt: p.timestamp,
            status: isTerminal ? p.phase as 'completed' | 'failed' : 'browsing',
          });
          liveProgress = updated;
          if (isTerminal) invalidateAll();
        }
      );
      cleanups.push(cleanup);
    }
    return () => cleanups.forEach(fn => fn());
  });

  // Initialize checked state from config
  $effect(() => {
    checkedTags = new Set(configuredTags());
  });

  // Dirty tracking
  const isDirty = $derived(() => {
    const cfg = configuredTags();
    if (checkedTags.size !== cfg.size) return true;
    for (const key of checkedTags) { if (!cfg.has(key)) return true; }
    for (const key of cfg) { if (!checkedTags.has(key)) return true; }
    return false;
  });

  // Per-device browse views (atomic tags only — no UDTs for PLC import)
  type DeviceBrowseView = {
    deviceId: string;
    protocol: string;
    cachedAt: string | null;
    groups: { typeName: string; items: { tag: string; name: string; datatype: string; protocolType: string }[] }[];
  };

  const deviceBrowseViews = $derived((): DeviceBrowseView[] => {
    const q = filter?.toLowerCase() ?? '';
    const views: DeviceBrowseView[] = [];
    for (const cache of browseCaches ?? []) {
      const structTags = cache.structTags ?? {};
      const typeMap = new Map<string, { tag: string; name: string; datatype: string; protocolType: string }[]>();
      for (const item of cache.items) {
        if (structTags[item.tag]) continue;
        if (item.tag.includes('.')) continue;
        if (q && !item.name.toLowerCase().includes(q) && !item.tag.toLowerCase().includes(q)) continue;
        const typeName = item.protocolType || item.datatype || 'Unknown';
        if (!typeMap.has(typeName)) typeMap.set(typeName, []);
        typeMap.get(typeName)!.push(item);
      }
      const groups = [...typeMap.entries()]
        .map(([typeName, items]) => ({ typeName, items: items.sort((a, b) => a.tag.localeCompare(b.tag)) }))
        .sort((a, b) => a.typeName.localeCompare(b.typeName));

      if (groups.length > 0 || isDeviceBrowsing(cache.deviceId)) {
        views.push({ deviceId: cache.deviceId, protocol: cache.protocol, cachedAt: cache.cachedAt, groups });
      }
    }
    return views;
  });

  const checkedCount = $derived(checkedTags.size);
  const manualVarCount = $derived(
    Object.values(plcConfig?.variables ?? {}).filter(v => !v.source).length
  );

  function toggleTag(deviceId: string, tag: string) {
    const key = `${deviceId}::${tag}`;
    const next = new Set(checkedTags);
    if (next.has(key)) next.delete(key); else next.add(key);
    checkedTags = next;
  }

  function selectAllInGroup(deviceId: string, items: { tag: string }[]) {
    const next = new Set(checkedTags);
    for (const item of items) next.add(`${deviceId}::${item.tag}`);
    checkedTags = next;
  }

  function deselectAllInGroup(deviceId: string, items: { tag: string }[]) {
    const next = new Set(checkedTags);
    for (const item of items) next.delete(`${deviceId}::${item.tag}`);
    checkedTags = next;
  }

  function toggleType(name: string) {
    expandedTypes[name] = !expandedTypes[name];
    expandedTypes = { ...expandedTypes };
  }

  function isTagDirty(deviceId: string, tag: string): boolean {
    const key = `${deviceId}::${tag}`;
    return configuredTags().has(key) !== checkedTags.has(key);
  }

  function resetChanges() {
    checkedTags = new Set(configuredTags());
  }

  const mapDatatype = (d: string) => {
    const lower = d.toLowerCase();
    if (lower === 'bool' || lower === 'boolean') return 'boolean';
    if (['dint', 'int', 'sint', 'lint', 'real', 'lreal', 'udint', 'uint', 'usint', 'ulint', 'number'].includes(lower)) return 'number';
    if (lower === 'string') return 'string';
    return 'number';
  };

  const defaultForType = (dt: string) => {
    if (dt === 'boolean') return false;
    if (dt === 'string') return '';
    return 0;
  };

  async function applyChanges() {
    saving = true;
    try {
      // Determine additions and removals
      const cfg = configuredTags();
      const toAdd: string[] = [];
      const toRemove: string[] = [];

      for (const key of checkedTags) {
        if (!cfg.has(key)) toAdd.push(key);
      }
      for (const key of cfg) {
        if (!checkedTags.has(key)) toRemove.push(key);
      }

      // Remove unchecked variables
      for (const key of toRemove) {
        const varEntry = Object.entries(plcConfig?.variables ?? {}).find(([, v]) =>
          v.source && `${v.source.deviceId}::${v.source.tag}` === key
        );
        if (varEntry) {
          await apiDelete(`/plcs/plc/variables/${encodeURIComponent(varEntry[0])}`);
        }
      }

      // Import newly checked tags
      if (toAdd.length > 0) {
        const imports = toAdd.map(key => {
          const [deviceId, tag] = key.split('::');
          const cache = browseCaches.find(c => c.deviceId === deviceId);
          const item = cache?.items.find(i => i.tag === tag);
          const dt = mapDatatype(item?.datatype ?? 'number');
          return {
            variableId: item?.name || tag,
            deviceId,
            tag,
            datatype: dt,
            protocol: cache?.protocol ?? 'ethernetip',
            cipType: item?.protocolType || '',
            direction: 'input',
            default: defaultForType(dt),
          };
        });

        const result = await apiPost(`/plcs/plc/import-browse`, {
          gatewayId: gatewayConfig?.gatewayId ?? 'gateway',
          imports,
        });
        if (result.error) {
          saltState.addNotification({ message: result.error.error, type: 'error' });
          return;
        }
      }

      saltState.addNotification({ message: `Applied: ${checkedCount} scanner variables configured`, type: 'success' });
      await invalidateAll();
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Apply failed', type: 'error' });
    } finally {
      saving = false;
    }
  }

  async function addManualVariable() {
    if (!manualName.trim()) return;
    manualSaving = true;
    try {
      let def: unknown = 0;
      if (manualDatatype === 'boolean') def = manualDefault === 'true';
      else if (manualDatatype === 'string') def = manualDefault;
      else def = parseFloat(manualDefault) || 0;

      const result = await apiPut(`/plcs/plc/variables/${encodeURIComponent(manualName.trim())}`, {
        id: manualName.trim(),
        datatype: manualDatatype,
        direction: manualDirection,
        default: def,
      });
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
        return;
      }
      saltState.addNotification({ message: `Variable "${manualName.trim()}" created`, type: 'success' });
      manualName = '';
      manualDefault = manualDatatype === 'boolean' ? 'false' : manualDatatype === 'string' ? '' : '0';
      showAddManual = false;
      await invalidateAll();
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Failed to create variable', type: 'error' });
    } finally {
      manualSaving = false;
    }
  }

  async function deleteManualVariable(id: string) {
    try {
      const result = await apiDelete(`/plcs/plc/variables/${encodeURIComponent(id)}`);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
        return;
      }
      await invalidateAll();
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Delete failed', type: 'error' });
    }
  }

  async function refreshDevice(deviceId: string) {
    const device = gatewayConfig?.devices?.find(d => d.deviceId === deviceId);
    if (!device) return;
    try {
      const input: Record<string, unknown> = { deviceId, protocol: device.protocol };
      if (device.host) input.host = device.host;
      if (device.port) input.port = device.port;
      if (device.endpointUrl) input.endpointUrl = device.endpointUrl;
      if (device.version) input.version = device.version;
      if (device.community) input.community = device.community;

      const result = await apiPost<{ browseId: string; deviceId: string }>('/gateways/gateway/browse', input);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else if (result.data) {
        const b = result.data;
        const now = new Date().toISOString();
        const updated = new Map(liveProgress);
        updated.set(deviceId, {
          deviceId, browseId: b.browseId, protocol: device.protocol,
          status: 'browsing', phase: 'connecting', discoveredCount: 0, totalCount: 0,
          message: 'Starting browse...', startedAt: now, updatedAt: now,
        });
        liveProgress = updated;
      }
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Browse failed', type: 'error' });
    }
  }
</script>

<div class="config-page">
  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  <div class="config-header">
    <h2>Variable Configuration</h2>
    <span class="count-badge">{Object.keys(plcConfig?.variables ?? {}).length} configured</span>
    {#if isDirty()}
      <span class="dirty-badge">{checkedCount} selected (unsaved)</span>
    {/if}
  </div>

  <!-- Manual / Internal Variables -->
  <section class="section">
    <div class="section-header">
      <h3>Internal / Output Variables</h3>
      <span class="muted">{manualVarCount} variables</span>
      <button class="action-btn" onclick={() => { showAddManual = !showAddManual; }}>
        <Plus size="0.875rem" /> Add
      </button>
    </div>

    {#if showAddManual}
      <div class="manual-form" transition:slide={{ duration: 200 }}>
        <div class="form-row">
          <label class="form-field">
            <span>Name</span>
            <input type="text" bind:value={manualName} placeholder="myVariable" class="form-input" />
          </label>
          <label class="form-field">
            <span>Type</span>
            <select bind:value={manualDatatype} class="form-input" onchange={() => { manualDefault = manualDatatype === 'boolean' ? 'false' : manualDatatype === 'string' ? '' : '0'; }}>
              <option value="number">number</option>
              <option value="boolean">boolean</option>
              <option value="string">string</option>
            </select>
          </label>
          <label class="form-field">
            <span>Direction</span>
            <select bind:value={manualDirection} class="form-input">
              <option value="internal">internal</option>
              <option value="output">output</option>
              <option value="input">input</option>
            </select>
          </label>
          <label class="form-field">
            <span>Default</span>
            {#if manualDatatype === 'boolean'}
              <select bind:value={manualDefault} class="form-input">
                <option value="false">false</option>
                <option value="true">true</option>
              </select>
            {:else}
              <input type={manualDatatype === 'number' ? 'number' : 'text'} bind:value={manualDefault} class="form-input" />
            {/if}
          </label>
        </div>
        <div class="form-actions">
          <button class="apply-btn small" onclick={addManualVariable} disabled={manualSaving || !manualName.trim()}>
            {manualSaving ? 'Creating...' : 'Create Variable'}
          </button>
          <button class="reset-btn small" onclick={() => { showAddManual = false; }}>Cancel</button>
        </div>
      </div>
    {/if}

    {#if manualVarCount > 0}
      <div class="tree">
        {#each Object.entries(plcConfig?.variables ?? {}).filter(([, v]) => !v.source) as [id, v]}
          <div class="tree-leaf manual-row">
            <span class="leaf-name">{id}</span>
            <span class="leaf-type">{v.datatype}</span>
            <span class="direction-badge" class:dir-input={v.direction === 'input'} class:dir-output={v.direction === 'output'}>{v.direction}</span>
            <span class="leaf-value">{JSON.stringify(v.default)}</span>
            <button class="delete-btn" onclick={() => deleteManualVariable(id)} title="Delete variable">
              <Trash size="0.875rem" />
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  <!-- Scanner Variables (from browse) -->
  <section class="section">
    <div class="section-header">
      <h3>Scanner Variables (from Browse)</h3>
    </div>

    {#if (browseCaches ?? []).length === 0 && ![...activeBrowseStates().values()].some(s => s.status === 'browsing')}
      <div class="empty-state">
        <p>No browse data cached. Configure gateway devices and browse to discover tags.</p>
      </div>
    {/if}

    {#if deviceBrowseViews().length > 0 || [...activeBrowseStates().values()].some(s => s.status === 'browsing')}
      <div class="filter-row">
        <input type="text" bind:value={filter} placeholder="Filter tags..." class="gw-filter" />
      </div>
    {/if}

    {#each [...activeBrowseStates().values()].filter(s => s.status === 'browsing' && !deviceBrowseViews().some(v => v.deviceId === s.deviceId)) as bs}
      <div class="device-section">
        <div class="device-section-header">
          <span class="protocol-badge">{bs.protocol}</span>
          <span class="device-name">{bs.deviceId}</span>
          <div class="browse-progress">
            <span class="browse-phase">{bs.phase}</span>
            {#if bs.totalCount > 0}
              <span class="browse-count">{bs.discoveredCount}/{bs.totalCount}</span>
              <progress value={bs.discoveredCount} max={bs.totalCount}></progress>
            {:else if bs.discoveredCount > 0}
              <span class="browse-count">{bs.discoveredCount} discovered</span>
            {/if}
          </div>
        </div>
      </div>
    {/each}

    {#each deviceBrowseViews() as view}
      <div class="device-section">
        <div class="device-section-header">
          <span class="protocol-badge">{view.protocol}</span>
          <span class="device-name">{view.deviceId}</span>
          {#if isDeviceBrowsing(view.deviceId)}
            {@const bs = getDeviceBrowseState(view.deviceId)}
            <div class="browse-progress">
              <span class="browse-phase">{bs?.phase ?? 'browsing'}</span>
              {#if bs && bs.totalCount > 0}
                <span class="browse-count">{bs.discoveredCount}/{bs.totalCount}</span>
                <progress value={bs.discoveredCount} max={bs.totalCount}></progress>
              {:else if bs && bs.discoveredCount > 0}
                <span class="browse-count">{bs.discoveredCount} discovered</span>
              {/if}
            </div>
          {:else}
            {#if view.cachedAt}
              <span class="cached-indicator">browsed {new Date(view.cachedAt).toLocaleString()}</span>
            {/if}
            <button class="action-btn" onclick={() => refreshDevice(view.deviceId)} disabled={saving}>
              Refresh
            </button>
          {/if}
        </div>

        <div class="tree">
          {#each view.groups as group}
            {@const checkedInGroup = group.items.filter(item => checkedTags.has(`${view.deviceId}::${item.tag}`)).length}
            <div class="tree-node">
              <div class="tree-toggle" role="button" tabindex="0" onclick={() => toggleType(`${view.deviceId}::${group.typeName}`)} onkeydown={(e) => e.key === 'Enter' && toggleType(`${view.deviceId}::${group.typeName}`)}>
                <span class="chevron" class:expanded={expandedTypes[`${view.deviceId}::${group.typeName}`]}><ChevronRight size="0.875rem" /></span>
                <span class="leaf-type">{group.typeName}</span>
                <span class="member-count">{checkedInGroup}/{group.items.length}</span>
                <button class="inline-btn" onclick={(e: MouseEvent) => { e.stopPropagation(); selectAllInGroup(view.deviceId, group.items); }}>All</button>
                <button class="inline-btn" onclick={(e: MouseEvent) => { e.stopPropagation(); deselectAllInGroup(view.deviceId, group.items); }}>None</button>
              </div>
              {#if expandedTypes[`${view.deviceId}::${group.typeName}`]}
                <div class="tree-children" transition:slide|local={{ duration: 200 }}>
                  {#each group.items as item}
                    {@const key = `${view.deviceId}::${item.tag}`}
                    {@const checked = checkedTags.has(key)}
                    {@const dirty = isTagDirty(view.deviceId, item.tag)}
                    <div class="tree-leaf instance-row" class:dirty>
                      <label class="instance-label">
                        <input type="checkbox" checked={checked} onchange={() => toggleTag(view.deviceId, item.tag)} />
                        <span class="leaf-name">{item.tag}</span>
                      </label>
                      <span class="leaf-type">{mapDatatype(item.datatype)}</span>
                      {#if dirty}
                        <span class="dirty-icon" title={checked ? 'Will be added' : 'Will be removed'}>*</span>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      </div>
    {/each}
  </section>

  {#if isDirty()}
    <div class="apply-bar">
      <button class="apply-btn" onclick={applyChanges} disabled={saving}>
        {saving ? 'Applying...' : `Apply Changes (${checkedCount} scanner variables)`}
      </button>
      <button class="reset-btn" onclick={resetChanges} disabled={saving}>Reset</button>
    </div>
  {/if}
</div>

<style lang="scss">
  .config-page { padding: 0; }

  .config-header {
    display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1.5rem;
    h2 { font-size: 1.25rem; font-weight: 600; color: var(--theme-text); margin: 0; }
  }

  .count-badge {
    padding: 0.2rem 0.5rem; border-radius: var(--rounded-md); font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace; background: var(--badge-teal-bg); color: var(--badge-teal-text);
  }

  .dirty-badge {
    padding: 0.2rem 0.5rem; border-radius: var(--rounded-md); font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace; background: var(--badge-amber-bg, #fef3c7); color: var(--badge-amber-text, #92400e);
  }

  .section { margin-bottom: 2rem; }

  .section-header {
    display: flex; align-items: center; gap: 0.75rem; margin-bottom: 0.75rem;
    h3 {
      font-size: 0.8125rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em;
      color: var(--theme-text-muted); margin: 0;
    }
    .muted { font-size: 0.75rem; color: var(--theme-text-muted); }
  }

  .manual-form {
    padding: 1rem; margin-bottom: 0.75rem; background: var(--theme-surface);
    border: 1px solid var(--theme-border); border-radius: var(--rounded-lg);
  }

  .form-row {
    display: flex; flex-wrap: wrap; gap: 0.75rem; margin-bottom: 0.75rem;
  }

  .form-field {
    display: flex; flex-direction: column; gap: 0.25rem; flex: 1; min-width: 120px;
    span { font-size: 0.6875rem; font-weight: 600; text-transform: uppercase; color: var(--theme-text-muted); }
  }

  .form-input {
    padding: 0.375rem 0.5rem; font-size: 0.8125rem; font-family: 'IBM Plex Mono', monospace;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: var(--theme-input-bg); color: var(--theme-text);
  }

  .form-actions { display: flex; gap: 0.5rem; }

  .device-section { margin-bottom: 1.5rem; }
  .device-section-header { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 0.75rem; }
  .device-name { font-family: 'IBM Plex Mono', monospace; font-weight: 600; font-size: 1rem; color: var(--theme-text); }

  .protocol-badge {
    font-size: 0.6875rem; font-weight: 700; text-transform: uppercase; padding: 0.15rem 0.4rem;
    border-radius: var(--rounded-sm); background: var(--badge-teal-bg); color: var(--badge-teal-text);
  }

  .cached-indicator { font-size: 0.6875rem; color: var(--theme-text-muted); margin-left: auto; }

  .browse-progress {
    display: flex; align-items: center; gap: 0.5rem; margin-left: auto;
    font-size: 0.75rem; font-family: 'IBM Plex Mono', monospace; color: var(--theme-text-muted);
  }
  .browse-phase {
    padding: 0.15rem 0.4rem; border-radius: var(--rounded-sm);
    background: var(--badge-amber-bg, #fef3c7); color: var(--badge-amber-text, #92400e);
    font-size: 0.6875rem; font-weight: 600; text-transform: uppercase;
  }
  .browse-count { white-space: nowrap; }
  progress {
    height: 0.375rem; width: 6rem; border-radius: var(--rounded-sm); overflow: hidden;
    appearance: none; -webkit-appearance: none;
    &::-webkit-progress-bar { background: var(--theme-border); border-radius: var(--rounded-sm); }
    &::-webkit-progress-value { background: var(--badge-teal-bg); border-radius: var(--rounded-sm); }
    &::-moz-progress-bar { background: var(--badge-teal-bg); border-radius: var(--rounded-sm); }
  }

  .filter-row {
    display: flex; align-items: center; gap: 0.75rem; margin-bottom: 0.75rem;
  }

  .gw-filter {
    flex: 1; padding: 0.375rem 0.75rem; font-size: 0.8125rem; font-family: 'IBM Plex Mono', monospace;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: var(--theme-input-bg); color: var(--theme-text);
  }

  .tree { background: var(--theme-surface); border: 1px solid var(--theme-border); border-radius: var(--rounded-lg); overflow: hidden; }
  .tree-node { &:not(:last-child) { border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent); } }

  .tree-toggle {
    display: flex; align-items: center; gap: 0.5rem; padding: 0.5rem 0.75rem;
    background: none; border: none; color: var(--theme-text); font-size: 0.8125rem;
    cursor: pointer; text-align: left; font-family: inherit;
    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
  }

  .chevron { display: inline-flex; flex-shrink: 0; color: var(--theme-text-muted); transition: transform 0.15s ease; &.expanded { transform: rotate(90deg); } }

  .tree-children {
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    .tree-leaf { padding-left: 2rem; }
  }

  .tree-leaf {
    display: flex; align-items: center; gap: 0.5rem; padding: 0.375rem 0.75rem; font-size: 0.8125rem;
    &:not(:last-child) { border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 30%, transparent); }
  }

  .manual-row {
    padding: 0.5rem 0.75rem;
  }

  .instance-row { &.dirty { background: color-mix(in srgb, var(--badge-amber-bg, #fef3c7) 15%, transparent); } }
  .instance-label {
    display: flex; align-items: center; gap: 0.5rem; cursor: pointer; flex: 1;
    input[type="checkbox"] {
      appearance: none;
      width: 16px; height: 16px;
      border: 1.5px solid var(--theme-border);
      border-radius: var(--rounded-sm, 3px);
      background: var(--theme-input-bg);
      cursor: pointer;
      flex-shrink: 0;
      position: relative;
      transition: background 0.15s ease, border-color 0.15s ease;
      &:checked {
        background: var(--theme-primary);
        border-color: var(--theme-primary);
        &::after {
          content: '';
          position: absolute;
          left: 4px; top: 1px;
          width: 5px; height: 9px;
          border: solid white;
          border-width: 0 2px 2px 0;
          transform: rotate(45deg);
        }
      }
      &:hover { border-color: var(--theme-primary); }
    }
  }

  .dirty-icon { color: var(--badge-amber-text, #f59e0b); font-weight: 700; flex-shrink: 0; }

  .leaf-name { font-family: 'IBM Plex Mono', monospace; color: var(--theme-text); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .leaf-value { margin-left: auto; font-family: 'IBM Plex Mono', monospace; color: var(--theme-text-muted); font-size: 0.75rem; flex-shrink: 0; }
  .leaf-type { font-size: 0.6875rem; color: var(--badge-muted-text); padding: 0.1rem 0.35rem; border-radius: var(--rounded-sm); background: var(--badge-muted-bg); flex-shrink: 0; }
  .member-count { font-size: 0.75rem; color: var(--theme-text-muted); flex-shrink: 0; white-space: nowrap; }

  .direction-badge {
    font-size: 0.6875rem; padding: 0.1rem 0.35rem; border-radius: var(--rounded-sm); flex-shrink: 0;
    background: var(--badge-muted-bg); color: var(--badge-muted-text);
    &.dir-input { background: var(--badge-teal-bg); color: var(--badge-teal-text); }
    &.dir-output { background: var(--badge-purple-bg); color: var(--badge-purple-text); }
  }

  .inline-btn {
    padding: 0.1rem 0.35rem; font-size: 0.6875rem; border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm); background: none; color: var(--theme-text-muted); cursor: pointer;
    flex-shrink: 0;
    &:hover { color: var(--theme-text); background: color-mix(in srgb, var(--theme-text) 8%, transparent); }
  }

  .action-btn {
    display: inline-flex; align-items: center; gap: 0.25rem;
    padding: 0.25rem 0.5rem; font-size: 0.75rem; font-weight: 500;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: var(--theme-surface); color: var(--theme-text); cursor: pointer;
    margin-left: auto;
    &:hover:not(:disabled) { background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface)); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

  .delete-btn {
    display: inline-flex; align-items: center; padding: 0.2rem;
    border: none; border-radius: var(--rounded-sm); background: none;
    color: var(--theme-text-muted); cursor: pointer; flex-shrink: 0;
    &:hover { color: var(--color-red-500, #ef4444); background: color-mix(in srgb, var(--color-red-500, #ef4444) 10%, transparent); }
  }

  .error-box {
    padding: 1rem; border-radius: var(--rounded-lg); background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444); margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .empty-state { padding: 2rem; text-align: center; p { color: var(--theme-text-muted); font-size: 0.875rem; } }

  .apply-bar {
    position: sticky; bottom: 1rem; display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem;
    background: var(--theme-surface); border: 1px solid var(--badge-amber-text, #f59e0b);
    border-radius: var(--rounded-lg); box-shadow: 0 -2px 12px rgba(0, 0, 0, 0.3); justify-content: flex-end;
  }

  .apply-btn {
    padding: 0.5rem 1.25rem; font-size: 0.8125rem; font-weight: 600; border: none;
    border-radius: var(--rounded-md); background: var(--theme-primary); color: white; cursor: pointer;
    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
    &.small { padding: 0.375rem 0.75rem; font-size: 0.75rem; }
  }

  .reset-btn {
    padding: 0.5rem 1rem; font-size: 0.8125rem; font-weight: 500;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: none; color: var(--theme-text); cursor: pointer;
    &:hover:not(:disabled) { background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
    &.small { padding: 0.375rem 0.75rem; font-size: 0.75rem; }
  }
</style>
