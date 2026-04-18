<script lang="ts" module>
  export type HistoryMetricRef = {
    groupId: string;
    nodeId: string;
    deviceId: string;
    metricId: string;
  };
</script>

<script lang="ts">
  import { slide } from 'svelte/transition';

  type Props = {
    metrics: HistoryMetricRef[];
    onAdd: (ref: HistoryMetricRef) => void;
  };

  let { metrics, onAdd }: Props = $props();

  let filter = $state('');

  type DeviceNode = { deviceId: string; metricIds: string[] };
  type NodeNode = { nodeId: string; devices: Map<string, DeviceNode> };
  type GroupNode = { groupId: string; nodes: Map<string, NodeNode> };

  const tree = $derived.by(() => {
    const root = new Map<string, GroupNode>();
    const q = filter.trim().toLowerCase();
    for (const m of metrics) {
      if (q) {
        const hay = `${m.groupId}/${m.nodeId}/${m.deviceId}/${m.metricId}`.toLowerCase();
        if (!hay.includes(q)) continue;
      }
      let g = root.get(m.groupId);
      if (!g) {
        g = { groupId: m.groupId, nodes: new Map() };
        root.set(m.groupId, g);
      }
      let n = g.nodes.get(m.nodeId);
      if (!n) {
        n = { nodeId: m.nodeId, devices: new Map() };
        g.nodes.set(m.nodeId, n);
      }
      let d = n.devices.get(m.deviceId);
      if (!d) {
        d = { deviceId: m.deviceId, metricIds: [] };
        n.devices.set(m.deviceId, d);
      }
      d.metricIds.push(m.metricId);
    }
    return root;
  });

  let expanded = $state<Record<string, boolean>>({});

  function toggle(key: string) {
    expanded[key] = !expanded[key];
  }
</script>

<div class="browser">
  <input
    class="filter"
    type="text"
    placeholder="Filter metrics..."
    bind:value={filter}
  />

  {#if tree.size === 0}
    <p class="empty">No metrics recorded yet.</p>
  {:else}
    <ul class="tree">
      {#each [...tree.values()] as group (group.groupId)}
        {@const gKey = `g:${group.groupId}`}
        <li>
          <button class="node group" onclick={() => toggle(gKey)}>
            <span class="caret" class:open={expanded[gKey] ?? true}>▸</span>
            <span class="label">{group.groupId}</span>
          </button>
          {#if expanded[gKey] ?? true}
            <ul transition:slide={{ duration: 150 }}>
              {#each [...group.nodes.values()] as node (node.nodeId)}
                {@const nKey = `n:${group.groupId}:${node.nodeId}`}
                <li>
                  <button class="node" onclick={() => toggle(nKey)}>
                    <span class="caret" class:open={expanded[nKey] ?? true}>▸</span>
                    <span class="label">{node.nodeId}</span>
                  </button>
                  {#if expanded[nKey] ?? true}
                    <ul transition:slide={{ duration: 150 }}>
                      {#each [...node.devices.values()] as device (device.deviceId)}
                        {@const dKey = `d:${group.groupId}:${node.nodeId}:${device.deviceId}`}
                        <li>
                          <button class="node" onclick={() => toggle(dKey)}>
                            <span class="caret" class:open={expanded[dKey] ?? false}>▸</span>
                            <span class="label">{device.deviceId || '(no device)'}</span>
                          </button>
                          {#if expanded[dKey]}
                            <ul transition:slide={{ duration: 150 }}>
                              {#each device.metricIds as metricId (metricId)}
                                <li>
                                  <button
                                    class="metric"
                                    onclick={() =>
                                      onAdd({
                                        groupId: group.groupId,
                                        nodeId: node.nodeId,
                                        deviceId: device.deviceId,
                                        metricId,
                                      })}
                                  >
                                    + {metricId}
                                  </button>
                                </li>
                              {/each}
                            </ul>
                          {/if}
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style lang="scss">
  .browser {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    font-size: 0.8125rem;
  }

  .filter {
    padding: 0.5rem 0.625rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-bg);
    color: var(--theme-text);
    font-size: 0.8125rem;

    &:focus {
      outline: none;
      border-color: var(--theme-accent);
    }
  }

  .empty {
    color: var(--theme-text-muted);
    font-size: 0.8125rem;
    text-align: center;
    padding: 1rem 0;
  }

  .tree,
  .tree ul {
    list-style: none;
    padding: 0;
    margin: 0;
  }

  .tree ul {
    padding-left: 1rem;
  }

  .node,
  .metric {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    padding: 0.25rem 0.375rem;
    color: var(--theme-text);
    font-size: inherit;
    cursor: pointer;
    border-radius: var(--rounded-sm);

    &:hover {
      background: var(--theme-hover);
    }

    .label {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }

  .metric {
    color: var(--theme-text-muted);
    padding-left: 1.5rem;

    &:hover {
      color: var(--theme-accent);
    }
  }

  .caret {
    display: inline-block;
    width: 0.75rem;
    color: var(--theme-text-muted);
    transition: transform 0.12s ease;

    &.open {
      transform: rotate(90deg);
    }
  }

  .group .label {
    font-weight: 600;
  }
</style>
