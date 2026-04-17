<script lang="ts" module>
  export type HistoryMode = 'local' | 'external';
  export interface HistoryConfig {
    mode: HistoryMode;
    host: string;
    port: string;
    user: string;
    password: string;
    dbname: string;
    localInstalled: boolean;
  }
</script>

<script lang="ts">
  import { api, apiPost } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import { slide } from 'svelte/transition';
  import { CheckCircle, XCircle } from '@joyautomation/salt/icons';

  interface Status {
    params: { host: string; port: number; user: string; password: string; dbname: string };
    pgBinaryInstalled: boolean;
    timescaleInstalled: boolean;
    canInstallLocally: boolean;
    reachable: boolean;
    extensionAvailable: boolean;
    extensionCreated: boolean;
  }

  interface Props {
    config: HistoryConfig;
    onchange: (config: HistoryConfig) => void;
    /**
     * When provided, the form shows commit buttons ("Install & Configure" in local mode
     * calls this after install; "Save & Start" in external mode calls this directly).
     * When absent, the form only collects values — a parent (e.g. setup wizard) handles
     * writing env vars and enabling the module at its own apply step.
     */
    onCommit?: () => Promise<void>;
  }

  let { config, onchange, onCommit }: Props = $props();

  let status = $state<Status | null>(null);
  let installing = $state(false);
  let installSteps = $state<Array<{ id: number; step: string; status: string; error?: string }>>([]);
  let installError = $state('');
  let installDone = $state(false);
  let installFailed = $state(false);
  let installFailure = $state('');
  let testing = $state(false);
  let testResult = $state<{ success: boolean; error?: string; extensionAvailable?: boolean } | null>(null);
  let committing = $state(false);

  $effect(() => { loadStatus(); });

  async function loadStatus() {
    const result = await api<Status>('/history/db-status');
    if (result.data) {
      status = result.data;
      // Auto-flip to external mode if we can't install locally.
      if (!result.data.canInstallLocally && config.mode === 'local') {
        update('mode', 'external');
      }
      // Mark local as already installed if PG + timescale are present and reachable.
      if (result.data.pgBinaryInstalled && result.data.timescaleInstalled && result.data.reachable) {
        update('localInstalled', true);
      }
    }
  }

  function update<K extends keyof HistoryConfig>(field: K, value: HistoryConfig[K]) {
    onchange({ ...config, [field]: value });
  }

  async function installLocally() {
    installing = true;
    installError = '';
    installSteps = [];
    installDone = false;
    installFailed = false;
    installFailure = '';
    let errMsg = '';
    try {
      const res = await fetch('/api/v1/history/db-install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host: config.host,
          port: parseInt(config.port, 10) || 5432,
          user: config.user,
          password: config.password,
          dbname: config.dbname,
        }),
      });
      if (!res.ok || !res.body) {
        errMsg = `install request failed (HTTP ${res.status})`;
      } else {
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = '';
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          let nl = buf.indexOf('\n');
          while (nl !== -1) {
            const line = buf.slice(0, nl).trim();
            buf = buf.slice(nl + 1);
            nl = buf.indexOf('\n');
            if (!line) continue;
            handleInstallEvent(JSON.parse(line));
          }
        }
      }
    } catch (e) {
      errMsg = e instanceof Error ? e.message : String(e);
    }
    installing = false;
    if (installDone && !installFailed) {
      update('localInstalled', true);
      await loadStatus();
      saltState.addNotification({ message: 'PostgreSQL + TimescaleDB installed', type: 'success' });
      if (onCommit) await runCommit();
    } else {
      installError = errMsg || installFailure || 'Installation failed';
    }
  }

  function handleInstallEvent(ev: { type: string; id?: number; step?: string; status?: string; error?: string; success?: boolean }) {
    if (ev.type === 'step' && typeof ev.id === 'number' && ev.step && ev.status) {
      const existing = installSteps.findIndex((s) => s.id === ev.id);
      const entry = { id: ev.id, step: ev.step, status: ev.status, error: ev.error };
      if (existing >= 0) {
        installSteps[existing] = entry;
      } else {
        installSteps = [...installSteps, entry];
      }
    } else if (ev.type === 'done') {
      installDone = true;
      installFailed = !ev.success;
      if (ev.error) installFailure = ev.error;
    }
  }

  async function runCommit() {
    if (!onCommit) return;
    committing = true;
    try {
      await onCommit();
    } finally {
      committing = false;
    }
  }

  async function testConnection() {
    testing = true;
    testResult = null;
    const result = await apiPost<{ success: boolean; error?: string; extensionAvailable?: boolean }>(
      '/history/db-test',
      {
        host: config.host,
        port: parseInt(config.port, 10) || 5432,
        user: config.user,
        password: config.password,
        dbname: config.dbname,
      },
    );
    testing = false;
    if (result.data) {
      testResult = result.data;
    } else if (result.error) {
      testResult = { success: false, error: result.error.error };
    }
  }
