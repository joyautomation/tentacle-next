<script lang="ts">
  import { api, apiPost, apiPut } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { slide } from 'svelte/transition';
  import { CheckCircle, XCircle } from '@joyautomation/salt/icons';

  type Mode = 'local' | 'external';
  type DBParams = { host: string; port: number; user: string; password: string; dbname: string };

  type Status = {
    params: DBParams;
    pgBinaryInstalled: boolean;
    timescaleInstalled: boolean;
    canInstallLocally: boolean;
    reachable: boolean;
    extensionAvailable: boolean;
    extensionCreated: boolean;
    error?: string;
  };

  let status = $state<Status | null>(null);
  let mode = $state<Mode>('local');

  let params = $state<DBParams>({
    host: 'localhost',
    port: 5432,
    user: 'postgres',
    password: 'postgres',
    dbname: 'tentacle',
  });

  let installing = $state(false);
  let installSteps = $state<Array<{ step: string; status: string; error?: string }>>([]);
  let installError = $state('');

  let testing = $state(false);
  let testResult = $state<{ success: boolean; error?: string; extensionAvailable?: boolean } | null>(null);

  let saving = $state(false);

  // Poll status after changes so the page updates when the module starts.
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  $effect(() => {
    if (installing) {
      if (!pollTimer) pollTimer = setInterval(loadStatus, 2000);
    } else if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    return () => {
      if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
    };
  });

  $effect(() => { loadStatus(); });

  async function loadStatus() {
    const result = await api<Status>('/history/db-status');
    if (result.data) {
      status = result.data;
      // Seed form with saved config on first load.
      if (!testing && !installing && result.data.params) {
        params = { ...result.data.params };
      }
      // Pick a sensible default mode: if not reachable and we can install, go local.
      if (!result.data.reachable && result.data.canInstallLocally && mode === 'local') {
        // keep local
      } else if (result.data.reachable) {
        mode = 'external';
      }
    }
  }

  async function installLocally() {
    installing = true;
    installError = '';
    installSteps = [];
    const result = await apiPost<{ success: boolean; error?: string; steps?: Array<{step: string; status: string; error?: string}> }>(
      '/history/db-install',
      params,
    );
    installing = false;
    if (result.data?.steps) installSteps = result.data.steps;
    if (result.data?.success) {
      await saveConfigAndStart();
    } else {
      installError = result.data?.error ?? result.error?.error ?? 'Installation failed';
    }
  }

  async function testConnection() {
    testing = true;
    testResult = null;
    const result = await apiPost<{ success: boolean; error?: string; extensionAvailable?: boolean }>(
      '/history/db-test',
      params,
    );
    testing = false;
    if (result.data) {
      testResult = result.data;
    } else if (result.error) {
      testResult = { success: false, error: result.error.error };
    }
  }

  async function saveConfigAndStart() {
    saving = true;
    const configs: [string, string][] = [
      ['HISTORY_DB_HOST', params.host],
      ['HISTORY_DB_PORT', String(params.port)],
      ['HISTORY_DB_USER', params.user],
      ['HISTORY_DB_PASSWORD', params.password],
      ['HISTORY_DB_NAME', params.dbname],
    ];
    const errors: string[] = [];
    for (const [envVar, value] of configs) {
      const result = await apiPut(`/config/history/${envVar}`, { value });
      if (result.error) errors.push(`${envVar}: ${result.error.error}`);
    }
    // Enable the module (idempotent — PUT on the desired-services key).
    const enable = await apiPut('/orchestrator/desired-services/history', {
      version: 'latest',
      running: true,
    });
    if (enable.error) errors.push(`enable module: ${enable.error.error}`);

    saving = false;
    if (errors.length > 0) {
      saltState.addNotification({ message: errors.join('; '), type: 'error' });
    } else {
      saltState.addNotification({ message: 'History configuration saved — module starting', type: 'success' });
      await invalidateAll();
      await loadStatus();
    }
  }
</script>

