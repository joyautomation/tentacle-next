<script lang="ts">
  import type { PageData } from './$types';
  import type { MqttConfig } from '$lib/components/setup/MqttConfigForm.svelte';
  import WizardStepper from '$lib/components/setup/WizardStepper.svelte';
  import ArchitectureCard from '$lib/components/setup/ArchitectureCard.svelte';
  import SparkplugDiagram from '$lib/components/setup/SparkplugDiagram.svelte';
  import ProtocolSelector from '$lib/components/setup/ProtocolSelector.svelte';
  import MqttConfigForm from '$lib/components/setup/MqttConfigForm.svelte';
  import ReviewPanel from '$lib/components/setup/ReviewPanel.svelte';

  let { data }: { data: PageData } = $props();

  const STEPS = ['Architecture', 'Protocols', 'MQTT Config', 'Review'];

  // Wizard state
  let currentStep = $state(0);
  let selectedArchetype = $state<string | null>(null);
  let selectedProtocols = $state<Set<string>>(new Set());
  let mqttConfig = $state<MqttConfig>({
    MQTT_BROKER_URL: 'tcp://localhost:1883',
    MQTT_CLIENT_ID: 'tentacle-mqtt',
    MQTT_GROUP_ID: 'TentacleGroup',
    MQTT_EDGE_NODE: 'EdgeNode1',
    MQTT_USERNAME: '',
    MQTT_PASSWORD: '',
  });

  // Validation per step
  const canProceed = $derived.by(() => {
    switch (currentStep) {
      case 0: return selectedArchetype !== null;
      case 1: return selectedProtocols.size > 0;
      case 2: return mqttConfig.MQTT_BROKER_URL.trim() !== '' &&
                     mqttConfig.MQTT_GROUP_ID.trim() !== '' &&
                     mqttConfig.MQTT_EDGE_NODE.trim() !== '';
      case 3: return true;
      default: return false;
    }
  });

  function next() {
    if (canProceed && currentStep < STEPS.length - 1) {
      currentStep++;
    }
  }

  function back() {
    if (currentStep > 0) {
      currentStep--;
    }
  }

  // Show a notice if services are already configured
  const hasExistingConfig = $derived((data.desiredServices?.length ?? 0) > 0);
</script>

<div class="setup-page">
  <div class="setup-header">
    <h1>Quickstart Setup</h1>
    <p class="subtitle">Configure your tentacle in a few steps</p>
  </div>

  {#if hasExistingConfig && currentStep === 0}
    <div class="notice">
      Services are already configured. Running the wizard again will update your configuration.
    </div>
  {/if}

  <WizardStepper steps={STEPS} {currentStep} onStepClick={(s) => { currentStep = s; }} />

  <div class="step-content">
    {#if currentStep === 0}
      <div class="step-intro">
        <h2>Choose an Architecture</h2>
        <p>Select how you'd like to set up your tentacle.</p>
      </div>
      <div class="card-grid">
        <ArchitectureCard
          title="Sparkplug Gateway"
          description="Connect industrial device scanners to an MQTT Sparkplug B infrastructure. Supports EtherNet/IP, OPC UA, Modbus, and SNMP."
          selected={selectedArchetype === 'sparkplug-gateway'}
          onclick={() => { selectedArchetype = 'sparkplug-gateway'; }}
          badge="Recommended"
        >
          {#snippet diagram()}
            <SparkplugDiagram compact={true} />
          {/snippet}
        </ArchitectureCard>

        <!-- Future archetypes go here -->
        <div class="card-placeholder">
          <span>More architectures coming soon</span>
        </div>
      </div>

    {:else if currentStep === 1}
      <div class="step-intro">
        <h2>Select Protocols</h2>
        <p>Which industrial protocols does your environment use? Select all that apply.</p>
      </div>
      <ProtocolSelector
        selected={selectedProtocols}
        onchange={(s) => { selectedProtocols = s; }}
      />
      <div class="diagram-preview">
        <SparkplugDiagram activeProtocols={selectedProtocols} compact={false} />
      </div>

    {:else if currentStep === 2}
      <div class="step-intro">
        <h2>MQTT Broker Settings</h2>
        <p>Configure the connection to your MQTT broker and Sparkplug B identity.</p>
      </div>
      <MqttConfigForm
        config={mqttConfig}
        onchange={(c) => { mqttConfig = c; }}
      />

    {:else if currentStep === 3}
      <ReviewPanel
        {selectedProtocols}
        {mqttConfig}
      />
    {/if}
  </div>

  {#if currentStep < 3}
    <div class="nav-buttons">
      {#if currentStep > 0}
        <button class="btn-secondary" onclick={back}>Back</button>
      {:else}
        <div></div>
      {/if}
      <button class="btn-primary" onclick={next} disabled={!canProceed}>
        Next
      </button>
    </div>
  {/if}
</div>

<style lang="scss">
  .setup-page {
    padding: 1.5rem;
    max-width: 800px;
    margin: 0 auto;
  }

  .setup-header {
    text-align: center;
    margin-bottom: 0.5rem;

    h1 {
      font-size: 1.5rem;
      font-weight: 700;
      color: var(--theme-text);
      margin: 0 0 0.25rem;
    }

    .subtitle {
      font-size: 0.875rem;
      color: var(--theme-text-muted);
      margin: 0;
    }
  }

  .notice {
    padding: 0.75rem 1rem;
    background: var(--theme-surface);
    border: 1px solid var(--badge-amber-border, var(--theme-border));
    border-radius: var(--rounded-md);
    font-size: 0.8125rem;
    color: var(--badge-amber-text, var(--theme-text-muted));
    text-align: center;
    margin-bottom: 0.5rem;
  }

  .step-content {
    min-height: 300px;
    padding: 0.5rem 0 1.5rem;
  }

  .step-intro {
    margin-bottom: 1.25rem;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 0.25rem;
    }

    p {
      font-size: 0.8125rem;
      color: var(--theme-text-muted);
      margin: 0;
    }
  }

  .card-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 1rem;
  }

  .card-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 200px;
    border: 2px dashed var(--theme-border);
    border-radius: var(--rounded-lg);

    span {
      font-size: 0.8125rem;
      color: var(--theme-text-muted);
      opacity: 0.5;
    }
  }

  .diagram-preview {
    margin-top: 1.5rem;
    padding: 1rem;
    background: var(--theme-surface);
    border-radius: var(--rounded-lg);
    border: 1px solid var(--theme-border);
  }

  .nav-buttons {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding-top: 1rem;
    border-top: 1px solid var(--theme-border);
  }

  .btn-primary {
    padding: 0.5rem 1.5rem;
    font-size: 0.875rem;
    font-weight: 600;
    color: white;
    background: var(--theme-primary);
    border: none;
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover:not(:disabled) {
      background: var(--theme-primary-hover);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }

  .btn-secondary {
    padding: 0.5rem 1.25rem;
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--theme-text);
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover {
      border-color: var(--theme-primary);
    }
  }

  @media (max-width: 480px) {
    .setup-page {
      padding: 1rem;
    }

    .card-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
