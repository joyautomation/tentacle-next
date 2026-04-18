<script lang="ts">
  import type { PageData } from './$types';
  import { invalidateAll } from '$app/navigation';
  import { apiPut, apiDelete } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import {
    GlobeAlt,
    CheckCircle,
    XCircle,
    ArrowDownTray,
  } from '@joyautomation/salt/icons';
  import GitOpsSetup from '$lib/components/GitOpsSetup.svelte';
  import HistorySetup from '$lib/components/HistorySetup.svelte';
  import HistoryTrends from '$lib/components/HistoryTrends.svelte';
  import { getModuleName } from '$lib/constants/services';
  import { isMonolith } from '$lib/stores/mode';
  import { get } from 'svelte/store';

  let { data }: { data: PageData } = $props();

  let selectedVersion = $state('latest');
  let installing = $state(false);
  let savingConfig = $state(false);

  // Config form values for required config fields
  let configValues: Record<string, string> = $state({});

  const requiredConfig = $derived(data.module?.requiredConfig ?? []);

  // Build lookup from existing config
  const existingConfigByEnvVar = $derived(
    Object.fromEntries((data.existingConfig ?? []).map((e: { envVar: string; value: string }) => [e.envVar, e.value]))
  );

  // Initialize form values from existing config or defaults
  $effect(() => {
    const vals: Record<string, string> = {};
    for (const field of requiredConfig) {
      vals[field.envVar] = existingConfigByEnvVar[field.envVar] ?? field.default ?? '';
    }
    configValues = vals;
  });

  async function saveConfig() {
    savingConfig = true;
    const errors: string[] = [];

    for (const field of requiredConfig) {
      const value = configValues[field.envVar] ?? '';
      if (field.required && !value) {
        errors.push(`${field.envVar} is required`);
        continue;
      }
      const result = await apiPut(`/config/${data.moduleId}/${field.envVar}`, { value });
      if (result.error) {
        errors.push(`${field.envVar}: ${result.error.error}`);
      }
    }

    savingConfig = false;

    if (errors.length > 0) {
      saltState.addNotification({ message: errors.join('; '), type: 'error' });
    } else {
      saltState.addNotification({ message: 'Configuration saved', type: 'success' });
      await invalidateAll();
    }
  }

  // Derive available version options
  const versionOptions = $derived(() => {
    const options: string[] = ['latest'];
    if (data.versions?.latestVersion) {
      // Only add if not already 'latest'
    }
    // Add installed versions
    for (const v of data.versions?.installedVersions ?? []) {
      if (v !== 'unknown' && !options.includes(v)) {
        options.push(v);
      }
    }
    return options;
  });

  const isInstalled = $derived(data.desiredService !== null || data.serviceStatus !== null);
  const isRunning = $derived(data.serviceStatus?.systemdState === 'active');
  const reconcileState = $derived(data.serviceStatus?.reconcileState ?? null);
  const needsConfig = $derived(reconcileState === 'needs_config' && requiredConfig.length > 0);

  async function installModule() {
    installing = true;
    try {
      const result = await apiPut<{ moduleId: string; version: string; running: boolean }>(
        `/orchestrator/desired-services/${data.moduleId}`,
        {
          version: selectedVersion,
          running: true,
        }
      );

      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        const desc = data.module?.description ?? data.moduleId;
        saltState.addNotification({
          message: get(isMonolith) ? `Enabling ${desc}...` : `Installing ${desc}...`,
          type: 'success',
        });
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({
        message: err instanceof Error ? err.message : 'Failed to install module',
        type: 'error',
      });
    } finally {
      installing = false;
    }
  }

  async function uninstallModule() {
    installing = true;
    try {
      const result = await apiDelete<boolean>(
        `/orchestrator/desired-services/${data.moduleId}`
      );

      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: 'error' });
      } else {
        saltState.addNotification({
          message: get(isMonolith) ? `Disabled ${data.moduleId}` : `Removed ${data.moduleId} from managed services`,
          type: 'success',
        });
        await invalidateAll();
      }
    } catch (err) {
      saltState.addNotification({
        message: err instanceof Error ? err.message : 'Failed to uninstall module',
        type: 'error',
      });
    } finally {
      installing = false;
    }
  }
</script>

