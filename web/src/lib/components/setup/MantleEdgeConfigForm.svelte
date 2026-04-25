<script lang="ts">
  import { mantleRepoUrl } from './mantleRepo';

  // Single-form config for an edge tentacle whose source-of-truth lives on a
  // mantle. Group/Node feed BOTH the Sparkplug identity (MQTT_GROUP_ID +
  // MQTT_EDGE_NODE) and the GitOps repo name (<group>--<node>.git on the
  // mantle), so the operator only ever enters them once.
  export interface MantleEdgeConfig {
    group: string;
    node: string;
    mantleUrl: string;
    mqttBrokerUrl: string;
    mqttUsername: string;
    mqttPassword: string;
  }

  interface Props {
    config: MantleEdgeConfig;
    onchange: (config: MantleEdgeConfig) => void;
  }

  let { config, onchange }: Props = $props();

  function update<K extends keyof MantleEdgeConfig>(field: K, value: MantleEdgeConfig[K]) {
    onchange({ ...config, [field]: value });
  }

  const computedRepoUrl = $derived(
    config.mantleUrl && config.group && config.node
      ? mantleRepoUrl(config.mantleUrl, config.group, config.node)
      : ''
  );
</script>

<div class="form">
  <section class="form-section">
    <h3>Identity</h3>
    <p class="section-desc">
      Used for both the Sparkplug B node identity and the per-device git repo
      on the mantle (<code>&lt;group&gt;--&lt;node&gt;.git</code>).
    </p>
    <div class="row-2">
      <div class="form-field">
        <label for="me-group">Group</label>
        <p class="field-desc">Sparkplug Group ID — the fleet bucket this edge belongs to.</p>
        <input
          id="me-group"
          type="text"
          value={config.group}
          oninput={(e) => update('group', e.currentTarget.value)}
          placeholder="MyGroup"
        />
      </div>
      <div class="form-field">
        <label for="me-node">Node</label>
        <p class="field-desc">Edge node name — unique within the group.</p>
        <input
          id="me-node"
          type="text"
          value={config.node}
          oninput={(e) => update('node', e.currentTarget.value)}
          placeholder="EdgeNode1"
        />
      </div>
    </div>
  </section>

  <section class="form-section">
    <h3>Mantle</h3>
    <p class="section-desc">
      The mantle hosts both the MQTT-side aggregator and the per-edge config
      repo. We'll create <code>&lt;group&gt;--&lt;node&gt;.git</code> on it
      automatically when you click <strong>Apply &amp; Start</strong>.
    </p>
    <div class="form-field">
      <label for="me-mantle-url">Mantle URL</label>
      <p class="field-desc">Base URL of the mantle — where the API and git server are exposed.</p>
      <input
        id="me-mantle-url"
        type="text"
        value={config.mantleUrl}
        oninput={(e) => update('mantleUrl', e.currentTarget.value)}
        placeholder="http://mantle.local:4000"
      />
    </div>

    {#if computedRepoUrl}
      <div class="repo-preview">
        <span class="preview-label">Will create</span>
        <code class="preview-url">{computedRepoUrl}</code>
      </div>
    {/if}
  </section>

  <section class="form-section">
    <h3>MQTT Broker</h3>
    <p class="section-desc">
      Where this edge publishes Sparkplug B traffic. Often the same host as
      the mantle but on the broker port (1883/8883). Username/password are
      optional — leave blank for anonymous brokers.
    </p>
    <div class="form-field">
      <label for="me-broker-url">Broker URL</label>
      <input
        id="me-broker-url"
        type="text"
        value={config.mqttBrokerUrl}
        oninput={(e) => update('mqttBrokerUrl', e.currentTarget.value)}
        placeholder="tcp://mantle.local:1883"
      />
    </div>
    <div class="row-2">
      <div class="form-field">
        <label for="me-mqtt-user">Username (optional)</label>
        <input
          id="me-mqtt-user"
          type="text"
          value={config.mqttUsername}
          oninput={(e) => update('mqttUsername', e.currentTarget.value)}
          autocomplete="off"
        />
      </div>
      <div class="form-field">
        <label for="me-mqtt-pass">Password (optional)</label>
        <input
          id="me-mqtt-pass"
          type="password"
          value={config.mqttPassword}
          oninput={(e) => update('mqttPassword', e.currentTarget.value)}
          autocomplete="off"
        />
      </div>
    </div>
  </section>
</div>

<style lang="scss">
  .form {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }

  .form-section {
    h3 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 0.25rem;
    }
  }

  .section-desc {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 1rem;
    line-height: 1.5;

    code {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
      background: var(--theme-surface);
      border: 1px solid var(--theme-border);
      padding: 0.05rem 0.3rem;
      border-radius: var(--rounded-sm, 0.25rem);
    }
  }

  .row-2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.75rem;
  }

  .form-field {
    margin-bottom: 0.75rem;

    label {
      display: block;
      font-size: 0.8125rem;
      font-weight: 500;
      color: var(--theme-text-muted);
      margin-bottom: 0.25rem;
    }

    .field-desc {
      font-size: 0.75rem;
      color: var(--theme-text-muted);
      margin: 0 0 0.375rem;
    }

    input {
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

      &:focus { border-color: var(--theme-primary); }
    }
  }

  .repo-preview {
    margin-top: 0.5rem;
    padding: 0.5rem 0.75rem;
    background: var(--theme-surface);
    border: 1px dashed var(--theme-border);
    border-radius: var(--rounded-md);
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.5rem;
  }

  .preview-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .preview-url {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.8125rem;
    color: var(--theme-text);
    word-break: break-all;
  }
</style>
