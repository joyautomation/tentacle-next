<script lang="ts">
  import type { PageData } from './$types';
  import { page } from '$app/stores';
  import { apiPost } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import ProfinetDeviceConfig from '$lib/components/ProfinetDeviceConfig.svelte';

  let { data }: { data: PageData } = $props();

  const serviceType = $derived($page.params.serviceType ?? '');

  // ═══════════════════════════════════════════════════════════════════════════
  // Network config state
  // ═══════════════════════════════════════════════════════════════════════════

  interface InterfaceConfig {
    interfaceName: string;
    dhcp4: boolean;
    addresses: string[];
    gateway4: string;
    nameservers: string[];
    mtu: string;
  }

  function toEditable(cfg: { interfaceName: string; dhcp4: boolean | null; addresses: string[] | null; gateway4: string | null; nameservers: string[] | null; mtu: number | null }): InterfaceConfig {
    return {
      interfaceName: cfg.interfaceName,
      dhcp4: cfg.dhcp4 ?? true,
      addresses: cfg.addresses ?? [],
      gateway4: cfg.gateway4 ?? '',
      nameservers: cfg.nameservers ?? [],
      mtu: cfg.mtu != null ? String(cfg.mtu) : '',
    };
  }

  let availableInterfaces: string[] = $state([]);

  function newInterface(): InterfaceConfig {
    const used = new Set(configs.map((c) => c.interfaceName));
    const unused = availableInterfaces.find((n) => !used.has(n)) ?? '';
    return {
      interfaceName: unused,
      dhcp4: true,
      addresses: [],
      gateway4: '',
      nameservers: [],
      mtu: '',
    };
  }

  // ── Shared validation ────────────────────────────────────────────────────
  const IPV4_RE = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;

  function isValidIpv4(ip: string): boolean {
    const m = IPV4_RE.exec(ip.trim());
    if (!m) return false;
    return m.slice(1).every((o) => { const n = Number(o); return n >= 0 && n <= 255; });
  }

  function isValidCidr(addr: string): boolean {
    const parts = addr.trim().split('/');
    if (parts.length !== 2) return false;
    if (!isValidIpv4(parts[0])) return false;
    const prefix = Number(parts[1]);
    return Number.isInteger(prefix) && prefix >= 0 && prefix <= 32;
  }

  function isValidMtu(mtu: string): boolean {
    if (mtu.trim() === '') return true;
    const n = Number(mtu);
    return Number.isInteger(n) && n >= 68 && n <= 9000;
  }

  function isValidPort(port: string): boolean {
    const n = Number(port);
    return Number.isInteger(n) && n >= 1 && n <= 65535;
  }

  function validateConfig(cfg: InterfaceConfig): string[] {
    const errors: string[] = [];
    if (!cfg.interfaceName) errors.push('Interface must be selected');
    if (!cfg.dhcp4) {
      for (const addr of cfg.addresses) {
        if (addr.trim() && !isValidCidr(addr)) errors.push(`Invalid CIDR address: ${addr}`);
      }
      if (cfg.gateway4.trim() && !isValidIpv4(cfg.gateway4)) errors.push('Invalid gateway IP');
      for (const ns of cfg.nameservers) {
        if (ns.trim() && !isValidIpv4(ns)) errors.push(`Invalid nameserver IP: ${ns}`);
      }
    }
    if (!isValidMtu(cfg.mtu)) errors.push('MTU must be 68-9000');
    return errors;
  }

  let configs: InterfaceConfig[] = $state([]);
  let originalConfigs: string = $state('[]');
  let saving = $state(false);
  let error: string | null = $state(null);
  let allConfigured = $derived(configs.length >= availableInterfaces.length);
  let dirty = $derived(JSON.stringify(configs) !== originalConfigs);
  let validationErrors = $derived(configs.map(validateConfig));
  let valid = $derived(validationErrors.every((e) => e.length === 0));

  $effect(() => {
    if (serviceType !== 'network') return;
    configs = (data.configs ?? []).map(toEditable);
    originalConfigs = JSON.stringify((data.configs ?? []).map(toEditable));
    availableInterfaces = data.availableInterfaces ?? [];
  });

  function cancelChanges() {
    configs = (data.configs ?? []).map(toEditable);
    error = null;
  }

  function addInterface() {
    configs = [...configs, newInterface()];
  }

  function removeInterface(index: number) {
    configs = configs.filter((_, i) => i !== index);
  }

  function addAddress(index: number) {
    configs[index].addresses = [...configs[index].addresses, ''];
  }

  function removeAddress(ifaceIndex: number, addrIndex: number) {
    configs[ifaceIndex].addresses = configs[ifaceIndex].addresses.filter((_, i) => i !== addrIndex);
  }

  function addNameserver(index: number) {
    configs[index].nameservers = [...configs[index].nameservers, ''];
  }

  function removeNameserver(ifaceIndex: number, nsIndex: number) {
    configs[ifaceIndex].nameservers = configs[ifaceIndex].nameservers.filter((_, i) => i !== nsIndex);
  }

  async function saveConfig() {
    saving = true;
    error = null;

    try {
      const interfaces = configs.map((cfg) => ({
        interfaceName: cfg.interfaceName,
        dhcp4: cfg.dhcp4,
        ...(!cfg.dhcp4 && {
          addresses: cfg.addresses.filter((a) => a.trim() !== ''),
          gateway4: cfg.gateway4.trim() || null,
          nameservers: cfg.nameservers.filter((n) => n.trim() !== ''),
        }),
        mtu: cfg.mtu.trim() ? parseInt(cfg.mtu, 10) : null,
      }));

      const result = await apiPost<{ requestId: string; success: boolean; error: string | null }>(
        '/network/config',
        { interfaces }
      );

      if (result.error) {
        error = result.error.error;
      } else if (result.data?.success) {
        saltState.addNotification({
          message: 'Network configuration applied successfully',
          type: 'success',
        });
        await invalidateAll();
      } else {
        error = result.data?.error ?? 'Unknown error';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to apply config';
    } finally {
      saving = false;
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Nftables config state — unified NAT rules
  // ═══════════════════════════════════════════════════════════════════════════

  interface NatRuleForm {
    id: string;
    enabled: boolean;
    protocol: string;
    connectingDevices: string;
    incomingInterface: string;
    outgoingInterface: string;
    natAddr: string;
    originalPort: string;
    translatedPort: string;
    deviceAddr: string;
    deviceName: string;
    doubleNat: boolean;
    doubleNatAddr: string;
    comment: string;
  }

  let natRules: NatRuleForm[] = $state([]);
  let originalNft: string = $state(JSON.stringify([]));
  let nftSaving = $state(false);
  let nftError: string | null = $state(null);
  let interfaceAddresses: Record<string, { address: string; prefixlen: number }> = $state({});

  let nftDirty = $derived(JSON.stringify(natRules) !== originalNft);

  function isValidPortOrRange(val: string): boolean {
    if (!val.trim()) return false;
    if (val.includes('-')) {
      const [start, end] = val.split('-');
      return isValidPort(start) && isValidPort(end) && parseInt(start, 10) <= parseInt(end, 10);
    }
    return isValidPort(val);
  }

  function getPortRangeSize(val: string): number {
    if (!val.includes('-')) return 1;
    const [start, end] = val.split('-');
    return parseInt(end, 10) - parseInt(start, 10) + 1;
  }

  function isValidConnectingDevices(val: string): boolean {
    if (!val.trim() || val.trim() === 'any') return true;
    // Single IP
    if (isValidIpv4(val)) return true;
    // CIDR
    if (isValidCidr(val)) return true;
    // IP range: "1.2.3.4-1.2.3.10"
    if (val.includes('-') && !val.includes('/')) {
      const [start, end] = val.split('-');
      return isValidIpv4(start.trim()) && isValidIpv4(end.trim());
    }
    return false;
  }

  function validateNatRule(r: NatRuleForm, index: number): string[] {
    const errors: string[] = [];
    if (!r.incomingInterface) errors.push('Incoming interface is required');
    if (!r.outgoingInterface) errors.push('Outgoing interface is required');
    if (r.incomingInterface && r.outgoingInterface && r.incomingInterface === r.outgoingInterface) {
      errors.push('Incoming and outgoing interfaces must be different');
    }
    if (!r.natAddr.trim()) errors.push('NAT IP address is required');
    else if (!isValidIpv4(r.natAddr)) errors.push('Invalid NAT IP address');
    else {
      // Unique natAddr check
      const dup = natRules.findIndex((other, j) => j !== index && other.natAddr.trim() === r.natAddr.trim());
      if (dup !== -1) errors.push('NAT IP address must be unique across all rules');
    }
    if (!r.deviceAddr.trim()) errors.push('Device IP address is required');
    else if (!isValidIpv4(r.deviceAddr)) errors.push('Invalid device IP address');
    if (r.connectingDevices.trim() && r.connectingDevices.trim() !== 'any' && !isValidConnectingDevices(r.connectingDevices)) {
      errors.push('Invalid connecting devices filter (use IP, CIDR, range, or "any")');
    }
    // Ports optional for tcp/udp (empty = all ports), but validate if provided
    if (r.protocol === 'tcp' || r.protocol === 'udp') {
      if (r.originalPort.trim() && !isValidPortOrRange(r.originalPort)) errors.push('Invalid original port (1-65535 or range X-Y)');
      if (r.translatedPort.trim() && !isValidPortOrRange(r.translatedPort)) errors.push('Invalid translated port (1-65535 or range X-Y)');
      // If one port is set, both must be set
      if (r.originalPort.trim() && !r.translatedPort.trim()) errors.push('Translated port is required when original port is set');
      if (!r.originalPort.trim() && r.translatedPort.trim()) errors.push('Original port is required when translated port is set');
      // Range size check
      if (r.originalPort.includes('-') && r.translatedPort.includes('-')) {
        if (isValidPortOrRange(r.originalPort) && isValidPortOrRange(r.translatedPort)) {
          if (getPortRangeSize(r.originalPort) !== getPortRangeSize(r.translatedPort)) {
            errors.push('Port range sizes must match');
          }
        }
      }
    }
    // Double NAT validation
    if (r.doubleNat && r.doubleNatAddr.trim()) {
      if (!isValidIpv4(r.doubleNatAddr)) errors.push('Invalid Double NAT address');
    }
    return errors;
  }

  let natValidationErrors = $derived(natRules.map((r, i) => validateNatRule(r, i)));
  let nftValid = $derived(natValidationErrors.every((e) => e.length === 0));

  $effect(() => {
    if (serviceType !== 'nftables') return;
    const nftConfig = data.nftablesConfig ?? { natRules: [] };
    const rules = nftConfig.natRules.map((r: any) => ({ ...r }));
    natRules = rules;
    originalNft = JSON.stringify(rules);
    availableInterfaces = data.availableInterfaces ?? [];
    interfaceAddresses = data.interfaceAddresses ?? {};
  });

  function generateId(): string {
    return Math.random().toString(36).substring(2) + Date.now().toString(36);
  }

  function addNatRule() {
    natRules = [...natRules, {
      id: generateId(),
      enabled: true,
      protocol: 'tcp',
      connectingDevices: 'any',
      incomingInterface: '',
      outgoingInterface: '',
      natAddr: '',
      originalPort: '',
      translatedPort: '',
      deviceAddr: '',
      deviceName: '',
      doubleNat: false,
      doubleNatAddr: '',
      comment: '',
    }];
  }

  function removeNatRule(index: number) {
    natRules = natRules.filter((_, i) => i !== index);
  }

  function cancelNftChanges() {
    const nftConfig = data.nftablesConfig ?? { natRules: [] };
    natRules = nftConfig.natRules.map((r: any) => ({ ...r }));
    nftError = null;
  }

  function natRuleSummary(r: NatRuleForm): string {
    const proto = r.protocol.toUpperCase();
    const port = r.originalPort || '*';
    return `${proto} ${r.natAddr || '?'}:${port} → ${r.deviceAddr || '?'}:${r.translatedPort || port}`;
  }

  function getOutgoingInterfacePlaceholder(ifaceName: string): string {
    const addr = interfaceAddresses[ifaceName];
    return addr ? addr.address : '';
  }

  function suggestNatAddr(ifaceName: string, ruleIndex: number): string {
    const info = interfaceAddresses[ifaceName];
    if (!info) return '';
    const octets = info.address.split('.').map(Number);
    if (octets.length !== 4) return '';
    // Collect natAddrs already used by other rules
    const used = new Set(natRules.filter((_, j) => j !== ruleIndex).map((r) => r.natAddr.trim()));
    // Try incrementing the last octet from the interface IP
    for (let offset = 1; offset < 254; offset++) {
      const candidate = [...octets];
      candidate[3] = ((octets[3] + offset - 1) % 254) + 1; // stay in 1-254
      const ip = candidate.join('.');
      if (!used.has(ip) && ip !== info.address) return ip;
    }
    return '';
  }

  function onIncomingInterfaceChange(ruleIndex: number) {
    const rule = natRules[ruleIndex];
    rule.natAddr = suggestNatAddr(rule.incomingInterface, ruleIndex);
  }

  async function saveNftConfig() {
    nftSaving = true;
    nftError = null;

    try {
      const result = await apiPost<{ requestId: string; success: boolean; error: string | null }>(
        '/nftables/config',
        { natRules: natRules.map((r) => ({
          id: r.id,
          enabled: r.enabled,
          protocol: r.protocol,
          connectingDevices: r.connectingDevices.trim() || 'any',
          incomingInterface: r.incomingInterface,
          outgoingInterface: r.outgoingInterface,
          natAddr: r.natAddr.trim(),
          originalPort: r.originalPort.trim(),
          translatedPort: r.translatedPort.trim(),
          deviceAddr: r.deviceAddr.trim(),
          deviceName: r.deviceName.trim(),
          doubleNat: r.doubleNat,
          doubleNatAddr: r.doubleNatAddr.trim(),
          comment: r.comment.trim(),
        }))}
      );

      if (result.error) {
        nftError = result.error.error;
      } else if (result.data?.success) {
        saltState.addNotification({
          message: 'NAT configuration applied successfully',
          type: 'success',
        });
        await invalidateAll();
      } else {
        nftError = result.data?.error ?? 'Unknown error';
      }
    } catch (e) {
      nftError = e instanceof Error ? e.message : 'Failed to apply nftables config';
    } finally {
      nftSaving = false;
    }
  }
</script>

{#if serviceType === 'profinet'}
  <ProfinetDeviceConfig
    config={data.profinetConfig ?? null}
    status={data.profinetStatus ?? null}
    interfaces={data.profinetInterfaces ?? []}
    variables={data.profinetVariables ?? []}
    error={data.error ?? null}
  />
{:else if serviceType === 'nftables'}
<!-- ═══════════════════════════════════════════════════════════════════════ -->
<!-- Nftables Config Page — Unified NAT Rules                              -->
<!-- ═══════════════════════════════════════════════════════════════════════ -->
<div class="config-page">
  <div class="config-header">
    <div>
      <h1>NAT / Firewall Configuration</h1>
      <p class="subtitle">
        Manage NAT rules via nftables. Config persists to
        <code>/etc/tentacle/nftables.json</code> and applies immediately.
      </p>
    </div>
  </div>

  {#if nftError}
    <div class="info-box error">
      <p>{nftError}</p>
    </div>
  {/if}

  {#if data.error && !nftError}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {/if}

  <!-- NAT Rules -->
  <h2 class="section-title">NAT Rules</h2>
  <div class="config-list">
    {#each natRules as rule, i (rule.id)}
      <div class="card">
        <div class="card-header">
          <div class="card-title">
            <span class="badge" class:badge-enabled={rule.enabled} class:badge-disabled={!rule.enabled}>
              {rule.enabled ? 'Enabled' : 'Disabled'}
            </span>
            <span class="mono">{natRuleSummary(rule)}</span>
          </div>
          <button class="btn-ghost btn-danger" onclick={() => removeNatRule(i)} aria-label="Delete rule">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
            </svg>
          </button>
        </div>

        <div class="card-body">
          <!-- Row 1: Protocol, Connecting Devices, Enabled -->
          <div class="form-row">
            <div class="form-group form-group-small">
              <label for="nat-proto-{i}">Protocol</label>
              <select id="nat-proto-{i}" bind:value={rule.protocol} class="input">
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
                <option value="icmp">ICMP</option>
                <option value="all">All</option>
              </select>
            </div>
            <div class="form-group">
              <label for="nat-connecting-{i}">Connecting Devices</label>
              <input id="nat-connecting-{i}" type="text" bind:value={rule.connectingDevices} placeholder="any" class="input mono"
                class:input-invalid={rule.connectingDevices.trim() !== '' && rule.connectingDevices.trim() !== 'any' && !isValidConnectingDevices(rule.connectingDevices)} />
            </div>
            <div class="form-group form-group-toggle">
              <label class="checkbox-label">
                <input type="checkbox" bind:checked={rule.enabled} />
                <span>Enabled</span>
              </label>
            </div>
          </div>

          <!-- Row 2: Incoming Interface, Outgoing Interface -->
          <div class="form-row">
            <div class="form-group">
              <label for="nat-in-iface-{i}">Incoming Interface</label>
              <select id="nat-in-iface-{i}" bind:value={rule.incomingInterface} class="input mono"
                class:input-invalid={!rule.incomingInterface}
                onchange={() => onIncomingInterfaceChange(i)}>
                <option value="" disabled>Select interface...</option>
                {#each availableInterfaces as name}
                  <option value={name}>{name}</option>
                {/each}
              </select>
            </div>
            <div class="form-group">
              <label for="nat-out-iface-{i}">Outgoing Interface</label>
              <select id="nat-out-iface-{i}" bind:value={rule.outgoingInterface} class="input mono"
                class:input-invalid={!rule.outgoingInterface || rule.outgoingInterface === rule.incomingInterface}>
                <option value="" disabled>Select interface...</option>
                {#each availableInterfaces as name}
                  {#if name !== rule.incomingInterface}
                    <option value={name}>{name}</option>
                  {/if}
                {/each}
              </select>
            </div>
          </div>

          <!-- Row 3: NAT IP Address, Original Port, Translated Port (ports hidden for ICMP/All) -->
          <div class="form-row">
            <div class="form-group">
              <label for="nat-addr-{i}">NAT IP Address</label>
              <input id="nat-addr-{i}" type="text" bind:value={rule.natAddr} placeholder="10.0.0.100" class="input mono"
                class:input-invalid={rule.natAddr.trim() !== '' && !isValidIpv4(rule.natAddr)} />
            </div>
            {#if rule.protocol === 'tcp' || rule.protocol === 'udp'}
              <div class="form-group form-group-port">
                <label for="nat-oport-{i}">Original Port</label>
                <input id="nat-oport-{i}" type="text" bind:value={rule.originalPort} placeholder="All" class="input mono"
                  class:input-invalid={rule.originalPort.trim() !== '' && !isValidPortOrRange(rule.originalPort)} />
              </div>
              <div class="form-group form-group-port">
                <label for="nat-tport-{i}">Translated Port</label>
                <input id="nat-tport-{i}" type="text" bind:value={rule.translatedPort} placeholder="All" class="input mono"
                  class:input-invalid={rule.translatedPort.trim() !== '' && !isValidPortOrRange(rule.translatedPort)} />
              </div>
            {/if}
          </div>

          <!-- Row 4: Device IP, Device Name -->
          <div class="form-row">
            <div class="form-group">
              <label for="nat-devaddr-{i}">Device IP</label>
              <input id="nat-devaddr-{i}" type="text" bind:value={rule.deviceAddr} placeholder="192.168.1.100" class="input mono"
                class:input-invalid={rule.deviceAddr.trim() !== '' && !isValidIpv4(rule.deviceAddr)} />
            </div>
            <div class="form-group">
              <label for="nat-devname-{i}">Device Name</label>
              <input id="nat-devname-{i}" type="text" bind:value={rule.deviceName} placeholder="Camera 1" class="input" />
            </div>
          </div>

          <!-- Row 5: Double NAT toggle + address -->
          <div class="form-row">
            <div class="form-group form-group-toggle">
              <label class="checkbox-label">
                <input type="checkbox" bind:checked={rule.doubleNat} />
                <span>Double NAT</span>
              </label>
            </div>
            {#if rule.doubleNat}
              <div class="form-group">
                <label for="nat-dnat-addr-{i}">Double NAT Address</label>
                <input id="nat-dnat-addr-{i}" type="text" bind:value={rule.doubleNatAddr}
                  placeholder={getOutgoingInterfacePlaceholder(rule.outgoingInterface) || 'Outgoing interface IP'}
                  class="input mono"
                  class:input-invalid={rule.doubleNatAddr.trim() !== '' && !isValidIpv4(rule.doubleNatAddr)} />
              </div>
            {/if}
          </div>

          <!-- Row 6: Comment -->
          <div class="form-row">
            <div class="form-group">
              <label for="nat-comment-{i}">Comment</label>
              <input id="nat-comment-{i}" type="text" bind:value={rule.comment} placeholder="Description" class="input" />
            </div>
          </div>

          {#if natValidationErrors[i]?.length > 0}
            <div class="validation-errors">
              {#each natValidationErrors[i] as err}
                <p>{err}</p>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {/each}
  </div>

  <div class="section-actions">
    <button class="btn-ghost btn-add" onclick={addNatRule}>+ Add NAT Rule</button>
  </div>

  <!-- Actions -->
  <div class="actions">
    <div></div>
    <div class="actions-right">
      {#if nftDirty}
        <button class="btn-ghost" onclick={cancelNftChanges} disabled={nftSaving}>
          Cancel
        </button>
      {/if}
      <button class="btn-primary" onclick={saveNftConfig} disabled={nftSaving || !nftDirty || !nftValid}>
        {#if nftSaving}
          Applying...
        {:else}
          Apply Configuration
        {/if}
      </button>
    </div>
  </div>

  <div class="warning-box">
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/>
      <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
    </svg>
    <p>Applying firewall changes takes effect immediately and may affect connectivity.</p>
  </div>
</div>

{:else}
<!-- ═══════════════════════════════════════════════════════════════════════ -->
<!-- Network Config Page (existing)                                        -->
<!-- ═══════════════════════════════════════════════════════════════════════ -->
<div class="config-page">
  <div class="config-header">
    <div>
      <h1>Network Configuration</h1>
      <p class="subtitle">
        Manage persistent network configuration via netplan. Changes are written to
        <code>/etc/netplan/60-tentacle.yaml</code> and applied immediately.
      </p>
    </div>
  </div>

  {#if error}
    <div class="info-box error">
      <p>{error}</p>
    </div>
  {/if}

  {#if data.error && !error}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {/if}

  <div class="config-list">
    {#each configs as cfg, i (i)}
      {@const usedByOthers = new Set(configs.filter((_, j) => j !== i).map((c) => c.interfaceName))}
      <div class="card">
        <div class="card-header">
          <div class="iface-name-input">
            <label for="iface-{i}">Interface</label>
            <select
              id="iface-{i}"
              bind:value={cfg.interfaceName}
              class="input mono"
            >
              <option value="" disabled>Select interface...</option>
              {#each availableInterfaces as name}
                {#if name === cfg.interfaceName || !usedByOthers.has(name)}
                  <option value={name}>{name}</option>
                {/if}
              {/each}
            </select>
          </div>
          <button class="btn-ghost btn-danger" onclick={() => removeInterface(i)} aria-label="Delete interface">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
            </svg>
          </button>
        </div>

        <div class="card-body">
          <div class="form-group">
            <label class="checkbox-label">
              <input type="checkbox" bind:checked={cfg.dhcp4} />
              <span>DHCP (automatic addressing)</span>
            </label>
          </div>

          {#if !cfg.dhcp4}
            <div class="form-group">
              <label>Static Addresses (CIDR format)</label>
              {#each cfg.addresses as _, addrIdx}
                <div class="list-input">
                  <input
                    type="text"
                    bind:value={cfg.addresses[addrIdx]}
                    placeholder="192.168.1.100/24"
                    class="input mono"
                    class:input-invalid={cfg.addresses[addrIdx].trim() !== '' && !isValidCidr(cfg.addresses[addrIdx])}
                  />
                  <button class="btn-ghost btn-small" onclick={() => removeAddress(i, addrIdx)} aria-label="Remove address">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                    </svg>
                  </button>
                </div>
              {/each}
              <button class="btn-ghost btn-small btn-add" onclick={() => addAddress(i)}>
                + Add address
              </button>
            </div>

            <div class="form-group">
              <label for="gw-{i}">Gateway</label>
              <input
                id="gw-{i}"
                type="text"
                bind:value={cfg.gateway4}
                placeholder="192.168.1.1"
                class="input mono"
                class:input-invalid={cfg.gateway4.trim() !== '' && !isValidIpv4(cfg.gateway4)}
              />
            </div>

            <div class="form-group">
              <label>Nameservers</label>
              {#each cfg.nameservers as _, nsIdx}
                <div class="list-input">
                  <input
                    type="text"
                    bind:value={cfg.nameservers[nsIdx]}
                    placeholder="8.8.8.8"
                    class="input mono"
                    class:input-invalid={cfg.nameservers[nsIdx].trim() !== '' && !isValidIpv4(cfg.nameservers[nsIdx])}
                  />
                  <button class="btn-ghost btn-small" onclick={() => removeNameserver(i, nsIdx)} aria-label="Remove nameserver">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                    </svg>
                  </button>
                </div>
              {/each}
              <button class="btn-ghost btn-small btn-add" onclick={() => addNameserver(i)}>
                + Add nameserver
              </button>
            </div>
          {/if}

          <div class="form-group">
            <label for="mtu-{i}">MTU</label>
            <input
              id="mtu-{i}"
              type="number"
              bind:value={cfg.mtu}
              placeholder="1500 (default)"
              class="input"
              class:input-invalid={cfg.mtu.trim() !== '' && !isValidMtu(cfg.mtu)}
              min="68"
              max="9000"
            />
          </div>

          {#if validationErrors[i]?.length > 0}
            <div class="validation-errors">
              {#each validationErrors[i] as err}
                <p>{err}</p>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {/each}
  </div>

  <div class="actions">
    <button class="btn-ghost btn-add" onclick={addInterface} disabled={allConfigured}>
      + Add Interface
    </button>

    <div class="actions-right">
      {#if dirty}
        <button class="btn-ghost" onclick={cancelChanges} disabled={saving}>
          Cancel
        </button>
      {/if}
      <button class="btn-primary" onclick={saveConfig} disabled={saving || configs.length === 0 || !dirty || !valid}>
        {#if saving}
          Applying...
        {:else}
          Apply Configuration
        {/if}
      </button>
    </div>
  </div>

  <div class="warning-box">
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/>
      <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
    </svg>
    <p>Applying configuration changes may temporarily disrupt network connectivity.</p>
  </div>
</div>
{/if}

<style lang="scss">
  .config-page {
    padding: 2rem;
    max-width: 700px;
  }

  .config-header {
    margin-bottom: 1.5rem;

    h1 {
      font-size: 1.5rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }

    .subtitle {
      margin: 0.25rem 0 0;
      color: var(--theme-text-muted);
      font-size: 0.875rem;
      line-height: 1.5;

      code {
        font-family: 'IBM Plex Mono', monospace;
        font-size: 0.8125rem;
        background: color-mix(in srgb, var(--theme-border) 40%, transparent);
        padding: 0.125rem 0.375rem;
        border-radius: var(--rounded-md, 4px);
      }
    }
  }

  .section-title {
    font-size: 1rem;
    font-weight: 600;
    color: var(--theme-text);
    margin: 1.5rem 0 0.75rem;

    &:first-of-type {
      margin-top: 0;
    }
  }

  .section-actions {
    margin-top: 0.5rem;
    margin-bottom: 0.5rem;
  }

  .config-list {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--theme-border);
    background: color-mix(in srgb, var(--theme-surface) 50%, var(--theme-background));
    gap: 0.75rem;
  }

  .card-title {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex: 1;
    min-width: 0;
  }

  .badge {
    font-size: 0.6875rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-md, 4px);
    flex-shrink: 0;
  }

  .badge-enabled {
    color: var(--color-green-500, #22c55e);
    background: color-mix(in srgb, var(--color-green-500, #22c55e) 12%, transparent);
  }

  .badge-disabled {
    color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-border) 40%, transparent);
  }

  .iface-name-input {
    flex: 1;

    label {
      display: block;
      font-size: 0.6875rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--theme-text-muted);
      margin-bottom: 0.25rem;
    }
  }

  .card-body {
    padding: 0.75rem 1rem;
  }

  .form-row {
    display: flex;
    gap: 0.75rem;
    align-items: flex-end;
    margin-bottom: 0.75rem;

    &:last-child {
      margin-bottom: 0;
    }
  }

  .form-group {
    margin-bottom: 0.75rem;
    flex: 1;

    &:last-child {
      margin-bottom: 0;
    }

    > label {
      display: block;
      font-size: 0.8125rem;
      font-weight: 500;
      color: var(--theme-text-muted);
      margin-bottom: 0.25rem;
    }
  }

  .form-group-small {
    flex: 0 0 auto;
    width: 100px;
  }

  .form-group-port {
    flex: 0 0 auto;
    width: 140px;
  }

  .form-group-medium {
    flex: 0 0 auto;
    width: 140px;
  }

  .form-group-toggle {
    flex: 0 0 auto;
    display: flex;
    align-items: flex-end;
    padding-bottom: 0.25rem;
  }

  .form-row .form-group {
    margin-bottom: 0;
  }

  .input {
    width: 100%;
    padding: 0.5rem 0.625rem;
    font-size: 0.8125rem;
    color: var(--theme-text);
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md, 4px);
    outline: none;
    transition: border-color 0.15s ease;
    box-sizing: border-box;

    &:focus {
      border-color: var(--theme-primary);
    }

    &::placeholder {
      color: var(--theme-text-muted);
      opacity: 0.5;
    }
  }

  select.input {
    cursor: pointer;
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%23888' stroke-width='2'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 0.5rem center;
    padding-right: 1.75rem;
  }

  .mono {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.8125rem;
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    font-size: 0.8125rem;
    color: var(--theme-text);

    input[type="checkbox"] {
      width: 1rem;
      height: 1rem;
      accent-color: var(--theme-primary);
    }
  }

  .list-input {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    margin-bottom: 0.375rem;

    .input {
      flex: 1;
    }
  }

  .btn-ghost {
    background: none;
    border: 1px solid transparent;
    color: var(--theme-text-muted);
    cursor: pointer;
    border-radius: var(--rounded-md, 4px);
    padding: 0.375rem;
    transition: all 0.15s ease;

    &:hover {
      color: var(--theme-text);
      background: color-mix(in srgb, var(--theme-border) 30%, transparent);
    }
  }

  .btn-danger:hover {
    color: var(--color-red-500, #ef4444);
  }

  .btn-small {
    padding: 0.25rem 0.375rem;
    font-size: 0.75rem;
  }

  .btn-add {
    font-size: 0.8125rem;
    padding: 0.375rem 0.625rem;
    color: var(--theme-primary);

    &:hover:not(:disabled) {
      background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    }

    &:disabled {
      opacity: 0.4;
      cursor: not-allowed;
    }
  }

  .actions {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-top: 1rem;
    padding-top: 1rem;
  }

  .actions-right {
    display: flex;
    gap: 0.5rem;
  }

  .btn-primary {
    padding: 0.5rem 1.25rem;
    font-size: 0.875rem;
    font-weight: 500;
    color: white;
    background: var(--theme-primary);
    border: none;
    border-radius: var(--rounded-md, 4px);
    cursor: pointer;
    transition: opacity 0.15s ease;

    &:hover:not(:disabled) {
      opacity: 0.9;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .warning-box {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    margin-top: 1.5rem;
    padding: 0.75rem 1rem;
    border-radius: var(--rounded-lg);
    background: color-mix(in srgb, var(--color-amber-500, #f59e0b) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--color-amber-500, #f59e0b) 30%, transparent);

    svg {
      flex-shrink: 0;
      color: var(--color-amber-500, #f59e0b);
      margin-top: 0.125rem;
    }

    p {
      margin: 0;
      font-size: 0.8125rem;
      color: var(--color-amber-500, #f59e0b);
    }
  }

  .info-box {
    padding: 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    margin-bottom: 1rem;

    p {
      margin: 0;
      font-size: 0.875rem;
      color: var(--theme-text-muted);
    }

    &.error {
      border-color: var(--color-red-500, #ef4444);
      p { color: var(--color-red-500, #ef4444); }
    }
  }

  .input-invalid {
    border-color: var(--color-red-500, #ef4444) !important;

    &:focus {
      border-color: var(--color-red-500, #ef4444) !important;
    }
  }

  .validation-errors {
    margin-top: 0.5rem;
    padding: 0.375rem 0.625rem;
    border-radius: var(--rounded-md, 4px);
    background: color-mix(in srgb, var(--color-red-500, #ef4444) 8%, transparent);

    p {
      margin: 0;
      font-size: 0.75rem;
      color: var(--color-red-500, #ef4444);
      line-height: 1.5;
    }
  }
</style>
