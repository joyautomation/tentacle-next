<script lang="ts">
  export interface MqttConfig {
    MQTT_BROKER_URL: string;
    MQTT_CLIENT_ID: string;
    MQTT_GROUP_ID: string;
    MQTT_EDGE_NODE: string;
    MQTT_USERNAME: string;
    MQTT_PASSWORD: string;
  }

  interface Props {
    config: MqttConfig;
    onchange: (config: MqttConfig) => void;
  }

  let { config, onchange }: Props = $props();

  function update(field: keyof MqttConfig, value: string) {
    onchange({ ...config, [field]: value });
  }
</script>

<div class="mqtt-form">
  <section class="form-group">
    <h2>MQTT Broker</h2>
    <div class="form-field">
      <label class="field-label" for="mqtt-broker-url">Broker URL <span class="required">*</span></label>
      <input
        id="mqtt-broker-url"
        type="text"
        value={config.MQTT_BROKER_URL}
        oninput={(e) => update('MQTT_BROKER_URL', (e.target as HTMLInputElement).value)}
        placeholder="tcp://localhost:1883"
      />
      <span class="field-hint">Use tcp:// or mqtt:// for plain MQTT, ssl:// or mqtts:// for TLS</span>
    </div>
    <div class="form-field">
      <label class="field-label" for="mqtt-username">Username</label>
      <input
        id="mqtt-username"
        type="text"
        value={config.MQTT_USERNAME}
        oninput={(e) => update('MQTT_USERNAME', (e.target as HTMLInputElement).value)}
        placeholder="Optional"
      />
    </div>
    <div class="form-field">
      <label class="field-label" for="mqtt-password">Password</label>
      <input
        id="mqtt-password"
        type="password"
        value={config.MQTT_PASSWORD}
        oninput={(e) => update('MQTT_PASSWORD', (e.target as HTMLInputElement).value)}
        placeholder="Optional"
      />
    </div>
  </section>

  <section class="form-group">
    <h2>Sparkplug Identity</h2>
    <div class="form-field">
      <label class="field-label" for="mqtt-group-id">Group ID <span class="required">*</span></label>
      <input
        id="mqtt-group-id"
        type="text"
        value={config.MQTT_GROUP_ID}
        oninput={(e) => update('MQTT_GROUP_ID', (e.target as HTMLInputElement).value)}
        placeholder="TentacleGroup"
      />
    </div>
    <div class="form-field">
      <label class="field-label" for="mqtt-edge-node">Node ID <span class="required">*</span></label>
      <input
        id="mqtt-edge-node"
        type="text"
        value={config.MQTT_EDGE_NODE}
        oninput={(e) => update('MQTT_EDGE_NODE', (e.target as HTMLInputElement).value)}
        placeholder="EdgeNode1"
      />
    </div>
    <div class="form-field">
      <label class="field-label" for="mqtt-client-id">Client ID</label>
      <input
        id="mqtt-client-id"
        type="text"
        value={config.MQTT_CLIENT_ID}
        oninput={(e) => update('MQTT_CLIENT_ID', (e.target as HTMLInputElement).value)}
        placeholder="tentacle-mqtt"
      />
    </div>
  </section>
</div>

<style lang="scss">
  .mqtt-form {
    max-width: 500px;
  }

  .form-group {
    margin-bottom: 1.5rem;

    h2 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 0.75rem;
    }
  }

  .form-field {
    margin-bottom: 0.75rem;
  }

  .field-label {
    display: block;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    margin-bottom: 0.25rem;
  }

  .required {
    color: var(--color-red-400, #f87171);
  }

  .field-hint {
    display: block;
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    margin-top: 0.25rem;
    opacity: 0.7;
  }

  input[type='text'],
  input[type='password'] {
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

    &:focus {
      border-color: var(--theme-primary);
    }

    &::placeholder {
      color: var(--theme-text-muted);
      opacity: 0.5;
    }
  }
</style>
