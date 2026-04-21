<script lang="ts">
  import type { PageData } from "./$types";
  import { subscribe } from "$lib/api/subscribe";
  import { onMount } from "svelte";
  import { toEpochMs, getFreshnessColor, getGlowStyle, formatAge, formatAgeShort } from "$lib/utils/freshness";

  let { data }: { data: PageData } = $props();

  let expandedDevices: Record<string, boolean> = $state({});
  let now = $state(Date.now());

  type Variable = {
    variableId: string;
    value: unknown;
    datatype: string;
    quality: string;
    moduleId: string;
    deviceId: string | null;
    lastUpdated: string | number;
    timestamp?: number;
    source: string | null;
  };

  // Mutable variable map for live updates
  let variableMap: Map<string, Variable> = $state(new Map());

  // Debounced version counter — drives $derived recomputation
  let updateVersion = $state(0);
  let flushTimer: ReturnType<typeof setTimeout> | null = null;

  function scheduleFlush() {
    if (!flushTimer) {
      flushTimer = setTimeout(() => {
        flushTimer = null;
        updateVersion++;
      }, 500);
    }
  }

  // Initialize from server data
  $effect(() => {
    const m = new Map<string, Variable>();
    for (const v of data.variables) {
      m.set(v.variableId, v);
    }
    variableMap = m;
  });

  // Subscribe to batched variable updates via SSE
  onMount(() => {
    const tickInterval = setInterval(() => { now = Date.now(); }, 1000);
    const unsub = subscribe<Variable[]>(
      '/variables/stream/watch?moduleId=snmp&flushMs=500',
      (batch) => {
        if (batch) {
          for (const v of batch) {
            if (variableMap.has(v.variableId)) {
              const existing = variableMap.get(v.variableId)!;
              variableMap.set(v.variableId, {
                ...existing,
                value: v.value,
                quality: v.quality ?? existing.quality,
                lastUpdated: v.timestamp ?? v.lastUpdated ?? existing.lastUpdated,
              });
            }
          }
          scheduleFlush();
        }
      },
    );
    return () => {
      clearInterval(tickInterval);
      if (flushTimer) clearTimeout(flushTimer);
      unsub();
    };
  });

  type OidEntry = {
    oid: string;
    variable: Variable;
  };

  type Device = {
    deviceId: string;
    host: string;
    port: number;
    version: string;
    oids: OidEntry[];
    totalOids: number;
  };

  const devices = $derived(() => {
    void updateVersion;
    const vars = [...variableMap.values()];
    const deviceMap = new Map<string, Variable[]>();
    for (const v of vars) {
      const did = v.deviceId ?? "unknown";
      if (!deviceMap.has(did)) deviceMap.set(did, []);
      deviceMap.get(did)!.push(v);
    }

    const result: Device[] = [];
    for (const [deviceId, dvars] of deviceMap) {
      const info = data.deviceInfo[deviceId];
      const oids: OidEntry[] = dvars
        .map(v => ({ oid: v.variableId, variable: v }))
        .sort((a, b) => a.oid.localeCompare(b.oid));

      result.push({
        deviceId,
        host: info?.host ?? "unknown",
        port: info?.port ?? 161,
        version: info?.version ?? "v2c",
        oids,
        totalOids: oids.length,
      });
    }

    result.sort((a, b) => a.deviceId.localeCompare(b.deviceId));
    return result;
  });

  function toggleDevice(id: string) {
    expandedDevices[id] = !expandedDevices[id];
  }

  function formatValue(value: unknown): string {
    if (value === null || value === undefined) return "\u2014";
    if (typeof value === "number") {
      if (Number.isInteger(value)) return value.toString();
      return value.toFixed(3);
    }
    if (typeof value === "boolean") return value ? "true" : "false";
    const str = String(value);
    if (str.length > 80) return str.slice(0, 77) + "\u2026";
    return str;
  }

</script>