<div class="wizard">
  <p class="intro">
    The history module stores PLC data in PostgreSQL with TimescaleDB.
    Install locally for a self-contained setup, or point at an existing database.
  </p>

  <div class="mode-switch">
    <button
      type="button"
      class="mode-btn"
      class:active={mode === 'local'}
      onclick={() => { mode = 'local'; }}
      disabled={status ? !status.canInstallLocally : false}
    >
      Install Locally
      {#if status && !status.canInstallLocally}
        <span class="mode-hint">(requires root on Linux)</span>
      {/if}
    </button>
    <button
      type="button"
      class="mode-btn"
      class:active={mode === 'external'}
      onclick={() => { mode = 'external'; }}
    >
      Use Existing Database
    </button>
  </div>

  {#if mode === 'local'}
    <div class="section" transition:slide={{ duration: 200 }}>
      <h3>Install PostgreSQL + TimescaleDB</h3>
      <p class="section-desc">
        This will install PostgreSQL and TimescaleDB on this device using apt,
        create a database, and configure the history module to use it.
      </p>

      {#if status}
        <div class="status-grid">
          <div class="status-row">
            <span class="status-label">PostgreSQL</span>
            <span class="status-badge" class:ok={status.pgBinaryInstalled} class:missing={!status.pgBinaryInstalled}>
              {status.pgBinaryInstalled ? 'Installed' : 'Not installed'}
            </span>
          </div>
          <div class="status-row">
            <span class="status-label">TimescaleDB</span>
            <span class="status-badge" class:ok={status.timescaleInstalled} class:missing={!status.timescaleInstalled}>
              {status.timescaleInstalled ? 'Installed' : 'Not installed'}
            </span>
          </div>
          <div class="status-row">
            <span class="status-label">Database reachable</span>
            <span class="status-badge" class:ok={status.reachable} class:missing={!status.reachable}>
              {status.reachable ? 'Yes' : 'No'}
            </span>
          </div>
        </div>
      {/if}

      <div class="form-field">
        <label for="local-dbname">Database name</label>
        <input id="local-dbname" type="text" bind:value={params.dbname} />
      </div>
      <div class="form-field">
        <label for="local-pass">postgres user password</label>
        <p class="field-desc">Used for both the OS-level postgres role and the historian connection.</p>
        <input id="local-pass" type="text" bind:value={params.password} />
      </div>

      <div class="actions">
        <button class="btn primary" onclick={installLocally} disabled={installing || !status?.canInstallLocally}>
          {installing ? 'Installing...' : 'Install & Configure'}
        </button>
      </div>

      {#if installSteps.length > 0}
        <div class="install-log" transition:slide={{ duration: 200 }}>
          {#each installSteps as step}
            <div class="step-line" class:ok={step.status === 'ok'} class:failed={step.status === 'failed'} class:warning={step.status === 'warning'}>
              <span class="step-status">
                {#if step.status === 'ok'}
                  <CheckCircle size="0.875rem" />
                {:else if step.status === 'failed'}
                  <XCircle size="0.875rem" />
                {:else}
                  ·
                {/if}
              </span>
              <span class="step-text">
                {step.step}
                {#if step.error}<em class="step-err">— {step.error}</em>{/if}
              </span>
            </div>
          {/each}
        </div>
      {/if}

      {#if installError}
        <div class="error-box" transition:slide={{ duration: 200 }}>{installError}</div>
      {/if}
    </div>
  {:else}
    <div class="section" transition:slide={{ duration: 200 }}>
      <h3>Connect to Existing Database</h3>
      <p class="section-desc">
        Enter PostgreSQL connection details. The database must have the
        <code>timescaledb</code> extension available for hypertable support.
      </p>

      <div class="form-row">
        <div class="form-field" style="flex: 2;">
          <label for="ext-host">Host</label>
          <input id="ext-host" type="text" bind:value={params.host} />
        </div>
        <div class="form-field" style="flex: 1;">
          <label for="ext-port">Port</label>
          <input id="ext-port" type="number" bind:value={params.port} />
        </div>
      </div>

      <div class="form-row">
        <div class="form-field">
          <label for="ext-user">User</label>
          <input id="ext-user" type="text" bind:value={params.user} />
        </div>
        <div class="form-field">
          <label for="ext-pass">Password</label>
          <input id="ext-pass" type="password" bind:value={params.password} />
        </div>
      </div>

      <div class="form-field">
        <label for="ext-db">Database name</label>
        <input id="ext-db" type="text" bind:value={params.dbname} />
      </div>

      {#if testResult}
        <div class="test-result" class:success={testResult.success} class:fail={!testResult.success} transition:slide={{ duration: 200 }}>
          {#if testResult.success}
            <CheckCircle size="1rem" />
            <span>
              Connected.
              {#if testResult.extensionAvailable}
                TimescaleDB extension available.
              {:else}
                <em>TimescaleDB not available — history will fall back to plain PostgreSQL.</em>
              {/if}
            </span>
          {:else}
            <XCircle size="1rem" />
            <span>{testResult.error || 'Connection failed'}</span>
          {/if}
        </div>
      {/if}

      <div class="actions">
        <button class="btn secondary" onclick={testConnection} disabled={testing}>
          {testing ? 'Testing...' : 'Test Connection'}
        </button>
        <button class="btn primary" onclick={saveConfigAndStart} disabled={saving || !testResult?.success}>
          {saving ? 'Saving...' : 'Save & Start'}
        </button>
      </div>
    </div>
  {/if}
</div>

<style lang="scss">
  .wizard {
    margin-top: 0.5rem;
  }

  .intro {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 1rem;
    line-height: 1.5;
  }

  .mode-switch {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 1.25rem;
    border-bottom: 1px solid var(--theme-border);
  }

  .mode-btn {
    padding: 0.625rem 1rem;
    background: none;
    border: none;
    border-radius: 0;
    border-bottom: 2px solid transparent;
    color: var(--theme-text-muted);
    font-size: 0.8125rem;
    font-weight: 500;
    cursor: pointer;
    font-family: inherit;
    transition: color 0.15s, border-color 0.15s;
    margin-bottom: -1px;

    &.active {
      color: var(--theme-primary);
      border-bottom-color: var(--theme-primary);
    }

    &:hover:not(:disabled):not(.active) {
      color: var(--theme-text);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }

    .mode-hint {
      font-size: 0.7rem;
      opacity: 0.75;
      margin-left: 0.25rem;
    }
  }

  .section {
    h3 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 0.375rem;
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
      padding: 0.1rem 0.3rem;
      border-radius: 3px;
    }
  }

  .status-grid {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
    margin-bottom: 1rem;
    padding: 0.75rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
  }

  .status-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.8125rem;
  }

  .status-label {
    color: var(--theme-text-muted);
  }

  .status-badge {
    padding: 0.125rem 0.5rem;
    font-size: 0.7rem;
    font-weight: 600;
    border-radius: var(--rounded-full);
    text-transform: uppercase;
    letter-spacing: 0.05em;

    &.ok {
      background: var(--badge-green-bg);
      color: var(--badge-green-text);
    }

    &.missing {
      background: var(--badge-amber-bg);
      color: var(--badge-amber-text);
    }
  }

  .form-row {
    display: flex;
    gap: 0.75rem;

    .form-field {
      flex: 1;
    }
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

    input[type='text'],
    input[type='password'],
    input[type='number'] {
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
    }
  }

  .actions {
    display: flex;
    gap: 0.75rem;
    margin-top: 1rem;
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.5rem 1rem;
    font-size: 0.8125rem;
    font-weight: 600;
    border-radius: var(--rounded-md);
    border: none;
    cursor: pointer;
    font-family: inherit;
    transition: opacity 0.15s;

    &:hover:not(:disabled) { opacity: 0.9; }
    &:disabled { opacity: 0.5; cursor: not-allowed; }

    &.primary {
      background: var(--theme-primary);
      color: white;
    }

    &.secondary {
      background: var(--theme-surface);
      color: var(--theme-text);
      border: 1px solid var(--theme-border);
    }
  }

  .test-result {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.625rem 0.75rem;
    border-radius: var(--rounded-md);
    font-size: 0.8125rem;
    margin: 0.75rem 0;

    &.success {
      background: var(--badge-green-bg);
      color: var(--badge-green-text);
    }

    &.fail {
      background: color-mix(in srgb, var(--color-red-500, #ef4444) 10%, transparent);
      color: var(--color-red-500, #ef4444);
    }

    em {
      font-style: italic;
      opacity: 0.85;
    }
  }

  .install-log {
    margin-top: 0.75rem;
    padding: 0.75rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
  }

  .step-line {
    display: flex;
    gap: 0.5rem;
    align-items: flex-start;
    padding: 0.125rem 0;
    color: var(--theme-text-muted);

    &.ok { color: var(--badge-green-text); }
    &.failed { color: var(--color-red-500, #ef4444); }
    &.warning { color: var(--badge-amber-text); }

    .step-status {
      flex-shrink: 0;
      width: 1rem;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .step-text {
      flex: 1;
    }

    .step-err {
      font-style: italic;
      opacity: 0.85;
    }
  }

  .error-box {
    margin-top: 0.75rem;
    padding: 0.625rem 0.75rem;
    background: color-mix(in srgb, var(--color-red-500, #ef4444) 10%, transparent);
    border: 1px solid var(--color-red-500, #ef4444);
    color: var(--color-red-500, #ef4444);
    font-size: 0.8125rem;
    border-radius: var(--rounded-md);
  }
</style>