</script>

<div class="history-form">
  <div class="mode-switch">
    <button
      type="button"
      class="mode-btn"
      class:active={config.mode === 'local'}
      onclick={() => update('mode', 'local')}
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
      class:active={config.mode === 'external'}
      onclick={() => update('mode', 'external')}
    >
      Use Existing Database
    </button>
  </div>

  {#if config.mode === 'local'}
    <section class="form-section" transition:slide={{ duration: 200 }}>
      <h3>Install PostgreSQL + TimescaleDB</h3>
      <p class="section-desc">
        Install PostgreSQL and TimescaleDB on this device using apt, create a database,
        and configure the history module to use it.
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
        <label for="hist-local-dbname">Database name</label>
        <input
          id="hist-local-dbname"
          type="text"
          value={config.dbname}
          oninput={(e) => update('dbname', e.currentTarget.value)}
        />
      </div>
      <div class="form-field">
        <label for="hist-local-pass">postgres user password</label>
        <p class="field-desc">Used for both the OS-level postgres role and the historian connection.</p>
        <input
          id="hist-local-pass"
          type="text"
          value={config.password}
          oninput={(e) => update('password', e.currentTarget.value)}
        />
      </div>

      <button
        class="btn primary"
        onclick={installLocally}
        disabled={installing || !status?.canInstallLocally || config.localInstalled}
      >
        {#if config.localInstalled}
          Installed
        {:else}
          {installing ? 'Installing...' : 'Install & Configure'}
        {/if}
      </button>

      {#if installSteps.length > 0}
        <div class="install-log" transition:slide={{ duration: 200 }}>
          {#each installSteps as step (step.id)}
            <div
              class="step-line"
              class:ok={step.status === 'ok'}
              class:failed={step.status === 'failed'}
              class:warning={step.status === 'warning'}
              class:running={step.status === 'running'}
            >
              <span class="step-status">
                {#if step.status === 'ok'}
                  <CheckCircle size="0.875rem" />
                {:else if step.status === 'failed'}
                  <XCircle size="0.875rem" />
                {:else if step.status === 'running'}
                  <span class="spinner" aria-hidden="true"></span>
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
    </section>
  {:else}
    <section class="form-section" transition:slide={{ duration: 200 }}>
      <h3>Connect to Existing Database</h3>
      <p class="section-desc">
        Enter PostgreSQL connection details. The database must have the
        <code>timescaledb</code> extension available for hypertable support.
      </p>

      <div class="form-row">
        <div class="form-field" style="flex: 2;">
          <label for="hist-ext-host">Host</label>
          <input
            id="hist-ext-host"
            type="text"
            value={config.host}
            oninput={(e) => update('host', e.currentTarget.value)}
          />
        </div>
        <div class="form-field" style="flex: 1;">
          <label for="hist-ext-port">Port</label>
          <input
            id="hist-ext-port"
            type="text"
            value={config.port}
            oninput={(e) => update('port', e.currentTarget.value)}
          />
        </div>
      </div>

      <div class="form-row">
        <div class="form-field">
          <label for="hist-ext-user">User</label>
          <input
            id="hist-ext-user"
            type="text"
            value={config.user}
            oninput={(e) => update('user', e.currentTarget.value)}
          />
        </div>
        <div class="form-field">
          <label for="hist-ext-pass">Password</label>
          <input
            id="hist-ext-pass"
            type="password"
            value={config.password}
            oninput={(e) => update('password', e.currentTarget.value)}
          />
        </div>
      </div>

      <div class="form-field">
        <label for="hist-ext-db">Database name</label>
        <input
          id="hist-ext-db"
          type="text"
          value={config.dbname}
          oninput={(e) => update('dbname', e.currentTarget.value)}
        />
      </div>

      <div class="actions">
        <button class="btn secondary" onclick={testConnection} disabled={testing}>
          {testing ? 'Testing...' : 'Test Connection'}
        </button>
        {#if onCommit}
          <button class="btn primary" onclick={runCommit} disabled={committing || !testResult?.success}>
            {committing ? 'Saving...' : 'Save & Start'}
          </button>
        {/if}
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
    </section>
  {/if}
</div>

<style lang="scss">
  .history-form {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .mode-switch {
    display: flex;
    gap: 0.5rem;
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
    }
  }

  .actions {
    display: flex;
    gap: 0.75rem;
    margin-top: 0.25rem;
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
    margin-top: 0.75rem;

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
    &.running { color: var(--theme-text); }

    .step-status {
      flex-shrink: 0;
      width: 1rem;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .spinner {
      width: 0.75rem;
      height: 0.75rem;
      border: 2px solid var(--theme-border);
      border-top-color: var(--theme-primary);
      border-radius: 50%;
      animation: spin 0.8s linear infinite;
    }

    .step-text {
      flex: 1;
    }

    .step-err {
      font-style: italic;
      opacity: 0.85;
    }
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
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
