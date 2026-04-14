<script lang="ts">
  import type { GatewayConfig, GatewayUdtTemplate, GatewayUdtVariable, BrowseCache, GatewayBrowseState, DeadBandConfig } from '$lib/types/gateway';
  import { apiPut, apiPost } from '$lib/api/client';
  import { subscribe } from '$lib/api/subscribe';
  import { invalidateAll } from '$app/navigation';
  import { fly, slide } from 'svelte/transition';
  import { untrack } from 'svelte';
  import { state as saltState } from '@joyautomation/salt';
  import { ArrowPath, ChevronRight, ExclamationTriangle, PencilSquare, Signal } from '@joyautomation/salt/icons';
  import { mapDatatype, type RbeState, type InstanceInfo, type ActiveSection } from './utils';
  import TemplateDefaultsTab from './TemplateDefaultsTab.svelte';
  import InstancesTab from './InstancesTab.svelte';
  import AtomicTagsTab from './AtomicTagsTab.svelte';

  let { gatewayConfig, browseCaches, browseStates, error }: {
    gatewayConfig: GatewayConfig | null;
    browseCaches: BrowseCache[];
    browseStates: GatewayBrowseState[];
    error: string | null;
  } = $props();

  let activeSection: ActiveSection | null = $state<ActiveSection | null>(null);
  let activeTab: 'template' | 'instances' = $state('instances');
  let templatesExpanded = $state(false);

  // ── Derived data ──
  const templates = $derived(gatewayConfig?.udtTemplates ?? []);
  const allConfigInstances = $derived(gatewayConfig?.udtVariables ?? []);
  const devices = $derived(gatewayConfig?.devices ?? []);

  // ── UDT state ──
  let expandedInstances: Set<string> = $state(new Set());
  let editingCell: string | null = $state(null);
  let editDraft = $state('');

  // Guard to prevent re-initialization from invalidateAll() wiping working state
  let needsInit = $state(true);
  let initialized = $state(false);

  // Working copies for dirty tracking
  let workingTemplates: Map<string, GatewayUdtTemplate> = $state(new Map());
  let workingInstanceOverrides: Map<string, Record<string, DeadBandConfig>> = $state(new Map());

  // UDT member exclusion (MQTT toggle)
  let excludedUdtMembers: Record<string, Set<string>> = $state({});

  // UDT instance publish state
  let checkedUdtInstances: Set<string> = $state(new Set());
  const publishedUdtInstances = $derived(new Set(
    (gatewayConfig?.udtVariables ?? []).map(v => `${v.deviceId}::${v.tag}`)
  ));

  // ── Atomic tag state ──
  let checkedAtomicTags: Set<string> = $state(new Set());
  const publishedAtomicTags = $derived(new Set(
    (gatewayConfig?.variables ?? []).map(v => `${v.deviceId}::${v.tag}`)
  ));

  // ── History enable state ──
  let checkedHistoryAtomicTags: Set<string> = $state(new Set());
  const publishedHistoryAtomicTags = $derived(new Set(
    (gatewayConfig?.variables ?? []).filter(v => v.historyEnabled).map(v => `${v.deviceId}::${v.tag}`)
  ));
  let checkedHistoryUdtInstances: Set<string> = $state(new Set());
  const publishedHistoryUdtInstances = $derived(new Set(
    (gatewayConfig?.udtVariables ?? []).filter(v => v.historyEnabled).map(v => `${v.deviceId}::${v.tag}`)
  ));

  let rbeOverrides: Map<string, RbeState> = $state(new Map());
  let initialRbeOverrides: Map<string, RbeState> = $state(new Map());

  // Template name overrides: deviceId → { originalName → overrideName }
  let workingTemplateOverrides: Map<string, Record<string, string>> = $state(new Map());

  /** Resolve a template name for a device, applying any override */
  function resolveTemplateName(deviceId: string, originalName: string): string {
    return workingTemplateOverrides.get(deviceId)?.[originalName] ?? originalName;
  }

  /** Get all template names (after overrides) across all devices for a given browse original name */
  function allResolvedNames(originalName: string): Set<string> {
    const names = new Set<string>();
    for (const [deviceId, overrides] of workingTemplateOverrides) {
      if (overrides[originalName]) names.add(overrides[originalName]);
    }
    names.add(originalName); // also include the original
    return names;
  }

  // ── SNMP root OID presets ──
  const snmpRootOidPresets = [
    { label: 'MIB-2 (mgmt)', value: '.1.3.6.1.2.1' },
    { label: 'System', value: '.1.3.6.1.2.1.1' },
    { label: 'Interfaces', value: '.1.3.6.1.2.1.2' },
    { label: 'IF-MIB (ext)', value: '.1.3.6.1.2.1.31' },
    { label: 'Enterprise', value: '.1.3.6.1.4.1' },
    { label: 'Full tree', value: '.1.3.6.1' },
  ];
  let snmpRootOids: Map<string, string> = $state(new Map());

  // ── Browse state ──
  let liveProgress: Map<string, GatewayBrowseState> = $state(new Map());
  let saving = $state(false);

  // Tracks locally-started browses for subscription management (set in refreshDevice, cleared on terminal)
  // This is separate from liveProgress so that progress updates don't trigger subscription re-creation
  let localBrowseSubs: Map<string, { browseId: string; protocol: string }> = $state(new Map());

  const activeBrowseStates = $derived.by((): Map<string, GatewayBrowseState> => {
    const merged = new Map<string, GatewayBrowseState>();
    for (const s of browseStates ?? []) merged.set(s.deviceId, s);
    for (const [k, v] of liveProgress) merged.set(k, v);
    return merged;
  });

  // ── Initialize all state from config ──
  function initializeState() {
    // Load template name overrides from device configs
    const overrideMap = new Map<string, Record<string, string>>();
    for (const device of devices) {
      if (device.templateNameOverrides && Object.keys(device.templateNameOverrides).length > 0) {
        overrideMap.set(device.deviceId, { ...device.templateNameOverrides });
      }
    }
    workingTemplateOverrides = overrideMap;

    // Working template copies (configured + browse-only, applying overrides)
    const tMap = new Map<string, GatewayUdtTemplate>();
    for (const t of templates) {
      tMap.set(t.name, JSON.parse(JSON.stringify(t)));
    }
    for (const cache of browseCaches ?? []) {
      for (const udt of cache.udts) {
        const resolved = resolveTemplateName(cache.deviceId, udt.name);
        if (!tMap.has(resolved)) {
          tMap.set(resolved, {
            name: resolved,
            version: '1.0',
            members: udt.members.map(m => ({
              name: m.name,
              datatype: mapDatatype(m.datatype),
              templateRef: m.udtType ?? null,
            })),
          });
        }
      }
    }
    workingTemplates = tMap;

    // Auto-select first template or first device
    if (!activeSection) {
      const firstTemplate = [...tMap.keys()][0];
      if (firstTemplate) {
        activeSection = { kind: 'template', templateName: firstTemplate };
      } else if (browseCaches.length > 0) {
        activeSection = { kind: 'atomic', deviceId: browseCaches[0].deviceId };
      }
    }

    // Instance overrides
    const iMap = new Map<string, Record<string, DeadBandConfig>>();
    for (const inst of allConfigInstances) {
      if (inst.memberDeadbands && Object.keys(inst.memberDeadbands).length > 0) {
        iMap.set(inst.id, JSON.parse(JSON.stringify(inst.memberDeadbands)));
      }
    }
    workingInstanceOverrides = iMap;

    // Excluded UDT members (from browse vs config template members)
    const excl: Record<string, Set<string>> = {};
    for (const cache of browseCaches ?? []) {
      for (const udt of cache.udts) {
        const resolved = resolveTemplateName(cache.deviceId, udt.name);
        const cfgTmpl = templates.find(t => t.name === resolved);
        if (cfgTmpl) {
          const cfgMemberNames = new Set(cfgTmpl.members.map(m => m.name));
          const excluded = new Set<string>();
          for (const m of udt.members) {
            if (!cfgMemberNames.has(m.name)) excluded.add(m.name);
          }
          if (excluded.size > 0) excl[resolved] = excluded;
        }
      }
    }
    excludedUdtMembers = excl;

    // UDT instance publish state
    checkedUdtInstances = new Set(publishedUdtInstances);

    // Atomic tag state
    checkedAtomicTags = new Set(publishedAtomicTags);

    // History state
    checkedHistoryAtomicTags = new Set(publishedHistoryAtomicTags);
    checkedHistoryUdtInstances = new Set(publishedHistoryUdtInstances);

    // Atomic RBE overrides
    const overrides = new Map<string, RbeState>();
    for (const v of gatewayConfig?.variables ?? []) {
      const key = `${v.deviceId}::${v.tag}`;
      if (v.deadband || v.disableRBE) {
        overrides.set(key, {
          mode: v.deadband ? 'custom' : 'default',
          ...(v.deadband ? { deadband: { ...v.deadband } } : {}),
          ...(v.disableRBE ? { disableRBE: true } : {}),
        });
      }
    }
    rbeOverrides = new Map(overrides);
    initialRbeOverrides = new Map(overrides);

    // Reset transient state
    expandedInstances = new Set();
    editingCell = null;
  }

  // Only initialize on first mount or when explicitly requested (save/discard).
  // When browse data arrives (invalidateAll from subscription), merge new templates
  // without resetting working state.
  $effect(() => {
    // Read dependencies so Svelte tracks them
    templates; allConfigInstances; gatewayConfig;
    const caches = browseCaches;

    if (needsInit) {
      initializeState();
      needsInit = false;
      initialized = true;
      return;
    }

    // Merge any new browse-only templates into working state without resetting
    untrack(() => {
      let merged = false;
      let next = workingTemplates;
      for (const cache of caches ?? []) {
        for (const udt of cache.udts) {
          const resolved = resolveTemplateName(cache.deviceId, udt.name);
          if (!next.has(resolved)) {
            if (!merged) { next = new Map(next); merged = true; }
            next.set(resolved, {
              name: resolved,
              version: '1.0',
              members: udt.members.map(m => ({
                name: m.name,
                datatype: mapDatatype(m.datatype),
                templateRef: m.udtType ?? null,
              })),
            });
          }
        }
      }
      if (merged) workingTemplates = next;
    });
  });

  // Subscribe to browse progress
  // Depends on browseStates (server prop) + localBrowseSubs (set by refreshDevice).
  // Does NOT depend on liveProgress, so progress updates won't tear down/re-create subscriptions.
  $effect(() => {
    const cleanups: (() => void)[] = [];

    // Collect devices that need subscriptions
    const toSubscribe = new Map<string, { browseId: string; protocol: string }>();
    for (const s of browseStates ?? []) {
      if (s.status === 'browsing') {
        toSubscribe.set(s.deviceId, { browseId: s.browseId, protocol: s.protocol });
      }
    }
    for (const [did, info] of localBrowseSubs) {
      toSubscribe.set(did, info);
    }

    for (const [deviceId, info] of toSubscribe) {
      const cleanup = subscribe<{
        browseId: string; deviceId: string; phase: string;
        discoveredCount: number; totalCount: number; message: string; timestamp: string;
      }>(
        `/gateways/gateway/browse/${info.browseId}/progress`,
        (p) => {
          const isTerminal = p.phase === 'completed' || p.phase === 'failed' ||
            (p.totalCount > 0 && p.discoveredCount >= p.totalCount);
          const updated = new Map(liveProgress);
          updated.set(deviceId, {
            deviceId, browseId: info.browseId, protocol: info.protocol,
            status: isTerminal ? (p.phase === 'failed' ? 'failed' : 'completed') as 'completed' | 'failed' : 'browsing',
            phase: p.phase,
            discoveredCount: p.discoveredCount,
            totalCount: p.totalCount,
            message: p.message,
            startedAt: '',
            updatedAt: p.timestamp,
          });
          liveProgress = updated;
          if (isTerminal) {
            // Remove from local subs so we don't re-subscribe
            const next = new Map(localBrowseSubs);
            next.delete(deviceId);
            localBrowseSubs = next;
            // Refresh data from server
            setTimeout(() => invalidateAll(), 500);
            // Clear progress indicator after a brief "done" display
            const capturedDeviceId = deviceId;
            setTimeout(() => {
              const cleared = new Map(liveProgress);
              cleared.delete(capturedDeviceId);
              liveProgress = cleared;
            }, 2000);
          }
        }
      );
      cleanups.push(cleanup);
    }
    return () => cleanups.forEach(fn => fn());
  });

  // ── Derived for active template ──
  const activeTemplateName = $derived(
    activeSection?.kind === 'template' ? activeSection.templateName : null
  );
  const activeTemplate = $derived(
    activeTemplateName ? workingTemplates.get(activeTemplateName) : null
  );
  const analogMembers = $derived(
    (activeTemplate?.members ?? []).filter(m => m.datatype === 'number')
  );
  const enabledAnalogMembers = $derived(
    analogMembers.filter(m => !(excludedUdtMembers[activeTemplateName ?? '']?.has(m.name)))
  );
  const activeDeviceId = $derived(
    activeSection?.kind === 'atomic' ? activeSection.deviceId : null
  );

  const atomicDeviceDeadband = $derived.by((): DeadBandConfig | null => {
    if (!activeDeviceId) return null;
    const device = devices.find(d => d.deviceId === activeDeviceId);
    return device?.deadband ?? null;
  });

  // Device deadband for active template (from first device that uses this template)
  const templateDeviceDeadband = $derived.by((): DeadBandConfig | null => {
    if (!activeTemplateName) return null;
    for (const cache of browseCaches ?? []) {
      const structTags = cache.structTags ?? {};
      if (Object.values(structTags).some(u => resolveTemplateName(cache.deviceId, u) === activeTemplateName)) {
        const device = devices.find(d => d.deviceId === cache.deviceId);
        return device?.deadband ?? null;
      }
    }
    return null;
  });

  // Build full instance list for active template (from browse + config)
  const activeInstances = $derived.by((): InstanceInfo[] => {
    if (!activeTemplateName) return [];
    const instances: InstanceInfo[] = [];
    const seen = new Set<string>();

    // From browse caches
    for (const cache of browseCaches ?? []) {
      const structTags = cache.structTags ?? {};
      for (const [tag, udtName] of Object.entries(structTags)) {
        // Skip raw OID keys (used only for atomic tag filtering, not instances)
        if (tag.startsWith('.')) continue;
        if (resolveTemplateName(cache.deviceId, udtName) !== activeTemplateName) continue;
        const key = `${cache.deviceId}::${tag}`;
        if (seen.has(key)) continue;
        seen.add(key);
        instances.push({
          id: tag,
          deviceId: cache.deviceId,
          tag,
          published: checkedUdtInstances.has(key),
        });
      }
    }

    // From config (in case some aren't in browse cache)
    for (const inst of allConfigInstances) {
      if (inst.templateName !== activeTemplateName) continue;
      const key = `${inst.deviceId}::${inst.tag}`;
      if (seen.has(key)) continue;
      seen.add(key);
      instances.push({
        id: inst.id,
        deviceId: inst.deviceId,
        tag: inst.tag,
        published: checkedUdtInstances.has(key),
      });
    }

    return instances.sort((a, b) => a.tag.localeCompare(b.tag));
  });

  // ── Dirty tracking ──
  const isDirty = $derived.by((): boolean => {
    if (!initialized) return false;

    // Atomic tag selection
    if (checkedAtomicTags.size !== publishedAtomicTags.size) return true;
    for (const key of checkedAtomicTags) { if (!publishedAtomicTags.has(key)) return true; }
    for (const key of publishedAtomicTags) { if (!checkedAtomicTags.has(key)) return true; }

    // History atomic tag selection
    if (checkedHistoryAtomicTags.size !== publishedHistoryAtomicTags.size) return true;
    for (const key of checkedHistoryAtomicTags) { if (!publishedHistoryAtomicTags.has(key)) return true; }
    for (const key of publishedHistoryAtomicTags) { if (!checkedHistoryAtomicTags.has(key)) return true; }

    // UDT instance selection
    if (checkedUdtInstances.size !== publishedUdtInstances.size) return true;
    for (const key of checkedUdtInstances) { if (!publishedUdtInstances.has(key)) return true; }
    for (const key of publishedUdtInstances) { if (!checkedUdtInstances.has(key)) return true; }

    // History UDT instance selection
    if (checkedHistoryUdtInstances.size !== publishedHistoryUdtInstances.size) return true;
    for (const key of checkedHistoryUdtInstances) { if (!publishedHistoryUdtInstances.has(key)) return true; }
    for (const key of publishedHistoryUdtInstances) { if (!checkedHistoryUdtInstances.has(key)) return true; }

    // Atomic RBE changes
    if (rbeOverrides.size !== initialRbeOverrides.size) return true;
    for (const [key, state] of rbeOverrides) {
      const initial = initialRbeOverrides.get(key);
      if (!initial) return true;
      if (state.mode !== initial.mode) return true;
      if (!!state.disableRBE !== !!initial.disableRBE) return true;
      if (state.mode === 'custom' && initial.mode === 'custom') {
        if (state.deadband?.value !== initial.deadband?.value ||
            state.deadband?.minTime !== initial.deadband?.minTime ||
            state.deadband?.maxTime !== initial.deadband?.maxTime) return true;
      }
    }
    for (const key of initialRbeOverrides.keys()) {
      if (!rbeOverrides.has(key)) return true;
    }

    // UDT template deadband changes
    for (const [name, working] of workingTemplates) {
      const original = templates.find(t => t.name === name);
      if (!original) continue;
      for (const wm of working.members) {
        const om = original.members.find(m => m.name === wm.name);
        if (!om) continue;
        const wdb = wm.defaultDeadband;
        const odb = om.defaultDeadband;
        if ((!wdb && odb) || (wdb && !odb)) return true;
        if (wdb && odb && (wdb.value !== odb.value || wdb.minTime !== odb.minTime || wdb.maxTime !== odb.maxTime || !!wdb.disableRBE !== !!odb.disableRBE)) return true;
      }
    }

    // UDT instance override changes
    for (const inst of allConfigInstances) {
      const origDb = inst.memberDeadbands ?? {};
      const workDb = workingInstanceOverrides.get(inst.id) ?? {};
      const origKeys = Object.keys(origDb);
      const workKeys = Object.keys(workDb);
      if (origKeys.length !== workKeys.length) return true;
      for (const k of workKeys) {
        if (!origDb[k]) return true;
        if (workDb[k].value !== origDb[k].value || workDb[k].minTime !== origDb[k].minTime || workDb[k].maxTime !== origDb[k].maxTime || !!workDb[k].disableRBE !== !!origDb[k].disableRBE) return true;
      }
    }

    // UDT member exclusion changes
    for (const cache of browseCaches ?? []) {
      for (const udt of cache.udts) {
        const resolved = resolveTemplateName(cache.deviceId, udt.name);
        const cfgTmpl = templates.find(t => t.name === resolved);
        if (!cfgTmpl) continue;
        const cfgMemberNames = new Set(cfgTmpl.members.map(m => m.name));
        const currentExcluded = excludedUdtMembers[resolved] ?? new Set();
        for (const m of udt.members) {
          const wasExcluded = !cfgMemberNames.has(m.name);
          const isExcluded = currentExcluded.has(m.name);
          if (wasExcluded !== isExcluded) return true;
        }
      }
    }

    // Template name override changes
    for (const device of devices) {
      const currentOverrides = device.templateNameOverrides ?? {};
      const workingOverrides = workingTemplateOverrides.get(device.deviceId) ?? {};
      if (JSON.stringify(currentOverrides) !== JSON.stringify(workingOverrides)) return true;
    }

    return false;
  });

  // ── Granular dirty tracking (per-row + sidebar breadcrumbs) ──

  /** Atomic tag keys that have been modified */
  const dirtyAtomicKeys = $derived.by((): Set<string> => {
    if (!initialized) return new Set();
    const dirty = new Set<string>();
    for (const key of checkedAtomicTags) { if (!publishedAtomicTags.has(key)) dirty.add(key); }
    for (const key of publishedAtomicTags) { if (!checkedAtomicTags.has(key)) dirty.add(key); }
    for (const key of checkedHistoryAtomicTags) { if (!publishedHistoryAtomicTags.has(key)) dirty.add(key); }
    for (const key of publishedHistoryAtomicTags) { if (!checkedHistoryAtomicTags.has(key)) dirty.add(key); }
    for (const [key, state] of rbeOverrides) {
      const initial = initialRbeOverrides.get(key);
      if (!initial) { dirty.add(key); continue; }
      if (state.mode !== initial.mode || !!state.disableRBE !== !!initial.disableRBE) { dirty.add(key); continue; }
      if (state.mode === 'custom' && initial.mode === 'custom') {
        if (state.deadband?.value !== initial.deadband?.value ||
            state.deadband?.minTime !== initial.deadband?.minTime ||
            state.deadband?.maxTime !== initial.deadband?.maxTime) dirty.add(key);
      }
    }
    for (const key of initialRbeOverrides.keys()) { if (!rbeOverrides.has(key)) dirty.add(key); }
    return dirty;
  });

  /** Template members with changes: templateName → Set of dirty member names */
  const dirtyTemplateMemberMap = $derived.by((): Map<string, Set<string>> => {
    if (!initialized) return new Map();
    const result = new Map<string, Set<string>>();
    // Deadband changes
    for (const [name, working] of workingTemplates) {
      const original = templates.find(t => t.name === name);
      if (!original) continue;
      for (const wm of working.members) {
        const om = original.members.find(m => m.name === wm.name);
        if (!om) continue;
        const wdb = wm.defaultDeadband;
        const odb = om.defaultDeadband;
        if ((!wdb && odb) || (wdb && !odb) ||
            (wdb && odb && (wdb.value !== odb.value || wdb.minTime !== odb.minTime || wdb.maxTime !== odb.maxTime || !!wdb.disableRBE !== !!odb.disableRBE))) {
          if (!result.has(name)) result.set(name, new Set());
          result.get(name)!.add(wm.name);
        }
      }
    }
    // Exclusion changes
    for (const cache of browseCaches ?? []) {
      for (const udt of cache.udts) {
        const resolved = resolveTemplateName(cache.deviceId, udt.name);
        const cfgTmpl = templates.find(t => t.name === resolved);
        if (!cfgTmpl) continue;
        const cfgMemberNames = new Set(cfgTmpl.members.map(m => m.name));
        const currentExcluded = excludedUdtMembers[resolved] ?? new Set();
        for (const m of udt.members) {
          if ((!cfgMemberNames.has(m.name)) !== currentExcluded.has(m.name)) {
            if (!result.has(resolved)) result.set(resolved, new Set());
            result.get(resolved)!.add(m.name);
          }
        }
      }
    }
    return result;
  });

  /** Instance keys with changes (publish toggle or override changes) */
  const dirtyInstanceKeys = $derived.by((): Set<string> => {
    if (!initialized) return new Set();
    const dirty = new Set<string>();
    // MQTT toggle
    for (const key of checkedUdtInstances) { if (!publishedUdtInstances.has(key)) dirty.add(key); }
    for (const key of publishedUdtInstances) { if (!checkedUdtInstances.has(key)) dirty.add(key); }
    // History toggle
    for (const key of checkedHistoryUdtInstances) { if (!publishedHistoryUdtInstances.has(key)) dirty.add(key); }
    for (const key of publishedHistoryUdtInstances) { if (!checkedHistoryUdtInstances.has(key)) dirty.add(key); }
    // Override changes - map instance.id → publish key
    for (const inst of allConfigInstances) {
      const pubKey = `${inst.deviceId}::${inst.tag}`;
      const origDb = inst.memberDeadbands ?? {};
      const workDb = workingInstanceOverrides.get(inst.id) ?? {};
      const origKeys = Object.keys(origDb);
      const workKeys = Object.keys(workDb);
      if (origKeys.length !== workKeys.length) { dirty.add(pubKey); continue; }
      for (const k of workKeys) {
        if (!origDb[k] || workDb[k].value !== origDb[k].value || workDb[k].minTime !== origDb[k].minTime || workDb[k].maxTime !== origDb[k].maxTime || !!workDb[k].disableRBE !== !!origDb[k].disableRBE) {
          dirty.add(pubKey); break;
        }
      }
    }
    return dirty;
  });

  /** Per-instance member dirty tracking: instanceId → Set of dirty member names */
  const dirtyInstanceMembers = $derived.by((): Map<string, Set<string>> => {
    if (!initialized) return new Map();
    const result = new Map<string, Set<string>>();
    for (const inst of allConfigInstances) {
      const origDb = inst.memberDeadbands ?? {};
      const workDb = workingInstanceOverrides.get(inst.id) ?? {};
      const allKeys = new Set([...Object.keys(origDb), ...Object.keys(workDb)]);
      for (const k of allKeys) {
        const o = origDb[k];
        const w = workDb[k];
        if ((!o && w) || (o && !w) ||
            (o && w && (o.value !== w.value || o.minTime !== w.minTime || o.maxTime !== w.maxTime || !!o.disableRBE !== !!w.disableRBE))) {
          if (!result.has(inst.id)) result.set(inst.id, new Set());
          result.get(inst.id)!.add(k);
        }
      }
    }
    return result;
  });

  /** Sidebar: which nav sections have dirty children. Keys match sidebar format: "t::Name" or "a::deviceId" */
  const dirtySidebarSections = $derived.by((): Set<string> => {
    const dirty = new Set<string>();
    // Templates: dirty if any member changed or any instance of that template changed
    for (const name of dirtyTemplateMemberMap.keys()) dirty.add(`t::${name}`);
    // Instances: map dirty instance keys to their template names
    // Use config instances directly (they have templateName), with browse cache as fallback
    for (const inst of allConfigInstances) {
      const pubKey = `${inst.deviceId}::${inst.tag}`;
      if (dirtyInstanceKeys.has(pubKey)) {
        dirty.add(`t::${inst.templateName}`);
      }
    }
    // Also check browse cache for instances not yet in config (newly checked)
    for (const key of dirtyInstanceKeys) {
      const [deviceId, tag] = key.split('::');
      for (const cache of browseCaches ?? []) {
        if (cache.deviceId !== deviceId) continue;
        const structTags = cache.structTags ?? {};
        if (tag && structTags[tag]) dirty.add(`t::${resolveTemplateName(cache.deviceId, structTags[tag])}`);
      }
    }
    // Atomic: group by device
    for (const key of dirtyAtomicKeys) {
      const deviceId = key.split('::')[0];
      dirty.add(`a::${deviceId}`);
    }
    return dirty;
  });

  /** Sidebar: which templates have at least one MQTT-published instance */
  const publishedTemplateSections = $derived.by((): Set<string> => {
    const published = new Set<string>();
    for (const inst of allConfigInstances) {
      published.add(inst.templateName);
    }
    return published;
  });

  /** Sidebar: which devices have at least one MQTT-published atomic tag */
  const publishedAtomicDevices = $derived.by((): Set<string> => {
    const published = new Set<string>();
    for (const v of gatewayConfig?.variables ?? []) {
      published.add(v.deviceId);
    }
    return published;
  });

  /** Sidebar: which devices have any dirty atomic children */
  const dirtyDevices = $derived.by((): Set<string> => {
    const dirty = new Set<string>();
    for (const key of dirtyAtomicKeys) {
      dirty.add(key.split('::')[0]);
    }
    return dirty;
  });

  // ── Cascade helper: instance override → template default → device deadband ──
  function getEffectiveRbe(memberName: string, instanceId: string): { deadband: DeadBandConfig; inherited: boolean } {
    const instOverrides = workingInstanceOverrides.get(instanceId);
    if (instOverrides?.[memberName]) {
      return { deadband: instOverrides[memberName], inherited: false };
    }
    const tmplName = activeTemplateName;
    const tmpl = tmplName ? workingTemplates.get(tmplName) : null;
    const member = tmpl?.members.find(m => m.name === memberName);
    if (member?.defaultDeadband) {
      return { deadband: member.defaultDeadband, inherited: true };
    }
    if (templateDeviceDeadband) {
      return { deadband: templateDeviceDeadband, inherited: true };
    }
    return { deadband: { value: 0 }, inherited: true };
  }

  function getOverrideCount(instanceId: string): number {
    return Object.keys(workingInstanceOverrides.get(instanceId) ?? {}).length;
  }

  // ── Template rename (conflict resolution) ──
  let renameDrafts: Map<string, string> = $state(new Map());

  /** Rebuild workingTemplates from browse caches + config, using current overrides */
  function rebuildWorkingTemplates() {
    const tMap = new Map<string, GatewayUdtTemplate>();
    // Start with configured templates
    for (const t of templates) {
      tMap.set(t.name, JSON.parse(JSON.stringify(t)));
    }
    // Add browse-discovered templates (with overrides applied)
    for (const cache of browseCaches ?? []) {
      for (const udt of cache.udts) {
        const resolved = resolveTemplateName(cache.deviceId, udt.name);
        if (!tMap.has(resolved)) {
          tMap.set(resolved, {
            name: resolved,
            version: '1.0',
            members: udt.members.map(m => ({
              name: m.name,
              datatype: mapDatatype(m.datatype),
              templateRef: m.udtType ?? null,
            })),
          });
        }
      }
    }
    // Preserve deadband settings from old workingTemplates where names match
    for (const [name, oldTmpl] of workingTemplates) {
      const newTmpl = tMap.get(name);
      if (newTmpl) {
        for (const om of oldTmpl.members) {
          if (om.defaultDeadband) {
            const nm = newTmpl.members.find(m => m.name === om.name);
            if (nm) nm.defaultDeadband = om.defaultDeadband;
          }
        }
      }
    }
    workingTemplates = tMap;
  }

  /** Apply all conflict renames for the current template at once */
  async function commitAllRenames(deviceOriginalNames: Array<{ deviceId: string; originalBrowseName: string }>) {
    // Build the proposed name map: deviceId → proposed new name
    const proposed = new Map<string, string>();
    for (const { deviceId, originalBrowseName } of deviceOriginalNames) {
      const draft = (renameDrafts.get(deviceId) ?? `${deviceId}_${originalBrowseName}`).trim();
      proposed.set(deviceId, draft || originalBrowseName);
    }

    // Collect all resolved names that WON'T be affected by this rename (other templates)
    const currentBrowseName = activeTemplateName ?? '';
    const otherNames = new Set<string>();
    for (const cache of browseCaches ?? []) {
      const structTags = cache.structTags ?? {};
      for (const udtName of new Set(Object.values(structTags))) {
        if (udtName === currentBrowseName || resolveTemplateName(cache.deviceId, udtName) === currentBrowseName) continue;
        otherNames.add(resolveTemplateName(cache.deviceId, udtName));
      }
    }

    // Validate: each proposed name must be unique among other templates AND among the proposed set
    const proposedNames = [...proposed.values()];
    const proposedSet = new Set(proposedNames);
    if (proposedSet.size !== proposedNames.length) {
      saltState.addNotification({ message: 'Template names must be unique — two devices have the same proposed name', type: 'error' });
      return;
    }
    for (const name of proposedNames) {
      if (otherNames.has(name)) {
        saltState.addNotification({ message: `Template name "${name}" conflicts with another template`, type: 'error' });
        return;
      }
    }

    // Check if anything actually changed
    let anyChanged = false;
    const oldResolved = currentBrowseName;
    for (const { deviceId, originalBrowseName } of deviceOriginalNames) {
      const newName = proposed.get(deviceId)!;
      if (newName !== oldResolved) anyChanged = true;
    }
    if (!anyChanged) return;

    // Apply all overrides
    const overridesNext = new Map(workingTemplateOverrides);
    for (const { deviceId, originalBrowseName } of deviceOriginalNames) {
      const newName = proposed.get(deviceId)!;
      if (newName !== originalBrowseName) {
        const devOverrides = { ...(overridesNext.get(deviceId) ?? {}), [originalBrowseName]: newName };
        overridesNext.set(deviceId, devOverrides);
      } else {
        // Remove override if set back to original
        const devOverrides = { ...(overridesNext.get(deviceId) ?? {}) };
        delete devOverrides[originalBrowseName];
        if (Object.keys(devOverrides).length === 0) overridesNext.delete(deviceId);
        else overridesNext.set(deviceId, devOverrides);
      }
    }
    workingTemplateOverrides = overridesNext;

    // Persist overrides to server immediately so they survive navigation
    for (const { deviceId } of deviceOriginalNames) {
      const overrides = overridesNext.get(deviceId) ?? {};
      const result = await apiPut(`/gateways/gateway/devices/${deviceId}/template-overrides`, { overrides });
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
        return;
      }
    }

    // Remap excludedUdtMembers keys
    for (const { deviceId, originalBrowseName } of deviceOriginalNames) {
      const finalName = resolveTemplateName(deviceId, originalBrowseName);
      if (oldResolved !== finalName && excludedUdtMembers[oldResolved]) {
        excludedUdtMembers[finalName] = excludedUdtMembers[oldResolved];
      }
    }
    delete excludedUdtMembers[oldResolved];

    // Rebuild all templates from browse data with updated overrides
    rebuildWorkingTemplates();

    // Navigate to the first renamed template
    const firstFinal = proposedNames[0];
    if (firstFinal && activeSection?.kind === 'template') {
      activeSection = { kind: 'template', templateName: firstFinal };
    }

    renameDrafts = new Map();

    // Update server-side device data so isDirty won't flag these overrides as unsaved
    await invalidateAll();
  }

  // ── Template mutations ──
  function setTemplateMemberDeadband(memberName: string, field: 'value' | 'minTime' | 'maxTime', val: number | null) {
    if (!activeTemplateName) return;
    const tmpl = workingTemplates.get(activeTemplateName);
    if (!tmpl) return;
    const memberIdx = tmpl.members.findIndex(m => m.name === memberName);
    if (memberIdx === -1) return;
    const member = tmpl.members[memberIdx];
    const existing = member.defaultDeadband ?? { value: templateDeviceDeadband?.value ?? 0 };
    const updated = { ...existing };
    if (field === 'value') updated.value = val ?? 0;
    else if (field === 'minTime') { if (val != null) updated.minTime = val; else delete updated.minTime; }
    else { if (val != null) updated.maxTime = val; else delete updated.maxTime; }
    // Create new objects so Svelte 5 detects the change (same-reference mutations aren't tracked)
    const newMembers = [...tmpl.members];
    newMembers[memberIdx] = { ...member, defaultDeadband: updated };
    const newTmpl = { ...tmpl, members: newMembers };
    const next = new Map(workingTemplates);
    next.set(activeTemplateName, newTmpl);
    workingTemplates = next;
  }

  function clearTemplateMemberDeadband(memberName: string) {
    if (!activeTemplateName) return;
    const tmpl = workingTemplates.get(activeTemplateName);
    if (!tmpl) return;
    const memberIdx = tmpl.members.findIndex(m => m.name === memberName);
    if (memberIdx === -1) return;
    const member = tmpl.members[memberIdx];
    const newMember = { ...member };
    delete newMember.defaultDeadband;
    const newMembers = [...tmpl.members];
    newMembers[memberIdx] = newMember;
    const newTmpl = { ...tmpl, members: newMembers };
    const next = new Map(workingTemplates);
    next.set(activeTemplateName, newTmpl);
    workingTemplates = next;
  }

  // ── Instance mutations ──
  function setInstanceOverride(instanceId: string, memberName: string, field: 'value' | 'minTime' | 'maxTime', val: number | null) {
    const overrides = { ...(workingInstanceOverrides.get(instanceId) ?? {}) };
    // Initialize from inherited values so min/max aren't blank on first override
    const inherited = getEffectiveRbe(memberName, instanceId).deadband;
    const existing = overrides[memberName] ?? { ...inherited };
    const updated = { ...existing };
    if (field === 'value') updated.value = val ?? 0;
    else if (field === 'minTime') { if (val != null) updated.minTime = val; else delete updated.minTime; }
    else { if (val != null) updated.maxTime = val; else delete updated.maxTime; }

    // Auto-clean if matches template default
    const tmplName = activeTemplateName;
    const tmpl = tmplName ? workingTemplates.get(tmplName) : null;
    const member = tmpl?.members.find(m => m.name === memberName);
    const dflt = member?.defaultDeadband;
    if (dflt && updated.value === dflt.value && updated.minTime === dflt.minTime && updated.maxTime === dflt.maxTime && !updated.disableRBE === !dflt.disableRBE) {
      delete overrides[memberName];
    } else {
      overrides[memberName] = updated;
    }
    const next = new Map(workingInstanceOverrides);
    if (Object.keys(overrides).length > 0) next.set(instanceId, overrides);
    else next.delete(instanceId);
    workingInstanceOverrides = next;
  }

  function clearInstanceOverride(instanceId: string, memberName: string) {
    const overrides = { ...(workingInstanceOverrides.get(instanceId) ?? {}) };
    delete overrides[memberName];
    const next = new Map(workingInstanceOverrides);
    if (Object.keys(overrides).length > 0) {
      next.set(instanceId, overrides);
    } else {
      next.delete(instanceId);
    }
    workingInstanceOverrides = next;
  }

  function toggleInstanceRbe(instanceId: string, memberName: string) {
    const overrides = { ...(workingInstanceOverrides.get(instanceId) ?? {}) };
    const inherited = getEffectiveRbe(memberName, instanceId).deadband;
    const existing = overrides[memberName] ?? { ...inherited };
    const updated = { ...existing };
    if (updated.disableRBE) {
      delete updated.disableRBE;
    } else {
      updated.disableRBE = true;
    }
    // Auto-clean if matches template default
    const tmplName = activeTemplateName;
    const tmpl = tmplName ? workingTemplates.get(tmplName) : null;
    const member = tmpl?.members.find(m => m.name === memberName);
    const dflt = member?.defaultDeadband;
    if (dflt && updated.value === dflt.value && updated.minTime === dflt.minTime && updated.maxTime === dflt.maxTime && !updated.disableRBE === !dflt.disableRBE) {
      delete overrides[memberName];
    } else if (!dflt && updated.value === 0 && !updated.minTime && !updated.maxTime && !updated.disableRBE) {
      delete overrides[memberName];
    } else {
      overrides[memberName] = updated;
    }
    const next = new Map(workingInstanceOverrides);
    if (Object.keys(overrides).length > 0) next.set(instanceId, overrides);
    else next.delete(instanceId);
    workingInstanceOverrides = next;
  }

  // ── MQTT member toggle ──
  function toggleUdtMemberExcluded(memberName: string) {
    const tmplName = activeTemplateName;
    if (!tmplName) return;
    const next = new Set(excludedUdtMembers[tmplName] ?? new Set());
    if (next.has(memberName)) next.delete(memberName); else next.add(memberName);
    excludedUdtMembers = { ...excludedUdtMembers, [tmplName]: next };
  }

  function batchMqttEnableMembers(memberNames: string[]) {
    const tmplName = activeTemplateName;
    if (!tmplName) return;
    const next = new Set(excludedUdtMembers[tmplName] ?? new Set());
    for (const name of memberNames) next.delete(name);
    excludedUdtMembers = { ...excludedUdtMembers, [tmplName]: next };
    saltState.addNotification({ message: `Enabled MQTT for ${memberNames.length} members`, type: 'success' });
  }

  function batchMqttDisableMembers(memberNames: string[]) {
    const tmplName = activeTemplateName;
    if (!tmplName) return;
    const next = new Set(excludedUdtMembers[tmplName] ?? new Set());
    for (const name of memberNames) next.add(name);
    excludedUdtMembers = { ...excludedUdtMembers, [tmplName]: next };
    saltState.addNotification({ message: `Disabled MQTT for ${memberNames.length} members`, type: 'success' });
  }

  function toggleExpand(id: string) {
    const next = new Set(expandedInstances);
    if (next.has(id)) next.delete(id); else next.add(id);
    expandedInstances = next;
  }

  // ── UDT instance publish ──
  function toggleUdtPublish(deviceId: string, tag: string) {
    const key = `${deviceId}::${tag}`;
    const next = new Set(checkedUdtInstances);
    if (next.has(key)) next.delete(key); else next.add(key);
    checkedUdtInstances = next;
  }

  function batchMqttEnableInstances(keys: string[]) {
    const next = new Set(checkedUdtInstances);
    for (const key of keys) next.add(key);
    checkedUdtInstances = next;
    saltState.addNotification({ message: `Enabled MQTT for ${keys.length} instances`, type: 'success' });
  }

  function batchMqttDisableInstances(keys: string[]) {
    const next = new Set(checkedUdtInstances);
    for (const key of keys) next.delete(key);
    checkedUdtInstances = next;
    saltState.addNotification({ message: `Disabled MQTT for ${keys.length} instances`, type: 'success' });
  }

  // ── UDT instance history toggle ──
  function toggleHistoryUdtInstance(deviceId: string, tag: string) {
    const key = `${deviceId}::${tag}`;
    const next = new Set(checkedHistoryUdtInstances);
    if (next.has(key)) next.delete(key); else next.add(key);
    checkedHistoryUdtInstances = next;
  }

  function batchHistoryEnableInstances(keys: string[]) {
    const next = new Set(checkedHistoryUdtInstances);
    for (const key of keys) next.add(key);
    checkedHistoryUdtInstances = next;
    saltState.addNotification({ message: `Enabled History for ${keys.length} instances`, type: 'success' });
  }

  function batchHistoryDisableInstances(keys: string[]) {
    const next = new Set(checkedHistoryUdtInstances);
    for (const key of keys) next.delete(key);
    checkedHistoryUdtInstances = next;
    saltState.addNotification({ message: `Disabled History for ${keys.length} instances`, type: 'success' });
  }

  // ── Atomic tag toggle ──
  function toggleAtomicTag(deviceId: string, tag: string) {
    const key = `${deviceId}::${tag}`;
    const next = new Set(checkedAtomicTags);
    if (next.has(key)) next.delete(key); else next.add(key);
    checkedAtomicTags = next;
  }

  function batchMqttEnable(keys: string[]) {
    const next = new Set(checkedAtomicTags);
    for (const key of keys) next.add(key);
    checkedAtomicTags = next;
    saltState.addNotification({ message: `Enabled MQTT for ${keys.length} tags`, type: 'success' });
  }

  function batchMqttDisable(keys: string[]) {
    const next = new Set(checkedAtomicTags);
    for (const key of keys) next.delete(key);
    checkedAtomicTags = next;
    saltState.addNotification({ message: `Disabled MQTT for ${keys.length} tags`, type: 'success' });
  }

  // ── Atomic history toggle ──
  function toggleHistoryAtomicTag(deviceId: string, tag: string) {
    const key = `${deviceId}::${tag}`;
    const next = new Set(checkedHistoryAtomicTags);
    if (next.has(key)) next.delete(key); else next.add(key);
    checkedHistoryAtomicTags = next;
  }

  function batchHistoryEnable(keys: string[]) {
    const next = new Set(checkedHistoryAtomicTags);
    for (const key of keys) next.add(key);
    checkedHistoryAtomicTags = next;
    saltState.addNotification({ message: `Enabled History for ${keys.length} tags`, type: 'success' });
  }

  function batchHistoryDisable(keys: string[]) {
    const next = new Set(checkedHistoryAtomicTags);
    for (const key of keys) next.delete(key);
    checkedHistoryAtomicTags = next;
    saltState.addNotification({ message: `Disabled History for ${keys.length} tags`, type: 'success' });
  }

  // ── Atomic RBE ──
  function setRbeMode(key: string, mode: 'default' | 'custom') {
    const next = new Map(rbeOverrides);
    if (mode === 'default') {
      const existing = next.get(key);
      if (existing?.disableRBE) {
        // Keep disableRBE even when clearing deadband overrides
        next.set(key, { mode: 'default', disableRBE: true });
      } else {
        next.delete(key);
      }
    } else {
      const existing = next.get(key);
      next.set(key, { mode: 'custom', deadband: existing?.deadband ?? { value: 0 }, ...(existing?.disableRBE ? { disableRBE: true } : {}) });
    }
    rbeOverrides = next;
  }

  function toggleAtomicRbe(key: string) {
    const next = new Map(rbeOverrides);
    const existing = next.get(key);
    if (existing?.disableRBE) {
      // Re-enable RBE
      if (existing.mode === 'custom') {
        next.set(key, { mode: 'custom', deadband: existing.deadband });
      } else {
        next.delete(key);
      }
    } else {
      // Disable RBE, preserve deadband values
      next.set(key, { mode: existing?.mode ?? 'default', ...(existing?.deadband ? { deadband: existing.deadband } : {}), disableRBE: true });
    }
    rbeOverrides = next;
  }

  function updateRbeDeadband(key: string, field: 'value' | 'minTime' | 'maxTime', val: number | null) {
    const next = new Map(rbeOverrides);
    const existing = next.get(key);
    const db = existing?.deadband ? { ...existing.deadband } : { value: 0 };
    if (field === 'value') db.value = val ?? 0;
    else if (field === 'minTime') db.minTime = val;
    else db.maxTime = val;
    next.set(key, { mode: 'custom', deadband: db, ...(existing?.disableRBE ? { disableRBE: true } : {}) });
    rbeOverrides = next;
  }

  // ── Global batch RBE ──
  // ── Per-tab batch callbacks ──
  function atomicBatchApply(keys: string[], db: DeadBandConfig) {
    const nextRbe = new Map(rbeOverrides);
    for (const key of keys) {
      nextRbe.set(key, {
        mode: 'custom',
        deadband: { value: db.value, ...(db.minTime != null ? { minTime: db.minTime } : {}), ...(db.maxTime != null ? { maxTime: db.maxTime } : {}) },
        ...(db.disableRBE ? { disableRBE: true } : {}),
      });
    }
    rbeOverrides = nextRbe;
    saltState.addNotification({ message: `Batch applied to ${keys.length} tags`, type: 'success' });
  }

  function atomicBatchClear(keys: string[]) {
    const nextRbe = new Map(rbeOverrides);
    for (const key of keys) nextRbe.delete(key);
    rbeOverrides = nextRbe;
    saltState.addNotification({ message: `Cleared ${keys.length} tags to defaults`, type: 'success' });
  }

  function templateBatchApply(memberNames: string[], db: DeadBandConfig) {
    for (const name of memberNames) {
      setTemplateMemberDeadband(name, 'value', db.value);
      if (db.minTime != null) setTemplateMemberDeadband(name, 'minTime', db.minTime);
      if (db.maxTime != null) setTemplateMemberDeadband(name, 'maxTime', db.maxTime);
    }
    saltState.addNotification({ message: `Batch applied to ${memberNames.length} members`, type: 'success' });
  }

  function templateBatchClear(memberNames: string[]) {
    for (const name of memberNames) clearTemplateMemberDeadband(name);
    saltState.addNotification({ message: `Cleared ${memberNames.length} members to defaults`, type: 'success' });
  }

  function instancesBatchApply(instanceIds: string[], memberNames: string[], db: DeadBandConfig) {
    const tmplName = activeTemplateName;
    if (!tmplName) return;
    const tmpl = workingTemplates.get(tmplName);
    if (!tmpl) return;
    const memberSet = new Set(memberNames);
    const targetMembers = tmpl.members.filter(m => m.datatype === 'number' && memberSet.has(m.name));
    let nextOverrides = new Map(workingInstanceOverrides);
    for (const instanceId of instanceIds) {
      for (const member of targetMembers) {
        const overrides = { ...(nextOverrides.get(instanceId) ?? {}) };
        const existing = overrides[member.name] ?? { value: 0 };
        const updated = { ...existing, value: db.value };
        if (db.minTime != null) updated.minTime = db.minTime;
        if (db.maxTime != null) updated.maxTime = db.maxTime;
        if (db.disableRBE) updated.disableRBE = true;
        else delete updated.disableRBE;
        const dflt = member.defaultDeadband;
        if (dflt && updated.value === dflt.value && updated.minTime === dflt.minTime && updated.maxTime === dflt.maxTime && !updated.disableRBE === !dflt.disableRBE) {
          delete overrides[member.name];
        } else {
          overrides[member.name] = updated;
        }
        if (Object.keys(overrides).length > 0) nextOverrides.set(instanceId, overrides);
        else nextOverrides.delete(instanceId);
      }
    }
    workingInstanceOverrides = nextOverrides;
    saltState.addNotification({ message: `Batch applied to ${instanceIds.length} instances × ${targetMembers.length} members`, type: 'success' });
  }

  function instancesBatchClear(instanceIds: string[], memberNames: string[]) {
    const memberSet = new Set(memberNames);
    const nextOverrides = new Map(workingInstanceOverrides);
    for (const instanceId of instanceIds) {
      const existing = nextOverrides.get(instanceId);
      if (!existing) continue;
      const updated = { ...existing };
      for (const name of memberSet) delete updated[name];
      if (Object.keys(updated).length > 0) nextOverrides.set(instanceId, updated);
      else nextOverrides.delete(instanceId);
    }
    workingInstanceOverrides = nextOverrides;
    saltState.addNotification({ message: `Cleared ${instanceIds.length} instances × ${memberNames.length} members`, type: 'success' });
  }


  // ── Inline editing ──
  function startEdit(key: string, currentValue: number) {
    editingCell = key;
    editDraft = String(currentValue);
  }

  function cancelEdit() {
    editingCell = null;
  }

  // ── Browse/refresh ──
  async function refreshDevice(deviceId: string) {
    const device = gatewayConfig?.devices?.find(d => d.deviceId === deviceId);
    if (!device) return;
    try {
      const input: Record<string, unknown> = { deviceId, protocol: device.protocol };
      if (device.host) input.host = device.host;
      if (device.port) input.port = device.port;
      if (device.protocol === 'ethernetip' && device.slot != null) input.slot = device.slot;
      if (device.endpointUrl) input.endpointUrl = device.endpointUrl;
      if (device.version) input.version = device.version;
      if (device.community) input.community = device.community;
      if (device.protocol === 'snmp') {
        const rootOid = snmpRootOids.get(deviceId) || snmpRootOidPresets[0].value;
        input.rootOid = rootOid;
      }

      const result = await apiPost<{ browseId: string; deviceId: string }>('/gateways/gateway/browse', input);

      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else if (result.data) {
        const b = result.data;
        const now = new Date().toISOString();
        // Add to localBrowseSubs to trigger subscription (this triggers the subscription effect)
        localBrowseSubs = new Map([...localBrowseSubs, [deviceId, { browseId: b.browseId, protocol: device.protocol }]]);
        // Update liveProgress for display (this does NOT trigger the subscription effect)
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

  // ── Unified save ──
  async function saveChanges() {
    saving = true;
    try {
      // Phase 1: sync tag selection per device
      const syncedTemplates = new Set<string>();
      for (const cache of browseCaches ?? []) {
        const deviceId = cache.deviceId;
        const structTags = cache.structTags ?? {};
        const udtLookup = new Map(cache.udts.map(u => [u.name, u]));

        const isSnmp = cache.protocol === 'snmp';
        const atomicVariables = cache.items
          .filter(item => !structTags[item.tag] && (isSnmp || !item.tag.includes('.')) && checkedAtomicTags.has(`${deviceId}::${item.tag}`))
          .map(item => {
            const key = `${deviceId}::${item.tag}`;
            const rbe = rbeOverrides.get(key);
            return {
              id: `${deviceId}/${item.name || item.tag}`,
              deviceId,
              tag: item.tag,
              datatype: item.datatype,
              default: item.datatype === 'number' ? 0 : item.datatype === 'boolean' ? false : '',
              ...(item.protocolType ? { cipType: item.protocolType } : {}),
              ...(rbe?.mode === 'custom' && rbe.deadband ? { deadband: { value: rbe.deadband.value, ...(rbe.deadband.minTime != null ? { minTime: rbe.deadband.minTime } : {}), ...(rbe.deadband.maxTime != null ? { maxTime: rbe.deadband.maxTime } : {}) } } : {}),
              ...(rbe?.disableRBE ? { disableRBE: true } : {}),
              ...(checkedHistoryAtomicTags.has(`${deviceId}::${item.tag}`) ? { historyEnabled: true } : {}),
            };
          });

        const udtTemplates: Array<{ name: string; version: string; members: Array<{ name: string; datatype: string; templateRef?: string }> }> = [];
        const udtVariables: Array<{ id: string; deviceId: string; tag: string; templateName: string; memberTags: Record<string, string>; memberCipTypes: Record<string, string> }> = [];
        const seenTemplates = new Set<string>();

        for (const [tag, udtName] of Object.entries(structTags)) {
          if (tag.startsWith('.')) continue;
          if (!checkedUdtInstances.has(`${deviceId}::${tag}`)) continue;
          const udt = udtLookup.get(udtName);
          if (!udt) continue;
          const resolved = resolveTemplateName(deviceId, udtName);
          const excluded = excludedUdtMembers[resolved] ?? new Set();
          const includedMembers = udt.members.filter(m => !excluded.has(m.name));
          if (includedMembers.length === 0) continue;

          if (!seenTemplates.has(resolved)) {
            seenTemplates.add(resolved);
            syncedTemplates.add(resolved);
            udtTemplates.push({
              name: resolved,
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
          udtVariables.push({ id: tag, deviceId, tag, templateName: resolved, memberTags, memberCipTypes, ...(checkedHistoryUdtInstances.has(`${deviceId}::${tag}`) ? { historyEnabled: true } : {}) });
        }

        const syncResult = await apiPost(`/gateways/gateway/devices/${deviceId}/sync`, { atomicVariables, udtTemplates, udtVariables });

        if (syncResult.error) {
          saltState.addNotification({ message: syncResult.error.error, type: 'error' });
          return;
        }
      }

      // Phase 2: save UDT deadband config per template (only for templates synced in Phase 1)
      for (const [tmplName, working] of workingTemplates) {
        if (!syncedTemplates.has(tmplName)) continue;
        const memberUpdates: Record<string, unknown> = {};
        for (const m of working.members.filter(m => m.datatype === 'number')) {
          memberUpdates[m.name] = m.defaultDeadband ? {
            value: m.defaultDeadband.value,
            ...(m.defaultDeadband.minTime != null ? { minTime: m.defaultDeadband.minTime } : {}),
            ...(m.defaultDeadband.maxTime != null ? { maxTime: m.defaultDeadband.maxTime } : {}),
            ...(m.defaultDeadband.disableRBE ? { disableRBE: true } : {}),
          } : null;
        }

        const templateInstances = allConfigInstances.filter(i => i.templateName === tmplName);
        const instanceUpdates: Record<string, unknown> = {};
        for (const inst of templateInstances) {
          instanceUpdates[inst.id] = workingInstanceOverrides.get(inst.id) ?? {};
        }

        if (Object.keys(memberUpdates).length > 0 || Object.keys(instanceUpdates).length > 0) {
          const udtResult = await apiPut(`/gateways/gateway/udt-config/${tmplName}`, { memberUpdates, instanceUpdates });

          if (udtResult.error) {
            saltState.addNotification({ message: udtResult.error.error, type: 'error' });
            return;
          }
        }
      }

      // Phase 3: save template name overrides per device
      for (const device of devices) {
        const overrides = workingTemplateOverrides.get(device.deviceId) ?? {};
        const currentOverrides = device.templateNameOverrides ?? {};
        // Only save if overrides changed
        if (JSON.stringify(overrides) !== JSON.stringify(currentOverrides)) {
          const overrideResult = await apiPut(`/gateways/gateway/devices/${device.deviceId}/template-overrides`, { overrides });

          if (overrideResult.error) {
            saltState.addNotification({ message: overrideResult.error.error, type: 'error' });
            return;
          }
        }
      }

      saltState.addNotification({ message: 'Changes saved successfully', type: 'success' });
      await invalidateAll();
      needsInit = true;
    } catch (err) {
      saltState.addNotification({ message: err instanceof Error ? err.message : 'Save failed', type: 'error' });
    } finally {
      saving = false;
    }
  }

  // ── Side nav data ──
  type MergedTemplate = {
    name: string;
    totalInstanceCount: number;
    deviceIds: string[];
    hasConflict: boolean;
  };

  type SideNavDevice = {
    deviceId: string;
    protocol: string;
    autoManaged?: boolean;
    atomicCount: number;
  };

  /** Merged templates: same resolved name across devices = single entry with conflict detection */
  const mergedTemplates = $derived.by((): MergedTemplate[] => {
    const map = new Map<string, { count: number; deviceIds: string[]; memberKeys: string[][] }>();
    for (const cache of browseCaches ?? []) {
      const structTags = cache.structTags ?? {};
      const udtLookup = new Map(cache.udts.map(u => [u.name, u]));
      const counts = new Map<string, number>();
      for (const udtName of Object.values(structTags)) {
        const resolved = resolveTemplateName(cache.deviceId, udtName);
        counts.set(resolved, (counts.get(resolved) ?? 0) + 1);
      }
      for (const [resolvedName, count] of counts) {
        const existing = map.get(resolvedName);
        // Find the original name for this device (reverse lookup from resolved)
        const deviceOverrides = workingTemplateOverrides.get(cache.deviceId) ?? {};
        const originalName = Object.entries(deviceOverrides).find(([, v]) => v === resolvedName)?.[0] ?? resolvedName;
        const udt = udtLookup.get(originalName);
        const memberKey = udt ? udt.members.map(m => `${m.name}:${m.datatype}`).sort() : [];
        if (existing) {
          existing.count += count;
          existing.deviceIds.push(cache.deviceId);
          existing.memberKeys.push(memberKey);
        } else {
          map.set(resolvedName, { count, deviceIds: [cache.deviceId], memberKeys: [memberKey] });
        }
      }
    }
    // Also include templates from config that aren't in any browse cache
    for (const inst of allConfigInstances) {
      if (!map.has(inst.templateName)) {
        map.set(inst.templateName, { count: 1, deviceIds: [inst.deviceId], memberKeys: [] });
      }
    }
    return [...map.entries()]
      .map(([name, info]) => {
        // Detect conflict: compare member signatures across devices
        let hasConflict = false;
        if (info.memberKeys.length > 1) {
          const first = info.memberKeys[0].join(',');
          hasConflict = info.memberKeys.some(mk => mk.join(',') !== first);
        }
        return { name, totalInstanceCount: info.count, deviceIds: info.deviceIds, hasConflict };
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  });

  /** Devices for sidebar (atomic tags + browse button only) */
  const sideNavDevices = $derived.by((): SideNavDevice[] => {
    const cacheMap = new Map((browseCaches ?? []).map(c => [c.deviceId, c]));
    const result: SideNavDevice[] = [];
    const seen = new Set<string>();

    for (const device of devices) {
      seen.add(device.deviceId);
      const cache = cacheMap.get(device.deviceId);
      if (cache) {
        const structTags = cache.structTags ?? {};
        const isSnmp = cache.protocol === 'snmp';
        const atomicCount = cache.items.filter(item => !structTags[item.tag] && (isSnmp || !item.tag.includes('.'))).length;
        result.push({ deviceId: device.deviceId, protocol: device.protocol, autoManaged: device.autoManaged, atomicCount });
      } else {
        result.push({ deviceId: device.deviceId, protocol: device.protocol, autoManaged: device.autoManaged, atomicCount: 0 });
      }
    }

    for (const cache of browseCaches ?? []) {
      if (seen.has(cache.deviceId)) continue;
      const structTags = cache.structTags ?? {};
      const isSnmp = cache.protocol === 'snmp';
      const atomicCount = cache.items.filter(item => !structTags[item.tag] && (isSnmp || !item.tag.includes('.'))).length;
      result.push({ deviceId: cache.deviceId, protocol: cache.protocol, atomicCount });
    }

    return result;
  });
</script>

<div class="tag-config">
  {#if error}
    <div class="error-box"><p>{error}</p></div>
  {/if}

  {#if sideNavDevices.length === 0 && mergedTemplates.length === 0}
    <div class="empty-state">
      <p>No browse data or templates configured. Go to Devices to browse a PLC and discover tags.</p>
    </div>
  {:else}
    <div class="tc-layout">
      <!-- Side nav (desktop) -->
      <nav class="tc-side-nav">
        {#if mergedTemplates.length > 0}
          <button class="tc-side-section-label" onclick={() => templatesExpanded = !templatesExpanded}>
            <span class="section-chevron" class:expanded={templatesExpanded}><ChevronRight size="0.75rem" /></span>
            Templates
            <span class="tc-side-count">{mergedTemplates.length}</span>
          </button>
          {#if templatesExpanded}
            <div transition:slide|local={{ duration: 150 }}>
              {#each mergedTemplates as tmpl}
                <button
                  class="tc-side-item"
                  class:active={activeSection?.kind === 'template' && activeSection.templateName === tmpl.name}
                  onclick={() => { activeSection = { kind: 'template', templateName: tmpl.name }; activeTab = 'instances'; }}
                >
                  {#if dirtySidebarSections.has(`t::${tmpl.name}`)}<span class="dirty-icon" title="Unsaved changes" transition:slide|local={{ axis: 'x', duration: 150 }}><PencilSquare size="1rem" /></span>{/if}
                  <span class="tc-side-icon t-icon">T</span>
                  <span class="tc-side-name">{tmpl.name}</span>
                  {#if tmpl.hasConflict}
                    <span class="conflict-icon" title="Template members differ across devices — first device's definition will be used">
                      <ExclamationTriangle size="1rem" />
                    </span>
                  {/if}
                  {#if publishedTemplateSections.has(tmpl.name)}<span class="mqtt-icon" title="Has MQTT-published instances"><Signal size="1rem" /></span>{/if}
                  <span class="tc-side-count">{tmpl.totalInstanceCount}</span>
                </button>
              {/each}
            </div>
          {/if}
        {/if}

        {#each sideNavDevices as device}
          {@const browseState = activeBrowseStates.get(device.deviceId)}
          {@const isBusy = browseState?.status === 'browsing' || (liveProgress.has(device.deviceId) && liveProgress.get(device.deviceId)?.status === 'completed')}
          {@const cache = browseCaches.find(c => c.deviceId === device.deviceId)}
          {@const pct = isBusy && browseState && browseState.totalCount > 0 ? Math.round((browseState.discoveredCount / browseState.totalCount) * 100) : 0}
          <div class="tc-side-head">
            {#if dirtyDevices.has(device.deviceId)}<span class="dirty-icon" title="Unsaved changes" transition:slide|local={{ axis: 'x', duration: 150 }}><PencilSquare size="1rem" /></span>{/if}
            <span class="side-device-name">{device.deviceId}</span>
            <span class="side-proto">{device.protocol}</span>
            {#if !device.autoManaged}
            <span class="side-browse-area">
              {#if isBusy && browseState}
                <svg class="circular-progress" viewBox="0 0 20 20" width="16" height="16">
                  <circle cx="10" cy="10" r="8" fill="none" stroke="var(--theme-border)" stroke-width="2.5" />
                  <circle cx="10" cy="10" r="8" fill="none" stroke="var(--badge-teal-text)" stroke-width="2.5"
                    stroke-dasharray={`${browseState.status === 'completed' ? 50.3 : pct * 0.503} ${browseState.status === 'completed' ? 0 : 50.3 - pct * 0.503}`}
                    stroke-dashoffset="12.6" stroke-linecap="round"
                    class:spinning={browseState.status === 'browsing' && browseState.totalCount === 0} />
                </svg>
                {#if browseState.status === 'completed'}
                  <span class="side-browse-count">done</span>
                {:else if browseState.totalCount > 0}
                  <span class="side-browse-count">{browseState.discoveredCount}/{browseState.totalCount}</span>
                {:else if browseState.discoveredCount > 0}
                  <span class="side-browse-count">{browseState.discoveredCount}</span>
                {/if}
              {:else}
                <button class="side-refresh-btn" onclick={() => refreshDevice(device.deviceId)} title={cache?.cachedAt ? `Last: ${new Date(cache.cachedAt).toLocaleString()}` : 'Browse device'}>
                  <ArrowPath size="1rem" />
                </button>
              {/if}
            </span>
            {/if}
          </div>
          {#if device.protocol === 'snmp' && !isBusy}
            <div class="snmp-root-oid">
              <select
                class="snmp-root-oid-select"
                value={snmpRootOids.get(device.deviceId) || snmpRootOidPresets[0].value}
                onchange={(e) => { const next = new Map(snmpRootOids); next.set(device.deviceId, (e.target as HTMLSelectElement).value); snmpRootOids = next; }}
              >
                {#each snmpRootOidPresets as preset}
                  <option value={preset.value}>{preset.label}</option>
                {/each}
              </select>
            </div>
          {/if}
          {#if device.atomicCount > 0}
            <button
              class="tc-side-item"
              class:active={activeSection?.kind === 'atomic' && activeSection.deviceId === device.deviceId}
              onclick={() => { activeSection = { kind: 'atomic', deviceId: device.deviceId }; }}
            >
              {#if dirtySidebarSections.has(`a::${device.deviceId}`)}<span class="dirty-icon" title="Unsaved changes" transition:slide|local={{ axis: 'x', duration: 150 }}><PencilSquare size="1rem" /></span>{/if}
              <span class="tc-side-icon a-icon">A</span>
              <span class="tc-side-name">Atomic Tags</span>
              {#if publishedAtomicDevices.has(device.deviceId)}<span class="mqtt-icon" title="Has MQTT-published tags"><Signal size="1rem" /></span>{/if}
              <span class="tc-side-count">{device.atomicCount}</span>
            </button>
          {/if}
          {#if device.atomicCount === 0 && !cache && !device.autoManaged}
            <div class="tc-side-empty">Click <button class="side-refresh-link" onclick={() => refreshDevice(device.deviceId)}>browse</button> to discover tags</div>
          {/if}
        {/each}
      </nav>

      <div class="tc-main">
        <!-- Mobile/tablet selector -->
        <div class="tc-top-bar">
          <select
            class="tc-tpl-select"
            value={activeSection?.kind === 'template' ? `t::${activeSection.templateName}` : activeSection?.kind === 'atomic' ? `a::${activeSection.deviceId}` : ''}
            onchange={(e) => {
              const val = (e.target as HTMLSelectElement).value;
              if (val.startsWith('t::')) {
                activeSection = { kind: 'template', templateName: val.slice(3) };
                activeTab = 'instances';
              } else if (val.startsWith('a::')) {
                activeSection = { kind: 'atomic', deviceId: val.slice(3) };
              }
            }}
          >
            {#if mergedTemplates.length > 0}
              <optgroup label="Templates">
                {#each mergedTemplates as tmpl}
                  <option value={`t::${tmpl.name}`}>{dirtySidebarSections.has(`t::${tmpl.name}`) ? '● ' : ''}{tmpl.name} ({tmpl.totalInstanceCount}){tmpl.hasConflict ? ' ⚠' : ''}</option>
                {/each}
              </optgroup>
            {/if}
            {#each sideNavDevices as device}
              {#if device.atomicCount > 0}
                <optgroup label="{device.deviceId} ({device.protocol})">
                  <option value={`a::${device.deviceId}`}>{dirtySidebarSections.has(`a::${device.deviceId}`) ? '● ' : ''}Atomic Tags ({device.atomicCount})</option>
                </optgroup>
              {/if}
            {/each}
          </select>
        </div>

        {#if activeSection?.kind === 'template' && activeTemplate}
          <!-- Template header -->
          <div class="tc-header">
            <span class="tc-title">{activeTemplateName?.replace(/_/g, ' ')}</span>
            <span class="tc-subtitle">{activeTemplate.members.length} members / {activeInstances.length} instances</span>
          </div>

          <!-- Conflict resolution banner -->
          {@const activeMerged = mergedTemplates.find(t => t.name === activeTemplateName)}
          {#if activeMerged?.hasConflict}
            {@const conflictDevices = activeMerged.deviceIds.map(devId => {
              const cache = browseCaches.find(c => c.deviceId === devId);
              const originalName = Object.entries(workingTemplateOverrides.get(devId) ?? {}).find(([, v]) => v === activeTemplateName)?.[0] ?? activeTemplateName ?? '';
              const memberCount = cache?.udts.find(u => u.name === originalName)?.members.length ?? 0;
              return { deviceId: devId, originalBrowseName: originalName, memberCount };
            })}
            {@const anyDraftChanged = conflictDevices.some(d => {
              const draft = (renameDrafts.get(d.deviceId) ?? `${d.deviceId}_${activeTemplateName ?? ''}`).trim();
              return draft !== '' && draft !== activeTemplateName;
            })}
            <div class="conflict-banner">
              <span class="conflict-banner-icon"><ExclamationTriangle size="1rem" /></span>
              <span class="conflict-banner-text">Different members on different devices. Rename to make unique:</span>
              <div class="conflict-device-list">
                {#each conflictDevices as cd}
                  <div class="conflict-device-row">
                    <span class="conflict-device-id mono">{cd.deviceId}</span>
                    <span class="conflict-member-count">{cd.memberCount} members</span>
                    <input
                      class="conflict-rename-input"
                      value={renameDrafts.get(cd.deviceId) ?? `${cd.deviceId}_${activeTemplateName ?? ''}`}
                      oninput={(e) => { const next = new Map(renameDrafts); next.set(cd.deviceId, (e.target as HTMLInputElement).value); renameDrafts = next; }}
                      onkeydown={(e) => { if (e.key === 'Enter' && anyDraftChanged) commitAllRenames(conflictDevices); }}
                      placeholder="New template name..."
                    />
                  </div>
                {/each}
              </div>
              <button class="conflict-apply-btn" disabled={!anyDraftChanged} onclick={() => commitAllRenames(conflictDevices)}>Apply Renames</button>
            </div>
          {/if}

          <div class="tc-tabs">
            <button class="tc-tab" class:active={activeTab === 'template'} onclick={() => activeTab = 'template'}>Template Defaults</button>
            <button class="tc-tab" class:active={activeTab === 'instances'} onclick={() => activeTab = 'instances'}>Instances</button>
          </div>

          {#if activeTab === 'template'}
            <TemplateDefaultsTab
              members={activeTemplate.members}
              excludedMembers={excludedUdtMembers[activeTemplateName ?? ''] ?? new Set()}
              deviceDeadband={templateDeviceDeadband}
              dirtyMembers={dirtyTemplateMemberMap.get(activeTemplateName ?? '') ?? new Set()}
              {editingCell}
              {editDraft}
              onToggleMember={toggleUdtMemberExcluded}
              onStartEdit={startEdit}
              onCancelEdit={cancelEdit}
              onSetDeadband={setTemplateMemberDeadband}
              onClearDeadband={clearTemplateMemberDeadband}
              onBatchApply={templateBatchApply}
              onBatchClear={templateBatchClear}
              onBatchMqttEnable={batchMqttEnableMembers}
              onBatchMqttDisable={batchMqttDisableMembers}
            />
          {:else}
            <InstancesTab
              instances={activeInstances}
              analogMembers={enabledAnalogMembers}
              {expandedInstances}
              checkedInstances={checkedUdtInstances}
              checkedHistoryInstances={checkedHistoryUdtInstances}
              {dirtyInstanceKeys}
              {dirtyInstanceMembers}
              workingOverrides={workingInstanceOverrides}
              {editingCell}
              {editDraft}
              onTogglePublish={toggleUdtPublish}
              onToggleHistoryPublish={toggleHistoryUdtInstance}
              onToggleExpand={toggleExpand}
              onStartEdit={startEdit}
              onCancelEdit={cancelEdit}
              onSetOverride={setInstanceOverride}
              onClearOverride={clearInstanceOverride}
              onToggleRbe={toggleInstanceRbe}
              {getEffectiveRbe}
              {getOverrideCount}
              onGoToDefaults={() => activeTab = 'template'}
              onBatchApplyFiltered={instancesBatchApply}
              onBatchClearFiltered={instancesBatchClear}
              onBatchMqttEnable={batchMqttEnableInstances}
              onBatchMqttDisable={batchMqttDisableInstances}
              onBatchHistoryEnable={batchHistoryEnableInstances}
              onBatchHistoryDisable={batchHistoryDisableInstances}
            />
          {/if}
        {:else if activeSection?.kind === 'atomic' && activeDeviceId}
          <AtomicTagsTab
            deviceId={activeDeviceId}
            browseCache={browseCaches.find(c => c.deviceId === activeDeviceId) ?? null}
            deviceDeadband={atomicDeviceDeadband}
            {checkedAtomicTags}
            {checkedHistoryAtomicTags}
            {rbeOverrides}
            {dirtyAtomicKeys}
            {editingCell}
            {editDraft}
            onToggleTag={toggleAtomicTag}
            onToggleHistoryTag={toggleHistoryAtomicTag}
            onStartEdit={startEdit}
            onCancelEdit={cancelEdit}
            onSetRbeMode={setRbeMode}
            onToggleRbe={toggleAtomicRbe}
            onUpdateRbeDeadband={updateRbeDeadband}
            onBatchApply={atomicBatchApply}
            onBatchClear={atomicBatchClear}
            onBatchMqttEnable={batchMqttEnable}
            onBatchMqttDisable={batchMqttDisable}
            onBatchHistoryEnable={batchHistoryEnable}
            onBatchHistoryDisable={batchHistoryDisable}
          />
        {/if}
      </div>
    </div>

    <!-- Save bar -->
    {#if isDirty}
      <div class="save-bar" transition:fly|local={{ y: 40, duration: 150 }}>
        <button class="save-btn" onclick={saveChanges} disabled={saving}>
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
        <button class="reset-btn" onclick={async () => { await invalidateAll(); needsInit = true; }} disabled={saving}>Discard</button>
      </div>
    {/if}
  {/if}
</div>

<style lang="scss">
  .tag-config {
    display: flex; flex-direction: column; flex: 1; min-height: 0;
    :global(input[type='number']) {
      -moz-appearance: textfield;
      &::-webkit-inner-spin-button, &::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
    }
  }

  .error-box {
    padding: 1rem; border-radius: var(--rounded-lg); background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444); margin: 1.5rem 2rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .empty-state { padding: 3rem 2rem; text-align: center; p { color: var(--theme-text-muted); font-size: 0.875rem; } }

  // ── Layout ──
  .tc-layout { display: flex; flex: 1; overflow: hidden; }

  .tc-side-nav {
    width: 260px; min-width: 260px;
    background: var(--theme-surface);
    border-right: 1px solid var(--theme-border);
    display: flex; flex-direction: column;
    overflow-y: auto;
  }

  .tc-side-head {
    padding: 0.625rem 1rem; font-size: 0.75rem; font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--theme-text); border-bottom: 1px solid var(--theme-border);
    display: flex; align-items: center; gap: 0.5rem;
    font-family: 'IBM Plex Mono', monospace;
    background: color-mix(in srgb, var(--theme-surface) 80%, var(--theme-border));
  }

  .side-device-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .side-proto {
    font-size: 0.5625rem; font-weight: 700; text-transform: uppercase;
    padding: 0.05rem 0.3rem; border-radius: var(--rounded-sm);
    background: var(--badge-teal-bg); color: var(--badge-teal-text);
    flex-shrink: 0;
  }

  .side-browse-area {
    display: flex; align-items: center; gap: 0.375rem; margin-left: auto; flex-shrink: 0;
  }

  .side-browse-count {
    font-size: 0.625rem; color: var(--theme-text-muted); font-family: 'IBM Plex Mono', monospace; white-space: nowrap;
  }

  .snmp-root-oid {
    padding: 2px 12px 4px;
  }
  .snmp-root-oid-select {
    width: 100%; font-size: 0.7rem; padding: 2px 4px;
    background: var(--theme-surface); color: var(--theme-text);
    border: 1px solid var(--theme-border); border-radius: var(--rounded-md);
    font-family: 'IBM Plex Mono', monospace;
    &:focus { outline: none; border-color: var(--theme-primary); }
  }

  .side-refresh-btn {
    display: inline-flex; align-items: center; justify-content: center;
    width: 26px; height: 26px; border-radius: var(--rounded-md);
    border: 1px solid var(--theme-border);
    background: var(--theme-surface); color: var(--theme-text-muted);
    cursor: pointer; flex-shrink: 0;
    :global(svg) { flex-shrink: 0; }
    &:hover { color: var(--theme-text); background: var(--theme-surface-hover); border-color: var(--theme-primary); }
  }

  .circular-progress {
    flex-shrink: 0;
    circle { transition: stroke-dasharray 0.3s ease; }
    .spinning { animation: spin-progress 1s linear infinite; transform-origin: center; }
  }
  @keyframes spin-progress { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

  .tc-side-item {
    display: flex; align-items: center; gap: 0.5rem;
    padding: 0.5rem 1rem; cursor: pointer;
    border: none; background: none; color: var(--theme-text);
    font-size: 0.8125rem; font-family: inherit; text-align: left;
    border-left: 2px solid transparent; border-radius: 0;
    &:hover { background: color-mix(in srgb, var(--theme-text) 5%, transparent); }
    &.active { background: color-mix(in srgb, var(--theme-primary) 10%, transparent); border-left-color: var(--theme-primary); }
  }

  .tc-side-icon {
    display: inline-flex; align-items: center; justify-content: center;
    width: 20px; height: 20px; border-radius: var(--rounded-sm);
    font-size: 0.6875rem; font-weight: 700; flex-shrink: 0;
  }
  .t-icon { background: var(--badge-purple-bg); color: var(--badge-purple-text); }
  .a-icon { background: var(--badge-teal-bg); color: var(--badge-teal-text); }

  .tc-side-section-label {
    display: flex; align-items: center; gap: 0.25rem; width: 100%;
    padding: 0.5rem 1rem 0.25rem; font-size: 0.625rem; font-weight: 700;
    text-transform: uppercase; letter-spacing: 0.06em;
    color: var(--theme-text-muted); background: none; border: none; cursor: pointer;
    &:not(:first-child) { border-top: 1px solid var(--theme-border); margin-top: 0.25rem; padding-top: 0.625rem; }
    &:hover { color: var(--theme-text); }
    .tc-side-count { margin-left: auto; }
  }
  .section-chevron {
    display: inline-flex; flex-shrink: 0; transition: transform 0.15s ease;
    &.expanded { transform: rotate(90deg); }
  }

  .tc-side-name { font-family: 'IBM Plex Mono', monospace; font-weight: 500; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tc-side-count { font-size: 0.6875rem; color: var(--theme-text-muted); font-family: 'IBM Plex Mono', monospace; }

  .conflict-icon {
    display: inline-flex; align-items: center; flex-shrink: 0;
    color: #f59e0b;
    :global(svg) { flex-shrink: 0; }
  }
  .mqtt-icon {
    display: inline-flex; align-items: center; flex-shrink: 0;
    color: var(--badge-teal-text, #2dd4bf);
    :global(svg) { flex-shrink: 0; }
  }

  .tc-side-empty {
    padding: 0.5rem 1rem 0.625rem; font-size: 0.6875rem; color: var(--theme-text-muted);
    font-style: italic;
  }
  .side-refresh-link {
    border: none; background: none; color: var(--theme-primary); cursor: pointer;
    font: inherit; font-size: inherit; font-style: inherit; padding: 0; text-decoration: underline;
    &:hover { opacity: 0.8; }
  }

  .dirty-icon {
    display: inline-flex; align-items: center; flex-shrink: 0;
    color: #f59e0b;
    overflow: hidden;
    :global(svg) { flex-shrink: 0; }
  }

  .tc-main { flex: 1; display: flex; flex-direction: column; overflow: hidden; min-width: 0; }

  // ── Top bar (tablet/mobile) ──
  .tc-top-bar {
    display: none; padding: 0.625rem 1rem; gap: 0.625rem; align-items: center;
    background: var(--theme-surface); border-bottom: 1px solid var(--theme-border);
  }

  .tc-tpl-select {
    font-family: 'IBM Plex Mono', monospace; font-size: 0.8125rem; font-weight: 600;
    background: var(--theme-input-bg); border: 1px solid var(--theme-border);
    color: var(--theme-text); padding: 0.4rem 0.75rem; border-radius: var(--rounded-md);
    flex: 1; max-width: 320px; outline: none;
    &:focus { border-color: var(--theme-primary); }
  }

  // ── Header + tabs ──
  .tc-header {
    padding: 0.875rem 1.25rem;
    background: var(--theme-surface);
    border-bottom: 1px solid var(--theme-border);
    display: flex; align-items: center; gap: 0.75rem;
  }

  .tc-title { font-size: 1rem; font-weight: 700; color: var(--theme-text); }

  .tc-subtitle {
    font-size: 0.6875rem; color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-text) 6%, transparent);
    padding: 0.15rem 0.5rem; border-radius: var(--rounded-sm);
    font-family: 'IBM Plex Mono', monospace;
  }

  // ── Conflict banner ──
  .conflict-banner {
    padding: 0.625rem 1.25rem; display: flex; flex-wrap: wrap; align-items: flex-start; gap: 0.5rem;
    background: color-mix(in srgb, #f59e0b 8%, var(--theme-surface));
    border-bottom: 1px solid color-mix(in srgb, #f59e0b 30%, var(--theme-border));
    font-size: 0.75rem; color: var(--theme-text);
  }
  .conflict-banner-icon { color: #f59e0b; display: flex; align-items: center; flex-shrink: 0; margin-top: 0.1rem; :global(svg) { flex-shrink: 0; } }
  .conflict-banner-text { flex: 1 1 100%; font-weight: 500; }
  .conflict-device-list { flex: 1 1 100%; display: flex; flex-direction: column; gap: 0.375rem; }
  .conflict-device-row {
    display: flex; align-items: center; gap: 0.5rem; padding: 0.25rem 0;
  }
  .conflict-device-id { font-weight: 600; min-width: 5rem; }
  .conflict-member-count { font-size: 0.6875rem; color: var(--theme-text-muted); min-width: 5rem; }
  .conflict-rename-input {
    font-family: 'IBM Plex Mono', monospace; font-size: 0.75rem;
    background: var(--theme-input-bg); border: 1px solid var(--theme-border);
    color: var(--theme-text); padding: 0.25rem 0.5rem; border-radius: var(--rounded-sm);
    outline: none; min-width: 10rem;
    &:focus { border-color: var(--theme-primary); }
  }
  .conflict-apply-btn {
    font-size: 0.75rem; font-weight: 600; font-family: inherit;
    padding: 0.35rem 0.75rem; border-radius: var(--rounded-sm);
    background: var(--theme-primary); color: white; border: 1px solid var(--theme-primary);
    cursor: pointer; align-self: flex-start; margin-top: 0.25rem;
    &:hover { opacity: 0.9; }
    &:disabled { opacity: 0.4; cursor: not-allowed; }
  }

  .tc-tabs {
    display: flex; gap: 0;
    border-bottom: 1px solid var(--theme-border);
    background: var(--theme-surface);
    padding: 0 1.25rem;
  }

  .tc-tab {
    padding: 0.625rem 1rem;
    font-size: 0.8125rem; font-weight: 600;
    color: var(--theme-text-muted); cursor: pointer;
    border: none; background: none; font-family: inherit;
    border-bottom: 2px solid transparent; margin-bottom: -1px;
    border-radius: 0; outline: none;
    &:hover { color: var(--theme-text); }
    &.active { color: var(--theme-primary); border-bottom-color: var(--theme-primary); }
  }

  // ── Save bar ──
  .save-bar {
    position: sticky; bottom: 1rem; display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem;
    background: var(--theme-surface); border: 1px solid var(--badge-amber-text, #f59e0b);
    border-radius: var(--rounded-lg); box-shadow: 0 -2px 12px rgba(0, 0, 0, 0.3); justify-content: flex-end;
    margin: 0 1.25rem;
  }

  .save-btn {
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

  // ── Responsive ──
  @media (max-width: 1024px) {
    .tc-side-nav { display: none; }
    .tc-top-bar { display: flex; }
  }

  @media (max-width: 640px) {
    .tc-header { padding: 0.625rem 0.875rem; }
    .tc-tabs { padding: 0 0.875rem; }
    .tc-tab { flex: 1; text-align: center; font-size: 0.75rem; padding: 0.625rem 0.5rem; }
    .save-bar { margin: 0 0.75rem; }
    .tc-subtitle { display: none; }
  }
</style>
