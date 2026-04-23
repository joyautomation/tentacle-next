<script lang="ts">
  import type { BrowseCacheItem, GatewayDevice } from "$lib/types/gateway";
  import { ChevronRight } from "@joyautomation/salt/icons";
  import Self from "./DeviceTagTree.svelte";

  type TagTreeNode = {
    key: string;
    label: string;
    leaf?: BrowseCacheItem;
    children: TagTreeNode[];
    leafCount: number;
  };

  type Props = {
    nodes: TagTreeNode[];
    depth?: number;
    device: GatewayDevice;
    expandedNodes: Record<string, boolean>;
    forceExpandAll?: boolean;
    onToggle: (key: string) => void;
    onDragStart: (e: DragEvent, item: BrowseCacheItem) => void;
  };

  let {
    nodes,
    depth = 0,
    device,
    expandedNodes,
    forceExpandAll = false,
    onToggle,
    onDragStart,
  }: Props = $props();

  function typeBadge(datatype: string): string {
    if (!datatype) return "?";
    return datatype.slice(0, 4).toUpperCase();
  }
</script>

<ul class="tree-level">
  {#each nodes as node (node.key)}
    {@const isOpen = forceExpandAll || !!expandedNodes[node.key]}
    {@const isLeaf = node.children.length === 0 && node.leaf != null}
    <li>
      {#if isLeaf}
        <button
          type="button"
          class="tree-row leaf draggable"
          style:padding-left="{0.125 + depth * 0.75}rem"
          draggable="true"
          ondragstart={(e) => onDragStart(e, node.leaf!)}
          title={`${node.leaf!.datatype} · drag into editor to insert read_tag()`}
        >
          <span class="grip" aria-hidden="true">⋮⋮</span>
          <span class="badge">{typeBadge(node.leaf!.datatype)}</span>
          <span class="name">{node.label}</span>
        </button>
      {:else}
        <button
          type="button"
          class="tree-row branch"
          style:padding-left="{0.125 + depth * 0.75}rem"
          onclick={() => onToggle(node.key)}
          aria-expanded={isOpen}
          title={node.leaf ? `${node.leaf.datatype}` : undefined}
        >
          <span class="chevron" class:open={isOpen}>
            <ChevronRight size="0.625rem" />
          </span>
          <span class="name">{node.label}</span>
          <span class="count">{node.leafCount}</span>
        </button>
        {#if isOpen}
          <Self
            nodes={node.children}
            depth={depth + 1}
            {device}
            {expandedNodes}
            {forceExpandAll}
            {onToggle}
            {onDragStart}
          />
        {/if}
      {/if}
    </li>
  {/each}
</ul>

<style>
  .tree-level {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .tree-row {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    width: 100%;
    padding: 0.1875rem 0.375rem;
    background: transparent;
    border: none;
    color: var(--theme-text);
    font-size: 0.75rem;
    text-align: left;
    cursor: pointer;

    &:hover {
      background: var(--theme-surface);
    }

    &.leaf {
      cursor: grab;

      &:hover .grip {
        opacity: 0.5;
      }
      &:active {
        cursor: grabbing;
      }
    }
  }

  .grip {
    color: var(--theme-text-muted);
    font-size: 0.625rem;
    opacity: 0.25;
    cursor: grab;
    flex-shrink: 0;
  }

  .chevron {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 0.625rem;
    color: var(--theme-text-muted);
    transition: transform 0.15s ease;
    flex-shrink: 0;

    &.open {
      transform: rotate(90deg);
    }
  }

  .badge {
    flex-shrink: 0;
    padding: 0 0.25rem;
    font-size: 0.5625rem;
    font-weight: 700;
    background: var(--badge-teal-bg, color-mix(in srgb, var(--theme-primary) 20%, transparent));
    color: var(--badge-teal-text, var(--theme-primary));
    border-radius: 2px;
    letter-spacing: 0.02em;
  }

  .name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: "IBM Plex Mono", monospace;
  }

  .count {
    flex-shrink: 0;
    font-size: 0.625rem;
    color: var(--theme-text-muted);
    font-family: "IBM Plex Mono", monospace;
  }
</style>
