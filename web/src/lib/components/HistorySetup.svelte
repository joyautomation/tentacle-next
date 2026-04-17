<script lang="ts">
  import HistoryConfigForm, { type HistoryConfig } from './setup/HistoryConfigForm.svelte';
  import { api, apiPut } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';

  let config = $state<HistoryConfig>({
    mode: 'local',
    host: 'localhost',
    port: '5432',
    user: 'postgres',
    password: 'postgres',
    dbname: 'tentacle',
    localInstalled: false,
  });

  // Pre-populate form from stored env config so subsequent edits start from the real values.
  $effect(() => { loadExistingConfig(); });

  async function loadExistingConfig() {
    const result = await api<Array<{ envVar: string; value: string }>>('/config/history');
    if (!result.data) return;
    const fieldMap: Record<string, keyof HistoryConfig> = {
      HISTORY_DB_HOST: 'host',
      HISTORY_DB_PORT: 'port',
      HISTORY_DB_USER: 'user',
      HISTORY_DB_PASSWORD: 'password',
      HISTORY_DB_NAME: 'dbname',
    };
    const next = { ...config };
    for (const entry of result.data) {
      const field = fieldMap[entry.envVar];
      if (field) (next[field] as string) = entry.value;
    }
    if (next.host && next.host !== 'localhost' && next.host !== '127.0.0.1') {
      next.mode = 'external';
    }
    config = next;
  }

  async function commit() {
    const configs: [string, string][] = [
      ['HISTORY_DB_HOST', config.host],
      ['HISTORY_DB_PORT', config.port],
      ['HISTORY_DB_USER', config.user],
      ['HISTORY_DB_PASSWORD', config.password],
      ['HISTORY_DB_NAME', config.dbname],
    ];
    const errors: string[] = [];
    for (const [envVar, value] of configs) {
      const result = await apiPut(`/config/history/${envVar}`, { value });
      if (result.error) errors.push(`${envVar}: ${result.error.error}`);
    }
    const enable = await apiPut('/orchestrator/desired-services/history', {
      version: 'latest',
      running: true,
    });
    if (enable.error) errors.push(`enable module: ${enable.error.error}`);

    if (errors.length > 0) {
      saltState.addNotification({ message: errors.join('; '), type: 'error' });
    } else {
      saltState.addNotification({ message: 'History configuration saved — module starting', type: 'success' });
      await invalidateAll();
    }
  }
</script>

<p class="intro">
  The history module stores PLC data in PostgreSQL with TimescaleDB.
  Install locally for a self-contained setup, or point at an existing database.
</p>

<HistoryConfigForm
  {config}
  onchange={(c) => { config = c; }}
  onCommit={commit}
/>

<style lang="scss">
  .intro {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0.5rem 0 1rem;
    line-height: 1.5;
  }
</style>
