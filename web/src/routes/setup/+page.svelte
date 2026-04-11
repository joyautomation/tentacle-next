<script lang="ts">
  import type { PageData } from './$types';
  import type { MqttConfig } from '$lib/components/setup/MqttConfigForm.svelte';
  import WizardStepper from '$lib/components/setup/WizardStepper.svelte';
  import ArchitectureCard from '$lib/components/setup/ArchitectureCard.svelte';
  import SparkplugDiagram from '$lib/components/setup/SparkplugDiagram.svelte';
  import NatDiagram from '$lib/components/setup/NatDiagram.svelte';
  import ProtocolSelector from '$lib/components/setup/ProtocolSelector.svelte';
  import MqttConfigForm from '$lib/components/setup/MqttConfigForm.svelte';
  import AddOnSelector from '$lib/components/setup/AddOnSelector.svelte';
  import ReviewPanel from '$lib/components/setup/ReviewPanel.svelte';

  let { data }: { data: PageData } = $props();

  // Step IDs used by archetypes
  type StepId = 'architecture' | 'protocols' | 'mqtt-config' | 'add-ons' | 'review';

  const STEP_LABELS: Record<StepId, string> = {
    'architecture': 'Architecture',
    'protocols': 'Protocols',
    'mqtt-config': 'MQTT Config',
    'add-ons': 'Add-ons',
    'review': 'Review',
  };

  // Each archetype defines which steps it needs (architecture + review are always first/last)
  const ARCHETYPE_STEPS: Record<string, StepId[]> = {
    'sparkplug-gateway': ['architecture', 'protocols', 'mqtt-config', 'add-ons', 'review'],
    'nat-gateway': ['architecture', 'add-ons', 'review'],
  };

  // Add-on modules that are NOT already core to each archetype
  const ARCHETYPE_ADDONS: Record<string, Set<string>> = {
    'sparkplug-gateway': new Set(['network', 'gitops']),
    'nat-gateway': new Set(['gitops']),
  };

  // Pre-populate from existing config
  const SCANNER_MODULE_IDS = new Set(['ethernetip', 'opcua', 'modbus', 'snmp']);
  const MQTT_FIELDS: (keyof MqttConfig)[] = [
    'MQTT_BROKER_URL', 'MQTT_CLIENT_ID', 'MQTT_GROUP_ID',
    'MQTT_EDGE_NODE', 'MQTT_USERNAME', 'MQTT_PASSWORD',
  ];
  const MQTT_DEFAULTS: MqttConfig = {
    MQTT_BROKER_URL: 'tcp://localhost:1883',
    MQTT_CLIENT_ID: 'tentacle-mqtt',
    MQTT_GROUP_ID: 'TentacleGroup',
    MQTT_EDGE_NODE: 'EdgeNode1',
    MQTT_USERNAME: '',
    MQTT_PASSWORD: '',
  };

  function initMqttConfig(): MqttConfig {
    const config = { ...MQTT_DEFAULTS };
    for (const entry of data.mqttConfig ?? []) {
      if (MQTT_FIELDS.includes(entry.envVar as keyof MqttConfig)) {
        config[entry.envVar as keyof MqttConfig] = entry.value;
      }
    }
    return config;
  }

  const ADDON_MODULE_IDS = new Set(['network', 'gitops']);

  function initProtocols(): Set<string> {
    const desiredIds = new Set((data.desiredServices ?? []).map(d => d.moduleId));
    const active = new Set<string>();
    for (const id of SCANNER_MODULE_IDS) {
      if (desiredIds.has(id)) active.add(id);
    }
    return active;
  }

  function initAddOns(): Set<string> {
    const desiredIds = new Set((data.desiredServices ?? []).map(d => d.moduleId));
    const active = new Set<string>();
    for (const id of ADDON_MODULE_IDS) {
      if (desiredIds.has(id)) active.add(id);
    }
    return active;
  }

  // Wizard state
  let currentStep = $state(0);
  let selectedArchetype = $state<string | null>(null);
  let selectedProtocols = $state<Set<string>>(initProtocols());
  let selectedAddOns = $state<Set<string>>(initAddOns());
  let mqttConfig = $state<MqttConfig>(initMqttConfig());

  // Dynamic steps based on selected archetype
  const activeSteps = $derived<StepId[]>(
    selectedArchetype ? ARCHETYPE_STEPS[selectedArchetype] : ['architecture']
  );
  const stepLabels = $derived(activeSteps.map(id => STEP_LABELS[id]));
  const currentStepId = $derived<StepId>(activeSteps[currentStep] ?? 'architecture');
  const isLastStep = $derived(currentStep === activeSteps.length - 1);

  // Validation per step
  const canProceed = $derived.by(() => {
    switch (currentStepId) {
      case 'architecture': return selectedArchetype !== null;
      case 'protocols': return selectedProtocols.size > 0;
      case 'mqtt-config': return mqttConfig.MQTT_BROKER_URL.trim() !== '' &&
                     mqttConfig.MQTT_GROUP_ID.trim() !== '' &&
                     mqttConfig.MQTT_EDGE_NODE.trim() !== '';
      case 'add-ons': return true;
      case 'review': return true;
      default: return false;
    }
  });

  function next() {
    if (canProceed && !isLastStep) {
      currentStep++;
    }
  }

  function back() {
    if (currentStep > 0) {
      currentStep--;
    }
  }

  function selectArchetype(id: string) {
    if (selectedArchetype !== id) {
      selectedArchetype = id;
      // Reset to step 0 when switching archetype
      currentStep = 0;
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

  {#if hasExistingConfig && currentStepId === 'architecture'}
    <div class="notice">
      Services are already configured. Running the wizard again will update your configuration.
    </div>
  {/if}

  <WizardStepper steps={stepLabels} {currentStep} onStepClick={(s) => { currentStep = s; }} />

  <div class="step-content">
    {#if currentStepId === 'architecture'}
      <div class="step-intro">
        <h2>Choose an Architecture</h2>
        <p>Select how you'd like to set up your tentacle.</p>
      </div>
      <div class="card-grid">
        <ArchitectureCard
          title="Sparkplug Gateway"
          description="Connect industrial device scanners to an MQTT Sparkplug B infrastructure. Supports EtherNet/IP, OPC UA, Modbus, and SNMP."
          selected={selectedArchetype === 'sparkplug-gateway'}
          onclick={() => selectArchetype('sparkplug-gateway')}
          badge="Recommended"
        >
          {#snippet diagram()}
            <SparkplugDiagram compact={true} />
          {/snippet}
        </ArchitectureCard>

        <ArchitectureCard
          title="NAT"
          description="Network address translation between networks. Manage one to one communications between subnets."
          selected={selectedArchetype === 'nat-gateway'}
          onclick={() => selectArchetype('nat-gateway')}
        >
          {#snippet diagram()}
            <NatDiagram compact={true} />
          {/snippet}
        </ArchitectureCard>
      </div>

    {:else if currentStepId === 'protocols'}
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

    {:else if currentStepId === 'mqtt-config'}
      <div class="step-intro">
        <h2>MQTT Broker Settings</h2>
        <p>Configure the connection to your MQTT broker and Sparkplug B identity.</p>
      </div>
      <MqttConfigForm
        config={mqttConfig}
        onchange={(c) => { mqttConfig = c; }}
      />

    {:else if currentStepId === 'add-ons'}
      <div class="step-intro">
        <h2>Add-ons</h2>
        <p>Enable optional modules to extend your tentacle's capabilities.</p>
      </div>
      <AddOnSelector
        available={ARCHETYPE_ADDONS[selectedArchetype ?? ''] ?? new Set()}
        selected={selectedAddOns}
        onchange={(s) => { selectedAddOns = s; }}
      />

    {:else if currentStepId === 'review'}
      <ReviewPanel
        archetype={selectedArchetype ?? 'sparkplug-gateway'}
        {selectedProtocols}
        {selectedAddOns}
        {mqttConfig}
      />
    {/if}
  </div>

  {#if !isLastStep}
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
