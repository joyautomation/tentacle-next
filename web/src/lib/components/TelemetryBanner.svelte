<script lang="ts">
  import { onMount } from 'svelte';
  import { slide } from 'svelte/transition';
  import { api, apiPut } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';

  interface Props {
    apiConnected: boolean;
  }

  let { apiConnected }: Props = $props();

  let telemetryEnabled = $state(false);
  let dismissed = $state(false);
  let enabling = $state(false);
  let loaded = $state(false);

  const visible = $derived(
    apiConnected && loaded && !telemetryEnabled && !dismissed
  );

  onMount(() => {
    dismissed = localStorage.getItem('telemetry_dismissed') === 'true';
    checkStatus();
  });

  async function checkStatus() {
    // Use the generic config endpoint (always available) rather than the
    // telemetry-specific status endpoint which requires the new binary.
    const result = await api<{ moduleId: string; envVar: string; value: string }[]>('/config/telemetry');
    if (result.data) {
      const entry = result.data.find((e) => e.envVar === 'TELEMETRY_ENABLED');
      telemetryEnabled = entry?.value === 'true';
    }
    loaded = true;
  }

  async function enable() {
    enabling = true;

    // Enable telemetry config.
    const configResult = await apiPut('/config/telemetry/TELEMETRY_ENABLED', { value: 'true' });
    if (configResult.error) {
      saltState.addNotification({ message: 'Failed to enable telemetry: ' + configResult.error.error, type: 'error' });
      enabling = false;
      return;
    }

    // Ensure the telemetry module is in desired services.
    await apiPut('/orchestrator/desired-services/telemetry', { version: 'latest', running: true });

    telemetryEnabled = true;
    enabling = false;
    saltState.addNotification({ message: 'Telemetry enabled. Thank you for helping improve Tentacle!', type: 'success' });
  }

  function dismiss() {
    dismissed = true;
    localStorage.setItem('telemetry_dismissed', 'true');
  }
</script>

{#if visible}
  <div class="telemetry-banner" transition:slide>
    <div class="banner-content">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M22 12h-4l-3 9L9 3l-3 9H2"/>
      </svg>
      <div class="banner-text">
        <strong>Help make Tentacle better</strong>
        <span>Share anonymous usage data and error reports to help us improve reliability and performance. No personal data, tag values, or credentials are ever collected.</span>
      </div>
      <button class="banner-btn enable" onclick={enable} disabled={enabling}>
        {enabling ? 'Enabling...' : 'Enable Telemetry'}
      </button>
      <button class="banner-dismiss" onclick={dismiss} aria-label="Dismiss">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18"/>
          <line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>
    </div>
  </div>
{/if}

<style lang="scss">
  .telemetry-banner {
    margin-bottom: 1rem;
  }

  .banner-content {
    display: flex;
    align-items: flex-start;
    gap: 0.75rem;
    padding: 1rem 1.25rem;
    border-radius: var(--rounded-lg);
    color: var(--theme-text);
    background: rgba(20, 184, 166, 0.08);
    border: 1px solid rgba(20, 184, 166, 0.25);

    > svg {
      flex-shrink: 0;
      margin-top: 0.125rem;
      color: #14b8a6;
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
  }

  .banner-btn {
    flex-shrink: 0;
    align-self: center;
    padding: 0.375rem 0.875rem;
    font-size: 0.8125rem;
    font-weight: 600;
    border-radius: var(--rounded);
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;

    &.enable {
      background: rgba(20, 184, 166, 0.12);
      color: #14b8a6;
      border: 1px solid rgba(20, 184, 166, 0.4);

      &:hover:not(:disabled) {
        background: rgba(20, 184, 166, 0.2);
        border-color: rgba(20, 184, 166, 0.6);
      }
    }

    &:disabled {
      opacity: 0.6;
      cursor: not-allowed;
    }
  }

  .banner-dismiss {
    flex-shrink: 0;
    align-self: flex-start;
    display: flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--theme-text-muted);
    padding: 0.25rem;
    border-radius: var(--rounded);
    transition: color 0.15s, background 0.15s;

    &:hover {
      color: var(--theme-text);
      background: var(--theme-surface);
    }
  }
</style>
