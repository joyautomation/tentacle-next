<script lang="ts">
  import { api, apiPost } from '$lib/api/client';
  import { slide } from 'svelte/transition';
  import { onMount } from 'svelte';

  interface ServiceStatus {
    mode: string;
    systemdAvailable: boolean;
    unitExists: boolean;
    unitEnabled: boolean;
    unitActive: boolean;
    binaryInstalled: boolean;
    canInstall: boolean;
    reason?: string;
  }

  let status = $state<ServiceStatus | null>(null);
  let phase = $state<'idle' | 'installing' | 'installed' | 'activating' | 'error'>('idle');
  let errorMsg = $state('');

  // Don't show if already a service or if systemd unavailable
  const visible = $derived(
    status != null &&
    status.systemdAvailable &&
    status.mode !== 'systemd' &&
    !status.unitActive &&
    phase !== 'activating'
  );

  onMount(() => {
    checkStatus();
  });

  async function checkStatus() {
    const result = await api<ServiceStatus>('/system/service');
    if (result.data) {
      status = result.data;
      if (status.unitExists && status.unitEnabled && !status.unitActive) {
        phase = 'installed';
      }
    }
  }

  async function install() {
    phase = 'installing';
    errorMsg = '';
    const result = await apiPost<{ success: boolean; message: string }>('/system/service/install');
    if (result.error) {
      phase = 'error';
      errorMsg = result.error.error;
      return;
    }
    phase = 'installed';
  }

  async function activate() {
    phase = 'activating';
    errorMsg = '';
    const result = await apiPost<{ success: boolean }>('/system/service/activate');
    if (result.error) {
      phase = 'error';
      errorMsg = result.error.error;
      return;
    }
    // The process will exit — poll until the service comes back up
    pollForReconnect();
  }

  function pollForReconnect() {
    const interval = setInterval(async () => {
      try {
        const result = await api<{ mode: string }>('/mode');
        if (result.data?.mode === 'systemd') {
          clearInterval(interval);
          window.location.reload();
        }
      } catch {
        // Still restarting
      }
    }, 2000);
    // Give up after 30s
    setTimeout(() => clearInterval(interval), 30000);
  }
</script>

{#if visible}
  <div class="service-banner" transition:slide>
    {#if phase === 'activating'}
      <div class="banner-content activating">
        <svg class="spinner" viewBox="0 0 24 24" width="18" height="18">
          <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="2" stroke-dasharray="31 31" />
        </svg>
        <div class="banner-text">
          <strong>Restarting as system service...</strong>
          <span>The page will reconnect automatically.</span>
        </div>
      </div>
    {:else if phase === 'error'}
      <div class="banner-content error">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10" />
          <line x1="15" y1="9" x2="9" y2="15" />
          <line x1="9" y1="9" x2="15" y2="15" />
        </svg>
        <div class="banner-text">
          <strong>Service installation failed</strong>
          <span>{errorMsg}</span>
        </div>
        <button class="banner-btn" onclick={() => { phase = 'idle'; }}>Retry</button>
      </div>
    {:else if phase === 'installed'}
      <div class="banner-content success">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10" />
          <path d="M9 12l2 2 4-4" />
        </svg>
        <div class="banner-text">
          <strong>Service installed and enabled</strong>
          <span>Activate to switch from standalone to service mode.</span>
        </div>
        <button class="banner-btn activate" onclick={activate}>Activate Service</button>
      </div>
    {:else if status?.canInstall}
      <div class="banner-content info">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
        <div class="banner-text">
          <strong>Running in standalone mode</strong>
          <span>Install as a system service to persist across reboots.</span>
        </div>
        <button class="banner-btn" onclick={install} disabled={phase === 'installing'}>
          {phase === 'installing' ? 'Installing...' : 'Install as Service'}
        </button>
      </div>
    {:else}
      <div class="banner-content info">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
        <div class="banner-text">
          <strong>Running in standalone mode</strong>
          <span>To persist as a service, run: <code>sudo tentacle service install</code></span>
        </div>
      </div>
    {/if}
  </div>
{/if}

<style lang="scss">
  .service-banner {
    margin-bottom: 1rem;
  }

  .banner-content {
    display: flex;
    align-items: flex-start;
    gap: 0.75rem;
    padding: 1rem 1.25rem;
    border-radius: var(--rounded-lg);
    color: var(--theme-text);

    svg {
      flex-shrink: 0;
      margin-top: 0.125rem;
    }

    &.info {
      background: rgba(59, 130, 246, 0.08);
      border: 1px solid rgba(59, 130, 246, 0.25);
      svg { color: #3b82f6; }
    }

    &.success {
      background: rgba(34, 197, 94, 0.08);
      border: 1px solid rgba(34, 197, 94, 0.25);
      svg { color: #22c55e; }
    }

    &.error {
      background: rgba(239, 68, 68, 0.08);
      border: 1px solid rgba(239, 68, 68, 0.25);
      svg { color: #ef4444; }
    }

    &.activating {
      background: rgba(59, 130, 246, 0.08);
      border: 1px solid rgba(59, 130, 246, 0.25);
      svg { color: #3b82f6; }
    }
  }

  .banner-text {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    flex: 1;

    strong {
      font-size: 0.875rem;
    }

    span {
      font-size: 0.8125rem;
      color: var(--theme-text-muted);
    }

    code {
      background: var(--theme-surface);
      padding: 0.125rem 0.375rem;
      border-radius: var(--rounded);
      font-size: 0.75rem;
    }
  }

  .banner-btn {
    flex-shrink: 0;
    align-self: center;
    padding: 0.375rem 0.875rem;
    font-size: 0.8125rem;
    font-weight: 600;
    border: 1px solid rgba(59, 130, 246, 0.4);
    border-radius: var(--rounded);
    background: rgba(59, 130, 246, 0.12);
    color: #3b82f6;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;

    &:hover:not(:disabled) {
      background: rgba(59, 130, 246, 0.2);
      border-color: rgba(59, 130, 246, 0.6);
    }

    &:disabled {
      opacity: 0.6;
      cursor: not-allowed;
    }

    &.activate {
      background: rgba(34, 197, 94, 0.12);
      color: #22c55e;
      border-color: rgba(34, 197, 94, 0.4);

      &:hover {
        background: rgba(34, 197, 94, 0.2);
        border-color: rgba(34, 197, 94, 0.6);
      }
    }
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
