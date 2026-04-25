<script lang="ts">
  import type { MqttConfig } from './MqttConfigForm.svelte';
  import type { GitOpsConfig } from './GitOpsConfigForm.svelte';
  import { mantleRepoName, mantleRepoUrl } from './mantleRepo';
  import type { HistoryConfig } from './HistoryConfigForm.svelte';
  import type { MantleEdgeConfig } from './MantleEdgeConfigForm.svelte';
  import { goto } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { apiPut, api } from '$lib/api/client';
  import { getServiceName } from '$lib/constants/services';

  const ADDON_NAMES: Record<string, string> = {
    caddy: 'Caddy',
    network: 'Network',
    gitops: 'GitOps',
    history: 'History',
    'mqtt-broker': 'MQTT Broker',
    'sparkplug-host': 'Sparkplug Host',
  };

  interface Props {
    archetype: string;
    selectedProtocols: Set<string>;
    selectedAddOns: Set<string>;
    mqttConfig: MqttConfig;
    gitopsConfig?: GitOpsConfig;
    historyConfig?: HistoryConfig;
    mantleEdgeConfig?: MantleEdgeConfig;
  }

  let { archetype, selectedProtocols, selectedAddOns, mqttConfig, gitopsConfig, historyConfig, mantleEdgeConfig }: Props = $props();

  const ARCHETYPE_NAMES: Record<string, string> = {
    'sparkplug-gateway': 'Sparkplug Gateway',
    'nat-gateway': 'NAT',
    'mantle-host': 'Mantle Aggregator',
    'mantle-paired-edge': 'Mantle-Paired Edge',
  };

  type StepStatus = 'pending' | 'running' | 'done' | 'error';

  interface ApplyStep {
    label: string;
    status: StepStatus;
    error?: string;
  }

  let applying = $state(false);
  let steps = $state<ApplyStep[]>([]);
  let done = $state(false);
  let hasError = $state(false);

  function updateStep(index: number, status: StepStatus, error?: string) {
    steps[index] = { ...steps[index], status, error };
  }

  async function createMantleRepo(mantleUrl: string, repoName: string): Promise<{ ok: boolean; error?: string }> {
    const base = mantleUrl.replace(/\/+$/, '');
    try {
      const res = await fetch(`${base}/api/v1/gitops/repos`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: repoName }),
      });
      if (res.ok) return { ok: true };
      // Already-exists is fine — the bare repo just needs to be there.
      const text = await res.text().catch(() => res.statusText);
      if (res.status === 400 && /already exists/i.test(text)) return { ok: true };
      let msg = text;
      try {
        const json = JSON.parse(text);
        if (json.error) msg = json.error;
      } catch { /* use raw text */ }
      return { ok: false, error: `${res.status}: ${msg}` };
    } catch (e) {
      return { ok: false, error: e instanceof Error ? e.message : 'network error' };
    }
  }

  async function saveGitOpsConfig(): Promise<boolean> {
    if (!selectedAddOns.has('gitops') || !gitopsConfig) return true;

    // Mantle mode: pre-create the bare repo on the mantle, then resolve the
    // repoUrl from <mantleUrl>/git/<group>--<node>.git so the edge gitops
    // module talks to it via HTTP. The user only types group/node.
    let resolvedRepoUrl = gitopsConfig.repoUrl;
    if (gitopsConfig.source === 'mantle') {
      const createIdx = steps.length;
      steps.push({ label: 'Creating bare repo on mantle', status: 'running' });
      const repoName = mantleRepoName(gitopsConfig.group, gitopsConfig.node);
      const created = await createMantleRepo(gitopsConfig.mantleUrl, repoName);
      if (!created.ok) {
        updateStep(createIdx, 'error', `Failed to create repo on mantle: ${created.error}`);
        return false;
      }
      updateStep(createIdx, 'done');
      resolvedRepoUrl = mantleRepoUrl(gitopsConfig.mantleUrl, gitopsConfig.group, gitopsConfig.node);
    }

    const idx = steps.length;
    steps.push({ label: 'Writing GitOps configuration', status: 'running' });
    const configs: [string, string][] = [
      ['GITOPS_REPO_URL', resolvedRepoUrl],
      ['GITOPS_BRANCH', gitopsConfig.branch],
      ['GITOPS_PATH', gitopsConfig.configPath],
      ['GITOPS_POLL_INTERVAL_S', gitopsConfig.pollInterval],
      ['GITOPS_AUTO_PUSH', String(gitopsConfig.autoPush)],
      ['GITOPS_AUTO_PULL', String(gitopsConfig.autoPull)],
    ];
    for (const [envVar, value] of configs) {
      const result = await apiPut(`/config/gitops/${envVar}`, { value });
      if (result.error) {
        updateStep(idx, 'error', `Failed to set ${envVar}: ${result.error.error}`);
        return false;
      }
    }
    updateStep(idx, 'done');
    return true;
  }

  async function saveHistoryConfig(): Promise<boolean> {
    if (!selectedAddOns.has('history') || !historyConfig) return true;
    const idx = steps.length;
    steps.push({ label: 'Writing History configuration', status: 'running' });
    const configs: [string, string][] = [
      ['HISTORY_DB_HOST', historyConfig.host],
      ['HISTORY_DB_PORT', historyConfig.port],
      ['HISTORY_DB_USER', historyConfig.user],
      ['HISTORY_DB_PASSWORD', historyConfig.password],
      ['HISTORY_DB_NAME', historyConfig.dbname],
    ];
    for (const [envVar, value] of configs) {
      const result = await apiPut(`/config/history/${envVar}`, { value });
      if (result.error) {
        updateStep(idx, 'error', `Failed to set ${envVar}: ${result.error.error}`);
        return false;
      }
    }
    updateStep(idx, 'done');
    return true;
  }

  async function enableModule(moduleId: string, label: string): Promise<boolean> {
    const idx = steps.length;
    steps.push({ label, status: 'running' });
    const result = await apiPut(`/orchestrator/desired-services/${moduleId}`, {
      version: 'latest',
      running: true,
    });
    if (result.error) {
      updateStep(idx, 'error', `Failed: ${result.error.error}`);
      return false;
    }
    updateStep(idx, 'done');
    return true;
  }

  async function waitForModules(moduleIds: Set<string>): Promise<boolean> {
    const idx = steps.length;
    steps.push({ label: 'Waiting for services to start', status: 'running' });

    const maxAttempts = 30;
    let attempts = 0;

    while (attempts < maxAttempts) {
      await new Promise(r => setTimeout(r, 2000));
      attempts++;

      const statusResult = await api<Array<{
        moduleId: string;
        systemdState: string;
        reconcileState: string;
      }>>('/orchestrator/service-statuses');

      if (statusResult.error) continue;

      const statuses = statusResult.data ?? [];
      const allActive = [...moduleIds].every(modId => {
        const s = statuses.find(st => st.moduleId === modId);
        return s && s.systemdState === 'active' && (s.reconcileState === 'ok' || s.reconcileState === 'needs_config');
      });

      if (allActive) {
        updateStep(idx, 'done');
        return true;
      }

      const errored = statuses.find(s =>
        moduleIds.has(s.moduleId) && s.reconcileState === 'error'
      );
      if (errored) {
        updateStep(idx, 'error', `${errored.moduleId} failed to start`);
        return false;
      }
    }

    updateStep(idx, 'error', 'Timed out waiting for services');
    return false;
  }

  async function applySparkplugGateway() {
    // Write MQTT config
    const configIdx = steps.length;
    steps.push({ label: 'Writing MQTT configuration', status: 'running' });
    const configEntries = Object.entries(mqttConfig).filter(([_, v]) => v !== '');
    for (const [envVar, value] of configEntries) {
      const result = await apiPut(`/config/mqtt/${envVar}`, { value });
      if (result.error) {
        updateStep(configIdx, 'error', `Failed to set ${envVar}: ${result.error.error}`);
        return false;
      }
    }
    updateStep(configIdx, 'done');

    // Enable scanners
    const protocols = [...selectedProtocols];
    for (const protocol of protocols) {
      if (!await enableModule(protocol, `Enabling ${getServiceName(protocol)}`)) return false;
    }

    // Enable gateway and MQTT
    if (!await enableModule('gateway', 'Enabling Gateway')) return false;
    if (!await enableModule('mqtt', 'Enabling MQTT bridge')) return false;

    // Write add-on configs before enabling their modules
    if (!await saveGitOpsConfig()) return false;
    if (!await saveHistoryConfig()) return false;

    // Enable add-ons
    for (const addon of selectedAddOns) {
      if (!await enableModule(addon, `Enabling ${ADDON_NAMES[addon] ?? addon}`)) return false;
    }

    // Wait for all
    const expected = new Set([...protocols, 'gateway', 'mqtt', ...selectedAddOns]);
    return await waitForModules(expected);
  }

  async function applyNatGateway() {
    if (!await enableModule('network', 'Enabling Network Manager')) return false;
    if (!await enableModule('nftables', 'Enabling Firewall (nftables)')) return false;

    // Write add-on configs before enabling their modules
    if (!await saveGitOpsConfig()) return false;
    if (!await saveHistoryConfig()) return false;

    // Enable add-ons
    for (const addon of selectedAddOns) {
      if (!await enableModule(addon, `Enabling ${ADDON_NAMES[addon] ?? addon}`)) return false;
    }

    return await waitForModules(new Set(['network', 'nftables', ...selectedAddOns]));
  }

  async function applyMantleHost() {
    if (!await enableModule('sparkplug-host', 'Enabling Sparkplug Host')) return false;

    if (!await saveGitOpsConfig()) return false;
    if (!await saveHistoryConfig()) return false;

    for (const addon of selectedAddOns) {
      if (!await enableModule(addon, `Enabling ${ADDON_NAMES[addon] ?? addon}`)) return false;
    }

    return await waitForModules(new Set(['sparkplug-host', ...selectedAddOns]));
  }

  async function applyMantleEdge() {
    if (!mantleEdgeConfig) {
      hasError = true;
      return false;
    }
    const cfg = mantleEdgeConfig;

    // 1. Pre-create bare repo on the mantle.
    const createIdx = steps.length;
    steps.push({ label: 'Creating bare repo on mantle', status: 'running' });
    const repoName = mantleRepoName(cfg.group, cfg.node);
    const created = await createMantleRepo(cfg.mantleUrl, repoName);
    if (!created.ok) {
      updateStep(createIdx, 'error', `Failed to create repo on mantle: ${created.error}`);
      return false;
    }
    updateStep(createIdx, 'done');

    // 2. Write MQTT config.
    const mqttIdx = steps.length;
    steps.push({ label: 'Writing MQTT configuration', status: 'running' });
    const mqttEntries: [string, string][] = [
      ['MQTT_BROKER_URL', cfg.mqttBrokerUrl],
      ['MQTT_GROUP_ID', cfg.group],
      ['MQTT_EDGE_NODE', cfg.node],
      ['MQTT_CLIENT_ID', `tentacle-${cfg.node}`],
      ['MQTT_USERNAME', cfg.mqttUsername],
      ['MQTT_PASSWORD', cfg.mqttPassword],
    ];
    for (const [envVar, value] of mqttEntries) {
      const result = await apiPut(`/config/mqtt/${envVar}`, { value });
      if (result.error) {
        updateStep(mqttIdx, 'error', `Failed to set ${envVar}: ${result.error.error}`);
        return false;
      }
    }
    updateStep(mqttIdx, 'done');

    // 3. Write GitOps config pointing at the mantle repo.
    const gitopsIdx = steps.length;
    steps.push({ label: 'Writing GitOps configuration', status: 'running' });
    const gitopsEntries: [string, string][] = [
      ['GITOPS_REPO_URL', mantleRepoUrl(cfg.mantleUrl, cfg.group, cfg.node)],
      ['GITOPS_BRANCH', 'main'],
      ['GITOPS_PATH', 'config'],
      ['GITOPS_POLL_INTERVAL_S', '60'],
      ['GITOPS_AUTO_PUSH', 'true'],
      ['GITOPS_AUTO_PULL', 'true'],
    ];
    for (const [envVar, value] of gitopsEntries) {
      const result = await apiPut(`/config/gitops/${envVar}`, { value });
      if (result.error) {
        updateStep(gitopsIdx, 'error', `Failed to set ${envVar}: ${result.error.error}`);
        return false;
      }
    }
    updateStep(gitopsIdx, 'done');

    // 4. Enable mqtt + gitops modules.
    if (!await enableModule('mqtt', 'Enabling MQTT bridge')) return false;
    if (!await enableModule('gitops', 'Enabling GitOps')) return false;

    return await waitForModules(new Set(['mqtt', 'gitops']));
  }

  async function applyConfiguration() {
    applying = true;
    done = false;
    hasError = false;
    steps = [];

    let success = false;
    switch (archetype) {
      case 'sparkplug-gateway':
        success = await applySparkplugGateway();
        break;
      case 'nat-gateway':
        success = await applyNatGateway();
        break;
      case 'mantle-host':
        success = await applyMantleHost();
        break;
      case 'mantle-paired-edge':
        success = await applyMantleEdge();
        break;
    }

    if (success) {
      done = true;
      saltState.addNotification({ message: 'Setup complete! All services are running.', type: 'success' });
    } else {
      hasError = true;
    }
    applying = false;
  }

  function goToNextStep() {
    sessionStorage.setItem('setup_dismissed', 'true');
    goto('/');
  }
