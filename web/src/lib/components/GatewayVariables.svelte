<script lang="ts">
  import type { GatewayConfig, BrowseCache, BrowseCacheUdt, GatewayBrowseState, DeadBandConfig } from '$lib/types/gateway';
  import { apiPost } from '$lib/api/client';
  import { subscribe } from '$lib/api/subscribe';
  import { invalidateAll } from '$app/navigation';
  import { slide } from 'svelte/transition';
  import { state as saltState } from '@joyautomation/salt';
  import { ChevronRight } from '@joyautomation/salt/icons';
  import DirtyIcon from '$lib/components/DirtyIcon.svelte';

  let { gatewayConfig, browseCaches, browseStates, error }: {
    gatewayConfig: GatewayConfig | null;
    browseCaches: BrowseCache[];
    browseStates: GatewayBrowseState[];
    error: string | null;
  } = $props();

  // Build sets of what's currently published (from config)
  const publishedAtomicTags = $derived(new Set(
    (gatewayConfig?.variables ?? []).map(v => `${v.deviceId}::${v.tag}`)
  ));
  const publishedUdtInstances = $derived(new Set(
    (gatewayConfig?.udtVariables ?? []).map(v => `${v.deviceId}::${v.tag}`)
  ));
  const configTemplates = $derived(new Map(
    (gatewayConfig?.udtTemplates ?? []).map(t => [t.name, t])
  ));

  // Working state
  let checkedAtomicTags: Set<string> = $state(new Set());
  let checkedUdtInstances: Set<string> = $state(new Set());
  let excludedUdtMembers: Record<string, Set<string>> = $state({});
  let expandedTypes: Record<string, boolean> = $state({});
  let expandedFields: Record<string, boolean> = $state({});
  let gwFilter = $state('');
  let gwSaving = $state(false);
  let showPublishedOnly = $state(false);

  // RBE override state per atomic variable (key: `deviceId::tag`)
  type RbeState = { mode: 'default' | 'custom' | 'disabled'; deadband?: DeadBandConfig };
  let rbeOverrides: Map<string, RbeState> = $state(new Map());
  let initialRbeOverrides: Map<string, RbeState> = $state(new Map());
  let editingRbeTag: string | null = $state(null);
  let batchDeadbandValue = $state(0);

  // Persistent browse state — merges server-loaded state with live subscription updates
  let liveProgress: Map<string, GatewayBrowseState> = $state(new Map());

  const activeBrowseStates = $derived((): Map<string, GatewayBrowseState> => {
    const merged = new Map<string, GatewayBrowseState>();
    for (const s of browseStates ?? []) merged.set(s.deviceId, s);
    for (const [k, v] of liveProgress) merged.set(k, v);
    return merged;
  });

  function isDeviceBrowsing(deviceId: string): boolean {
    const state = activeBrowseStates().get(deviceId);
    return state?.status === 'browsing';
  }

  function getDeviceBrowseState(deviceId: string): GatewayBrowseState | undefined {
    return activeBrowseStates().get(deviceId);
  }

  // Subscribe to progress for all actively browsing devices
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

          if (isTerminal) {
            invalidateAll();
          }
        }
      );
      cleanups.push(cleanup);
    }

    return () => cleanups.forEach(fn => fn());
  });

  // Initialize working state from config
  $effect(() => {
    checkedAtomicTags = new Set(publishedAtomicTags);
    checkedUdtInstances = new Set(publishedUdtInstances);
    const excl: Record<string, Set<string>> = {};
    for (const cache of browseCaches ?? []) {
      for (const udt of cache.udts) {
        const cfgTmpl = configTemplates.get(udt.name);
        if (cfgTmpl) {
          const cfgMemberNames = new Set(cfgTmpl.members.map(m => m.name));
          const excluded = new Set<string>();
          for (const m of udt.members) {
            if (!cfgMemberNames.has(m.name)) excluded.add(m.name);
          }
          if (excluded.size > 0) excl[udt.name] = excluded;
        }
      }
    }
    excludedUdtMembers = excl;
  });

  // Initialize RBE overrides from config
  $effect(() => {
    const overrides = new Map<string, RbeState>();
    for (const v of gatewayConfig?.variables ?? []) {
      const key = `${v.deviceId}::${v.tag}`;
      if (v.disableRBE) {
        overrides.set(key, { mode: 'disabled' });
      } else if (v.deadband) {
        overrides.set(key, { mode: 'custom', deadband: { ...v.deadband } });
      }
    }
    rbeOverrides = new Map(overrides);
    initialRbeOverrides = new Map(overrides);
    editingRbeTag = null;
  });

  // Dirty tracking
  const isDirty = $derived(() => {
    if (checkedAtomicTags.size !== publishedAtomicTags.size) return true;
    for (const key of checkedAtomicTags) { if (!publishedAtomicTags.has(key)) return true; }
    for (const key of publishedAtomicTags) { if (!checkedAtomicTags.has(key)) return true; }
    if (checkedUdtInstances.size !== publishedUdtInstances.size) return true;
    for (const key of checkedUdtInstances) { if (!publishedUdtInstances.has(key)) return true; }
    for (const key of publishedUdtInstances) { if (!checkedUdtInstances.has(key)) return true; }
    // RBE changes
    if (rbeOverrides.size !== initialRbeOverrides.size) return true;
    for (const [key, state] of rbeOverrides) {
      const initial = initialRbeOverrides.get(key);
      if (!initial) return true;
      if (state.mode !== initial.mode) return true;
      if (state.mode === 'custom' && initial.mode === 'custom') {
        if (state.deadband?.value !== initial.deadband?.value ||
            state.deadband?.minTime !== initial.deadband?.minTime ||
            state.deadband?.maxTime !== initial.deadband?.maxTime) return true;
      }
    }
    for (const key of initialRbeOverrides.keys()) {
      if (!rbeOverrides.has(key)) return true;
    }
    return false;
  });

  const publishedCount = $derived((gatewayConfig?.variables?.length ?? 0) + (gatewayConfig?.udtVariables?.length ?? 0));
  const checkedCount = $derived(checkedAtomicTags.size + checkedUdtInstances.size);

  // Per-device browse views
  type DeviceBrowseView = {
    deviceId: string;
    protocol: string;
    cachedAt: string | null;
    udtGroups: { udt: BrowseCacheUdt; instances: string[] }[];
    atomicGroups: { typeName: string; items: { tag: string; name: string; datatype: string; protocolType: string }[] }[];
  };

  const deviceBrowseViews = $derived((): DeviceBrowseView[] => {
    const q = gwFilter?.toLowerCase() ?? '';
    const views: DeviceBrowseView[] = [];
    for (const cache of browseCaches ?? []) {
      const structTags = cache.structTags ?? {};
      const udtGroups: DeviceBrowseView['udtGroups'] = [];
      for (const udt of cache.udts) {
        const instances: string[] = [];
        for (const [tag, udtName] of Object.entries(structTags)) {
          if (udtName !== udt.name) continue;
          if (q && !tag.toLowerCase().includes(q) && !udt.name.toLowerCase().includes(q)) continue;
          if (showPublishedOnly && !checkedUdtInstances.has(`${cache.deviceId}::${tag}`)) continue;
          instances.push(tag);
        }
        if (instances.length > 0) udtGroups.push({ udt, instances: instances.sort() });
      }
      const atomicMap = new Map<string, { tag: string; name: string; datatype: string; protocolType: string }[]>();
      for (const item of cache.items) {
        if (structTags[item.tag]) continue;
        if (item.tag.includes('.')) continue;
        if (q && !item.name.toLowerCase().includes(q) && !item.tag.toLowerCase().includes(q)) continue;
        if (showPublishedOnly && !checkedAtomicTags.has(`${cache.deviceId}::${item.tag}`)) continue;
        const typeName = item.protocolType || item.datatype || 'Unknown';
        if (!atomicMap.has(typeName)) atomicMap.set(typeName, []);
        atomicMap.get(typeName)!.push(item);
      }
      const atomicGroups = [...atomicMap.entries()]
        .map(([typeName, items]) => ({ typeName, items: items.sort((a, b) => a.tag.localeCompare(b.tag)) }))
        .sort((a, b) => a.typeName.localeCompare(b.typeName));

      views.push({
        deviceId: cache.deviceId,
        protocol: cache.protocol,
        cachedAt: cache.cachedAt,
        udtGroups: udtGroups.sort((a, b) => a.udt.name.localeCompare(b.udt.name)),
        atomicGroups,
      });
    }
    return views;
  });

  function toggleAtomicTag(deviceId: string, tag: string) {
    const key = `${deviceId}::${tag}`;
    const next = new Set(checkedAtomicTags);
    if (next.has(key)) next.delete(key); else next.add(key);
    checkedAtomicTags = next;
  }

  function toggleUdtInstance(deviceId: string, baseTag: string) {
    const key = `${deviceId}::${baseTag}`;
    const next = new Set(checkedUdtInstances);
    if (next.has(key)) next.delete(key); else next.add(key);
    checkedUdtInstances = next;
  }

  function selectAllUdtInstances(deviceId: string, instances: string[]) {
    const next = new Set(checkedUdtInstances);
    for (const tag of instances) next.add(`${deviceId}::${tag}`);
    checkedUdtInstances = next;
  }

  function deselectAllUdtInstances(deviceId: string, instances: string[]) {
    const next = new Set(checkedUdtInstances);
    for (const tag of instances) next.delete(`${deviceId}::${tag}`);
    checkedUdtInstances = next;
  }

  function selectAllAtomicInGroup(deviceId: string, items: { tag: string }[]) {
    const next = new Set(checkedAtomicTags);
    for (const item of items) next.add(`${deviceId}::${item.tag}`);
    checkedAtomicTags = next;
  }

  function deselectAllAtomicInGroup(deviceId: string, items: { tag: string }[]) {
    const next = new Set(checkedAtomicTags);
    for (const item of items) next.delete(`${deviceId}::${item.tag}`);
    checkedAtomicTags = next;
  }

  function toggleUdtMemberExcluded(udtName: string, memberName: string) {
    const next = new Set(excludedUdtMembers[udtName] ?? new Set());
    if (next.has(memberName)) next.delete(memberName); else next.add(memberName);
    excludedUdtMembers = { ...excludedUdtMembers, [udtName]: next };
  }

  function toggleType(name: string) {
    expandedTypes[name] = !expandedTypes[name];
    expandedTypes = { ...expandedTypes };
  }

  function isTagDirty(deviceId: string, tag: string, isUdt: boolean): boolean {
    const key = `${deviceId}::${tag}`;
    const publishedSet = isUdt ? publishedUdtInstances : publishedAtomicTags;
    const checkedSet = isUdt ? checkedUdtInstances : checkedAtomicTags;
    return publishedSet.has(key) !== checkedSet.has(key);
  }

  function resetChanges() {
    checkedAtomicTags = new Set(publishedAtomicTags);
    checkedUdtInstances = new Set(publishedUdtInstances);
    const excl: Record<string, Set<string>> = {};
    for (const cache of browseCaches ?? []) {
      for (const udt of cache.udts) {
        const cfgTmpl = configTemplates.get(udt.name);
        if (cfgTmpl) {
          const cfgMemberNames = new Set(cfgTmpl.members.map(m => m.name));
          const excluded = new Set<string>();
          for (const m of udt.members) {
            if (!cfgMemberNames.has(m.name)) excluded.add(m.name);
          }
          if (excluded.size > 0) excl[udt.name] = excluded;
        }
      }
    }
    excludedUdtMembers = excl;
    rbeOverrides = new Map(initialRbeOverrides);
    editingRbeTag = null;
  }

  // RBE helpers
  function getRbeState(key: string): RbeState {
    return rbeOverrides.get(key) ?? { mode: 'default' };
  }

  function getDeviceDeadband(deviceId: string): DeadBandConfig | null {
    const device = gatewayConfig?.devices?.find(d => d.deviceId === deviceId);
    return device?.deadband ?? null;
  }

  function setRbeMode(key: string, mode: 'default' | 'custom' | 'disabled') {
    const next = new Map(rbeOverrides);
    if (mode === 'default') {
      next.delete(key);
    } else if (mode === 'disabled') {
      next.set(key, { mode: 'disabled' });
    } else {
      const existing = next.get(key);
      next.set(key, { mode: 'custom', deadband: existing?.deadband ?? { value: batchDeadbandValue || 0 } });
    }
    rbeOverrides = next;
  }

  function updateRbeDeadband(key: string, field: 'value' | 'minTime' | 'maxTime', val: number | null) {
    const next = new Map(rbeOverrides);
    const existing = next.get(key);
    const db = existing?.deadband ?? { value: 0 };
    if (field === 'value') db.value = val ?? 0;
    else if (field === 'minTime') db.minTime = val;
    else db.maxTime = val;
    next.set(key, { mode: 'custom', deadband: db });
    rbeOverrides = next;
  }

  function getVisibleNumericKeys(): string[] {
    const keys: string[] = [];
    for (const view of deviceBrowseViews()) {
      for (const group of view.atomicGroups) {
        for (const item of group.items) {
          if (item.datatype === 'number') {
            const key = `${view.deviceId}::${item.tag}`;
            if (checkedAtomicTags.has(key)) keys.push(key);
          }
        }
      }
    }
    return keys;
  }

  function applyBatchDeadband() {
    const next = new Map(rbeOverrides);
    for (const key of getVisibleNumericKeys()) {
      next.set(key, { mode: 'custom', deadband: { value: batchDeadbandValue } });
    }
    rbeOverrides = next;
  }

  function clearBatchToDefault() {
    const next = new Map(rbeOverrides);
    for (const key of getVisibleNumericKeys()) {
      next.delete(key);
    }
    rbeOverrides = next;
  }

  function disableBatchRbe() {
    const next = new Map(rbeOverrides);
    for (const key of getVisibleNumericKeys()) {
      next.set(key, { mode: 'disabled' });
    }
    rbeOverrides = next;
  }

  const mapDatatype = (d: string) => {
    const lower = d.toLowerCase();
    if (lower === 'bool' || lower === 'boolean') return 'boolean';
    if (['dint', 'int', 'sint', 'lint', 'real', 'lreal', 'udint', 'uint', 'usint', 'ulint', 'number'].includes(lower)) return 'number';
    if (lower === 'string') return 'string';
    if (lower === 'struct') return 'struct';
    return 'number';
  };

  async function applyChanges() {
    gwSaving = true;
    try {
      for (const cache of browseCaches ?? []) {
        const deviceId = cache.deviceId;
        const structTags = cache.structTags ?? {};
        const udtLookup = new Map(cache.udts.map(u => [u.name, u]));

        const atomicVariables = cache.items
          .filter(item => !structTags[item.tag] && !item.tag.includes('.') && checkedAtomicTags.has(`${deviceId}::${item.tag}`))
          .map(item => {
            const key = `${deviceId}::${item.tag}`;
            const rbe = rbeOverrides.get(key);
            return {
              id: item.name || item.tag,
              deviceId,
              tag: item.tag,
              datatype: item.datatype,
              default: item.datatype === 'number' ? 0 : item.datatype === 'boolean' ? false : '',
              ...(item.protocolType ? { cipType: item.protocolType } : {}),
              ...(rbe?.mode === 'custom' && rbe.deadband ? { deadband: { value: rbe.deadband.value, ...(rbe.deadband.minTime != null ? { minTime: rbe.deadband.minTime } : {}), ...(rbe.deadband.maxTime != null ? { maxTime: rbe.deadband.maxTime } : {}) } } : {}),
              ...(rbe?.mode === 'disabled' ? { disableRBE: true } : {}),
            };
          });

        const udtTemplates: Array<{ name: string; version: string; members: Array<{ name: string; datatype: string; templateRef?: string }> }> = [];
        const udtVariables: Array<{ id: string; deviceId: string; tag: string; templateName: string; memberTags: Record<string, string>; memberCipTypes: Record<string, string> }> = [];
        const seenTemplates = new Set<string>();

        for (const [tag, udtName] of Object.entries(structTags)) {
          if (!checkedUdtInstances.has(`${deviceId}::${tag}`)) continue;
          const udt = udtLookup.get(udtName);
          if (!udt) continue;
          const excluded = excludedUdtMembers[udtName] ?? new Set();
          const includedMembers = udt.members.filter(m => !excluded.has(m.name));
          if (includedMembers.length === 0) continue;

          if (!seenTemplates.has(udtName)) {
            seenTemplates.add(udtName);
            udtTemplates.push({
              name: udt.name,
              version: '1.0',
              members: includedMembers.map(m => ({
                name: m.name,
                datatype: mapDatatype(m.datatype),
                ...(m.udtType ? { templateRef: m.udtType } : {}),
              })),
            });
          }

          const memberTags: Record<string, string> = {};
          const memberCipTypes: Record<string, string> = {};
          for (const m of includedMembers) {
            memberTags[m.name] = `${tag}.${m.name}`;
            memberCipTypes[m.name] = m.cipType || m.datatype;
          }
          udtVariables.push({ id: tag, deviceId, tag, templateName: udtName, memberTags, memberCipTypes });
        }

        const result = await apiPost(`/gateways/gateway/devices/${deviceId}/sync`, { atomicVariables, udtTemplates, udtVariables });

        if (result.error) {
          saltState.addNotification({ message: result.error.error, type: 'error' });
          return;
        }
      }

      saltState.addNotification({ message: `Applied: ${checkedCount} variables published`, type: 'success' });
      await invalidateAll();
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Apply failed', type: 'error' });
    } finally {
      gwSaving = false;
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
        // Seed live progress so the UI shows browsing state immediately
        const b = result.data;
        const now = new Date().toISOString();
        const updated = new Map(liveProgress);
        updated.set(deviceId, {
          deviceId,
          browseId: b.browseId,
          protocol: device.protocol,
          status: 'browsing',
          phase: 'connecting',
          discoveredCount: 0,
          totalCount: 0,
          message: 'Starting browse...',
          startedAt: now,
          updatedAt: now,
        });
        liveProgress = updated;
      }
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Browse failed', type: 'error' });
    }
  }