<div class="oids-page">
  {#if data.error}
    <div class="error-box">
      <p>{data.error}</p>
    </div>
  {/if}

  <div class="oids-header">
    <h1>OIDs</h1>
    <span class="count-badge">{devices().length} devices</span>
    <span class="count-badge">{variableMap.size} OIDs</span>
  </div>

  <div class="tree-content">
    {#if devices().length > 0}
      <div class="tree">
        {#each devices() as device}
          <div class="tree-node">
            <button
              class="tree-toggle"
              onclick={() => toggleDevice(device.deviceId)}
            >
              <svg
                class="chevron"
                class:expanded={expandedDevices[device.deviceId]}
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
              >
                <path d="M9 18l6-6-6-6" />
              </svg>
              <span class="device-icon">DEV</span>
              <span class="tree-label">{device.deviceId}</span>
              <span class="device-host">{device.host}:{device.port} ({device.version})</span>
              <span class="member-count">{device.totalOids} OIDs</span>
            </button>
            {#if expandedDevices[device.deviceId]}
              <div class="tree-children">
                {#each device.oids as entry}
                  {@const ts = toEpochMs(entry.variable.lastUpdated)}
                  <div class="tree-leaf">
                    <span
                      class="freshness-dot"
                      title={formatAge(ts, now)}
                      style="--dot-color: {getFreshnessColor(ts, now, entry.variable.quality)}; --dot-glow: {getGlowStyle(ts, now)};"
                    ></span>
                    <span class="staleness-label" style="color: {getFreshnessColor(ts, now, entry.variable.quality)}">{formatAgeShort(ts, now)}</span>
                    <span class="leaf-name-group">
                      <span class="leaf-name">{entry.oid}</span>
                      {#if entry.variable.source}
                        <span class="leaf-oid">{entry.variable.source}</span>
                      {/if}
                    </span>
                    <span class="leaf-value-group">
                      <span class="leaf-value">{formatValue(entry.variable.value)}</span>
                      <span class="leaf-type">{entry.variable.datatype}</span>
                    </span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {:else if !data.error}
      <div class="empty-state">
        <p>
          No SNMP devices connected. The SNMP scanner will show OIDs here
          when a PLC project subscribes to OIDs.
        </p>
      </div>
    {/if}
  </div>
</div>

<style lang="scss">
  .oids-page {
    padding: 2rem;

    @media (max-width: 640px) {
      padding: 1rem;
    }
  }

  .tree-content {
    max-width: 900px;
  }

  .oids-header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 1.5rem;

    h1 {
      font-size: 1.5rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }
  }

  .count-badge {
    padding: 0.2rem 0.5rem;
    border-radius: var(--rounded-md);
    font-size: 0.75rem;
    font-family: "IBM Plex Mono", monospace;
    background: var(--badge-teal-bg);
    color: var(--badge-teal-text);
  }

  .tree {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
  }

  .tree-node {
    &:not(:last-child) {
      border-bottom: 1px solid
        color-mix(in srgb, var(--theme-border) 50%, transparent);
    }
  }

  .tree-toggle {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.625rem 1rem;
    background: none;
    border: none;
    color: var(--theme-text);
    font-size: 0.8125rem;
    cursor: pointer;
    text-align: left;
    font-family: inherit;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 5%, transparent);
    }

    @media (max-width: 640px) {
      flex-wrap: wrap;
      gap: 0.25rem 0.5rem;
    }
  }

  .chevron {
    flex-shrink: 0;
    color: var(--theme-text-muted);
    transition: transform 0.15s ease;

    &.expanded {
      transform: rotate(90deg);
    }
  }

  .device-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 20px;
    border-radius: var(--rounded-sm);
    background: var(--badge-amber-bg);
    color: var(--badge-amber-text);
    font-size: 0.75rem;
    font-weight: 700;
    flex-shrink: 0;
  }

  .tree-label {
    font-family: "IBM Plex Mono", monospace;
    font-weight: 500;
  }

  .device-host {
    font-size: 0.6875rem;
    font-family: "IBM Plex Mono", monospace;
    color: var(--badge-muted-text);
    padding: 0.1rem 0.35rem;
    border-radius: var(--rounded-sm);
    background: var(--badge-muted-bg);
  }

  .member-count {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    margin-left: auto;
  }

  .tree-children {
    border-top: 1px solid
      color-mix(in srgb, var(--theme-border) 50%, transparent);

    .tree-leaf {
      padding-left: 2.5rem;

      @media (max-width: 640px) {
        padding-left: 1rem;
      }
    }
  }

  .tree-leaf {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;

    &:not(:last-child) {
      border-bottom: 1px solid
        color-mix(in srgb, var(--theme-border) 30%, transparent);
    }

    @media (max-width: 640px) {
      flex-wrap: wrap;
      align-items: flex-start;
    }
  }

  .freshness-dot {
    position: relative;
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    cursor: help;

    &::before {
      content: '';
      position: absolute;
      top: 50%;
      left: 50%;
      transform: translate(-50%, -50%);
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background-color: var(--dot-color, rgb(156, 163, 175));
      box-shadow: var(--dot-glow, none);
      transition: background-color 1s ease, box-shadow 1s ease;
    }

    @media (max-width: 640px) {
      margin-top: 0.3rem;
    }
  }

  .staleness-label {
    font-size: 0.6875rem;
    font-family: "IBM Plex Mono", monospace;
    flex-shrink: 0;
    min-width: 1.5rem;
    text-align: left;
    transition: color 1s ease;
  }

  .leaf-name-group {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    min-width: 0;

    @media (max-width: 640px) {
      flex: 1 1 100%;
      min-width: 0;
    }
  }

  .leaf-name {
    font-family: "IBM Plex Mono", monospace;
    color: var(--theme-text);
    word-break: break-all;
  }

  .leaf-oid {
    font-family: "IBM Plex Mono", monospace;
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    opacity: 0.6;
    word-break: break-all;
  }

  .leaf-value-group {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-left: auto;

    @media (max-width: 640px) {
      margin-left: 1rem;
      flex: 1 1 100%;
    }
  }

  .leaf-value {
    font-family: "IBM Plex Mono", monospace;
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;

    @media (max-width: 640px) {
      max-width: none;
      white-space: normal;
      word-break: break-all;
      flex: 1;
    }
  }

  .leaf-type {
    font-size: 0.6875rem;
    color: var(--badge-muted-text);
    padding: 0.1rem 0.35rem;
    border-radius: var(--rounded-sm);
    background: var(--badge-muted-bg);
    flex-shrink: 0;
  }

  .error-box {
    padding: 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444);
    margin-bottom: 1.5rem;

    p {
      margin: 0;
      font-size: 0.875rem;
      color: var(--color-red-500, #ef4444);
    }
  }

  .empty-state {
    padding: 3rem 2rem;
    text-align: center;

    p {
      color: var(--theme-text-muted);
      font-size: 0.875rem;
    }
  }
</style>
