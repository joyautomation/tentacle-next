<script lang="ts">
  import type { MqttConfig } from './MqttConfigForm.svelte';
  import { goto } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { apiPut, api } from '$lib/api/client';
  import { getServiceName } from '$lib/constants/services';

  interface Props {
    selectedProtocols: Set<string>;
    mqttConfig: MqttConfig;
  }

  let { selectedProtocols, mqttConfig }: Props = $props();

  type StepStatus = 'pending' | 'running' | 'done' | 'error';

  interface ApplyStep {
    label: string;
    status: StepStatus;
    error?: string;
  }

  let applying = $state(false);
  let steps = $state<ApplyStep[]>([]);
  let done = $state(false);

  function updateStep(index: number, status: StepStatus, error?: string) {
    steps[index] = { ...steps[index], status, error };
  }

  async function applyConfiguration() {
    applying = true;
    done = false;
    steps = [];

    // Step 1: Write MQTT config
    const configStep: ApplyStep = { label: 'Writing MQTT configuration', status: 'running' };
    steps.push(configStep);

    const configEntries = Object.entries(mqttConfig).filter(([_, v]) => v !== '');
    for (const [envVar, value] of configEntries) {
      const result = await apiPut(`/config/mqtt/${envVar}`, { value });
      if (result.error) {
        updateStep(0, 'error', `Failed to set ${envVar}: ${result.error.error}`);
        applying = false;
        return;
      }
    }
    updateStep(0, 'done');

    // Step 2: Enable scanner modules
    const protocols = [...selectedProtocols];
    for (const protocol of protocols) {
      const idx = steps.length;
      steps.push({ label: `Enabling ${getServiceName(protocol)}`, status: 'running' });
      const result = await apiPut(`/orchestrator/desired-services/${protocol}`, {
        version: 'latest',
        running: true,
      });
      if (result.error) {
        updateStep(idx, 'error', `Failed: ${result.error.error}`);
        applying = false;
        return;
      }
      updateStep(idx, 'done');
    }

    // Step 3: Enable gateway
    {
      const idx = steps.length;
      steps.push({ label: 'Enabling Gateway', status: 'running' });
      const result = await apiPut('/orchestrator/desired-services/gateway', {
        version: 'latest',
        running: true,
      });
      if (result.error) {
        updateStep(idx, 'error', `Failed: ${result.error.error}`);
        applying = false;
        return;
      }
      updateStep(idx, 'done');
    }

    // Step 4: Enable MQTT bridge
    {
      const idx = steps.length;
      steps.push({ label: 'Enabling MQTT bridge', status: 'running' });
      const result = await apiPut('/orchestrator/desired-services/mqtt', {
        version: 'latest',
        running: true,
      });
      if (result.error) {
        updateStep(idx, 'error', `Failed: ${result.error.error}`);
        applying = false;
        return;
      }
      updateStep(idx, 'done');
    }

    // Step 5: Wait for services to start
    {
      const idx = steps.length;
      steps.push({ label: 'Waiting for services to start', status: 'running' });

      const expectedModules = new Set([...protocols, 'gateway', 'mqtt']);
      const maxAttempts = 30; // 60 seconds max
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
        const allActive = [...expectedModules].every(modId => {
          const s = statuses.find(st => st.moduleId === modId);
          return s && s.systemdState === 'active' && s.reconcileState === 'ok';
        });

        if (allActive) {
          updateStep(idx, 'done');
          done = true;
          applying = false;
          saltState.addNotification({ message: 'Setup complete! All services are running.', type: 'success' });
          return;
        }

        // Check for errors
        const errored = statuses.find(s =>
          expectedModules.has(s.moduleId) && s.reconcileState === 'error'
        );
        if (errored) {
          updateStep(idx, 'error', `${errored.moduleId} failed to start`);
          applying = false;
          return;
        }
      }

      updateStep(idx, 'error', 'Timed out waiting for services');
      applying = false;
    }
  }

  function goToTopology() {
    sessionStorage.setItem('setup_dismissed', 'true');
    goto('/');
  }
</script>

<div class="review-panel">
  {#if !applying && !done}
    <section class="summary">
      <h2>Review Configuration</h2>

      <div class="summary-row">
        <span class="summary-label">Architecture</span>
        <span class="summary-value">Sparkplug Gateway</span>
      </div>

      <div class="summary-row">
        <span class="summary-label">Protocols</span>
        <span class="summary-value">
          {#each [...selectedProtocols] as protocol, i}
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
    </section>

    <button class="apply-btn" onclick={applyConfiguration}>
      Apply & Start
    </button>
  {/if}

  {#if applying || done}
    <section class="progress">
      <h2>{done ? 'Setup Complete' : 'Applying Configuration...'}</h2>

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
      <button class="apply-btn" onclick={goToTopology}>
        Go to Topology
      </button>
    {/if}

    {#if !applying && !done}
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
    &.error { color: var(--color-red-400, #f87171); }
  }

  .step-icon {
    flex-shrink: 0;
    width: 20px;
    height: 20px;
    display: flex;
    align-items: center;
    justify-content: center;

    .done & { color: var(--color-emerald-500, #10b981); }
    .error & { color: var(--color-red-400, #f87171); }
    .running & { color: var(--theme-primary); }
  }

  .step-text {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
  }

  .step-error {
    font-size: 0.75rem;
    color: var(--color-red-400, #f87171);
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
