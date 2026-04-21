<script lang="ts">
  import type { Variable, ActiveDevice } from '$lib/types/gateway';
  import { subscribe } from '$lib/api/subscribe';
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import { slide } from 'svelte/transition';
  import Sunburst from '$lib/components/Sunburst.svelte';
  import DiagramSelector from '$lib/components/DiagramSelector.svelte';
  import type { VizMode } from '$lib/components/DiagramSelector.svelte';
  import { ChevronRight } from '@joyautomation/salt/icons';
  import { toEpochMs, getFreshnessColor, getGlowStyle, formatAge, formatAgeShort } from '$lib/utils/freshness';

  let { variables: initialVariables, deviceInfo, error }: {
    variables: Variable[];
    deviceInfo: Record<string, ActiveDevice>;
    error: string | null;
  } = $props();

  let expandedDevices: Record<string, boolean> = $state({});
  let expandedTags: Record<string, boolean> = $state({});
  let vizMode: VizMode = $state('tree');

  let variableMap: Map<string, Variable> = $state(new Map());
  let updateVersion = $state(0);
  let flushTimer: ReturnType<typeof setTimeout> | null = null;
  let now = $state(Date.now());

  function scheduleFlush() {
    if (!flushTimer) {
      flushTimer = setTimeout(() => { flushTimer = null; updateVersion++; }, 500);
    }
  }

  $effect(() => {
    const m = new Map<string, Variable>();
    for (const v of initialVariables) { m.set(v.variableId, v); }
    variableMap = m;
  });

  onMount(() => {
    const tickInterval = setInterval(() => { now = Date.now(); }, 1000);
    const serviceType = $page.params.serviceType;
    const unsub = subscribe<Variable[]>(
      `/variables/stream/batch?moduleId=${encodeURIComponent(serviceType)}`,
      (batch) => {
        if (batch) {
          for (const v of batch) {
            if (variableMap.has(v.variableId)) {
              const existing = variableMap.get(v.variableId)!;
              variableMap.set(v.variableId, { ...existing, value: v.value, quality: v.quality ?? existing.quality, lastUpdated: v.timestamp ?? v.lastUpdated ?? existing.lastUpdated });
            }
          }
          scheduleFlush();
        }
      },
    );
    return () => { clearInterval(tickInterval); if (flushTimer) clearTimeout(flushTimer); unsub(); };
  });

  type TagNode = { name: string; udtType?: string; variable?: Variable; members: { name: string; variable: Variable }[] };
  type TypeGroup = { typeName: string; instances: TagNode[] };
  type Device = { deviceId: string; typeGroups: TypeGroup[]; scalars: TagNode[]; totalVars: number };

  const devices = $derived(() => {
    void updateVersion;
    const vars = [...variableMap.values()];
    const deviceMap = new Map<string, Variable[]>();
    for (const v of vars) {
      const did = v.deviceId ?? 'unknown';
      if (!deviceMap.has(did)) deviceMap.set(did, []);
      deviceMap.get(did)!.push(v);
    }
    const result: Device[] = [];
    for (const [did, dvars] of deviceMap) {
      const tagMap = new Map<string, TagNode>();
      const scalars: Variable[] = [];
      for (const v of dvars) {
        const dotIdx = v.variableId.indexOf('.');
        if (dotIdx === -1) {
          if (v.datatype !== 'udt') { scalars.push(v); }
          else {
            if (!tagMap.has(v.variableId)) tagMap.set(v.variableId, { name: v.variableId, udtType: v.udtType ?? undefined, members: [] });
            else tagMap.get(v.variableId)!.udtType = v.udtType ?? undefined;
          }
        } else {
          const baseName = v.variableId.substring(0, dotIdx);
          const memberName = v.variableId.substring(dotIdx + 1);
          if (!tagMap.has(baseName)) tagMap.set(baseName, { name: baseName, udtType: v.udtType ?? undefined, members: [] });
          tagMap.get(baseName)!.members.push({ name: memberName, variable: v });
        }
      }
      for (const tag of tagMap.values()) { tag.members.sort((a, b) => a.name.localeCompare(b.name)); }
      const typeGroupMap = new Map<string, TagNode[]>();
      for (const tag of tagMap.values()) {
        if (tag.members.length === 0) continue;
        const typeName = tag.udtType ?? 'Unknown Type';
        if (!typeGroupMap.has(typeName)) typeGroupMap.set(typeName, []);
        typeGroupMap.get(typeName)!.push(tag);
      }
      const typeGroups: TypeGroup[] = [...typeGroupMap.entries()]
        .map(([typeName, instances]) => ({ typeName, instances: instances.sort((a, b) => a.name.localeCompare(b.name)) }))
        .sort((a, b) => a.typeName.localeCompare(b.typeName));
      const scalarTags = scalars.map(s => ({ name: s.variableId, variable: s, members: [] }) as TagNode).sort((a, b) => a.name.localeCompare(b.name));
      result.push({ deviceId: did, typeGroups, scalars: scalarTags, totalVars: dvars.length });
    }
    result.sort((a, b) => a.deviceId.localeCompare(b.deviceId));
    return result;
  });

  type SunburstNode = { name: string; children?: SunburstNode[]; value?: number; displayValue?: string };
  const sunburstData = $derived((): SunburstNode => {
    const devs = devices();
    return {
      name: 'Devices',
      children: devs.map(device => ({
        name: device.deviceId,
        children: [
          ...device.typeGroups.map(group => ({
            name: group.typeName,
            children: group.instances.map(tag => ({
              name: tag.name,
              children: tag.members.length > 0
                ? tag.members.map(m => ({ name: m.name, value: 1, displayValue: formatValue(m.variable.value) }))
                : [{ name: '(empty)', value: 1 }],
            })),
          })),
          ...(device.scalars.filter(t => t.variable).length > 0
            ? [{ name: 'Atomic', children: device.scalars.filter(t => t.variable).map(tag => ({ name: tag.name, value: 1, displayValue: formatValue(tag.variable!.value) })) }]
            : []),
        ],
      })),
    };
  });

  function toggleDevice(id: string) { expandedDevices[id] = !expandedDevices[id]; }
  function toggleTag(key: string) { expandedTags[key] = !expandedTags[key]; }

  function formatValue(value: unknown): string {
    if (value === null || value === undefined) return '—';
    if (typeof value === 'number') { if (Number.isInteger(value)) return value.toString(); return value.toFixed(3); }
    if (typeof value === 'boolean') return value ? 'true' : 'false';
    return String(value);
  }

