<script lang="ts">
  import { onMount } from 'svelte';
  import { slide } from 'svelte/transition';
  import { api, apiPost } from '$lib/api/client';

  interface VersionInfo {
    version: string;
    commit: string;
    date: string;
  }

  interface ReleaseInfo {
    version: string;
    tagName: string;
    name: string;
    releaseUrl: string;
    publishedAt: string;
    current: boolean;
  }

  interface UpgradeStatus {
    state: string;
    error?: string;
    version?: string;
  }

  let versionInfo = $state<VersionInfo | null>(null);
  let releases = $state<ReleaseInfo[]>([]);
  let upgradeStatus = $state<UpgradeStatus | null>(null);
  let checkError = $state('');
  let offline = $state(false);
  let loadingReleases = $state(false);
  let showConfirm = $state(false);
  let confirmVersion = $state('');
  let phase = $state<'idle' | 'upgrading' | 'restarting' | 'success' | 'failed'>('idle');
  let mode = $state('unknown');

  onMount(() => {
    fetchVersion();
    fetchMode();
    fetchReleases();
  });

  async function fetchVersion() {
    const result = await api<VersionInfo>('/system/version');
    if (result.data) versionInfo = result.data;
  }

  async function fetchMode() {
    const result = await api<{ mode: string }>('/mode');
    if (result.data) mode = result.data.mode;
  }

  function isNewer(a: string, b: string): boolean {
    const pa = a.split('.').map(Number);
    const pb = b.split('.').map(Number);
    for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
      const va = pa[i] ?? 0;
      const vb = pb[i] ?? 0;
      if (va > vb) return true;
      if (va < vb) return false;
    }
    return false;
  }

  async function fetchReleases() {
    loadingReleases = true;
    const result = await api<ReleaseInfo[]>('/system/releases');
    if (result.error) {
      if (result.error.status === 503) {
        offline = true;
      } else {
        checkError = result.error.error;
      }
    } else if (result.data) {
      releases = result.data;
    }
    loadingReleases = false;
  }

  function promptUpgrade(version: string) {
    confirmVersion = version;
    showConfirm = true;
  }

  async function startUpgrade() {
    showConfirm = false;
    phase = 'upgrading';

    const result = await apiPost<{ status: string; version: string }>('/system/upgrade', {
      version: confirmVersion
    });

    if (result.error) {
      phase = 'failed';
      upgradeStatus = { state: 'failed', error: result.error.error };
      return;
    }

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
        phase = 'restarting';
        clearInterval(interval);
        pollForReconnect();
      }
    }, 1000);

    setTimeout(() => clearInterval(interval), 120000);
  }

  function pollForReconnect() {
    const interval = setInterval(async () => {
      try {
        const result = await api<VersionInfo>('/system/version');
        if (result.data) {
          clearInterval(interval);
          versionInfo = result.data;
          releases = [];
          phase = 'success';
        }
      } catch {
        // Still restarting.
      }
    }, 2000);

    setTimeout(() => {
      clearInterval(interval);
      if (phase === 'restarting') {
        phase = 'failed';
        upgradeStatus = { state: 'failed', error: 'Service did not come back within 60 seconds. Check system logs.' };
      }
    }, 60000);
  }

  function formatDate(iso: string): string {
    try {
      return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
    } catch {
      return iso;
    }
  }

  const stateLabel: Record<string, string> = {
    downloading: 'Downloading new version...',
    extracting: 'Extracting binary...',
    replacing: 'Replacing binary...',
    restarting: 'Restarting service...',
  };
</script>

