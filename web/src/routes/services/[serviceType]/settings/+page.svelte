<script lang="ts">
  import type { PageData } from './$types';
  import { page } from '$app/stores';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState, forms } from '@joyautomation/salt';
  import type { FormGroup } from '@joyautomation/salt/forms';
  import { apiPut } from '$lib/api/client';
  const { Form } = forms;

  let { data }: { data: PageData } = $props();

  const serviceType = $derived($page.params.serviceType ?? '');

  type ConfigEntry = {
    key: string;
    envVar: string;
    value: string;
    moduleId: string;
  };

  const configEntries = $derived((data?.config ?? []) as ConfigEntry[]);

  // Field display config: grouped, ordered, with custom labels
  type FieldDef = { envVar: string; label: string; type?: string };
  type FieldGroup = { name: string; fields: FieldDef[] };

  const fieldGroups: FieldGroup[] = [
    {
      name: 'Connection',
      fields: [
        { envVar: 'MQTT_BROKER_URL', label: 'Broker URL' },
        { envVar: 'MQTT_CLIENT_ID', label: 'Client ID' },
        { envVar: 'MQTT_GROUP_ID', label: 'Group ID' },
        { envVar: 'MQTT_EDGE_NODE', label: 'Node ID' },
        { envVar: 'MQTT_USERNAME', label: 'Username' },
        { envVar: 'MQTT_PASSWORD', label: 'Password', type: 'password' },
        { envVar: 'MQTT_KEEPALIVE', label: 'Keep Alive (seconds)' },
      ],
    },
    {
      name: 'Sparkplug B',
      fields: [
        { envVar: 'MQTT_USE_TEMPLATES', label: 'Use Templates', type: 'checkbox' },
      ],
    },
    {
      name: 'Store & Forward',
      fields: [
        { envVar: 'MQTT_PRIMARY_HOST_ID', label: 'Primary Host ID' },
        { envVar: 'MQTT_SF_MAX_MB', label: 'Max Buffer (MB)' },
        { envVar: 'MQTT_SF_MAX_RECORDS', label: 'Max Records' },
        { envVar: 'MQTT_SF_DRAIN_RATE', label: 'Drain Rate (records/sec)' },
      ],
    },
  ];

  // Build a lookup from envVar → value
  const configByEnvVar = $derived(
    Object.fromEntries(configEntries.map(e => [e.envVar, e.value]))
  );

  // Build salt FormGroups with headings
  const formGroups: FormGroup[] = $derived(
    fieldGroups
      .map(group => ({
        heading: group.name,
        rows: group.fields
          .map(f => [{
            id: f.envVar,
            name: f.envVar,
            label: f.label,
            type: f.type ?? 'text',
            value: configByEnvVar[f.envVar] ?? '',
            validations: [],
          }]),
      }))
      .filter(g => g.rows.length > 0)
  );

  let saving = $state(false);

  async function handleSave(event: SubmitEvent) {
    event.preventDefault();
    saving = true;

    const form = event.target as HTMLFormElement;
    const formData = new FormData(form);
    const errors: string[] = [];

    for (const [envVar, value] of formData.entries()) {
      if (typeof value !== 'string') continue;

      const result = await apiPut<ConfigEntry>(`/config/${serviceType}/${envVar}`, { value });
      if (result.error) {
        errors.push(`${envVar}: ${result.error.error}`);
      }
    }

    saving = false;

    if (errors.length > 0) {
      saltState.addNotification({ message: errors.join('; '), type: 'error' });
    } else {
      saltState.addNotification({ message: 'Settings saved', type: 'success' });
      await invalidateAll();
    }
  }
</script>

<div class="settings-page">
  {#if data.error}
    <div class="error-box">
      <p>{data.error}</p>
    </div>
  {/if}

  {#if configEntries.length > 0}
    <Form
      groups={formGroups}
      onsubmit={handleSave}
      buttonText={saving ? 'Saving...' : 'Save All'}
      showReset={true}
      resetButtonText="Reset"
    />
  {:else if !data.error}
    <div class="empty-state">
      <p>No configuration found. Start the service to populate config from environment variables.</p>
    </div>
  {/if}
</div>

<style lang="scss">
  .settings-page {
    padding: 1.5rem;
    max-width: 700px;
  }

  .error-box {
    padding: 1rem;
    border-radius: var(--rounded-lg);
    background: var(--theme-surface);
    border: 1px solid var(--color-red-500, #ef4444);
    margin-bottom: 1.5rem;
    p { margin: 0; font-size: 0.875rem; color: var(--color-red-500, #ef4444); }
  }

  .empty-state {
    padding: 3rem 2rem;
    text-align: center;
    p { color: var(--theme-text-muted); font-size: 0.875rem; }
  }
</style>
