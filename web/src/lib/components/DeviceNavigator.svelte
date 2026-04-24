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
  import { api, apiPost, apiPut } from "$lib/api/client";
  import { subscribe } from "$lib/api/subscribe";
  import { invalidateAll } from "$app/navigation";
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
  let editHost = $state("");
  let editPort = $state("");
  let editSlot = $state("");
  let editEndpointUrl = $state("");
  let editVersion = $state("2c");
  let editCommunity = $state("public");
  let editUnitId = $state("1");
  let editScanRate = $state("");
  let editDeadbandValue = $state("");
  let editDeadbandMinTime = $state("");
  let editDeadbandMaxTime = $state("");
  let editDisableRBE = $state(false);
  let savingEdit = $state(false);

  const protocolDefaults: Record<string, number> = {
    ethernetip: 1000,
    opcua: 1000,
    snmp: 5000,
    modbus: 1000,
  };

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
    if (editingDevice === device.deviceId) {
      editingDevice = null;
      return;
    }
    editingDevice = device.deviceId;
    editHost = device.host ?? "";
    editPort = device.port?.toString() ?? "";
    editSlot = device.slot?.toString() ?? "";
    editEndpointUrl = device.endpointUrl ?? "";
    editVersion = device.version ?? "2c";
    editCommunity = device.community ?? "public";
    editUnitId = device.unitId?.toString() ?? "1";
    editScanRate = device.scanRate?.toString() ?? "";
    editDeadbandValue = device.deadband?.value?.toString() ?? "";
    editDeadbandMinTime = device.deadband?.minTime?.toString() ?? "";
    editDeadbandMaxTime = device.deadband?.maxTime?.toString() ?? "";
    editDisableRBE = device.disableRBE ?? false;
  }

  async function saveDeviceSettings(device: GatewayDevice) {
    savingEdit = true;
    try {
      const input: Record<string, unknown> = {
        protocol: device.protocol,
        ...(!device.autoManaged && device.protocol !== "opcua" && editHost
          ? { host: editHost }
          : {}),
        ...(!device.autoManaged && editPort ? { port: parseInt(editPort) } : {}),
        ...(!device.autoManaged &&
        device.protocol === "ethernetip" &&
        editSlot
          ? { slot: parseInt(editSlot) }
          : {}),
        ...(!device.autoManaged &&
        device.protocol === "opcua" &&
        editEndpointUrl
          ? { endpointUrl: editEndpointUrl }
          : {}),
        ...(!device.autoManaged && device.protocol === "snmp"
          ? { version: editVersion, community: editCommunity }
          : {}),
        ...(!device.autoManaged &&
        device.protocol === "modbus" &&
        editUnitId
          ? { unitId: parseInt(editUnitId) }
          : {}),
      };

      if (!device.autoManaged && editScanRate)
        input.scanRate = parseInt(editScanRate);
      if (editDisableRBE) {
        input.disableRBE = true;
      } else if (editDeadbandValue) {
        input.deadband = {
          value: parseFloat(editDeadbandValue),
          ...(editDeadbandMinTime
            ? { minTime: parseInt(editDeadbandMinTime) }
            : {}),
          ...(editDeadbandMaxTime
            ? { maxTime: parseInt(editDeadbandMaxTime) }
            : {}),
        };
      }

      const result = await apiPut(
        `/devices/${encodeURIComponent(device.deviceId)}`,
        input,
      );
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: "error" });
      } else {
        saltState.addNotification({
          message: `Device "${device.deviceId}" settings saved`,
          type: "success",
        });
        editingDevice = null;
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({
        message: err instanceof Error ? err.message : "Failed",
        type: "error",
      });
    } finally {
      savingEdit = false;
    }
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
        <div class="device-settings" transition:slide={{ duration: 150 }}>
          <div class="settings-grid">
            {#if !device.autoManaged}
              <div class="setting-group">
                <h3>Connection</h3>
                {#if device.protocol === "opcua"}
                  <div class="form-row">
                    <label for="edit-endpoint-{device.deviceId}">Endpoint URL</label>
                    <input
                      id="edit-endpoint-{device.deviceId}"
                      type="text"
                      bind:value={editEndpointUrl}
                      placeholder="opc.tcp://192.168.1.50:4840"
                    />
                  </div>
                {:else}
                  <div class="form-row">
                    <label for="edit-host-{device.deviceId}">Host</label>
                    <input
                      id="edit-host-{device.deviceId}"
                      type="text"
                      bind:value={editHost}
                      placeholder="192.168.1.100"
                    />
                  </div>
                  <div class="form-row">
                    <label for="edit-port-{device.deviceId}">Port</label>
                    <input
                      id="edit-port-{device.deviceId}"
                      type="text"
                      bind:value={editPort}
                      placeholder={device.protocol === "ethernetip"
                        ? "44818"
                        : device.protocol === "snmp"
                          ? "161"
                          : "502"}
                    />
                  </div>
                  {#if device.protocol === "ethernetip"}
                    <div class="form-row">
                      <label for="edit-slot-{device.deviceId}">Slot</label>
                      <input
                        id="edit-slot-{device.deviceId}"
                        type="text"
                        bind:value={editSlot}
                        placeholder="0"
                      />
                    </div>
                  {/if}
                  {#if device.protocol === "snmp"}
                    <div class="form-row">
                      <label for="edit-version-{device.deviceId}">SNMP Version</label>
                      <select id="edit-version-{device.deviceId}" bind:value={editVersion}>
                        <option value="1">v1</option>
                        <option value="2c">v2c</option>
                        <option value="3">v3</option>
                      </select>
                    </div>
                    <div class="form-row">
                      <label for="edit-community-{device.deviceId}">Community</label>
                      <input
                        id="edit-community-{device.deviceId}"
                        type="text"
                        bind:value={editCommunity}
                        placeholder="public"
                      />
                    </div>
                  {/if}
                  {#if device.protocol === "modbus"}
                    <div class="form-row">
                      <label for="edit-unitid-{device.deviceId}">Unit ID</label>
                      <input
                        id="edit-unitid-{device.deviceId}"
                        type="text"
                        bind:value={editUnitId}
                        placeholder="1"
                      />
                    </div>
                  {/if}
                {/if}
              </div>
              <div class="setting-group">
                <h3>Polling</h3>
                <div class="form-row">
                  <label for="edit-sr-{device.deviceId}">Scan Rate (ms)</label>
                  <input
                    id="edit-sr-{device.deviceId}"
                    type="number"
                    bind:value={editScanRate}
                    placeholder={String(protocolDefaults[device.protocol] ?? 1000)}
                    min="100"
                    step="100"
                  />
                </div>
              </div>
            {/if}
            <div class="setting-group">
              <h3>RBE / Deadband</h3>
              <label class="checkbox-label">
                <input type="checkbox" bind:checked={editDisableRBE} />
                <span>Disable RBE (publish every update)</span>
              </label>
              {#if !editDisableRBE}
                <div class="form-row">
                  <label for="edit-db-val-{device.deviceId}">Deadband</label>
                  <input
                    id="edit-db-val-{device.deviceId}"
                    type="number"
                    bind:value={editDeadbandValue}
                    placeholder="0"
                    min="0"
                    step="0.1"
                  />
                </div>
                <div class="form-row">
                  <label for="edit-db-min-{device.deviceId}">Min Time (ms)</label>
                  <input
                    id="edit-db-min-{device.deviceId}"
                    type="number"
                    bind:value={editDeadbandMinTime}
                    placeholder="none"
                    min="0"
                    step="100"
                  />
                </div>
                <div class="form-row">
                  <label for="edit-db-max-{device.deviceId}">Max Time (ms)</label>
                  <input
                    id="edit-db-max-{device.deviceId}"
                    type="number"
                    bind:value={editDeadbandMaxTime}
                    placeholder="none"
                    min="0"
                    step="1000"
                  />
                </div>
              {/if}
            </div>
          </div>
          <div class="form-actions">
            <button
              type="button"
              class="cancel-btn"
              onclick={() => (editingDevice = null)}
              disabled={savingEdit}
            >
              Cancel
            </button>
            <button
              type="button"
              class="save-btn"
              onclick={() => saveDeviceSettings(device)}
              disabled={savingEdit}
            >
              {savingEdit ? "Saving…" : "Save"}
            </button>
          </div>
        </div>
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

  // ── Inline settings editor ──
  .device-settings {
    border-top: 1px solid
      color-mix(in srgb, var(--theme-border) 50%, transparent);
    padding: 0.75rem 0.75rem 0.75rem 1.625rem;
    background: color-mix(in srgb, var(--theme-text) 2%, transparent);
  }

  .settings-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;

    @media (max-width: 640px) {
      grid-template-columns: 1fr;
    }
  }

  .setting-group {
    h3 {
      font-size: 0.6875rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--theme-text-muted);
      margin: 0 0 0.5rem;
    }
  }

  .form-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.5rem;

    label {
      font-size: 0.75rem;
      color: var(--theme-text-muted);
      min-width: 6rem;
      flex-shrink: 0;
    }

    input,
    select {
      flex: 1;
      padding: 0.25rem 0.375rem;
      font-size: 0.75rem;
      font-family: var(--font-mono, "IBM Plex Mono", monospace);
      border: 1px solid var(--theme-border);
      border-radius: 0.25rem;
      background: var(--theme-input-bg, var(--theme-background));
      color: var(--theme-text);
    }
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.75rem;
    color: var(--theme-text);
    cursor: pointer;
    margin-bottom: 0.5rem;

    input[type="checkbox"] {
      appearance: none;
      width: 14px;
      height: 14px;
      border: 1.5px solid var(--theme-border);
      border-radius: var(--rounded-sm, 3px);
      background: var(--theme-input-bg);
      cursor: pointer;
      flex-shrink: 0;
      position: relative;
      transition:
        background 0.15s ease,
        border-color 0.15s ease;

      &:checked {
        background: var(--theme-primary);
        border-color: var(--theme-primary);

        &::after {
          content: "";
          position: absolute;
          left: 3px;
          top: 0;
          width: 4px;
          height: 8px;
          border: solid white;
          border-width: 0 2px 2px 0;
          transform: rotate(45deg);
        }
      }

      &:hover {
        border-color: var(--theme-primary);
      }
    }
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 0.75rem;
  }

  .cancel-btn,
  .save-btn {
    padding: 0.3125rem 0.75rem;
    font-size: 0.75rem;
    font-weight: 500;
    border-radius: 0.25rem;
    cursor: pointer;

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .cancel-btn {
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    color: var(--theme-text);

    &:hover:not(:disabled) {
      background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface));
    }
  }

  .save-btn {
    border: none;
    background: var(--theme-primary);
    color: var(--theme-on-primary, white);

    &:hover:not(:disabled) {
      opacity: 0.9;
    }
  }
</style>