<div class="system-page">
  <h1>Version & Updates</h1>

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

  <!-- Releases -->
  <section class="card">
    <h2>Releases</h2>

    {#if mode !== 'systemd'}
      <div class="notice warning" transition:slide>
        Upgrades are only available when running as a systemd service. Current mode: <strong>{mode}</strong>
      </div>
    {/if}

    {#if offline}
      <div class="notice warning" transition:slide>
        Unable to reach GitHub — check your internet connection.
      </div>
    {/if}

    {#if checkError}
      <div class="notice error" transition:slide>{checkError}</div>
    {/if}

    {#if phase === 'upgrading'}
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
      <button class="btn-secondary" onclick={() => { phase = 'idle'; fetchReleases(); }}>
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

    {#if loadingReleases}
      <p class="muted">Loading...</p>
    {:else if releases.length > 0 && phase === 'idle'}
      <div class="release-list">
        {#each releases as release}
          {@const currentVersion = releases.find(r => r.current)?.version ?? ''}
          <div class="release-row" class:current={release.current} class:newer={!release.current && isNewer(release.version, currentVersion)}>
            <div class="release-info">
              <span class="release-version">
                v{release.version}
                {#if release.current}
                  <span class="current-badge">current</span>
                {:else if isNewer(release.version, currentVersion)}
                  <span class="update-badge">update</span>
                {/if}
              </span>
              {#if release.name && release.name !== release.tagName}
                <span class="release-name">{release.name}</span>
              {/if}
              <span class="release-date">{formatDate(release.publishedAt)}</span>
            </div>
            <div class="release-actions">
              {#if release.releaseUrl}
                <a href={release.releaseUrl} target="_blank" rel="noopener" class="btn-link">Notes</a>
              {/if}
              {#if !release.current && mode === 'systemd'}
                <button class="btn-secondary btn-sm" onclick={() => promptUpgrade(release.version)}>
                  Switch
                </button>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </section>
</div>

{#if showConfirm}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={() => { showConfirm = false; }}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h2>Confirm Version Change</h2>
      <p class="modal-warning">
        This will download version <strong>v{confirmVersion}</strong>,
        replace the running binary, and restart the service.
        The UI will be briefly unavailable during the restart.
      </p>
      <div class="modal-actions">
        <button class="modal-cancel-btn" onclick={() => { showConfirm = false; }}>Cancel</button>
        <button class="modal-apply-btn" onclick={startUpgrade}>
          Switch to v{confirmVersion}
        </button>
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

  .notice-text {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;

    span {
      color: var(--theme-text-muted);
      font-size: 0.8125rem;
    }
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

  .btn-sm {
    padding: 0.25rem 0.625rem;
    font-size: 0.75rem;
  }

  .btn-link {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    text-decoration: none;

    &:hover {
      color: var(--theme-text);
      text-decoration: underline;
    }
  }

  /* Release list */
  .release-list {
    display: flex;
    flex-direction: column;
  }

  .release-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.625rem 0;
    border-bottom: 1px solid var(--theme-border);

    &:last-child { border-bottom: none; }

    &.current {
      .release-version { color: var(--badge-green-text, #22c55e); }
    }

    &.newer {
      .release-version { color: var(--badge-blue-text, #3b82f6); }
    }
  }

  .release-info {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    min-width: 0;
  }

  .release-version {
    font-size: 0.875rem;
    font-weight: 600;
    font-family: 'IBM Plex Mono', monospace;
    color: var(--theme-text);
    white-space: nowrap;
  }

  .current-badge, .update-badge {
    font-size: 0.625rem;
    font-weight: 600;
    font-family: 'Space Grotesk', sans-serif;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.0625rem 0.375rem;
    border-radius: var(--rounded-full);
    vertical-align: middle;
  }

  .current-badge {
    background: var(--badge-green-bg);
    color: var(--badge-green-text);
    border: 1px solid var(--badge-green-border);
  }

  .update-badge {
    background: var(--badge-blue-bg, rgba(59, 130, 246, 0.1));
    color: var(--badge-blue-text, #3b82f6);
    border: 1px solid var(--badge-blue-border, rgba(59, 130, 246, 0.3));
  }

  .release-name {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .release-date {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    white-space: nowrap;
  }

  .release-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
    margin-left: 1rem;
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* Modal styles */
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