</script>

<div class="variables-page">
  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  <div class="variables-header">
    <h1>Variables</h1>
    <span class="count-badge">{publishedCount} published</span>
    {#if isDirty()}
      <span class="dirty-badge">{checkedCount} selected (unsaved)</span>
    {/if}
  </div>

  {#if (browseCaches ?? []).length === 0 && ![...activeBrowseStates().values()].some(s => s.status === 'browsing')}
    <div class="empty-state">
      <p>No browse data cached. Go to Devices and click Browse to discover available tags.</p>
    </div>
  {/if}

  {#each [...activeBrowseStates().values()].filter(s => s.status === 'browsing' && !deviceBrowseViews().some(v => v.deviceId === s.deviceId)) as bs}
    <section class="device-section">
      <div class="device-section-header">
        <span class="protocol-badge">{bs.protocol}</span>
        <span class="device-section-name">{bs.deviceId}</span>
        <div class="browse-progress">
          <span class="browse-phase">{bs.phase}</span>
          {#if bs.totalCount > 0}
            <span class="browse-count">{bs.discoveredCount}/{bs.totalCount}</span>
            <progress value={bs.discoveredCount} max={bs.totalCount}></progress>
          {:else if bs.discoveredCount > 0}
            <span class="browse-count">{bs.discoveredCount} discovered</span>
          {/if}
          {#if bs.message}
            <span class="browse-message">{bs.message}</span>
          {/if}
        </div>
      </div>
    </section>
  {/each}

  {#each deviceBrowseViews() as view}
    <section class="device-section">
      <div class="device-section-header">
        <span class="protocol-badge">{view.protocol}</span>
        <span class="device-section-name">{view.deviceId}</span>
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
            {#if bs?.message}
              <span class="browse-message">{bs.message}</span>
            {/if}
          </div>
        {:else}
          {#if view.cachedAt}
            <span class="cached-indicator">browsed {new Date(view.cachedAt).toLocaleString()}</span>
          {/if}
          <button class="action-btn" onclick={() => refreshDevice(view.deviceId)} disabled={gwSaving}>
            Refresh
          </button>
        {/if}
      </div>

      <div class="filter-row">
        <input type="text" bind:value={gwFilter} placeholder="Filter tags/types..." class="gw-filter" />
        <label class="published-toggle">
          <input type="checkbox" bind:checked={showPublishedOnly} />
          Published only
        </label>
      </div>

      <div class="rbe-toolbar">
        <span class="rbe-toolbar-label">RBE</span>
        <label class="rbe-field">
          Deadband
          <input type="number" bind:value={batchDeadbandValue} step="0.1" min="0" class="rbe-input" />
        </label>
        <button class="inline-btn" onclick={applyBatchDeadband} title="Set custom deadband on all visible checked numeric tags">Apply to visible</button>
        <button class="inline-btn" onclick={clearBatchToDefault} title="Reset all visible checked numeric tags to device default">Clear to default</button>
        <button class="inline-btn" onclick={disableBatchRbe} title="Disable RBE on all visible checked numeric tags">Disable RBE</button>
      </div>

      {#if view.udtGroups.length > 0}
        <div class="section-label">UDT Types <span class="muted">{view.udtGroups.length} types</span></div>
        <div class="tree">
          {#each view.udtGroups as group}
            {@const checkedInGroup = group.instances.filter(tag => checkedUdtInstances.has(`${view.deviceId}::${tag}`)).length}
            <div class="tree-node">
              <div class="tree-toggle" role="button" tabindex="0" onclick={() => toggleType(`${view.deviceId}::${group.udt.name}`)} onkeydown={(e) => e.key === 'Enter' && toggleType(`${view.deviceId}::${group.udt.name}`)}>
                <span class="chevron" class:expanded={expandedTypes[`${view.deviceId}::${group.udt.name}`]}><ChevronRight size="0.875rem" /></span>
                <span class="template-icon">T</span>
                <span class="tree-label">{group.udt.name}</span>
                <span class="member-count">{checkedInGroup}/{group.instances.length} instances</span>
                <button class="inline-btn" onclick={(e: MouseEvent) => { e.stopPropagation(); selectAllUdtInstances(view.deviceId, group.instances); }}>All</button>
                <button class="inline-btn" onclick={(e: MouseEvent) => { e.stopPropagation(); deselectAllUdtInstances(view.deviceId, group.instances); }}>None</button>
              </div>
              {#if expandedTypes[`${view.deviceId}::${group.udt.name}`]}
                {@const totalFields = group.udt.members.length}
                {@const excludedCount = excludedUdtMembers[group.udt.name]?.size ?? 0}
                {@const includedCount = totalFields - excludedCount}
                <div class="tree-children" transition:slide|local={{ duration: 200 }}>
                  <div class="udt-fields-summary">
                    <button class="fields-toggle" onclick={() => { expandedFields[group.udt.name] = !expandedFields[group.udt.name]; expandedFields = { ...expandedFields }; }}>
                      <span class="chevron" class:expanded={expandedFields[group.udt.name]}><ChevronRight size="0.875rem" /></span>
                      <span class="fields-label">Fields</span>
                      <span class="fields-count" class:partial={excludedCount > 0}>{includedCount}/{totalFields}</span>
                    </button>
                  </div>
                  {#if expandedFields[group.udt.name]}
                    <div class="udt-fields-grid" transition:slide|local={{ duration: 200 }}>
                      {#each group.udt.members as member}
                        {@const isExcluded = excludedUdtMembers[group.udt.name]?.has(member.name) ?? false}
                        <label class="field-toggle" class:excluded={isExcluded}>
                          <input type="checkbox" checked={!isExcluded} onchange={() => toggleUdtMemberExcluded(group.udt.name, member.name)} />
                          <span class="field-name">{member.name}</span>
                          <span class="field-type">{member.udtType || member.cipType || member.datatype}</span>
                        </label>
                      {/each}
                    </div>
                  {/if}
                  {#each group.instances as baseTag}
                    {@const key = `${view.deviceId}::${baseTag}`}
                    {@const checked = checkedUdtInstances.has(key)}
                    {@const dirty = isTagDirty(view.deviceId, baseTag, true)}
                    <div class="tree-leaf instance-row" class:dirty>
                      <label class="instance-label">
                        <input type="checkbox" checked={checked} onchange={() => toggleUdtInstance(view.deviceId, baseTag)} />
                        <span class="leaf-name">{baseTag}</span>
                      </label>
                      {#if dirty}
                        <DirtyIcon title={checked ? 'Will be added' : 'Will be removed'} />
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}

      {#if view.atomicGroups.length > 0}
        <div class="section-label">Atomic Tags <span class="muted">{view.atomicGroups.reduce((s, g) => s + g.items.length, 0)} tags</span></div>
        <div class="tree">
          {#each view.atomicGroups as group}
            {@const checkedInGroup = group.items.filter(item => checkedAtomicTags.has(`${view.deviceId}::${item.tag}`)).length}
            <div class="tree-node">
              <div class="tree-toggle" role="button" tabindex="0" onclick={() => toggleType(`${view.deviceId}::atomic::${group.typeName}`)} onkeydown={(e) => e.key === 'Enter' && toggleType(`${view.deviceId}::atomic::${group.typeName}`)}>
                <span class="chevron" class:expanded={expandedTypes[`${view.deviceId}::atomic::${group.typeName}`]}><ChevronRight size="0.875rem" /></span>
                <span class="leaf-type">{group.typeName}</span>
                <span class="member-count">{checkedInGroup}/{group.items.length}</span>
                <button class="inline-btn" onclick={(e: MouseEvent) => { e.stopPropagation(); selectAllAtomicInGroup(view.deviceId, group.items); }}>All</button>
                <button class="inline-btn" onclick={(e: MouseEvent) => { e.stopPropagation(); deselectAllAtomicInGroup(view.deviceId, group.items); }}>None</button>
              </div>
              {#if expandedTypes[`${view.deviceId}::atomic::${group.typeName}`]}
                <div class="tree-children" transition:slide|local={{ duration: 200 }}>
                  {#each group.items as item}
                    {@const key = `${view.deviceId}::${item.tag}`}
                    {@const checked = checkedAtomicTags.has(key)}
                    {@const dirty = isTagDirty(view.deviceId, item.tag, false)}
                    {@const rbe = getRbeState(key)}
                    {@const isNumeric = item.datatype === 'number'}
                    <div class="tree-leaf instance-row" class:dirty>
                      <label class="instance-label">
                        <input type="checkbox" checked={checked} onchange={() => toggleAtomicTag(view.deviceId, item.tag)} />
                        <span class="leaf-name">{item.tag}</span>
                      </label>
                      {#if isNumeric && checked}
                        <button
                          class="rbe-badge"
                          class:rbe-custom={rbe.mode === 'custom'}
                          class:rbe-disabled={rbe.mode === 'disabled'}
                          class:rbe-dirty={rbe.mode !== (initialRbeOverrides.get(key)?.mode ?? 'default')}
                          onclick={(e: MouseEvent) => { e.stopPropagation(); editingRbeTag = editingRbeTag === key ? null : key; }}
                          title={rbe.mode === 'default' ? `Device default${getDeviceDeadband(view.deviceId) ? ': db ' + getDeviceDeadband(view.deviceId)!.value : ': no deadband'}` : rbe.mode === 'custom' ? `Custom deadband: ${rbe.deadband?.value}` : 'RBE disabled'}
                        >
                          {#if rbe.mode === 'disabled'}off{:else if rbe.mode === 'custom'}db:{rbe.deadband?.value}{:else}default{/if}
                        </button>
                      {/if}
                      {#if dirty}
                        <DirtyIcon title={checked ? 'Will be added' : 'Will be removed'} />
                      {/if}
                    </div>
                    {#if editingRbeTag === key}
                      <div class="rbe-inline-editor" transition:slide|local={{ duration: 150 }}>
                        <div class="rbe-mode-buttons">
                          <button class="rbe-mode-btn" class:active={rbe.mode === 'default'} onclick={() => setRbeMode(key, 'default')}>
                            Device Default
                            {#if getDeviceDeadband(view.deviceId)}<span class="rbe-hint">db:{getDeviceDeadband(view.deviceId)?.value}</span>{/if}
                          </button>
                          <button class="rbe-mode-btn" class:active={rbe.mode === 'custom'} onclick={() => setRbeMode(key, 'custom')}>Custom</button>
                          <button class="rbe-mode-btn" class:active={rbe.mode === 'disabled'} onclick={() => setRbeMode(key, 'disabled')}>Off</button>
                        </div>
                        {#if rbe.mode === 'custom'}
                          <div class="rbe-inputs">
                            <label class="rbe-field">
                              Deadband
                              <input type="number" value={rbe.deadband?.value ?? 0} onchange={(e) => updateRbeDeadband(key, 'value', parseFloat((e.target as HTMLInputElement).value) || 0)} step="0.1" min="0" class="rbe-input" />
                            </label>
                            <label class="rbe-field">
                              Min Time
                              <input type="number" value={rbe.deadband?.minTime ?? ''} onchange={(e) => { const v = (e.target as HTMLInputElement).value; updateRbeDeadband(key, 'minTime', v ? parseInt(v) : null); }} step="100" min="0" placeholder="ms" class="rbe-input" />
                            </label>
                            <label class="rbe-field">
                              Max Time
                              <input type="number" value={rbe.deadband?.maxTime ?? ''} onchange={(e) => { const v = (e.target as HTMLInputElement).value; updateRbeDeadband(key, 'maxTime', v ? parseInt(v) : null); }} step="100" min="0" placeholder="ms" class="rbe-input" />
                            </label>
                          </div>
                        {/if}
                      </div>
                    {/if}
                  {/each}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </section>
  {/each}

  {#if isDirty()}
    <div class="apply-bar">
      <button class="apply-btn" onclick={applyChanges} disabled={gwSaving}>
        {gwSaving ? 'Applying...' : `Apply Changes (${checkedCount} variables)`}
      </button>
      <button class="reset-btn" onclick={resetChanges} disabled={gwSaving}>Reset</button>
    </div>
  {/if}
</div>

<style lang="scss">
  .variables-page { padding: 2rem; }

  .variables-header {
    display: flex; align-items: center; gap: 0.75rem; margin-bottom: 1.5rem;
    h1 { font-size: 1.5rem; font-weight: 600; color: var(--theme-text); margin: 0; }
  }

  .count-badge {
    padding: 0.2rem 0.5rem; border-radius: var(--rounded-md); font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace; background: var(--badge-teal-bg); color: var(--badge-teal-text);
  }

  .dirty-badge {
    padding: 0.2rem 0.5rem; border-radius: var(--rounded-md); font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace; background: var(--badge-amber-bg, #fef3c7); color: var(--badge-amber-text, #92400e);
  }

  .device-section { margin-bottom: 2rem; }
  .device-section-header { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 0.75rem; }
  .device-section-name { font-family: 'IBM Plex Mono', monospace; font-weight: 600; font-size: 1rem; color: var(--theme-text); }

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
  .browse-message { color: var(--theme-text-muted); font-size: 0.6875rem; max-width: 20rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  progress {
    height: 0.375rem; width: 6rem; border-radius: var(--rounded-sm); overflow: hidden;
    appearance: none; -webkit-appearance: none;
    &::-webkit-progress-bar { background: var(--theme-border); border-radius: var(--rounded-sm); }
    &::-webkit-progress-value { background: var(--badge-teal-bg); border-radius: var(--rounded-sm); }
    &::-moz-progress-bar { background: var(--badge-teal-bg); border-radius: var(--rounded-sm); }
  }

  .section-label {
    font-size: 0.75rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em;
    color: var(--theme-text-muted); margin: 1rem 0 0.5rem;
    .muted { font-weight: 400; }
  }

  .filter-row {
    display: flex; align-items: center; gap: 0.75rem; margin-bottom: 0.5rem;
  }

  .gw-filter {
    flex: 1; padding: 0.375rem 0.75rem; font-size: 0.8125rem; font-family: 'IBM Plex Mono', monospace;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: var(--theme-input-bg); color: var(--theme-text);
  }

  .published-toggle {
    display: flex; align-items: center; gap: 0.375rem; font-size: 0.75rem;
    color: var(--theme-text-muted); cursor: pointer; white-space: nowrap; flex-shrink: 0;
    input[type="checkbox"] { cursor: pointer; }
  }

  .rbe-toolbar {
    display: flex; align-items: center; gap: 0.5rem; padding: 0.375rem 0.75rem;
    background: color-mix(in srgb, var(--theme-surface) 80%, var(--theme-border));
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    margin-bottom: 0.75rem; flex-wrap: wrap;
  }

  .rbe-toolbar-label {
    font-size: 0.6875rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em;
    color: var(--theme-text-muted); flex-shrink: 0;
  }

  .rbe-field {
    display: flex; align-items: center; gap: 0.25rem; font-size: 0.75rem;
    color: var(--theme-text-muted); flex-shrink: 0;
  }

  .rbe-input {
    width: 5rem; padding: 0.2rem 0.375rem; font-size: 0.75rem; font-family: 'IBM Plex Mono', monospace;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-sm);
    background: var(--theme-input-bg); color: var(--theme-text);
  }

  .rbe-badge {
    padding: 0.1rem 0.35rem; font-size: 0.625rem; font-family: 'IBM Plex Mono', monospace;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-sm);
    background: color-mix(in srgb, var(--theme-text) 6%, transparent);
    color: var(--theme-text-muted); cursor: pointer; flex-shrink: 0; white-space: nowrap;
    &:hover { border-color: var(--theme-primary); color: var(--theme-text); }
    &.rbe-custom { background: var(--badge-teal-bg); color: var(--badge-teal-text); border-color: var(--badge-teal-text); }
    &.rbe-disabled { background: color-mix(in srgb, var(--color-red-500, #ef4444) 15%, transparent); color: var(--color-red-500, #ef4444); border-color: var(--color-red-500, #ef4444); }
    &.rbe-dirty { box-shadow: 0 0 0 1.5px var(--badge-amber-text, #f59e0b); }
  }

  .rbe-inline-editor {
    padding: 0.5rem 0.75rem 0.5rem 2rem;
    background: color-mix(in srgb, var(--theme-primary) 5%, transparent);
    border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 30%, transparent);
  }

  .rbe-mode-buttons {
    display: flex; gap: 0.25rem; margin-bottom: 0.375rem;
  }

  .rbe-mode-btn {
    padding: 0.2rem 0.5rem; font-size: 0.6875rem; border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm); background: none; color: var(--theme-text-muted); cursor: pointer;
    display: flex; align-items: center; gap: 0.25rem;
    &:hover { color: var(--theme-text); background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
    &.active { background: var(--theme-primary); color: white; border-color: var(--theme-primary); }
  }

  .rbe-hint { font-size: 0.625rem; opacity: 0.7; font-family: 'IBM Plex Mono', monospace; }

  .rbe-inputs {
    display: flex; gap: 0.75rem; flex-wrap: wrap;
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

  .template-icon {
    display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px;
    border-radius: var(--rounded-sm); background: var(--badge-purple-bg); color: var(--badge-purple-text);
    font-size: 0.6875rem; font-weight: 700; flex-shrink: 0;
  }

  .tree-label { font-family: 'IBM Plex Mono', monospace; font-weight: 500; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .member-count { font-size: 0.75rem; color: var(--theme-text-muted); flex-shrink: 0; white-space: nowrap; }

  .inline-btn {
    padding: 0.1rem 0.35rem; font-size: 0.6875rem; border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm); background: none; color: var(--theme-text-muted); cursor: pointer;
    flex-shrink: 0;
    &:hover { color: var(--theme-text); background: color-mix(in srgb, var(--theme-text) 8%, transparent); }
  }

  .tree-children {
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    .tree-node { padding-left: 1rem; }
    .tree-leaf { padding-left: 2rem; }
  }

  .tree-leaf {
    display: flex; align-items: center; gap: 0.5rem; padding: 0.375rem 0.75rem; font-size: 0.8125rem;
    &:not(:last-child) { border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 30%, transparent); }
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

  .udt-fields-summary {
    border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 30%, transparent);
  }

  .fields-toggle {
    display: flex; align-items: center; gap: 0.375rem;
    padding: 0.375rem 0.75rem;
    background: none; border: none; color: var(--theme-text-muted);
    font-size: 0.6875rem; cursor: pointer; font-family: inherit;
    &:hover { color: var(--theme-text); }
  }

  .fields-label { font-weight: 600; }

  .fields-count {
    font-family: 'IBM Plex Mono', monospace;
    padding: 0.05rem 0.3rem;
    border-radius: var(--rounded-sm, 3px);
    background: color-mix(in srgb, var(--theme-text) 8%, transparent);
    &.partial { background: var(--badge-amber-bg, #fef3c7); color: var(--badge-amber-text, #92400e); }
  }

  .udt-fields-grid {
    display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 0.125rem 0.75rem;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 30%, transparent);
  }

  .field-toggle {
    display: flex; align-items: center; gap: 0.375rem; font-size: 0.75rem;
    cursor: pointer; padding: 0.25rem 0;
    &.excluded { opacity: 0.4; }
    input[type="checkbox"] {
      appearance: none;
      width: 13px; height: 13px;
      border: 1.5px solid var(--theme-border);
      border-radius: 2px;
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
          left: 3px; top: 0px;
          width: 4px; height: 8px;
          border: solid white;
          border-width: 0 1.5px 1.5px 0;
          transform: rotate(45deg);
        }
      }
      &:hover { border-color: var(--theme-primary); }
    }
  }
  .field-name { font-family: 'IBM Plex Mono', monospace; color: var(--theme-text); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .field-type { color: var(--theme-text-muted); font-size: 0.75rem; margin-left: auto; }

  .leaf-name { font-family: 'IBM Plex Mono', monospace; color: var(--theme-text); }
  .leaf-type { font-size: 0.6875rem; color: var(--badge-muted-text); padding: 0.1rem 0.35rem; border-radius: var(--rounded-sm); background: var(--badge-muted-bg); flex-shrink: 0; }

  .error-box {
    padding: 1rem; border-radius: var(--rounded-lg); background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444); margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .empty-state { padding: 3rem 2rem; text-align: center; p { color: var(--theme-text-muted); font-size: 0.875rem; } }

  .action-btn {
    padding: 0.25rem 0.5rem; font-size: 0.75rem; font-weight: 500;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: var(--theme-surface); color: var(--theme-text); cursor: pointer;
    &:hover:not(:disabled) { background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface)); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }

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
  }

  .reset-btn {
    padding: 0.5rem 1rem; font-size: 0.8125rem; font-weight: 500;
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    background: none; color: var(--theme-text); cursor: pointer;
    &:hover:not(:disabled) { background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }
</style>