<div class="module-page">
  {#if data.error}
    <div class="info-box error">
      <p>{data.error}</p>
    </div>
  {/if}

  {#if data.module}
    <div class="module-header">
      <div class="module-info">
        <h1>{getModuleName(data.module.moduleId)}</h1>
        <p class="module-desc">{data.module.description}</p>
        <p class="module-meta">{data.module.moduleId} &middot; {data.module.runtime} &middot; {data.module.category}</p>
      </div>
      <div class="header-badges">
        {#if data.module.experimental}
          <span class="status-badge experimental">Experimental</span>
        {/if}
        {#if isInstalled}
          <span class="status-badge" class:running={isRunning} class:stopped={!isRunning}>
            {isRunning ? 'Running' : reconcileState ?? 'Stopped'}
          </span>
        {:else}
          <span class="status-badge not-installed">{$isMonolith ? 'Disabled' : 'Not Installed'}</span>
        {/if}
      </div>
    </div>

    {#if !$isMonolith}
      <!-- Internet Connectivity -->
      <div class="section">
        <div class="detail-row">
          <span class="label">
            <GlobeAlt size="1rem" />
            Internet
          </span>
          <span class="value connectivity" class:online={data.online} class:offline={!data.online}>
            {#if data.online}
              <CheckCircle size="1rem" />
              Connected
            {:else}
              <XCircle size="1rem" />
              Offline
            {/if}
          </span>
        </div>
      </div>

      <!-- Version Information -->
      <div class="section">
        <h2>Versions</h2>

        {#if data.versions?.latestVersion}
          <div class="detail-row">
            <span class="label">Latest (GitHub)</span>
            <span class="value">{data.versions.latestVersion}</span>
          </div>
        {:else}
          <div class="detail-row">
            <span class="label">Latest (GitHub)</span>
            <span class="value muted">{data.online ? 'No releases found' : 'Unavailable (offline)'}</span>
          </div>
        {/if}

        {#if data.versions?.activeVersion}
          <div class="detail-row">
            <span class="label">Active Version</span>
            <span class="value">{data.versions.activeVersion}</span>
          </div>
        {/if}

        {#if (data.versions?.installedVersions ?? []).length > 0}
          <div class="detail-row">
            <span class="label">Installed on Disk</span>
            <span class="value">{data.versions?.installedVersions.join(', ')}</span>
          </div>
        {/if}
      </div>
    {/if}

    <!-- Install / Manage -->
    <div class="section">
      <h2>{isInstalled ? 'Manage' : ($isMonolith ? 'Enable' : 'Install')}</h2>

      {#if data.moduleId === 'history' && $isMonolith}
        <!-- History in monolith always goes through the DB setup wizard. -->
        <HistorySetup />
        {#if isInstalled}
          <div class="install-controls">
            <button
              class="uninstall-btn"
              onclick={uninstallModule}
              disabled={installing}
            >
              {installing ? 'Disabling...' : 'Disable'}
            </button>
          </div>
        {/if}
      {:else if !isInstalled}
        <div class="install-controls">
          {#if !$isMonolith}
            <div class="version-select">
              <label for="version-select">Version</label>
              <select id="version-select" bind:value={selectedVersion}>
                {#each versionOptions() as version}
                  <option value={version}>
                    {version}{version === 'latest' && data.versions?.latestVersion ? ` (${data.versions.latestVersion})` : ''}
                  </option>
                {/each}
              </select>
            </div>
          {/if}
          <button
            class="install-btn"
            onclick={installModule}
            disabled={installing || (!$isMonolith && !data.online && (data.versions?.installedVersions ?? []).length === 0)}
          >
            {#if !$isMonolith}<ArrowDownTray size="1rem" />{/if}
            {installing ? ($isMonolith ? 'Enabling...' : 'Installing...') : ($isMonolith ? 'Enable' : 'Install & Start')}
          </button>
          {#if !$isMonolith && !data.online && (data.versions?.installedVersions ?? []).length === 0}
            <p class="help-text">No local versions available and server is offline. Connect to the internet to download.</p>
          {/if}
        </div>
      {:else}
        <!-- Already installed — show current state and options -->
        {#if data.desiredService && !$isMonolith}
          <div class="detail-row">
            <span class="label">Desired Version</span>
            <span class="value">{data.desiredService.version}</span>
          </div>
          <div class="detail-row">
            <span class="label">Desired Running</span>
            <span class="value">{data.desiredService.running ? 'Yes' : 'No'}</span>
          </div>
        {/if}
        {#if needsConfig && data.moduleId === 'gitops'}
          <GitOpsSetup />
        {:else if needsConfig}
          <form class="config-form" onsubmit={(e) => { e.preventDefault(); saveConfig(); }}>
            <p class="config-hint">Complete the required configuration to start this module.</p>
            {#each requiredConfig as field}
              <div class="config-field">
                <label for="cfg-{field.envVar}">{field.envVar}</label>
                {#if field.description}
                  <p class="field-desc">{field.description}</p>
                {/if}
                <input
                  id="cfg-{field.envVar}"
                  type="text"
                  bind:value={configValues[field.envVar]}
                  placeholder={field.default ?? ''}
                />
              </div>
            {/each}
            <button type="submit" class="save-btn" disabled={savingConfig}>
              {savingConfig ? 'Saving...' : 'Save & Start'}
            </button>
          </form>
        {:else if data.serviceStatus?.lastError}
          <div class="info-box error">
            <p>{data.serviceStatus.lastError}</p>
          </div>
        {/if}
        <div class="install-controls">
          <button
            class="uninstall-btn"
            onclick={uninstallModule}
            disabled={installing}
          >
            {installing ? ($isMonolith ? 'Disabling...' : 'Removing...') : ($isMonolith ? 'Disable' : 'Remove from Managed Services')}
          </button>
        </div>
      {/if}
    </div>

    {#if data.moduleId === 'history' && isInstalled && isRunning}
      <div class="section trends-section">
        <h2>Trends</h2>
        <HistoryTrends />
      </div>
    {/if}
  {:else if !data.error}
    <div class="info-box">
      <p>Module "{data.moduleId}" not found in the orchestrator registry.</p>
    </div>
  {/if}
</div>

<style lang="scss">
  .module-page {
    padding: 2rem;
    max-width: 800px;
  }

  .module-page:has(.trends-section) {
    max-width: 1400px;
  }

  .trends-section {
    margin-top: 1.5rem;
  }

  .module-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 2rem;
  }

  .module-info {
    h1 {
      font-size: 1.5rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0;
    }
    .module-desc {
      margin: 0.25rem 0 0;
      color: var(--theme-text-secondary);
      font-size: 0.875rem;
    }
    .module-meta {
      margin: 0.25rem 0 0;
      color: var(--theme-text-muted);
      font-size: 0.8125rem;
      font-family: var(--font-mono, monospace);
    }
  }

  .header-badges {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }

  .status-badge {
    padding: 0.25rem 0.75rem;
    border-radius: var(--rounded-full);
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    flex-shrink: 0;
    &.running {
      background: var(--badge-green-bg);
      color: var(--badge-green-text);
    }
    &.stopped {
      background: var(--badge-amber-bg);
      color: var(--badge-amber-text);
    }
    &.not-installed {
      background: var(--badge-muted-bg);
      color: var(--badge-muted-text);
    }
    &.experimental {
      background: var(--badge-amber-bg);
      color: var(--badge-amber-text);
      border: 1px solid var(--badge-amber-border);
    }
  }

  .section {
    margin-bottom: 2rem;

    h2 {
      font-size: 0.875rem;
      font-weight: 600;
      color: var(--theme-text-muted);
      text-transform: uppercase;
      letter-spacing: 0.05em;
      margin: 0 0 0.75rem;
    }
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
    padding: 0.5rem 0;
    &:not(:last-child) {
      border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
    }
  }

  .label {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 0.375rem;
  }

  .value {
    font-size: 0.8125rem;
    color: var(--theme-text);
    &.muted {
      color: var(--theme-text-muted);
      font-style: italic;
    }
  }

  .connectivity {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    &.online {
      color: var(--badge-green-text);
    }
    &.offline {
      color: var(--badge-muted-text);
    }
  }

  .install-controls {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    margin-top: 0.5rem;
  }

  .version-select {
    display: flex;
    align-items: center;
    gap: 0.75rem;

    label {
      font-size: 0.8125rem;
      color: var(--theme-text-muted);
      flex-shrink: 0;
    }

    select {
      flex: 1;
      padding: 0.375rem 0.75rem;
      border-radius: var(--rounded-lg);
      border: 1px solid var(--theme-border);
      background: var(--theme-surface);
      color: var(--theme-text);
      font-size: 0.8125rem;
      font-family: var(--font-mono, monospace);
    }
  }

  .install-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    padding: 0.5rem 1.5rem;
    border-radius: var(--rounded-lg);
    border: none;
    background: var(--theme-primary);
    color: white;
    font-size: 0.875rem;
    font-weight: 600;
    cursor: pointer;
    transition: opacity 0.15s;

    &:hover:not(:disabled) {
      opacity: 0.9;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .uninstall-btn {
    padding: 0.5rem 1.5rem;
    border-radius: var(--rounded-lg);
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    color: var(--theme-text);
    font-size: 0.875rem;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;

    &:hover:not(:disabled) {
      border-color: var(--color-red-500, #ef4444);
      color: var(--color-red-500, #ef4444);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .help-text {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    margin: 0;
  }

  .config-form {
    margin-bottom: 1.5rem;
  }

  .config-hint {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 1rem;
  }

  .config-field {
    margin-bottom: 0.75rem;

    label {
      display: block;
      font-size: 0.8125rem;
      font-weight: 500;
      color: var(--theme-text);
      margin-bottom: 0.25rem;
      font-family: var(--font-mono, monospace);
    }

    .field-desc {
      font-size: 0.75rem;
      color: var(--theme-text-muted);
      margin: 0 0 0.375rem;
    }

    input[type='text'] {
      width: 100%;
      padding: 0.5rem 0.75rem;
      font-size: 0.875rem;
      font-family: 'IBM Plex Mono', monospace;
      background: var(--theme-surface);
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-md);
      color: var(--theme-text);
      outline: none;
      box-sizing: border-box;

      &:focus {
        border-color: var(--theme-primary);
      }
    }
  }

  .save-btn {
    margin-top: 0.5rem;
    padding: 0.5rem 1.25rem;
    font-size: 0.875rem;
    font-weight: 600;
    color: white;
    background: var(--theme-primary);
    border: none;
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover:not(:disabled) {
      opacity: 0.9;
    }

    &:disabled {
      opacity: 0.6;
      cursor: not-allowed;
    }
  }

  .info-box {
    padding: 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--theme-text-muted); }
    &.error {
      border-color: var(--color-red-500, #ef4444);
      p { color: var(--color-red-500, #ef4444); }
    }
  }
</style>
