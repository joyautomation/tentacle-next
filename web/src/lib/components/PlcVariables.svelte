<script lang="ts">
  import type { Variable } from '$lib/types/gateway';
  import { subscribe } from '$lib/api/subscribe';
  import { onMount } from 'svelte';
  import { slide } from 'svelte/transition';
  import Sunburst from '$lib/components/Sunburst.svelte';
  import TidyTree from '$lib/components/TidyTree.svelte';
  import DiagramSelector from '$lib/components/DiagramSelector.svelte';
  import type { VizMode } from '$lib/components/DiagramSelector.svelte';
  import { ChevronRight } from '@joyautomation/salt/icons';
  import { toEpochMs, getFreshnessColor, getGlowStyle, formatAge, formatAgeShort } from '$lib/utils/freshness';

  let { variables, error }: {
    variables: Variable[];
    error: string | null;
  } = $props();

  let expandedTypes: Record<string, boolean> = $state({});
  let expandedInstances: Record<string, boolean> = $state({});
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
    for (const v of variables) { m.set(v.variableId, v); }
    variableMap = m;
  });

  onMount(() => {
    const tickInterval = setInterval(() => { now = Date.now(); }, 1000);
    const unsub = subscribe<Variable[]>(
      `/variables/stream/batch`,
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
      }
    );
    return () => { clearInterval(tickInterval); if (flushTimer) clearTimeout(flushTimer); unsub(); };
  });

  type StructInstance = { name: string; members: { name: string; variable: Variable }[] };
  type StructType = { typeName: string; instances: StructInstance[]; memberNames: string[] };

  const organized = $derived(() => {
    void updateVersion;
    const instanceMap = new Map<string, { name: string; udtType?: string; members: Map<string, Variable> }>();
    const scalars: Variable[] = [];
    for (const v of variableMap.values()) {
      const dotIdx = v.variableId.indexOf('.');
      if (dotIdx === -1) {
        if (v.datatype === 'udt') {
          if (!instanceMap.has(v.variableId)) instanceMap.set(v.variableId, { name: v.variableId, udtType: v.udtType ?? undefined, members: new Map() });
          else instanceMap.get(v.variableId)!.udtType = v.udtType ?? undefined;
        } else { scalars.push(v); }
      } else {
        const baseName = v.variableId.substring(0, dotIdx);
        const memberName = v.variableId.substring(dotIdx + 1);
        if (!instanceMap.has(baseName)) instanceMap.set(baseName, { name: baseName, udtType: v.udtType ?? undefined, members: new Map() });
        instanceMap.get(baseName)!.members.set(memberName, v);
      }
    }
    const typeMap = new Map<string, StructInstance[]>();
    for (const [key, inst] of instanceMap) {
      if (inst.members.size === 0) {
        const parent = variableMap.get(key);
        if (parent && parent.datatype === 'udt' && parent.value && typeof parent.value === 'object') {
          for (const [memberName, memberValue] of Object.entries(parent.value as Record<string, unknown>)) {
            inst.members.set(memberName, { variableId: `${key}.${memberName}`, value: memberValue, datatype: typeof memberValue === 'boolean' ? 'boolean' : typeof memberValue === 'string' ? 'string' : 'number', udtType: parent.udtType, quality: parent.quality, moduleId: parent.moduleId, deviceId: parent.deviceId, lastUpdated: parent.lastUpdated });
          }
        }
        if (inst.members.size === 0) continue;
      }
      const typeName = inst.udtType ?? 'Unknown Type';
      if (!typeMap.has(typeName)) typeMap.set(typeName, []);
      const memberNames = Array.from(inst.members.keys()).sort();
      typeMap.get(typeName)!.push({ name: inst.name, members: memberNames.map(m => ({ name: m, variable: inst.members.get(m)! })) });
    }
    const structTypes: StructType[] = [...typeMap.entries()].map(([typeName, instances]) => ({ typeName, instances: instances.sort((a, b) => a.name.localeCompare(b.name)), memberNames: instances[0]?.members.map(m => m.name) ?? [] })).sort((a, b) => a.typeName.localeCompare(b.typeName));
    return { structTypes, scalars: scalars.sort((a, b) => a.variableId.localeCompare(b.variableId)) };
  });

  type SunburstNode = { name: string; children?: SunburstNode[]; value?: number; displayValue?: string };
  const sunburstData = $derived((): SunburstNode => {
    const org = organized();
    const children: SunburstNode[] = [];
    if (org.structTypes.length > 0) children.push({ name: 'UDT Variables', children: org.structTypes.map(st => ({ name: st.typeName, children: st.instances.map(inst => ({ name: inst.name, children: inst.members.map(m => ({ name: m.name, value: 1, displayValue: formatValue(m.variable.value) })) })) })) });
    if (org.scalars.length > 0) children.push({ name: 'Scalar Variables', children: org.scalars.map(v => ({ name: v.variableId, value: 1, displayValue: formatValue(v.value) })) });
    return { name: 'Variables', children };
  });

  function toggleType(name: string) { expandedTypes[name] = !expandedTypes[name]; }
  function toggleInstance(name: string) { expandedInstances[name] = !expandedInstances[name]; }

  function formatValue(value: unknown): string {
    if (value === null || value === undefined) return '—';
    if (typeof value === 'number') { if (Number.isInteger(value)) return value.toString(); return value.toFixed(3); }
    if (typeof value === 'boolean') return value ? 'true' : 'false';
    return String(value);
  }

