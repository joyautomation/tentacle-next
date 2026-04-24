<script lang="ts">
  import { slide } from "svelte/transition";
  import type {
    BrowseCache,
    BrowseCacheItem,
    GatewayConfig,
    GatewayDevice,
  } from "$lib/types/gateway";
  import { ChevronRight, Cog6Tooth, Trash } from "@joyautomation/salt/icons";
  import DeviceTagTree from "$lib/plc/workspace/DeviceTagTree.svelte";
  import DeviceSettingsPanel from "./DeviceSettingsPanel.svelte";
  import { api, apiPost } from "$lib/api/client";
  import { subscribe } from "$lib/api/subscribe";
  import { state as saltState } from "@joyautomation/salt";

  type TagTreeNode = {
    key: string;
    label: string;
    kind?: "template" | "instance";
    leaf?: BrowseCacheItem;
    children: TagTreeNode[];
    leafCount: number;
  };

  type Props = {
    gatewayConfig: GatewayConfig | null;
    localPlcId?: string | null;
    filter?: string;
    enableEditing?: boolean;
    selectedDeviceId?: string | null;
    varCounts?: Record<string, number>;
    storagePrefix?: string;
    onSelect?: (deviceId: string) => void;
    onRequestDelete?: (deviceId: string) => void;
  };

  let {
    gatewayConfig,
    localPlcId = null,
    filter = "",
    enableEditing = false,
    selectedDeviceId = null,
    varCounts = {},
    storagePrefix = "device-nav:",
    onSelect,
    onRequestDelete,
  }: Props = $props();

  const BROWSE_TAG_MIME = "application/x-plc-browse-tag";

  function loadStored<T>(key: string, fallback: T): T {
    if (typeof localStorage === "undefined") return fallback;
    try {
      const raw = localStorage.getItem(storagePrefix + key);
      return raw ? (JSON.parse(raw) as T) : fallback;
    } catch {
      return fallback;
    }
  }
  function persist(key: string, value: unknown) {
    if (typeof localStorage === "undefined") return;
    try {
      localStorage.setItem(storagePrefix + key, JSON.stringify(value));
    } catch {
      // quota / serialization failure — state stays in-memory only
    }
  }

  const gatewayId = $derived(gatewayConfig?.gatewayId ?? "gateway");

  const deviceEntries = $derived(
    (gatewayConfig?.devices ?? [])
      .filter(
        (d) =>
          d.deviceId !== localPlcId &&
          (!filter || d.deviceId.toLowerCase().includes(filter.toLowerCase())),
      )
      .sort((a, b) => a.deviceId.localeCompare(b.deviceId)),
  );

  function protocolBadge(protocol: string): string {
    if (protocol === "ethernetip") return "EIP";
    if (protocol === "opcua") return "OPC";
    if (protocol === "modbus") return "MOD";
    if (protocol === "snmp") return "SNMP";
    if (protocol === "plc") return "PLC";
    return protocol.slice(0, 4).toUpperCase();
  }

  type BrowseEntry =
    | { status: "idle" }
    | { status: "loading" }
    | { status: "empty" }
    | { status: "error"; message: string }
    | { status: "ready"; cache: BrowseCache };

  let expandedDevices = $state<Record<string, boolean>>(
    loadStored<Record<string, boolean>>("expandedDevices", {}),
  );
  $effect(() => persist("expandedDevices", expandedDevices));
  let browseCaches = $state<Record<string, BrowseEntry>>({});

  type BrowseProgress = {
    browseId: string;
    status: "browsing" | "completed" | "failed" | "cancelled";
    phase: string;
    discoveredCount: number;
    totalCount: number;
    message: string;
  };
  let liveProgress = $state<Record<string, BrowseProgress>>({});
  const browseSubs = new Map<string, () => void>();

  // Row whose inline settings panel is open. Distinct from expandedDevices
  // (which controls the tag tree) so a user can have tags open AND settings
  // open simultaneously.
  let editingDevice = $state<string | null>(null);

  async function loadBrowseCache(deviceId: string) {
    browseCaches[deviceId] = { status: "loading" };
    const res = await api<BrowseCache>(
      `/gateways/${encodeURIComponent(gatewayId)}/browse-cache/${encodeURIComponent(deviceId)}`,
    );
    if (res.error) {
      const msg = res.error.error ?? "";
      if (res.error.status === 404 || /not found/i.test(msg)) {
        browseCaches[deviceId] = { status: "empty" };
      } else {
        browseCaches[deviceId] = {
          status: "error",
          message: msg || "Failed to load browse cache",
        };
      }
      return;
    }
    if (!res.data || !res.data.items || res.data.items.length === 0) {
      browseCaches[deviceId] = { status: "empty" };
      return;
    }
    browseCaches[deviceId] = { status: "ready", cache: res.data };
  }

  // Devices restored as expanded from localStorage need their browse cache
  // fetched — toggleDeviceExpand only fires on user click.
  $effect(() => {
    for (const [deviceId, open] of Object.entries(expandedDevices)) {
      if (open && !browseCaches[deviceId]) {
        loadBrowseCache(deviceId);
      }
    }
  });

  async function toggleDeviceExpand(device: GatewayDevice, e: MouseEvent) {
    e.stopPropagation();
    const deviceId = device.deviceId;
    const open = !expandedDevices[deviceId];
    expandedDevices[deviceId] = open;
    if (open && !browseCaches[deviceId]) {
      await loadBrowseCache(deviceId);
    }
  }

  function clearProgressLater(deviceId: string, ms = 2000) {
    setTimeout(() => {
      const { [deviceId]: _drop, ...rest } = liveProgress;
      liveProgress = rest;
    }, ms);
  }

  async function rebrowseDevice(device: GatewayDevice, e: MouseEvent) {
    e.stopPropagation();
    if (device.autoManaged) {
      saltState.addNotification({
        message: "Auto-managed devices can't be rebrowsed from here.",
        type: "info",
      });
      return;
    }
    if (liveProgress[device.deviceId]?.status === "browsing") return;

    const payload: Record<string, unknown> = {
      deviceId: device.deviceId,
      protocol: device.protocol,
    };
    if (device.host) payload.host = device.host;
    if (device.port) payload.port = device.port;
    if (device.protocol === "ethernetip" && device.slot != null)
      payload.slot = device.slot;
    if (device.endpointUrl) payload.endpointUrl = device.endpointUrl;
    if (device.version) payload.version = device.version;
    if (device.community) payload.community = device.community;

    const res = await apiPost<{ browseId: string }>(
      `/gateways/${encodeURIComponent(gatewayId)}/browse`,
      payload,
    );
    if (res.error || !res.data?.browseId) {
      saltState.addNotification({
        message: res.error?.error ?? "Browse failed to start",
        type: "error",
      });
      return;
    }

    const deviceId = device.deviceId;
    const browseId = res.data.browseId;

    if (!expandedDevices[deviceId]) expandedDevices[deviceId] = true;

    liveProgress = {
      ...liveProgress,
      [deviceId]: {
        browseId,
        status: "browsing",
        phase: "connecting",
        discoveredCount: 0,
        totalCount: 0,
        message: "Starting browse…",
      },
    };

    browseSubs.get(deviceId)?.();
    const cleanup = subscribe<{
      phase: string;
      discoveredCount?: number;
      totalCount?: number;
      message?: string;
    }>(
      `/gateways/${encodeURIComponent(gatewayId)}/browse/${encodeURIComponent(browseId)}/progress`,
      async (p) => {
        const terminal =
          p.phase === "completed" ||
          p.phase === "failed" ||
          p.phase === "cancelled";
        liveProgress = {
          ...liveProgress,
          [deviceId]: {
            browseId,
            status: terminal
              ? (p.phase as "completed" | "failed" | "cancelled")
              : "browsing",
            phase: p.phase,
            discoveredCount: p.discoveredCount ?? 0,
            totalCount: p.totalCount ?? 0,
            message: p.message ?? "",
          },
        };
        if (terminal) {
          browseSubs.get(deviceId)?.();
          browseSubs.delete(deviceId);
          if (p.phase === "completed") {
            await loadBrowseCache(deviceId);
          } else if (p.phase === "failed") {
            browseCaches[deviceId] = {
              status: "error",
              message: p.message || "Browse failed",
            };
            saltState.addNotification({
              message: `Browse failed for "${deviceId}": ${p.message ?? ""}`,
              type: "error",
            });
          }
          clearProgressLater(deviceId);
        }
      },
      () => {
        if (liveProgress[deviceId]?.status === "browsing") {
          saltState.addNotification({
            message: `Lost browse progress stream for "${deviceId}"`,
            type: "error",
          });
          browseSubs.get(deviceId)?.();
          browseSubs.delete(deviceId);
          clearProgressLater(deviceId, 500);
        }
      },
    );
    browseSubs.set(deviceId, cleanup);
  }

  async function cancelBrowse(device: GatewayDevice, e: MouseEvent) {
    e.stopPropagation();
    const info = liveProgress[device.deviceId];
    if (!info || info.status !== "browsing") return;
    try {
      await apiPost(
        `/gateways/${encodeURIComponent(gatewayId)}/browse/${encodeURIComponent(info.browseId)}/cancel`,
        {},
      );
    } catch {
      // best-effort — server terminal event will also reach us via SSE
    }
  }

  function filteredBrowseItems(cache: BrowseCache) {
    if (!filter) return cache.items;
    const f = filter.toLowerCase();
    return cache.items.filter(
      (it) =>
        (it.tag ?? "").toLowerCase().includes(f) ||
        (it.name ?? "").toLowerCase().includes(f),
    );
  }

  // Tree shape: Device > Template > Instance > members. Atomic tags (whose
  // root name isn't in structTags) go directly under the device root.
  function buildTagTree(
    items: BrowseCacheItem[],
    structTags: Record<string, string>,
  ): TagTreeNode[] {
    const root: TagTreeNode = { key: "", label: "", children: [], leafCount: 0 };

    const findOrAdd = (
      parent: TagTreeNode,
      label: string,
      keyPath: string,
      kind?: "template" | "instance",
    ): TagTreeNode => {
      let child = parent.children.find((c) => c.label === label);
      if (!child) {
        child = { key: keyPath, label, kind, children: [], leafCount: 0 };
        parent.children.push(child);
      }
      return child;
    };

    for (const item of items) {
      const parts = item.tag.split(".").filter((p) => p.length > 0);
      if (parts.length === 0) continue;
      const rootName = parts[0];
      const templateName = structTags?.[rootName];

      let cur: TagTreeNode;
      let startIdx = 0;
      if (templateName) {
        const tplNode = findOrAdd(root, templateName, `__t/${templateName}`, "template");
        const instNode = findOrAdd(
          tplNode,
          rootName,
          `__t/${templateName}/${rootName}`,
          "instance",
        );
        cur = instNode;
        startIdx = 1;
      } else {
        cur = root;
      }

      let acc = parts.slice(0, startIdx).join(".");
      for (let i = startIdx; i < parts.length; i++) {
        const part = parts[i];
        acc = acc ? acc + "." + part : part;
        let child = cur.children.find((c) => c.label === part);
        if (!child) {
          child = { key: acc, label: part, children: [], leafCount: 0 };
          cur.children.push(child);
        }
        if (i === parts.length - 1) {
          child.leaf = item;
        }
        cur = child;
      }
    }

    const sortCount = (n: TagTreeNode): number => {
      n.children.sort((a, b) => {
        const aIsGroup = a.kind === "template" || a.kind === "instance";
        const bIsGroup = b.kind === "template" || b.kind === "instance";
        if (aIsGroup !== bIsGroup) return aIsGroup ? -1 : 1;
        return a.label.localeCompare(b.label);
      });
      let count = n.leaf ? 1 : 0;
      for (const c of n.children) count += sortCount(c);
      n.leafCount = count;
      return count;
    };
    for (const c of root.children) sortCount(c);
    root.children.sort((a, b) => {
      const aIsGroup = a.kind === "template" || a.kind === "instance";
      const bIsGroup = b.kind === "template" || b.kind === "instance";
      if (aIsGroup !== bIsGroup) return aIsGroup ? -1 : 1;
      return a.label.localeCompare(b.label);
    });
    return root.children;
  }

  let treeExpanded = $state<Record<string, Record<string, boolean>>>(
    loadStored<Record<string, Record<string, boolean>>>("treeExpanded", {}),
  );
  $effect(() => persist("treeExpanded", treeExpanded));

  function toggleTreeNode(deviceId: string, key: string) {
    const cur = treeExpanded[deviceId] ?? {};
    treeExpanded = {
      ...treeExpanded,
      [deviceId]: { ...cur, [key]: !cur[key] },
    };
  }

  function onBrowseTagDragStart(
    e: DragEvent,
    device: GatewayDevice,
    item: BrowseCacheItem,
  ) {
    if (!e.dataTransfer) return;
    const payload = JSON.stringify({
      deviceId: device.deviceId,
      protocol: device.protocol,
      tag: item.tag,
      datatype: item.datatype,
    });
    e.dataTransfer.setData(BROWSE_TAG_MIME, payload);
    e.dataTransfer.setData("text/plain", item.tag);
    e.dataTransfer.effectAllowed = "copy";
  }

  function openEditor(device: GatewayDevice, e: MouseEvent) {
    e.stopPropagation();
    editingDevice =
      editingDevice === device.deviceId ? null : device.deviceId;
  }
