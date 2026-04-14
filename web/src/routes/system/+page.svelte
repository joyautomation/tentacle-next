<script lang="ts">
  import { onMount } from 'svelte';
  import { slide } from 'svelte/transition';
  import { api, apiPost } from '$lib/api/client';

  interface VersionInfo {
    version: string;
    commit: string;
    date: string;
  }

  interface UpdateInfo {
    currentVersion: string;
    latestVersion: string;
    updateAvailable: boolean;
    releaseUrl?: string;
    checkedAt: number;
  }

  interface UpgradeStatus {
    state: string;
    error?: string;
    version?: string;
  }

  let versionInfo = $state<VersionInfo | null>(null);
  let updateInfo = $state<UpdateInfo | null>(null);
  let upgradeStatus = $state<UpgradeStatus | null>(null);
  let checking = $state(false);
  let checkError = $state('');
  let showConfirm = $state(false);
  let phase = $state<'idle' | 'upgrading' | 'restarting' | 'success' | 'failed'>('idle');
  let mode = $state('unknown');

  onMount(() => {
    fetchVersion();
    fetchMode();
  });

  async function fetchVersion() {
    const result = await api<VersionInfo>('/system/version');
    if (result.data) versionInfo = result.data;
  }

  async function fetchMode() {
    const result = await api<{ mode: string }>('/mode');
    if (result.data) mode = result.data.mode;
  }

  async function checkForUpdates() {
    checking = true;
    checkError = '';
    const result = await api<UpdateInfo>('/system/updates');
    if (result.error) {
      checkError = result.error.error;
    } else if (result.data) {
      updateInfo = result.data;
    }
    checking = false;
  }

  async function startUpgrade() {
    showConfirm = false;
    phase = 'upgrading';

    const result = await apiPost<{ status: string; version: string }>('/system/upgrade', {
      version: updateInfo?.latestVersion
    });

    if (result.error) {
      phase = 'failed';
      upgradeStatus = { state: 'failed', error: result.error.error };
      return;
    }

    // Poll upgrade status until restarting, then switch to reconnect polling.
    pollUpgradeStatus();
  }

  function pollUpgradeStatus() {
    const interval = setInterval(async () => {
      try {
        const result = await api<UpgradeStatus>('/system/upgrade/status');
        if (result.data) {
          upgradeStatus = result.data;
          if (result.data.state === 'failed') {
            phase = 'failed';
            clearInterval(interval);
          } else if (result.data.state === 'restarting') {
            phase = 'restarting';
            clearInterval(interval);
            pollForReconnect();
          }
        }
      } catch {
        // Connection lost — service is restarting.
        phase = 'restarting';
        clearInterval(interval);
        pollForReconnect();
      }
    }, 1000);

    // Safety: give up polling status after 120s.
    setTimeout(() => clearInterval(interval), 120000);
  }

  function pollForReconnect() {
    const interval = setInterval(async () => {
      try {
        const result = await api<VersionInfo>('/system/version');
        if (result.data) {
          clearInterval(interval);
          versionInfo = result.data;
          updateInfo = null;
          phase = 'success';
        }
      } catch {
        // Still restarting.
      }
    }, 2000);

    // Give up after 60s.
    setTimeout(() => {
      clearInterval(interval);
      if (phase === 'restarting') {
        phase = 'failed';
        upgradeStatus = { state: 'failed', error: 'Service did not come back within 60 seconds. Check system logs.' };
      }
    }, 60000);
  }

  const stateLabel: Record<string, string> = {
    downloading: 'Downloading new version...',
    extracting: 'Extracting binary...',
    replacing: 'Replacing binary...',
    restarting: 'Restarting service...',
  };
</script>