</script>

<div class="devices-page">
  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  <div class="devices-header">
    <h1>Devices</h1>
    <span class="count-badge">{devices().length} devices</span>
    <span class="count-badge">{variableMap.size} tags</span>
    <DiagramSelector bind:mode={vizMode} />
  </div>

  {#if vizMode === 'tree'}
  <div class="tree-content">
    {#if devices().length > 0}
      <div class="tree">
        {#each devices() as device}
          <div class="tree-node">
            <button class="tree-toggle" onclick={() => toggleDevice(device.deviceId)}>
              <span class="chevron" class:expanded={expandedDevices[device.deviceId]}><ChevronRight size="0.875rem" /></span>
              <span class="device-icon">DEV</span>
              <span class="tree-label">{device.deviceId}</span>
              {#if deviceInfo[device.deviceId]}
                <span class="device-host">{deviceInfo[device.deviceId].host}:{deviceInfo[device.deviceId].port}</span>
              {/if}
              <span class="member-count">{device.totalVars} tags</span>
            </button>
            {#if expandedDevices[device.deviceId]}
              <div class="tree-children" transition:slide|local={{ duration: 200 }}>
                {#each device.typeGroups as group}
                  <div class="tree-node">
                    <button class="tree-toggle" onclick={() => toggleTag(device.deviceId + ':type:' + group.typeName)}>
                      <span class="chevron" class:expanded={expandedTags[device.deviceId + ':type:' + group.typeName]}><ChevronRight size="0.875rem" /></span>
                      <span class="template-icon">T</span>
                      <span class="tree-label">{group.typeName}</span>
                      <span class="member-count">{group.instances.length} instances</span>
                    </button>
                    {#if expandedTags[device.deviceId + ':type:' + group.typeName]}
                      <div class="tree-children">
                        {#each group.instances as tag}
                          <div class="tree-node">
                            <button class="tree-toggle" onclick={() => toggleTag(device.deviceId + '.' + tag.name)}>
                              <span class="chevron" class:expanded={expandedTags[device.deviceId + '.' + tag.name]}><ChevronRight size="0.875rem" /></span>
                              <span class="instance-dot"></span>
                              <span class="tree-label">{tag.name}</span>
                              <span class="member-count">{tag.members.length} members</span>
                            </button>
                            {#if expandedTags[device.deviceId + '.' + tag.name]}
                              <div class="tree-children">
                                {#each tag.members as member}
                                  {@const ts = toEpochMs(member.variable.lastUpdated)}
                                  <div class="tree-leaf">
                                    <span
                                      class="freshness-dot"
                                      title={formatAge(ts, now)}
                                      style="--dot-color: {getFreshnessColor(ts, now, member.variable.quality)}; --dot-glow: {getGlowStyle(ts, now)};"
                                    ></span>
                                    <span class="staleness-label" style="color: {getFreshnessColor(ts, now, member.variable.quality)}">{formatAgeShort(ts, now)}</span>
                                    <span class="leaf-name">{member.name}</span>
                                    <span class="leaf-value">{formatValue(member.variable.value)}</span>
                                    <span class="leaf-type">{member.variable.datatype}</span>
                                  </div>
                                {/each}
                              </div>
                            {/if}
                          </div>
                        {/each}
                      </div>
                    {/if}
                  </div>
                {/each}
                {#each device.scalars as tag}
                  {#if tag.variable}
                    {@const ts = toEpochMs(tag.variable.lastUpdated)}
                    <div class="tree-leaf">
                      <span
                        class="freshness-dot"
                        title={formatAge(ts, now)}
                        style="--dot-color: {getFreshnessColor(ts, now, tag.variable.quality)}; --dot-glow: {getGlowStyle(ts, now)};"
                      ></span>
                      <span class="staleness-label" style="color: {getFreshnessColor(ts, now, tag.variable.quality)}">{formatAgeShort(ts, now)}</span>
                      <span class="leaf-name">{tag.name}</span>
                      <span class="leaf-value">{formatValue(tag.variable.value)}</span>
                      <span class="leaf-type">{tag.variable.datatype}</span>
                    </div>
                  {/if}
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {:else if !error}
      <div class="empty-state">
        <p>No devices connected. The EtherNet/IP scanner will show devices here when a PLC project subscribes to tags.</p>
      </div>
    {/if}
  </div>
  {:else if (sunburstData().children && sunburstData().children?.length) || 0 > 0}
    <div class="diagram-content">
      <Sunburst data={sunburstData()} />
    </div>
  {:else if !error}
    <div class="empty-state">
      <p>No devices connected. The EtherNet/IP scanner will show devices here when a PLC project subscribes to tags.</p>
    </div>
  {/if}
</div>

<style lang="scss">
  .devices-page { padding: 2rem; }
  .tree-content { max-width: 900px; }
  .diagram-content { display: flex; justify-content: center; }

  .devices-header {
    display: flex; align-items: center; flex-wrap: wrap; gap: 0.75rem; margin-bottom: 1.5rem;
    h1 { font-size: 1.5rem; font-weight: 600; color: var(--theme-text); margin: 0; }
  }

  .count-badge {
    padding: 0.2rem 0.5rem; border-radius: var(--rounded-md); font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace; background: var(--badge-teal-bg); color: var(--badge-teal-text);
  }

  .tree { background: var(--theme-surface); border: 1px solid var(--theme-border); border-radius: var(--rounded-lg); overflow: hidden; }
  .tree-node { &:not(:last-child) { border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent); } }

  .tree-toggle {
    display: flex; align-items: center; gap: 0.5rem; width: 100%; padding: 0.625rem 1rem;
    background: none; border: none; color: var(--theme-text); font-size: 0.8125rem;
    cursor: pointer; text-align: left; font-family: inherit;
    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
  }

  .chevron { display: inline-flex; flex-shrink: 0; color: var(--theme-text-muted); transition: transform 0.15s ease; &.expanded { transform: rotate(90deg); } }

  .device-icon {
    display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 20px;
    border-radius: var(--rounded-sm); background: var(--badge-amber-bg); color: var(--badge-amber-text);
    font-size: 0.75rem; font-weight: 700; flex-shrink: 0;
  }

  .template-icon {
    display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px;
    border-radius: var(--rounded-sm); background: var(--badge-purple-bg); color: var(--badge-purple-text);
    font-size: 0.6875rem; font-weight: 700; flex-shrink: 0;
  }

  .instance-dot {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
    background: var(--purple-400, #c084fc);
  }

  .tree-label { font-family: 'IBM Plex Mono', monospace; font-weight: 500; }

  .device-host {
    font-size: 0.6875rem; font-family: 'IBM Plex Mono', monospace; color: var(--badge-muted-text);
    padding: 0.1rem 0.35rem; border-radius: var(--rounded-sm); background: var(--badge-muted-bg);
  }

  .member-count { font-size: 0.75rem; color: var(--theme-text-muted); margin-left: auto; }

  .tree-children {
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    .tree-node { padding-left: 1rem; }
    .tree-leaf { padding-left: 2.5rem; }
  }

  .tree-leaf {
    display: flex; align-items: center; gap: 0.5rem; padding: 0.5rem 1rem; font-size: 0.8125rem;
    &:not(:last-child) { border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 30%, transparent); }
  }

  .freshness-dot {
    position: relative; width: 16px; height: 16px; flex-shrink: 0; cursor: help;
    &::before {
      content: ''; position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%);
      width: 8px; height: 8px; border-radius: 50%;
      background-color: var(--dot-color, rgb(156, 163, 175));
      box-shadow: var(--dot-glow, none);
      transition: background-color 1s ease, box-shadow 1s ease;
    }
  }
  .staleness-label {
    font-size: 0.6875rem; font-family: 'IBM Plex Mono', monospace;
    flex-shrink: 0; min-width: 1.5rem; text-align: left; transition: color 1s ease;
  }
  .leaf-name { font-family: 'IBM Plex Mono', monospace; color: var(--theme-text); }
  .leaf-value { margin-left: auto; font-family: 'IBM Plex Mono', monospace; color: var(--theme-text-muted); font-size: 0.75rem; max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .leaf-type { font-size: 0.6875rem; color: var(--badge-muted-text); padding: 0.1rem 0.35rem; border-radius: var(--rounded-sm); background: var(--badge-muted-bg); flex-shrink: 0; }

  .error-box {
    padding: 1rem; border-radius: var(--rounded-lg); background: var(--theme-surface);
    border: 1px solid var(--red-500, #ef4444); margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--red-500, #ef4444); }
  }

  .empty-state { padding: 3rem 2rem; text-align: center; p { color: var(--theme-text-muted); font-size: 0.875rem; } }
</style>