</script>

<ul class="items">
  {#each deviceEntries as device (device.deviceId)}
    <li class="device-row">
      <div class="item-row">
        <button
          type="button"
          class="expand-btn"
          onclick={(e) => toggleDeviceExpand(device, e)}
          aria-label={expandedDevices[device.deviceId]
            ? `Collapse ${device.deviceId}`
            : `Expand ${device.deviceId}`}
          aria-expanded={expandedDevices[device.deviceId] ?? false}
          title="Show tags"
        >
          <span
            class="chevron small"
            class:open={expandedDevices[device.deviceId]}
          >
            <ChevronRight size="0.625rem" />
          </span>
        </button>
        <button
          type="button"
          class="item"
          class:selected={selectedDeviceId === device.deviceId}
          onclick={() => onSelect?.(device.deviceId)}
          title={device.autoManaged
            ? `${device.protocol} · auto-managed by a module`
            : `${device.protocol} · ${device.host ?? device.endpointUrl ?? ""}`}
        >
          <span class="badge device">{protocolBadge(device.protocol)}</span>
          <span class="name">{device.deviceId}</span>
          {#if device.autoManaged}
            <span class="item-tag">auto</span>
          {/if}
          {#if varCounts[device.deviceId]}
            <span class="meta">{varCounts[device.deviceId]}</span>
          {/if}
        </button>
        {#if enableEditing}
          <button
            type="button"
            class="row-action edit-action"
            onclick={(e) => openEditor(device, e)}
            title={editingDevice === device.deviceId
              ? "Close settings"
              : "Device settings"}
            aria-label="Edit device settings"
            aria-expanded={editingDevice === device.deviceId}
          >
            <Cog6Tooth size="0.875rem" />
          </button>
        {/if}
        {#if !device.autoManaged}
          {@const progress = liveProgress[device.deviceId]}
          {#if progress}
            <div
              class="browse-progress"
              class:ok={progress.status === "completed"}
              class:err={progress.status === "failed"}
              title={progress.message || progress.phase}
            >
              <svg
                class="progress-ring"
                class:spin={progress.status === "browsing" &&
                  progress.totalCount === 0}
                viewBox="0 0 20 20"
                width="14"
                height="14"
                aria-hidden="true"
              >
                <circle
                  cx="10"
                  cy="10"
                  r="8"
                  fill="none"
                  stroke="var(--theme-border)"
                  stroke-width="2.5"
                />
                <circle
                  cx="10"
                  cy="10"
                  r="8"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                  stroke-linecap="round"
                  stroke-dasharray={progress.status === "completed"
                    ? "50.3 0"
                    : progress.totalCount > 0
                      ? `${Math.min(50.3, Math.round((progress.discoveredCount / progress.totalCount) * 50.3))} 50.3`
                      : "12 50.3"}
                  stroke-dashoffset="12.6"
                />
              </svg>
              {#if progress.status === "completed"}
                <span class="progress-count">done</span>
              {:else if progress.status === "failed"}
                <span class="progress-count">failed</span>
              {:else if progress.status === "cancelled"}
                <span class="progress-count">cancelled</span>
              {:else if progress.totalCount > 0}
                <span class="progress-count"
                  >{progress.discoveredCount}/{progress.totalCount}</span
                >
              {:else if progress.discoveredCount > 0}
                <span class="progress-count">{progress.discoveredCount}</span>
              {:else}
                <span class="progress-count">{progress.phase}</span>
              {/if}
              {#if progress.status === "browsing"}
                <button
                  type="button"
                  class="row-action cancel"
                  onclick={(e) => cancelBrowse(device, e)}
                  title="Cancel browse"
                  aria-label="Cancel browse"
                >×</button>
              {/if}
            </div>
          {:else}
            <button
              type="button"
              class="row-action"
              onclick={(e) => rebrowseDevice(device, e)}
              title="Rebrowse tags"
              aria-label="Rebrowse tags"
            >
              <span class="refresh-icon">↻</span>
            </button>
          {/if}
        {/if}
        {#if enableEditing && !device.autoManaged}
          <button
            type="button"
            class="row-action delete-action"
            onclick={(e) => {
              e.stopPropagation();
              onRequestDelete?.(device.deviceId);
            }}
            title="Remove device"
            aria-label="Remove device"
          >
            <Trash size="0.875rem" />
          </button>
        {/if}
      </div>
      {#if editingDevice === device.deviceId}
        <DeviceSettingsPanel
          {device}
          onClose={() => (editingDevice = null)}
        />
      {/if}
      {#if expandedDevices[device.deviceId]}
        <div class="tag-tree" transition:slide={{ duration: 120 }}>
          {#if liveProgress[device.deviceId]?.status === "browsing"}
            <div class="tag-status">
              {liveProgress[device.deviceId].phase}
              {#if liveProgress[device.deviceId].message}
                — {liveProgress[device.deviceId].message}
              {/if}
            </div>
          {:else if browseCaches[device.deviceId]?.status === "loading"}
            <div class="tag-status">Loading…</div>
          {:else if browseCaches[device.deviceId]?.status === "error"}
            <div class="tag-status err">
              {(browseCaches[device.deviceId] as {
                status: "error";
                message: string;
              }).message}
            </div>
          {:else if browseCaches[device.deviceId]?.status === "empty"}
            <div class="tag-status muted">
              No tags cached. Click ↻ to browse.
            </div>
          {:else if browseCaches[device.deviceId]?.status === "ready"}
            {@const items = filteredBrowseItems(
              (browseCaches[device.deviceId] as {
                status: "ready";
                cache: BrowseCache;
              }).cache,
            )}
            {#if items.length === 0}
              <div class="tag-status muted">No tags match filter.</div>
            {:else}
              {@const cache = (browseCaches[device.deviceId] as {
                status: "ready";
                cache: BrowseCache;
              }).cache}
              {@const tree = buildTagTree(items, cache.structTags ?? {})}
              <DeviceTagTree
                nodes={tree}
                {device}
                expandedNodes={treeExpanded[device.deviceId] ?? {}}
                forceExpandAll={!!filter}
                onToggle={(key) => toggleTreeNode(device.deviceId, key)}
                onDragStart={(e, item) => onBrowseTagDragStart(e, device, item)}
              />
            {/if}
          {/if}
        </div>
      {/if}
    </li>
  {:else}
    <li class="empty">No devices</li>
  {/each}
</ul>

<style lang="scss">
  .items {
    list-style: none;
    margin: 0;
    padding: 0 0 0.25rem 0;
  }

  .device-row {
    display: flex;
    flex-direction: column;
  }

  .item-row {
    display: flex;
    align-items: center;

    .item {
      flex: 1;
      min-width: 0;
      padding-left: 0.125rem;
    }

    .edit-action,
    .delete-action {
      opacity: 0;
      transition: opacity 0.12s ease, color 0.12s ease;
    }

    &:hover .edit-action,
    &:hover .delete-action,
    .edit-action[aria-expanded="true"] {
      opacity: 0.7;
    }

    .edit-action:hover,
    .delete-action:hover,
    .edit-action[aria-expanded="true"] {
      opacity: 1;
    }
  }

  .expand-btn {
    flex-shrink: 0;
    width: 1.125rem;
    height: 1.5rem;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    color: var(--theme-text-muted);
    cursor: pointer;

    &:hover {
      color: var(--theme-text);
    }
  }

  .chevron {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--theme-text-muted);
    transition: transform 0.15s ease;

    &.open {
      transform: rotate(90deg);
    }
  }

  .chevron.small {
    font-size: 0.625rem;
  }

  .item {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    width: 100%;
    padding: 0.25rem 0.5rem 0.25rem 0.625rem;
    background: transparent;
    border: none;
    border-radius: 0;
    cursor: pointer;
    color: var(--theme-text);
    font-size: 0.8125rem;
    text-align: left;

    &:hover {
      background: var(--theme-surface);
    }

    &.selected {
      background: color-mix(in srgb, var(--theme-primary) 18%, transparent);
      color: var(--theme-text);
    }
  }

  .name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono, monospace);
  }

  .badge {
    flex-shrink: 0;
    padding: 0.0625rem 0.3125rem;
    font-size: 0.625rem;
    font-weight: 600;
    color: var(--theme-text-muted);
    background: var(--theme-surface);
    border-radius: 0.1875rem;
    text-transform: uppercase;
    font-family: var(--font-mono, monospace);
    min-width: 2.25rem;
    text-align: center;
  }

  .badge.device {
    color: var(--theme-warning, #f59e0b);
  }

  .item-tag {
    padding: 0 0.25rem;
    font-family: var(--font-mono, monospace);
    font-size: 0.625rem;
    color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    border-radius: 0.1875rem;
  }

  .meta {
    flex-shrink: 0;
    color: var(--theme-text-muted);
    font-size: 0.75rem;
  }

  .row-action {
    flex-shrink: 0;
    width: 1.5rem;
    height: 1.5rem;
    margin-right: 0.25rem;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    color: var(--theme-text-muted);
    font-size: 0.875rem;
    line-height: 1;
    cursor: pointer;
    opacity: 0.7;
    transition: opacity 0.12s ease, color 0.12s ease;

    &:hover:not(:disabled) {
      opacity: 1;
      color: var(--theme-text);
    }

    &:disabled {
      opacity: 0.5;
      cursor: wait;
    }
  }

  .row-action.delete-action:hover {
    color: var(--theme-danger, #e5484d);
  }

  .refresh-icon {
    display: inline-block;
  }

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  .browse-progress {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    margin-right: 0.125rem;
    color: var(--badge-teal-text, var(--theme-primary));
    font-size: 0.625rem;
    font-family: "IBM Plex Mono", monospace;
    line-height: 1;

    &.ok {
      color: var(--theme-success, var(--badge-teal-text));
    }
    &.err {
      color: var(--theme-danger, #e5484d);
    }
  }

  .progress-ring {
    flex-shrink: 0;
    transform: rotate(-90deg);
    transform-origin: center;
    transition: stroke-dasharray 0.2s ease;

    &.spin {
      animation: spin 1.1s linear infinite;
    }
  }

  .progress-count {
    color: var(--theme-text-muted);
    white-space: nowrap;
  }

  .row-action.cancel {
    width: 1.125rem;
    height: 1.125rem;
    font-size: 0.875rem;
    margin-right: 0.125rem;

    &:hover {
      color: var(--theme-danger, #e5484d);
    }
  }

  .tag-tree {
    padding: 0 0 0.25rem 1.625rem;
    border-left: 1px dashed var(--theme-border);
    margin-left: 0.75rem;
  }

  .tag-status {
    padding: 0.25rem 0.5rem;
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    font-style: italic;

    &.err {
      color: var(--theme-error, #e5484d);
      font-style: normal;
    }
  }

  .empty {
    padding: 0.375rem 1rem;
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    font-style: italic;
  }

</style>