<div class="system-page">
  <h1>System</h1>

  <!-- Version Info -->
  <section class="card">
    <h2>Version</h2>
    {#if versionInfo}
      <div class="version-grid">
        <span class="label">Version</span>
        <span class="value">{versionInfo.version}</span>
        {#if versionInfo.commit}
          <span class="label">Commit</span>
          <span class="value mono">{versionInfo.commit}</span>
        {/if}
        {#if versionInfo.date}
          <span class="label">Built</span>
          <span class="value">{versionInfo.date}</span>
        {/if}
        <span class="label">Mode</span>
        <span class="value">{mode}</span>
      </div>
    {:else}
      <p class="muted">Loading...</p>
    {/if}
  </section>

  <!-- Updates -->
  <section class="card">
    <h2>Updates</h2>

    {#if mode !== 'systemd'}
      <div class="notice warning" transition:slide>
        Upgrades are only available when running as a systemd service. Current mode: <strong>{mode}</strong>
      </div>
    {/if}

    {#if phase === 'idle'}
      {#if updateInfo}
        {#if updateInfo.updateAvailable}
          <div class="notice info" transition:slide>
            <div class="notice-content">
              <strong>Version {updateInfo.latestVersion} is available</strong>
              <span>Currently running {updateInfo.currentVersion}</span>
            </div>
            {#if mode === 'systemd'}
              <button class="btn-primary" onclick={() => { showConfirm = true; }}>
                Upgrade to v{updateInfo.latestVersion}
              </button>
            {/if}
          </div>
        {:else}
          <div class="notice success" transition:slide>
            You are running the latest version ({updateInfo.currentVersion}).
          </div>
        {/if}
      {/if}

      {#if checkError}
        <div class="notice error" transition:slide>{checkError}</div>
      {/if}

      <button class="btn-secondary" onclick={checkForUpdates} disabled={checking}>
        {checking ? 'Checking...' : 'Check for Updates'}
      </button>

    {:else if phase === 'upgrading'}
      <div class="notice info upgrading" transition:slide>
        <svg class="spinner" viewBox="0 0 24 24" width="18" height="18">
          <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="2" stroke-dasharray="31 31" />
        </svg>
        <div class="notice-text">
          <strong>{stateLabel[upgradeStatus?.state ?? 'downloading'] ?? 'Upgrading...'}</strong>
          {#if upgradeStatus?.version}
            <span>Target: v{upgradeStatus.version}</span>
          {/if}
        </div>
      </div>

    {:else if phase === 'restarting'}
      <div class="notice info upgrading" transition:slide>
        <svg class="spinner" viewBox="0 0 24 24" width="18" height="18">
          <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="2" stroke-dasharray="31 31" />
        </svg>
        <div class="notice-text">
          <strong>Restarting service...</strong>
          <span>The page will reconnect automatically.</span>
        </div>
      </div>

    {:else if phase === 'success'}
      <div class="notice success" transition:slide>
        <strong>Upgrade complete!</strong> Now running version {versionInfo?.version}.
      </div>
      <button class="btn-secondary" onclick={() => { phase = 'idle'; }}>
        Done
      </button>

    {:else if phase === 'failed'}
      <div class="notice error" transition:slide>
        <strong>Upgrade failed</strong>
        {#if upgradeStatus?.error}
          <span>: {upgradeStatus.error}</span>
        {/if}
      </div>
      <button class="btn-secondary" onclick={() => { phase = 'idle'; upgradeStatus = null; }}>
        Dismiss
      </button>
    {/if}
  </section>
</div>

{#if showConfirm}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={() => { showConfirm = false; }}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h2>Confirm Upgrade</h2>
      <p class="modal-warning">
        This will download version <strong>{updateInfo?.latestVersion}</strong>,
        replace the running binary, and restart the service.
        The UI will be briefly unavailable during the restart.
      </p>
      <div class="modal-actions">
        <button class="modal-cancel-btn" onclick={() => { showConfirm = false; }}>Cancel</button>
        <button class="modal-apply-btn" onclick={startUpgrade}>Upgrade</button>
      </div>
    </div>
  </div>
{/if}

<style lang="scss">
  .system-page {
    padding: 1.5rem;
    max-width: 800px;
    margin: 0 auto;
  }

  h1 {
    font-size: 1.5rem;
    font-weight: 700;
    margin-bottom: 1.5rem;
    color: var(--theme-text);
  }

  .card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1.25rem;
    margin-bottom: 1rem;

    h2 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
      margin-bottom: 1rem;
    }
  }

  .version-grid {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.375rem 1rem;
    font-size: 0.875rem;

    .label {
      color: var(--theme-text-muted);
      font-weight: 500;
    }

    .value {
      color: var(--theme-text);
    }

    .mono {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.8125rem;
    }
  }

  .muted {
    color: var(--theme-text-muted);
    font-size: 0.875rem;
  }

  .notice {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.875rem 1rem;
    border-radius: var(--rounded-lg);
    font-size: 0.875rem;
    margin-bottom: 1rem;

    &.info {
      background: rgba(59, 130, 246, 0.08);
      border: 1px solid rgba(59, 130, 246, 0.25);
      color: var(--theme-text);
    }

    &.success {
      background: rgba(34, 197, 94, 0.08);
      border: 1px solid rgba(34, 197, 94, 0.25);
      color: var(--theme-text);
    }

    &.warning {
      background: rgba(245, 158, 11, 0.08);
      border: 1px solid rgba(245, 158, 11, 0.25);
      color: var(--theme-text);
    }

    &.error {
      background: rgba(239, 68, 68, 0.08);
      border: 1px solid rgba(239, 68, 68, 0.25);
      color: var(--theme-text);
    }

    &.upgrading {
      svg { color: #3b82f6; flex-shrink: 0; }
    }
  }

  .notice-content {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    flex: 1;

    span {
      color: var(--theme-text-muted);
      font-size: 0.8125rem;
    }
  }

  .notice-text {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;

    span {
      color: var(--theme-text-muted);
      font-size: 0.8125rem;
    }
  }

  .btn-primary {
    flex-shrink: 0;
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 600;
    border: none;
    border-radius: var(--rounded);
    background: var(--theme-primary);
    color: white;
    cursor: pointer;
    transition: background 0.15s;

    &:hover { background: var(--theme-primary-hover); }
  }

  .btn-secondary {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 600;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded);
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;

    &:hover {
      background: var(--theme-surface-hover);
      border-color: var(--theme-text-muted);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* Modal styles — match NavSidebar pattern */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 300;
  }

  .modal {
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1.5rem;
    max-width: 480px;
    width: 90%;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      margin-bottom: 0.75rem;
      color: var(--theme-text);
    }
  }

  .modal-warning {
    font-size: 0.875rem;
    color: var(--theme-text-muted);
    line-height: 1.5;
    margin-bottom: 1.25rem;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }

  .modal-cancel-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 600;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded);
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;

    &:hover {
      background: var(--theme-surface-hover);
    }
  }

  .modal-apply-btn {
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 600;
    border: none;
    border-radius: var(--rounded);
    background: var(--theme-primary);
    color: white;
    cursor: pointer;

    &:hover { background: var(--theme-primary-hover); }
  }
</style>