</script>

<div class="variables-page">
  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  <div class="variables-header">
    <h1>Variables</h1>
    <span class="count-badge">{variableMap.size} variables</span>
    <DiagramSelector bind:mode={vizMode} />
  </div>

  {#if vizMode === 'tree'}
  <div class="tree-content">
  {#if organized().structTypes.length > 0}
    <section class="section">
      <h2>UDT Variables</h2>
      <div class="tree">
        {#each organized().structTypes as structType}
          <div class="tree-node">
            <button class="tree-toggle" onclick={() => toggleType(structType.typeName)}>
              <span class="chevron" class:expanded={expandedTypes[structType.typeName]}><ChevronRight size="0.875rem" /></span>
              <span class="template-icon">T</span>
              <span class="tree-label">{structType.typeName}</span>
              <span class="member-count">{structType.instances.length} instances</span>
            </button>
            {#if expandedTypes[structType.typeName]}
              <div class="tree-children" transition:slide|local={{ duration: 200 }}>
                {#each structType.instances as instance}
                  <div class="tree-node">
                    <button class="tree-toggle" onclick={() => toggleInstance(instance.name)}>
                      <span class="chevron" class:expanded={expandedInstances[instance.name]}><ChevronRight size="0.875rem" /></span>
                      <span class="tree-label">{instance.name}</span>
                      <span class="member-count">{instance.members.length} members</span>
                    </button>
                    {#if expandedInstances[instance.name]}
                      <div class="tree-children" transition:slide|local={{ duration: 200 }}>
                        {#each instance.members as member}
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
      </div>
    </section>
  {/if}

  {#if organized().scalars.length > 0}
    <section class="section">
      <h2>Scalar Variables</h2>
      <div class="tree">
        {#each organized().scalars as variable}
          {@const ts = toEpochMs(variable.lastUpdated)}
          <div class="tree-leaf">
            <span
              class="freshness-dot"
              title={formatAge(ts, now)}
              style="--dot-color: {getFreshnessColor(ts, now, variable.quality)}; --dot-glow: {getGlowStyle(ts, now)};"
            ></span>
            <span class="staleness-label" style="color: {getFreshnessColor(ts, now, variable.quality)}">{formatAgeShort(ts, now)}</span>
            <span class="leaf-name">{variable.variableId}</span>
            <span class="leaf-value">{formatValue(variable.value)}</span>
            <span class="leaf-type">{variable.datatype}</span>
          </div>
        {/each}
      </div>
    </section>
  {/if}

  {#if variableMap.size === 0 && !error}
    <div class="empty-state">
      <p>No variables being polled. Start a PLC project to see variables here.</p>
    </div>
  {/if}
  </div>
  {:else if vizMode === 'sunburst'}
    {#if sunburstData().children && sunburstData().children!.length > 0}
      <div class="diagram-content"><Sunburst data={sunburstData()} /></div>
    {:else if !error}
      <div class="empty-state"><p>No variables being polled.</p></div>
    {/if}
  {:else if vizMode === 'tidy'}
    {#if sunburstData().children && sunburstData().children!.length > 0}
      <TidyTree data={sunburstData()} />
    {:else if !error}
      <div class="empty-state"><p>No variables being polled.</p></div>
    {/if}
  {/if}
</div>

<style lang="scss">
  .variables-page { padding: 2rem; }
  .tree-content { max-width: 900px; }
  .diagram-content { display: flex; justify-content: center; }

  .variables-header {
    display: flex; align-items: center; flex-wrap: wrap; gap: 0.75rem; margin-bottom: 1.5rem;
    h1 { font-size: 1.5rem; font-weight: 600; color: var(--theme-text); margin: 0; }
  }

  .count-badge {
    padding: 0.2rem 0.5rem; border-radius: var(--rounded-md); font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace; background: var(--badge-teal-bg); color: var(--badge-teal-text);
  }

  .section { margin-bottom: 1.5rem;
    h2 { font-size: 0.8125rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; color: var(--theme-text-muted); margin: 0 0 0.75rem; }
  }

  .tree { background: var(--theme-surface); border: 1px solid var(--theme-border); border-radius: var(--rounded-lg); overflow: hidden; }
  .tree-node { &:not(:last-child) { border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent); } }

  .tree-toggle {
    display: flex; align-items: center; gap: 0.5rem; width: 100%; padding: 0.5rem 0.75rem;
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

  .tree-label { font-family: 'IBM Plex Mono', monospace; font-weight: 500; }
  .member-count { font-size: 0.75rem; color: var(--theme-text-muted); margin-left: auto; }

  .tree-children {
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    .tree-node { padding-left: 1rem; }
    .tree-leaf { padding-left: 2rem; }
  }

  .tree-leaf {
    display: flex; align-items: center; gap: 0.5rem; padding: 0.375rem 0.75rem; font-size: 0.8125rem;
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
    border: 1px solid var(--color-red-500, #ef4444); margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .empty-state { padding: 3rem 2rem; text-align: center; p { color: var(--theme-text-muted); font-size: 0.875rem; } }
</style>
