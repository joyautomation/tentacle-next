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

  let { data }: { data: PageData } = $props();

  let selectedVersion = $state('latest');
  let installing = $state(false);

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
        saltState.addNotification({
          message: `Installing ${data.module?.description ?? data.moduleId}...`,
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
          message: `Removed ${data.moduleId} from managed services`,
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
        <h1>{data.module.description}</h1>
        <p class="module-meta">{data.module.moduleId} &middot; {data.module.runtime} &middot; {data.module.category}</p>
      </div>
      {#if isInstalled}
        <span class="status-badge" class:running={isRunning} class:stopped={!isRunning}>
          {isRunning ? 'Running' : reconcileState ?? 'Stopped'}
        </span>
      {:else}
        <span class="status-badge not-installed">Not Installed</span>
      {/if}
    </div>

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

    <!-- Install / Manage -->
    <div class="section">
      <h2>{isInstalled ? 'Manage' : 'Install'}</h2>

      {#if !isInstalled}
        <div class="install-controls">
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
          <button
            class="install-btn"
            onclick={installModule}
            disabled={installing || (!data.online && (data.versions?.installedVersions ?? []).length === 0)}
          >
            <ArrowDownTray size="1rem" />
            {installing ? 'Installing...' : 'Install & Start'}
          </button>
          {#if !data.online && (data.versions?.installedVersions ?? []).length === 0}
            <p class="help-text">No local versions available and server is offline. Connect to the internet to download.</p>
          {/if}
        </div>
      {:else}
        <!-- Already installed — show current state and options -->
        {#if data.desiredService}
          <div class="detail-row">
            <span class="label">Desired Version</span>
            <span class="value">{data.desiredService.version}</span>
          </div>
          <div class="detail-row">
            <span class="label">Desired Running</span>
            <span class="value">{data.desiredService.running ? 'Yes' : 'No'}</span>
          </div>
        {/if}
        {#if data.serviceStatus?.lastError}
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
            {installing ? 'Removing...' : 'Remove from Managed Services'}
          </button>
        </div>
      {/if}
    </div>
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
    .module-meta {
      margin: 0.25rem 0 0;
      color: var(--theme-text-muted);
      font-size: 0.8125rem;
      font-family: var(--font-mono, monospace);
    }
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
