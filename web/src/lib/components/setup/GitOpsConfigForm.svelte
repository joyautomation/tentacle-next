<script lang="ts">
  import { api, apiPost } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';
  import { slide } from 'svelte/transition';
  import { CheckCircle, XCircle } from '@joyautomation/salt/icons';

  import { mantleRepoUrl } from './mantleRepo';

  export interface GitOpsConfig {
    source: 'external' | 'mantle';
    repoUrl: string;
    mantleUrl: string;
    group: string;
    node: string;
    branch: string;
    configPath: string;
    pollInterval: string;
    autoPush: boolean;
    autoPull: boolean;
  }

  interface Props {
    config: GitOpsConfig;
    onchange: (config: GitOpsConfig) => void;
    /**
     * When provided, the form shows a "Save & Start" button that calls this handler.
     * When absent, the form only collects values — a parent (e.g. setup wizard) handles
     * writing env vars and enabling the module at its own apply step.
     */
    onCommit?: () => Promise<void>;
    /**
     * When true, source is locked to 'mantle' and the source toggle is hidden.
     * Group/Node inputs are also hidden — they're expected to be supplied by
     * the wizard from a prior step (typically the MQTT identity step).
     */
    lockedToMantle?: boolean;
  }

  let { config, onchange, onCommit, lockedToMantle = false }: Props = $props();

  let committing = $state(false);
  async function runCommit() {
    if (!onCommit) return;
    committing = true;
    try {
      await onCommit();
    } finally {
      committing = false;
    }
  }

  // Git availability
  let gitInstalled = $state<boolean | null>(null); // null = loading
  let installing = $state(false);
  let installError = $state('');

  // SSH key state
  let sshKey = $state({ exists: false, publicKey: '', path: '' });
  let generatingKey = $state(false);
  let copied = $state(false);

  // Test connection state
  let testing = $state(false);
  let testResult: { success: boolean; error?: string } | null = $state(null);

  // Check git + load SSH key on mount
  $effect(() => {
    checkGit();
    loadSSHKey();
  });

  async function checkGit() {
    const result = await api<{ installed: boolean }>('/gitops/git-check');
    gitInstalled = result.data?.installed ?? false;
  }

  async function installGit() {
    installing = true;
    installError = '';
    const result = await apiPost<{ success: boolean; error?: string }>('/gitops/git-install');
    installing = false;
    if (result.data?.success) {
      gitInstalled = true;
    } else {
      installError = result.data?.error ?? result.error?.error ?? 'Installation failed';
    }
  }

  async function loadSSHKey() {
    const result = await api<{ exists: boolean; publicKey: string; path: string }>('/gitops/ssh-key');
    if (result.data) sshKey = result.data;
  }

  async function generateKey() {
    generatingKey = true;
    const result = await apiPost<{ exists: boolean; publicKey: string; path: string }>('/gitops/ssh-key/generate');
    generatingKey = false;
    if (result.data) {
      sshKey = result.data;
    } else if (result.error) {
      saltState.addNotification({ message: result.error.error, type: 'error' });
    }
  }

  async function copyPublicKey() {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(sshKey.publicKey);
      } else {
        const ta = document.createElement('textarea');
        ta.value = sshKey.publicKey;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      copied = true;
      setTimeout(() => { copied = false; }, 2000);
    } catch {
      saltState.addNotification({ message: 'Failed to copy to clipboard', type: 'error' });
    }
  }

  async function testConnection() {
    testing = true;
    testResult = null;
    const result = await apiPost<{ success: boolean; error?: string }>('/gitops/test-connection', { repoUrl: config.repoUrl });
    testing = false;
    if (result.data) {
      testResult = result.data;
    } else if (result.error) {
      testResult = { success: false, error: result.error.error };
    }
  }

  function update(field: keyof GitOpsConfig, value: string | boolean) {
    onchange({ ...config, [field]: value });
  }

  const computedRepoUrl = $derived(
    config.source === 'mantle' && config.mantleUrl && config.group && config.node
      ? mantleRepoUrl(config.mantleUrl, config.group, config.node)
      : ''
  );
</script>

