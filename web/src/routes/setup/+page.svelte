<script lang="ts">
  import type { PageData } from './$types';
  import type { MqttConfig } from '$lib/components/setup/MqttConfigForm.svelte';
  import type { GitOpsConfig } from '$lib/components/setup/GitOpsConfigForm.svelte';
  import type { HistoryConfig } from '$lib/components/setup/HistoryConfigForm.svelte';
  import WizardStepper from '$lib/components/setup/WizardStepper.svelte';
  import ArchitectureCard from '$lib/components/setup/ArchitectureCard.svelte';
  import SparkplugDiagram from '$lib/components/setup/SparkplugDiagram.svelte';
  import NatDiagram from '$lib/components/setup/NatDiagram.svelte';
  import MantleDiagram from '$lib/components/setup/MantleDiagram.svelte';
  import ProtocolSelector from '$lib/components/setup/ProtocolSelector.svelte';
  import MqttConfigForm from '$lib/components/setup/MqttConfigForm.svelte';
  import AddOnSelector from '$lib/components/setup/AddOnSelector.svelte';
  import GitOpsConfigForm from '$lib/components/setup/GitOpsConfigForm.svelte';
  import HistoryConfigForm from '$lib/components/setup/HistoryConfigForm.svelte';
  import ReviewPanel from '$lib/components/setup/ReviewPanel.svelte';

  let { data }: { data: PageData } = $props();

  // Modules present in the registry but flagged experimental (dev build) — selectable with badge
  const allModuleIds = $derived(new Set((data.modules ?? []).map(m => m.moduleId)));
  const experimentalModules = $derived(new Set((data.modules ?? []).filter(m => m.experimental).map(m => m.moduleId)));
  // Modules NOT in the registry at all (stable build excluded them) — disabled
  const unavailableModules = $derived.by(() => {
    const result = new Set<string>();
    for (const id of SCANNER_MODULE_IDS) {
      if (!allModuleIds.has(id)) result.add(id);
    }
    return result;
  });

  // Step IDs used by archetypes
  type StepId = 'architecture' | 'protocols' | 'mqtt-config' | 'add-ons' | 'gitops-config' | 'history-config' | 'review';

  const STEP_LABELS: Record<StepId, string> = {
    'architecture': 'Architecture',
    'protocols': 'Protocols',
    'mqtt-config': 'MQTT Config',
    'add-ons': 'Add-ons',
    'gitops-config': 'GitOps',
    'history-config': 'History',
    'review': 'Review',
  };

  // Base steps per archetype. Addon-specific config steps (gitops-config, history-config)
  // are inserted dynamically based on add-on selection.
  const ARCHETYPE_STEPS: Record<string, StepId[]> = {
    'sparkplug-gateway': ['architecture', 'protocols', 'mqtt-config', 'add-ons', 'review'],
    'nat-gateway': ['architecture', 'add-ons', 'review'],
    'mantle-host': ['architecture', 'add-ons', 'review'],
  };

  // Add-on modules that are NOT already core to each archetype
  const ARCHETYPE_ADDONS: Record<string, Set<string>> = {
    'sparkplug-gateway': new Set(['caddy', 'network', 'gitops', 'history']),
    'nat-gateway': new Set(['caddy', 'gitops']),
    'mantle-host': new Set(['caddy', 'gitops', 'history', 'mqtt-broker']),
  };

  // Modules an archetype's wizard depends on. If any are missing from the
  // build (not in /api/v1/orchestrator/modules), the archetype is hidden
  // entirely. This makes build presets (stable / mantle / all) the source of
  // truth for which setup paths a binary offers.
  const ARCHETYPE_REQUIRED_MODULES: Record<string, string[]> = {
    'sparkplug-gateway': ['gateway', 'mqtt'],
    'nat-gateway': ['network', 'nftables'],
    'mantle-host': ['sparkplug-host'],
  };

  const visibleArchetypes = $derived.by(() => {
    const visible = new Set<string>();
    for (const [arch, required] of Object.entries(ARCHETYPE_REQUIRED_MODULES)) {
      if (required.every(m => allModuleIds.has(m))) visible.add(arch);
    }
    return visible;
  });

  // Pre-populate from existing config
  const SCANNER_MODULE_IDS = new Set(['ethernetip', 'opcua', 'modbus', 'snmp', 'profinetcontroller']);
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

  const GITOPS_FIELD_MAP: Record<string, keyof GitOpsConfig> = {
    GITOPS_REPO_URL: 'repoUrl',
    GITOPS_BRANCH: 'branch',
    GITOPS_PATH: 'configPath',
    GITOPS_POLL_INTERVAL_S: 'pollInterval',
    GITOPS_AUTO_PUSH: 'autoPush',
    GITOPS_AUTO_PULL: 'autoPull',
  };

  function initGitOpsConfig(): GitOpsConfig {
    const config: GitOpsConfig = {
      source: 'external',
      repoUrl: '',
      mantleUrl: '',
      group: '',
      node: '',
      branch: 'main',
      configPath: 'config',
      pollInterval: '60',
      autoPush: true,
      autoPull: true,
    };
    for (const entry of data.gitopsConfig ?? []) {
      const field = GITOPS_FIELD_MAP[entry.envVar];
      if (!field) continue;
      if (field === 'autoPush' || field === 'autoPull') {
        config[field] = entry.value === 'true';
      } else if (field === 'repoUrl' || field === 'branch' || field === 'configPath' || field === 'pollInterval') {
        config[field] = entry.value;
      }
    }
    // Infer mantle source from a previously-saved http(s)://host/git/<group>--<node>.git URL
    const mantleMatch = config.repoUrl.match(/^(https?:\/\/[^/]+)\/git\/([^/]+?)--([^/]+?)\.git$/);
    if (mantleMatch) {
      config.source = 'mantle';
      config.mantleUrl = mantleMatch[1];
      config.group = mantleMatch[2];
      config.node = mantleMatch[3];
    }
    return config;
  }

  const HISTORY_FIELD_MAP: Record<string, keyof HistoryConfig> = {
    HISTORY_DB_HOST: 'host',
    HISTORY_DB_PORT: 'port',
    HISTORY_DB_USER: 'user',
    HISTORY_DB_PASSWORD: 'password',
    HISTORY_DB_NAME: 'dbname',
  };

  function initHistoryConfig(): HistoryConfig {
    const config: HistoryConfig = {
      mode: 'local',
      host: 'localhost',
      port: '5432',
      user: 'postgres',
      password: 'postgres',
      dbname: 'tentacle',
      localInstalled: false,
    };
    for (const entry of data.historyConfig ?? []) {
      const field = HISTORY_FIELD_MAP[entry.envVar];
      if (field) (config[field] as string) = entry.value;
    }
    // If existing config points somewhere other than localhost, assume external mode.
    if (config.host && config.host !== 'localhost' && config.host !== '127.0.0.1') {
      config.mode = 'external';
    }
    return config;
  }

  const ADDON_MODULE_IDS = new Set(['network', 'gitops', 'history']);

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
  let gitopsConfig = $state<GitOpsConfig>(initGitOpsConfig());
  let historyConfig = $state<HistoryConfig>(initHistoryConfig());

  // Dynamic steps based on selected archetype + add-on selection.
  // Add-on config steps are inserted before the review step, in a stable order.
  const activeSteps = $derived.by<StepId[]>(() => {
    const base: StepId[] = selectedArchetype ? ARCHETYPE_STEPS[selectedArchetype] : ['architecture'];
    const reviewIdx = base.indexOf('review');
    if (reviewIdx === -1) return base;
    const inserts: StepId[] = [];
    if (selectedAddOns.has('gitops')) inserts.push('gitops-config');
    if (selectedAddOns.has('history')) inserts.push('history-config');
    if (inserts.length === 0) return base;
    return [...base.slice(0, reviewIdx), ...inserts, ...base.slice(reviewIdx)];
  });
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
      case 'gitops-config':
        return gitopsConfig.source === 'mantle'
          ? gitopsConfig.mantleUrl.trim() !== '' && gitopsConfig.group.trim() !== '' && gitopsConfig.node.trim() !== ''
          : gitopsConfig.repoUrl.trim() !== '';
      case 'history-config': return historyConfig.dbname.trim() !== '' &&
                   (historyConfig.mode === 'local' || historyConfig.host.trim() !== '');
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
        {#if visibleArchetypes.has('sparkplug-gateway')}
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
        {/if}

        {#if visibleArchetypes.has('nat-gateway')}
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
        {/if}

        {#if visibleArchetypes.has('mantle-host')}
          <ArchitectureCard
            title="Mantle Aggregator"
            description="Centralized Sparkplug B Host Application. Aggregates data from remote tentacles into a fleet-wide historian. Optionally embeds an MQTT broker for single-binary deployments."
            selected={selectedArchetype === 'mantle-host'}
            onclick={() => selectArchetype('mantle-host')}
          >
            {#snippet diagram()}
              <MantleDiagram compact={true} />
            {/snippet}
          </ArchitectureCard>
        {/if}
      </div>

    {:else if currentStepId === 'protocols'}
      <div class="step-intro">
        <h2>Select Protocols</h2>
        <p>Which industrial protocols does your environment use? Select all that apply.</p>
      </div>
      <ProtocolSelector
        selected={selectedProtocols}
        experimental={experimentalModules}
        unavailable={unavailableModules}
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

    {:else if currentStepId === 'gitops-config'}
      <div class="step-intro">
        <h2>Configure GitOps</h2>
        <p>Set up git-based configuration management for your device.</p>
      </div>
      <GitOpsConfigForm
        config={gitopsConfig}
        onchange={(c) => { gitopsConfig = c; }}
      />

    {:else if currentStepId === 'history-config'}
      <div class="step-intro">
        <h2>Configure History</h2>
        <p>Set up the edge historian database — install locally or connect to an existing one.</p>
      </div>
      <HistoryConfigForm
        config={historyConfig}
        onchange={(c) => { historyConfig = c; }}
      />

    {:else if currentStepId === 'review'}
      <ReviewPanel
        archetype={selectedArchetype ?? 'sparkplug-gateway'}
        {selectedProtocols}
        {selectedAddOns}
        {mqttConfig}
        {gitopsConfig}
        {historyConfig}
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
