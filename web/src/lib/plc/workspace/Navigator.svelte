<script lang="ts">
  import { slide } from "svelte/transition";
  import type {
    PlcVariableConfig,
    PlcTaskConfig,
    PlcTemplate,
    ProgramListItem,
    TestListItem,
  } from "$lib/types/plc";
  import type { BrowseCache, BrowseCacheItem, GatewayConfig, GatewayDevice } from "$lib/types/gateway";
  import { workspaceSelection, workspaceTabs } from "../workspace-state.svelte";
  import { ChevronRight, Plus } from "@joyautomation/salt/icons";
  import DeviceTagTree from "./DeviceTagTree.svelte";

  type TagTreeNode = {
    key: string;
    label: string;
    kind?: "template" | "instance";
    leaf?: BrowseCacheItem;
    children: TagTreeNode[];
    leafCount: number;
  };
  import { api, apiPost, apiPut } from "$lib/api/client";
  import { subscribe } from "$lib/api/subscribe";
  import { invalidateAll } from "$app/navigation";
  import { state as saltState } from "@joyautomation/salt";

  type Props = {
    variables: Record<string, PlcVariableConfig>;
    tasks: Record<string, PlcTaskConfig>;
    templates: PlcTemplate[];
    programs: ProgramListItem[];
    tests: TestListItem[];
    gatewayConfig: GatewayConfig | null;
    localPlcId?: string | null;
    onCreate?: (kind: "variable" | "task") => void;
    onRunAllTests?: () => void;
    testsRunning?: boolean;
  };

  let {
    variables,
    tasks,
    templates,
    programs,
    tests,
    gatewayConfig,
    localPlcId,
    onCreate,
    onRunAllTests,
    testsRunning,
  }: Props = $props();

  function newProgramTab() {
    // Functions are created in-editor: a blank tab opens, the user types
    // their def, and the program name is derived from the def header on
    // save. No modal.
    workspaceTabs.openNew("program", "starlark");
  }

  function newTestTab() {
    workspaceTabs.openNew("test", "starlark");
  }

  function newTypeTab() {
    // Types are created the same way programs are: a blank tab opens, the
    // user names the type in-editor, renameTab promotes the synthetic id on
    // first save.
    workspaceTabs.openNew("type");
  }

  function newDeviceTab() {
    workspaceTabs.openNew("device");
  }

  let sections = $state({
    devices: true,
    variables: true,
    types: true,
    tasks: true,
    programs: true,
    tests: true,
  });

  let filter = $state("");
  let activeTags = $state<string[]>([]);

  function matchesTags(itemTags: string[] | undefined): boolean {
    if (activeTags.length === 0) return true;
    const set = new Set(itemTags ?? []);
    return activeTags.every((t) => set.has(t));
  }

  function matchesFilter(name: string): boolean {
    return !filter || name.toLowerCase().includes(filter.toLowerCase());
  }

  const variableEntries = $derived(
    Object.entries(variables)
      .filter(([name]) => matchesFilter(name))
      .sort(([a], [b]) => a.localeCompare(b)),
  );

  const taskEntries = $derived(
    Object.values(tasks)
      .filter((t) => matchesFilter(t.name))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const typeEntries = $derived(
    templates
      .filter((t) => matchesFilter(t.name))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  // Devices are shared across Gateway and PLC — one source of truth in the
  // `devices` KV bucket, exposed on the gateway config as `devices`.
  const deviceEntries = $derived(
    (gatewayConfig?.devices ?? [])
      .filter((d) => d.deviceId !== localPlcId && matchesFilter(d.deviceId))
      .sort((a, b) => a.deviceId.localeCompare(b.deviceId)),
  );

  const deviceVarCounts = $derived.by(() => {
    const counts: Record<string, number> = {};
    for (const v of Object.values(variables)) {
      const deviceId = v.source?.deviceId;
      if (!deviceId) continue;
      counts[deviceId] = (counts[deviceId] ?? 0) + 1;
    }
    return counts;
  });

  function protocolBadge(protocol: string): string {
    if (protocol === "ethernetip") return "EIP";
    if (protocol === "opcua") return "OPC";
    if (protocol === "modbus") return "MOD";
    if (protocol === "snmp") return "SNMP";
    if (protocol === "plc") return "PLC";
    return protocol.slice(0, 4).toUpperCase();
  }

  const programEntries = $derived(
    programs
      .filter((p) => matchesFilter(p.name) && matchesTags(p.tags))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const testEntries = $derived(
    tests
      .filter((t) => matchesFilter(t.name) && matchesTags(t.tags))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const allTags = $derived.by(() => {
    const s = new Set<string>();
    for (const p of programs) for (const t of p.tags ?? []) s.add(t);
    for (const t of tests) for (const tag of t.tags ?? []) s.add(tag);
    return Array.from(s).sort();
  });

  function toggleTag(tag: string) {
    activeTags = activeTags.includes(tag)
      ? activeTags.filter((t) => t !== tag)
      : [...activeTags, tag];
  }

  function testDotClass(t: TestListItem): string {
    const status = t.lastResult?.status;
    if (status === "pass") return "pass";
    if (status === "fail") return "fail";
    if (status === "error") return "error";
    return "unknown";
  }

  function toggle(key: keyof typeof sections) {
    sections[key] = !sections[key];
  }

  function languageLabel(lang: string): string {
    if (lang === "starlark") return "PY";
    if (lang === "st" || lang === "structured-text") return "ST";
    if (lang === "ladder") return "LD";
    return lang.slice(0, 2).toUpperCase();
  }

  const VARIABLE_MIME = "application/x-plc-variable";
  const BROWSE_TAG_MIME = "application/x-plc-browse-tag";

  const gatewayId = $derived(gatewayConfig?.gatewayId ?? "gateway");

  type BrowseEntry =
    | { status: "idle" }
    | { status: "loading" }
    | { status: "empty" }
    | { status: "error"; message: string }
    | { status: "ready"; cache: BrowseCache };

  let expandedDevices = $state<Record<string, boolean>>({});
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

  async function loadBrowseCache(deviceId: string) {
    browseCaches[deviceId] = { status: "loading" };
    const res = await api<BrowseCache>(
      `/gateways/${encodeURIComponent(gatewayId)}/browse-cache/${encodeURIComponent(deviceId)}`,
    );
    if (res.error) {
      // A 404 just means we haven't browsed yet — show empty, not error.
      const msg = res.error.error ?? "";
      if (res.error.status === 404 || /not found/i.test(msg)) {
        browseCaches[deviceId] = { status: "empty" };
      } else {
        browseCaches[deviceId] = { status: "error", message: msg || "Failed to load browse cache" };
      }
      return;
    }
    if (!res.data || !res.data.items || res.data.items.length === 0) {
      browseCaches[deviceId] = { status: "empty" };
      return;
    }
    browseCaches[deviceId] = { status: "ready", cache: res.data };
  }

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
    if (device.protocol === "ethernetip" && device.slot != null) payload.slot = device.slot;
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

    // Expand the row so the user immediately sees progress + incoming tags.
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
          p.phase === "completed" || p.phase === "failed" || p.phase === "cancelled";
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
        // Stream dropped (network/server). Leave cache state alone and
        // surface the loss so the user can retry instead of silently stalling.
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
        // Templates/instances first (by name), then atomic branches/leaves.
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

  let treeExpanded = $state<Record<string, Record<string, boolean>>>({});

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

  function typeBadge(datatype: string): string {
    if (!datatype) return "?";
    return datatype.slice(0, 4).toUpperCase();
  }

  let togglingTask = $state<string | null>(null);

  async function toggleTaskEnabled(task: PlcTaskConfig, e: MouseEvent) {
    e.stopPropagation();
    if (togglingTask) return;
    togglingTask = task.name;
    try {
      const body: PlcTaskConfig = { ...task, enabled: !task.enabled };
      const res = await apiPut(
        `/plcs/plc/tasks/${encodeURIComponent(task.name)}`,
        body,
      );
      if (res.error) {
        saltState.addNotification({ message: res.error.error, type: "error" });
        return;
      }
      await invalidateAll();
    } finally {
      togglingTask = null;
    }
  }

  function onVariableDragStart(e: DragEvent, name: string, datatype: string) {
    if (!e.dataTransfer) return;
    const payload = JSON.stringify({ name, datatype });
    e.dataTransfer.setData(VARIABLE_MIME, payload);
    e.dataTransfer.setData("text/plain", name);
    e.dataTransfer.effectAllowed = "copy";
  }
</script>

<div class="navigator">
  <div class="filter-wrap">
    <input
      type="text"
      class="filter-input"
      placeholder="Filter…"
      bind:value={filter}
      aria-label="Filter navigator"
    />
    {#if allTags.length > 0}
      <div class="tag-filter" role="group" aria-label="Filter by tag">
        {#each allTags as tag (tag)}
          <button
            type="button"
            class="tag-chip"
            class:active={activeTags.includes(tag)}
            onclick={() => toggleTag(tag)}
          >
            {tag}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="sections">
    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("devices")}
          aria-expanded={sections.devices}
        >
          <span class="chevron" class:open={sections.devices}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Devices</span>
          <span class="count">{deviceEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={newDeviceTab}
          title="New device"
          aria-label="New device"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.devices}
        <ul class="items" transition:slide={{ duration: 150 }}>
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
                  class:selected={workspaceSelection.isSelected(
                    "device",
                    device.deviceId,
                  )}
                  onclick={() =>
                    workspaceSelection.select("device", device.deviceId)}
                  title={device.autoManaged
                    ? `${device.protocol} · auto-managed by a module`
                    : `${device.protocol} · ${device.host ?? device.endpointUrl ?? ""}`}
                >
                  <span class="badge device"
                    >{protocolBadge(device.protocol)}</span
                  >
                  <span class="name">{device.deviceId}</span>
                  {#if device.autoManaged}
                    <span class="item-tag">auto</span>
                  {/if}
                  {#if deviceVarCounts[device.deviceId]}
                    <span class="meta">{deviceVarCounts[device.deviceId]}</span>
                  {/if}
                </button>
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
                        <span class="progress-count"
                          >{progress.discoveredCount}</span
                        >
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
              </div>
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
                      {(browseCaches[device.deviceId] as { status: "error"; message: string }).message}
                    </div>
                  {:else if browseCaches[device.deviceId]?.status === "empty"}
                    <div class="tag-status muted">
                      No tags cached. Click ↻ to browse.
                    </div>
                  {:else if browseCaches[device.deviceId]?.status === "ready"}
                    {@const items = filteredBrowseItems(
                      (browseCaches[device.deviceId] as { status: "ready"; cache: BrowseCache }).cache,
                    )}
                    {#if items.length === 0}
                      <div class="tag-status muted">No tags match filter.</div>
                    {:else}
                      {@const cache = (browseCaches[device.deviceId] as { status: "ready"; cache: BrowseCache }).cache}
                      {@const tree = buildTagTree(items, cache.structTags ?? {})}
                      <DeviceTagTree
                        nodes={tree}
                        {device}
                        expandedNodes={treeExpanded[device.deviceId] ?? {}}
                        forceExpandAll={!!filter}
                        onToggle={(key) =>
                          toggleTreeNode(device.deviceId, key)}
                        onDragStart={(e, item) =>
                          onBrowseTagDragStart(e, device, item)}
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
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("variables")}
          aria-expanded={sections.variables}
        >
          <span class="chevron" class:open={sections.variables}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Variables</span>
          <span class="count">{variableEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={() => onCreate?.("variable")}
          title="New variable"
          aria-label="New variable"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.variables}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each variableEntries as [name, cfg] (name)}
            <li>
              <button
                type="button"
                class="item draggable"
                class:selected={workspaceSelection.isSelected("variable", name)}
                onclick={() => workspaceSelection.select("variable", name)}
                draggable="true"
                ondragstart={(e) => onVariableDragStart(e, name, cfg.datatype)}
                title="{cfg.datatype} · drag into editor to insert"
              >
                <span class="grip" aria-hidden="true">⋮⋮</span>
                <span class="badge type">{cfg.datatype.slice(0, 4)}</span>
                <span class="name">{name}</span>
              </button>
            </li>
          {:else}
            <li class="empty">No variables</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("types")}
          aria-expanded={sections.types}
        >
          <span class="chevron" class:open={sections.types}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Types</span>
          <span class="count">{typeEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={newTypeTab}
          title="New type"
          aria-label="New type"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.types}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each typeEntries as tmpl (tmpl.name)}
            <li>
              <button
                type="button"
                class="item"
                class:selected={workspaceSelection.isSelected(
                  "type",
                  tmpl.name,
                )}
                onclick={() => workspaceSelection.select("type", tmpl.name)}
                title={tmpl.description ?? `${tmpl.fields.length} field(s)`}
              >
                <span class="t-icon">T</span>
                <span class="name">{tmpl.name}</span>
                <span class="meta">{tmpl.fields.length}</span>
              </button>
            </li>
          {:else}
            <li class="empty">No types</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("tasks")}
          aria-expanded={sections.tasks}
        >
          <span class="chevron" class:open={sections.tasks}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Tasks</span>
          <span class="count">{taskEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={() => onCreate?.("task")}
          title="New task"
          aria-label="New task"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.tasks}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each taskEntries as task (task.name)}
            <li class="task-row">
              <button
                type="button"
                class="item"
                class:selected={workspaceSelection.isSelected(
                  "task",
                  task.name,
                )}
                onclick={() => workspaceSelection.select("task", task.name)}
                title="{task.scanRateMs}ms · {task.programRef || 'no program'}"
              >
                <span class="badge rate">{task.scanRateMs}ms</span>
                <span class="name">{task.name}</span>
              </button>
              <button
                type="button"
                class="task-toggle"
                class:on={task.enabled}
                onclick={(e) => toggleTaskEnabled(task, e)}
                disabled={togglingTask === task.name}
                role="switch"
                aria-checked={task.enabled}
                aria-label={task.enabled
                  ? `Disable task ${task.name}`
                  : `Enable task ${task.name}`}
                title={task.enabled ? "Disable task" : "Enable task"}
              >
                <span class="task-toggle-thumb"></span>
              </button>
            </li>
          {:else}
            <li class="empty">No tasks</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("programs")}
          aria-expanded={sections.programs}
        >
          <span class="chevron" class:open={sections.programs}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Functions</span>
          <span class="count">{programEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={newProgramTab}
          title="New function"
          aria-label="New function"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.programs}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each programEntries as program (program.name)}
            <li>
              <button
                type="button"
                class="item"
                class:selected={workspaceSelection.isSelected(
                  "program",
                  program.name,
                )}
                onclick={() =>
                  workspaceSelection.select("program", program.name)}
                title={program.language}
              >
                <span class="badge lang">{languageLabel(program.language)}</span>
                <span class="name">{program.name}</span>
                {#if program.tags && program.tags.length > 0}
                  <span class="item-tags">
                    {#each program.tags as tag (tag)}
                      <span class="item-tag">{tag}</span>
                    {/each}
                  </span>
                {/if}
              </button>
            </li>
          {:else}
            <li class="empty">No functions</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("tests")}
          aria-expanded={sections.tests}
        >
          <span class="chevron" class:open={sections.tests}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Tests</span>
          <span class="count">{testEntries.length}</span>
        </button>
        {#if tests.length > 0}
          <button
            type="button"
            class="add-btn"
            onclick={() => onRunAllTests?.()}
            disabled={testsRunning}
            title="Run all tests"
            aria-label="Run all tests"
          >
            <span class="play-icon" aria-hidden="true">▶</span>
          </button>
        {/if}
        <button
          type="button"
          class="add-btn"
          onclick={newTestTab}
          title="New test"
          aria-label="New test"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.tests}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each testEntries as test (test.name)}
            <li>
              <button
                type="button"
                class="item"
                class:selected={workspaceSelection.isSelected(
                  "test",
                  test.name,
                )}
                onclick={() => workspaceSelection.select("test", test.name)}
                title={test.lastResult?.message ?? "never run"}
              >
                <span
                  class="status-dot"
                  class:pass={testDotClass(test) === "pass"}
                  class:fail={testDotClass(test) === "fail"}
                  class:error={testDotClass(test) === "error"}
                ></span>
                <span class="name">{test.name}</span>
                {#if test.tags && test.tags.length > 0}
                  <span class="item-tags">
                    {#each test.tags as tag (tag)}
                      <span class="item-tag">{tag}</span>
                    {/each}
                  </span>
                {/if}
                {#if test.lastResult}
                  <span class="meta">{test.lastResult.durationMs}ms</span>
                {/if}
              </button>
            </li>
          {:else}
            <li class="empty">No tests</li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
</div>

<style lang="scss">
  .navigator {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .filter-wrap {
    padding: 0.5rem 0.625rem;
    border-bottom: 1px solid var(--theme-border);
  }

  .filter-input {
    width: 100%;
    padding: 0.3125rem 0.5rem;
    font-size: 0.75rem;
    background: var(--theme-background);
    color: var(--theme-text);
    border: 1px solid var(--theme-border);
    border-radius: 0.25rem;

    &:focus {
      outline: none;
      border-color: var(--theme-primary);
    }
  }

  .tag-filter {
    display: flex;
    flex-wrap: wrap;
    gap: 0.1875rem;
    margin-top: 0.375rem;
  }

  .tag-chip {
    padding: 0.0625rem 0.375rem;
    font-family: var(--font-mono, monospace);
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: 0.625rem;
    cursor: pointer;

    &:hover {
      color: var(--theme-text);
      background: var(--theme-surface);
    }

    &.active {
      color: var(--theme-primary);
      background: color-mix(in srgb, var(--theme-primary) 14%, transparent);
      border-color: color-mix(in srgb, var(--theme-primary) 40%, var(--theme-border));
    }
  }

  .item-tags {
    display: inline-flex;
    flex-shrink: 0;
    gap: 0.1875rem;
  }

  .item-tag {
    padding: 0 0.25rem;
    font-family: var(--font-mono, monospace);
    font-size: 0.625rem;
    color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    border-radius: 0.1875rem;
  }

  .sections {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .section {
    border-bottom: 1px solid var(--theme-border);
  }

  .section-header-row {
    display: flex;
    align-items: stretch;
  }

  .section-header {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    flex: 1;
    min-width: 0;
    padding: 0.375rem 0.5rem;
    background: transparent;
    border: none;
    border-radius: 0;
    cursor: pointer;
    color: var(--theme-text);
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    text-align: left;

    &:hover {
      background: var(--theme-surface);
    }
  }

  .add-btn {
    aspect-ratio: 1;
    border-radius: 0;
    flex-shrink: 0;
    width: 1.75rem;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    line-height: 1;
    cursor: pointer;
    opacity: 0.7;
    transition:
      opacity 0.12s ease,
      color 0.12s ease,
      background 0.12s ease;

    &:hover {
      opacity: 1;
      color: var(--theme-text);
      background: var(--theme-surface);
    }

    &:focus-visible {
      opacity: 1;
      outline: 2px solid var(--theme-primary);
      outline-offset: -2px;
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

  .label {
    flex: 1;
  }

  .count {
    padding: 0.0625rem 0.375rem;
    font-size: 0.6875rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    background: var(--theme-surface);
    border-radius: 0.625rem;
  }

  .items {
    list-style: none;
    margin: 0;
    padding: 0 0 0.25rem 0;
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

      .grip {
        opacity: 0.5;
      }
    }

    &.selected {
      background: color-mix(in srgb, var(--theme-primary) 18%, transparent);
      color: var(--theme-text);
    }

    &.draggable {
      cursor: grab;

      &:active {
        cursor: grabbing;
      }
    }
  }

  .grip {
    width: 0.75rem;
    flex-shrink: 0;
    color: var(--theme-text-muted);
    font-size: 0.625rem;
    letter-spacing: -0.1em;
    opacity: 0;
    transition: opacity 0.12s ease;
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

  .badge.lang {
    color: var(--theme-primary);
  }

  .t-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.125rem;
    height: 1.125rem;
    border-radius: 2px;
    font-size: 0.625rem;
    font-weight: 700;
    background: var(--badge-purple-bg);
    color: var(--badge-purple-text);
    flex-shrink: 0;
  }

  .badge.device {
    color: var(--theme-warning, #f59e0b);
  }

  .meta {
    flex-shrink: 0;
    color: var(--theme-text-muted);
    font-size: 0.75rem;

    &.off {
      opacity: 0.4;
    }
  }

  .task-row {
    display: flex;
    align-items: center;

    .item {
      flex: 1;
      min-width: 0;
    }
  }

  .task-toggle {
    flex-shrink: 0;
    position: relative;
    width: 1.75rem;
    height: 0.875rem;
    margin-right: 0.5rem;
    padding: 0;
    background: var(--theme-border);
    border: 0;
    border-radius: 0.4375rem;
    cursor: pointer;
    transition: background 0.15s ease;

    &:hover:not(:disabled) {
      background: color-mix(in srgb, var(--theme-text-muted) 40%, var(--theme-border));
    }

    &.on {
      background: var(--theme-primary);

      &:hover:not(:disabled) {
        background: color-mix(in srgb, var(--theme-primary) 80%, black);
      }

      .task-toggle-thumb {
        transform: translateX(0.875rem);
        background: var(--theme-on-primary, white);
      }
    }

    &:disabled {
      opacity: 0.6;
      cursor: not-allowed;
    }

    &:focus-visible {
      outline: 2px solid var(--theme-primary);
      outline-offset: 2px;
    }
  }

  .task-toggle-thumb {
    position: absolute;
    top: 0.125rem;
    left: 0.125rem;
    width: 0.625rem;
    height: 0.625rem;
    background: var(--theme-text);
    border-radius: 50%;
    transition: transform 0.15s ease, background 0.15s ease;
  }

  .empty {
    padding: 0.375rem 1rem;
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    font-style: italic;
  }

  .status-dot {
    flex-shrink: 0;
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 50%;
    background: var(--theme-border);

    &.pass {
      background: var(--theme-success, #10b981);
    }
    &.fail {
      background: var(--theme-danger, #ef4444);
    }
    &.error {
      background: var(--theme-warning, #f59e0b);
    }
  }

  .play-icon {
    font-size: 0.625rem;
    line-height: 1;
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

  .chevron.small {
    font-size: 0.625rem;
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

  .refresh-icon {
    display: inline-block;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .browse-progress {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    margin-right: 0.125rem;
    color: var(--badge-teal-text, var(--theme-primary));
    font-size: 0.625rem;
    font-family: 'IBM Plex Mono', monospace;
    line-height: 1;

    &.ok { color: var(--theme-success, var(--badge-teal-text)); }
    &.err { color: var(--theme-danger, #e5484d); }
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

    &:hover { color: var(--theme-danger, #e5484d); }
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
</style>