<div class="gitops-form">
  {#if gitInstalled === false}
    <div class="git-missing" transition:slide={{ duration: 200 }}>
      <div class="git-missing-content">
        <svg viewBox="0 0 20 20" fill="currentColor" width="20" height="20">
          <path fill-rule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.168 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 5a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 5zm0 9a1 1 0 100-2 1 1 0 000 2z" clip-rule="evenodd" />
        </svg>
        <div>
          <strong>Git is not installed</strong>
          <p>GitOps requires git to sync configuration with your repository.</p>
        </div>
      </div>
      <button class="btn primary" onclick={installGit} disabled={installing}>
        {installing ? 'Installing...' : 'Install Git'}
      </button>
      {#if installError}
        <p class="install-error">{installError}</p>
      {/if}
    </div>
  {/if}

  {#if !lockedToMantle}
  <!-- Source Section -->
  <section class="form-section source-section">
    <h3>Source</h3>
    <p class="section-desc">Where does this device's config repo live?</p>
    <div class="source-toggle" role="radiogroup">
      <button
        type="button"
        class="source-option"
        class:active={config.source === 'external'}
        onclick={() => update('source', 'external')}
        role="radio"
        aria-checked={config.source === 'external'}
      >
        <span class="source-title">External Git</span>
        <span class="source-sub">GitHub, GitLab, self-hosted (SSH)</span>
      </button>
      <button
        type="button"
        class="source-option"
        class:active={config.source === 'mantle'}
        onclick={() => update('source', 'mantle')}
        role="radio"
        aria-checked={config.source === 'mantle'}
      >
        <span class="source-title">Mantle</span>
        <span class="source-sub">Tentacle mantle git server (HTTP)</span>
      </button>
    </div>
  </section>
  {/if}

{#if config.source === 'external'}
  <!-- SSH Key Section -->
  <section class="form-section">
    <h3>SSH Key</h3>
    <p class="section-desc">An SSH key lets this device authenticate with your git host without a password.</p>

    {#if sshKey.exists}
      <div class="key-display">
        <p class="field-label">Public Key</p>
        <div class="key-box">
          <code>{sshKey.publicKey}</code>
        </div>
        <div class="key-actions">
          <button class="btn secondary copy-btn" onclick={copyPublicKey}>
            <span class="copy-label" class:hidden={copied}>Copy Public Key</span>
            <span class="copy-confirm" class:visible={copied}>Copied!</span>
          </button>
          <a class="btn secondary" href="https://github.com/settings/ssh/new" target="_blank" rel="noopener">
            Add to GitHub
          </a>
          <a class="btn secondary" href="https://gitlab.com/-/user_settings/ssh_keys" target="_blank" rel="noopener">
            Add to GitLab
          </a>
        </div>
        <p class="help-text">Copy the public key and add it to your git host's SSH key settings.</p>
        <button class="btn secondary small" onclick={generateKey} disabled={generatingKey}>
          {generatingKey ? 'Generating...' : 'Regenerate Key'}
        </button>
      </div>
    {:else}
      <p class="help-text">No SSH key found. Generate one to get started.</p>
      <button class="btn primary" onclick={generateKey} disabled={generatingKey}>
        {generatingKey ? 'Generating...' : 'Generate SSH Key'}
      </button>
    {/if}
  </section>

  <!-- Repository Section -->
  <section class="form-section">
    <h3>Repository</h3>
    <p class="section-desc">
      Enter the SSH URL of your git repository. If you haven't created one yet,
      <a href="https://github.com/new" target="_blank" rel="noopener">create a new repository</a>
      then paste the SSH URL below.
    </p>

    <div class="form-field">
      <label for="gitops-repo-url">Repository URL (SSH)</label>
      <input
        id="gitops-repo-url"
        type="text"
        value={config.repoUrl}
        oninput={(e) => update('repoUrl', e.currentTarget.value)}
        placeholder="git@github.com:your-org/your-device-config.git"
      />
    </div>

    <button class="btn secondary" onclick={testConnection} disabled={testing || !config.repoUrl}>
      {testing ? 'Testing...' : 'Test Connection'}
    </button>

    {#if testResult}
      <div class="test-result" class:success={testResult.success} class:fail={!testResult.success} transition:slide={{ duration: 200 }}>
        {#if testResult.success}
          <CheckCircle size="1rem" />
          <span>Connection successful</span>
        {:else}
          <XCircle size="1rem" />
          <span>{testResult.error || 'Connection failed'}</span>
        {/if}
      </div>
    {/if}
  </section>
{:else}
  <!-- Mantle Section -->
  <section class="form-section" transition:slide={{ duration: 150 }}>
    <h3>Mantle Server</h3>
    <p class="section-desc">
      Point at the mantle's HTTP API. A bare repo named <code>&lt;group&gt;--&lt;node&gt;.git</code>
      will be created on the mantle when you click <strong>Apply &amp; Start</strong>.
      {#if lockedToMantle}
        Group / Node come from the MQTT identity step.
      {/if}
    </p>

    <div class="form-field">
      <label for="gitops-mantle-url">Mantle URL</label>
      <p class="field-desc">Base URL of the mantle (where the API and git server are exposed)</p>
      <input
        id="gitops-mantle-url"
        type="text"
        value={config.mantleUrl}
        oninput={(e) => update('mantleUrl', e.currentTarget.value)}
        placeholder="http://mantle.local:4000"
      />
    </div>

    {#if !lockedToMantle}
      <div class="mantle-id-row">
        <div class="form-field">
          <label for="gitops-group">Group</label>
          <p class="field-desc">Sparkplug Group ID (or any logical fleet bucket)</p>
          <input
            id="gitops-group"
            type="text"
            value={config.group}
            oninput={(e) => update('group', e.currentTarget.value)}
            placeholder="MyGroup"
          />
        </div>
        <div class="form-field">
          <label for="gitops-node">Node</label>
          <p class="field-desc">Edge node name — unique within group</p>
          <input
            id="gitops-node"
            type="text"
            value={config.node}
            oninput={(e) => update('node', e.currentTarget.value)}
            placeholder="EdgeNode1"
          />
        </div>
      </div>
    {:else}
      <div class="identity-display">
        <span class="identity-label">Group / Node</span>
        <code class="identity-value">{config.group || '—'} / {config.node || '—'}</code>
      </div>
    {/if}

    {#if computedRepoUrl}
      <div class="repo-preview" transition:slide={{ duration: 150 }}>
        <span class="preview-label">Will create</span>
        <code class="preview-url">{computedRepoUrl}</code>
      </div>
    {/if}
  </section>

{/if}

  <!-- Settings Section -->
  <section class="form-section">
    <h3>Sync Settings</h3>
    <p class="section-desc">Configure sync behavior. The defaults work well for most setups.</p>

    <div class="form-field">
      <label for="gitops-branch">Branch</label>
      <input id="gitops-branch" type="text" value={config.branch} oninput={(e) => update('branch', e.currentTarget.value)} />
    </div>

    <div class="form-field">
      <label for="gitops-path">Config Path</label>
      <p class="field-desc">Directory within the repo for manifest files</p>
      <input id="gitops-path" type="text" value={config.configPath} oninput={(e) => update('configPath', e.currentTarget.value)} />
    </div>

    <div class="form-field">
      <label for="gitops-poll">Poll Interval (seconds)</label>
      <input id="gitops-poll" type="text" value={config.pollInterval} oninput={(e) => update('pollInterval', e.currentTarget.value)} />
    </div>

    <div class="toggle-row">
      <span class="field-label-inline">Auto Push Changes</span>
      <button
        type="button"
        class="toggle-switch"
        class:on={config.autoPush}
        onclick={() => update('autoPush', !config.autoPush)}
      >
        <span class="toggle-knob"></span>
      </button>
    </div>

    <div class="toggle-row">
      <span class="field-label-inline">Auto Pull Changes</span>
      <button
        type="button"
        class="toggle-switch"
        class:on={config.autoPull}
        onclick={() => update('autoPull', !config.autoPull)}
      >
        <span class="toggle-knob"></span>
      </button>
    </div>
  </section>

  {#if onCommit}
    <div class="commit-row">
      <button
        class="btn primary"
        onclick={runCommit}
        disabled={committing || (config.source === 'external'
          ? !config.repoUrl
          : !(config.mantleUrl && config.group && config.node))}
      >
        {committing ? 'Saving...' : 'Save & Start'}
      </button>
    </div>
  {/if}
</div>

<style lang="scss">
  .gitops-form {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }

  .git-missing {
    padding: 1rem;
    background: color-mix(in srgb, var(--badge-amber-border, #f59e0b) 10%, var(--theme-surface));
    border: 1px solid var(--badge-amber-border, #f59e0b);
    border-radius: var(--rounded-md);
  }

  .git-missing-content {
    display: flex;
    align-items: flex-start;
    gap: 0.75rem;
    margin-bottom: 0.75rem;
    color: var(--badge-amber-text, #f59e0b);

    strong {
      display: block;
      font-size: 0.875rem;
      color: var(--theme-text);
      margin-bottom: 0.125rem;
    }

    p {
      font-size: 0.8125rem;
      color: var(--theme-text-muted);
      margin: 0;
    }

    svg {
      flex-shrink: 0;
      margin-top: 0.125rem;
    }
  }

  .install-error {
    font-size: 0.75rem;
    color: var(--red-400, #f87171);
    margin: 0.5rem 0 0;
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

    a {
      color: var(--theme-primary);
      text-decoration: none;
      &:hover { text-decoration: underline; }
    }
  }

  .field-label {
    display: block;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    margin-bottom: 0.375rem;
  }

  .key-display {
    margin-bottom: 0.5rem;
  }

  .key-box {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    padding: 0.75rem;
    margin-bottom: 0.75rem;
    overflow-x: auto;

    code {
      font-size: 0.75rem;
      font-family: 'IBM Plex Mono', monospace;
      color: var(--theme-text);
      word-break: break-all;
      white-space: pre-wrap;
    }
  }

  .key-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    margin-bottom: 0.5rem;
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

    input[type='text'] {
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
      background: color-mix(in srgb, var(--red-500, #ef4444) 10%, transparent);
      color: var(--red-500, #ef4444);
    }
  }

  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.5rem 0;
    margin-bottom: 0.5rem;
  }

  .field-label-inline {
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-text-muted);
  }

  .toggle-switch {
    position: relative;
    width: 40px;
    height: 22px;
    border-radius: 11px;
    background: var(--theme-border);
    border: none;
    cursor: pointer;
    padding: 0;
    flex-shrink: 0;
    transition: background 0.2s;

    &.on {
      background: var(--theme-primary);
    }

    .toggle-knob {
      position: absolute;
      top: 2px;
      left: 2px;
      width: 18px;
      height: 18px;
      border-radius: 50%;
      background: white;
      transition: transform 0.2s;
    }

    &.on .toggle-knob {
      transform: translateX(18px);
    }
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
    text-decoration: none;
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

    &.small {
      font-size: 0.75rem;
      padding: 0.375rem 0.75rem;
    }

    &.copy-btn {
      position: relative;
    }
  }

  .copy-label {
    opacity: 1;
    transition: opacity 0.15s;
    &.hidden { opacity: 0; }
  }

  .copy-confirm {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    transition: opacity 0.15s;
    &.visible { opacity: 1; }
  }

  .help-text {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    margin: 0.5rem 0;
  }

  .commit-row {
    display: flex;
    justify-content: flex-end;
    padding-top: 0.5rem;
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
  }

  .source-toggle {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.5rem;
  }

  .source-option {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    text-align: left;
    gap: 0.125rem;
    padding: 0.75rem 1rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    cursor: pointer;
    color: var(--theme-text);
    font-family: inherit;
    transition: border-color 0.15s, background 0.15s;

    &:hover { border-color: color-mix(in srgb, var(--theme-primary) 40%, var(--theme-border)); }

    &.active {
      border-color: var(--theme-primary);
      background: color-mix(in srgb, var(--theme-primary) 8%, var(--theme-surface));
    }
  }

  .source-title {
    font-size: 0.875rem;
    font-weight: 600;
  }

  .source-sub {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
  }

  .mantle-id-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.75rem;
  }

  .identity-display {
    margin: 0 0 0.75rem;
    padding: 0.5rem 0.75rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
  }

  .identity-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .identity-value {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.8125rem;
    color: var(--theme-text);
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

  .section-desc code {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    padding: 0.05rem 0.3rem;
    border-radius: var(--rounded-sm, 0.25rem);
  }
</style>