</script>

<div class="review-panel">
  {#if !applying && !done && !hasError}
    <section class="summary">
      <h2>Review Configuration</h2>

      <div class="summary-row">
        <span class="summary-label">Architecture</span>
        <span class="summary-value">{ARCHETYPE_NAMES[archetype] ?? archetype}</span>
      </div>

      {#if archetype === 'sparkplug-gateway'}
        <div class="summary-row">
          <span class="summary-label">Protocols</span>
          <span class="summary-value">
            {#each [...selectedProtocols] as protocol}
              <span class="badge">{getServiceName(protocol)}</span>
            {/each}
          </span>
        </div>

        <div class="summary-row">
          <span class="summary-label">MQTT Broker</span>
          <span class="summary-value mono">{mqttConfig.MQTT_BROKER_URL}</span>
        </div>

        <div class="summary-row">
          <span class="summary-label">Group / Node</span>
          <span class="summary-value mono">{mqttConfig.MQTT_GROUP_ID} / {mqttConfig.MQTT_EDGE_NODE}</span>
        </div>

        {#if mqttConfig.MQTT_USERNAME}
          <div class="summary-row">
            <span class="summary-label">Authentication</span>
            <span class="summary-value">Username: {mqttConfig.MQTT_USERNAME}</span>
          </div>
        {/if}
      {:else if archetype === 'nat-gateway'}
        <div class="summary-row">
          <span class="summary-label">Modules</span>
          <span class="summary-value">
            <span class="badge">Network Manager</span>
            <span class="badge">Firewall (nftables)</span>
          </span>
        </div>
      {:else if archetype === 'mantle-host'}
        <div class="summary-row">
          <span class="summary-label">Modules</span>
          <span class="summary-value">
            <span class="badge">Sparkplug Host</span>
          </span>
        </div>
      {:else if archetype === 'mantle-paired-edge' && mantleEdgeConfig}
        <div class="summary-row">
          <span class="summary-label">Modules</span>
          <span class="summary-value">
            <span class="badge">MQTT</span>
            <span class="badge">GitOps</span>
          </span>
        </div>
        <div class="summary-row">
          <span class="summary-label">Group / Node</span>
          <span class="summary-value mono">{mantleEdgeConfig.group} / {mantleEdgeConfig.node}</span>
        </div>
        <div class="summary-row">
          <span class="summary-label">Mantle URL</span>
          <span class="summary-value mono">{mantleEdgeConfig.mantleUrl}</span>
        </div>
        <div class="summary-row">
          <span class="summary-label">MQTT Broker</span>
          <span class="summary-value mono">{mantleEdgeConfig.mqttBrokerUrl}</span>
        </div>
        {#if mantleEdgeConfig.mqttUsername}
          <div class="summary-row">
            <span class="summary-label">Authentication</span>
            <span class="summary-value">Username: {mantleEdgeConfig.mqttUsername}</span>
          </div>
        {/if}
        <div class="summary-row">
          <span class="summary-label">Repo URL</span>
          <span class="summary-value mono">{mantleRepoUrl(mantleEdgeConfig.mantleUrl, mantleEdgeConfig.group, mantleEdgeConfig.node)}</span>
        </div>
      {/if}

      {#if selectedAddOns.size > 0}
        <div class="summary-row">
          <span class="summary-label">Add-ons</span>
          <span class="summary-value">
            {#each [...selectedAddOns] as addon}
              <span class="badge">{ADDON_NAMES[addon] ?? addon}</span>
            {/each}
          </span>
        </div>
      {/if}

      {#if selectedAddOns.has('gitops') && gitopsConfig}
        {#if gitopsConfig.source === 'mantle' && gitopsConfig.mantleUrl && gitopsConfig.group && gitopsConfig.node}
          <div class="summary-row">
            <span class="summary-label">GitOps Source</span>
            <span class="summary-value">Mantle</span>
          </div>
          <div class="summary-row">
            <span class="summary-label">Mantle URL</span>
            <span class="summary-value mono">{gitopsConfig.mantleUrl}</span>
          </div>
          <div class="summary-row">
            <span class="summary-label">Group / Node</span>
            <span class="summary-value mono">{gitopsConfig.group} / {gitopsConfig.node}</span>
          </div>
          <div class="summary-row">
            <span class="summary-label">Repo URL</span>
            <span class="summary-value mono">{mantleRepoUrl(gitopsConfig.mantleUrl, gitopsConfig.group, gitopsConfig.node)}</span>
          </div>
          <div class="summary-row">
            <span class="summary-label">GitOps Branch</span>
            <span class="summary-value mono">{gitopsConfig.branch}</span>
          </div>
        {:else if gitopsConfig.repoUrl}
          <div class="summary-row">
            <span class="summary-label">GitOps Source</span>
            <span class="summary-value">External</span>
          </div>
          <div class="summary-row">
            <span class="summary-label">GitOps Repo</span>
            <span class="summary-value mono">{gitopsConfig.repoUrl}</span>
          </div>
          <div class="summary-row">
            <span class="summary-label">GitOps Branch</span>
            <span class="summary-value mono">{gitopsConfig.branch}</span>
          </div>
        {/if}
      {/if}

      {#if selectedAddOns.has('history') && historyConfig}
        <div class="summary-row">
          <span class="summary-label">History DB</span>
          <span class="summary-value mono">
            {historyConfig.mode === 'local' ? 'Local install' : `${historyConfig.host}:${historyConfig.port}`}
            &middot; {historyConfig.dbname}
          </span>
        </div>
      {/if}
    </section>

    <button class="apply-btn" onclick={applyConfiguration}>
      Apply & Start
    </button>
  {/if}

  {#if applying || done || hasError}
    <section class="progress">
      <h2>{done ? 'Setup Complete' : hasError ? 'Setup Failed' : 'Applying Configuration...'}</h2>

      <ul class="step-list">
        {#each steps as step}
          <li class="step-item" class:done={step.status === 'done'} class:error={step.status === 'error'} class:running={step.status === 'running'}>
            <span class="step-icon">
              {#if step.status === 'done'}
                <svg viewBox="0 0 20 20" fill="currentColor" width="16" height="16">
                  <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                </svg>
              {:else if step.status === 'error'}
                <svg viewBox="0 0 20 20" fill="currentColor" width="16" height="16">
                  <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
                </svg>
              {:else if step.status === 'running'}
                <span class="spinner"></span>
              {:else}
                <span class="dot"></span>
              {/if}
            </span>
            <span class="step-text">
              {step.label}
              {#if step.error}
                <span class="step-error">{step.error}</span>
              {/if}
            </span>
          </li>
        {/each}
      </ul>
    </section>

    {#if done}
      <button class="apply-btn" onclick={goToNextStep}>
        Continue
      </button>
    {/if}

    {#if !applying && hasError}
      <button class="retry-btn" onclick={applyConfiguration}>
        Retry
      </button>
    {/if}
  {/if}
</div>

<style lang="scss">
  .review-panel {
    max-width: 550px;
  }

  .summary {
    margin-bottom: 1.5rem;

    h2 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 1rem;
    }
  }

  .summary-row {
    display: flex;
    align-items: baseline;
    gap: 1rem;
    padding: 0.5rem 0;
    border-bottom: 1px solid var(--theme-border);

    &:last-child {
      border-bottom: none;
    }
  }

  .summary-label {
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    flex: 0 0 120px;
  }

  .summary-value {
    font-size: 0.875rem;
    color: var(--theme-text);
    display: flex;
    flex-wrap: wrap;
    gap: 0.375rem;

    &.mono {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.8125rem;
    }
  }

  .badge {
    font-size: 0.6875rem;
    font-weight: 600;
    padding: 0.125rem 0.5rem;
    border-radius: var(--rounded-full);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    color: var(--theme-text);
  }

  .apply-btn {
    padding: 0.625rem 1.5rem;
    font-size: 0.875rem;
    font-weight: 600;
    color: white;
    background: var(--theme-primary);
    border: none;
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover {
      background: var(--theme-primary-hover);
    }
  }

  .retry-btn {
    padding: 0.5rem 1.25rem;
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--theme-text);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    cursor: pointer;
    margin-top: 0.75rem;

    &:hover {
      border-color: var(--theme-primary);
    }
  }

  .progress {
    margin-bottom: 1.5rem;

    h2 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 1rem;
    }
  }

  .step-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .step-item {
    display: flex;
    align-items: flex-start;
    gap: 0.625rem;
    padding: 0.5rem 0;
    font-size: 0.875rem;
    color: var(--theme-text-muted);

    &.done { color: var(--theme-text); }
    &.running { color: var(--theme-text); }
    &.error { color: var(--red-400, #f87171); }
  }

  .step-icon {
    flex-shrink: 0;
    width: 20px;
    height: 20px;
    display: flex;
    align-items: center;
    justify-content: center;

    .done & { color: var(--emerald-500, #10b981); }
    .error & { color: var(--red-400, #f87171); }
    .running & { color: var(--theme-primary); }
  }

  .step-text {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
  }

  .step-error {
    font-size: 0.75rem;
    color: var(--red-400, #f87171);
  }

  .spinner {
    width: 14px;
    height: 14px;
    border: 2px solid var(--theme-border);
    border-top-color: var(--theme-primary);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--theme-border);
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
