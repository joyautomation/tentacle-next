<script lang="ts">
  import { api, apiPost, apiPut } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { slide } from 'svelte/transition';
  import { CheckCircle, XCircle } from '@joyautomation/salt/icons';

  type Step = 'ssh-key' | 'repository' | 'settings' | 'complete';
  const STEPS: Step[] = ['ssh-key', 'repository', 'settings'];

  let step: Step = $state('ssh-key');

  // SSH key state
  let sshKey = $state({ exists: false, publicKey: '', path: '' });
  let generatingKey = $state(false);
  let copied = $state(false);

  // Repository state
  let repoUrl = $state('');
  let testing = $state(false);
  let testResult: { success: boolean; error?: string } | null = $state(null);

  // Settings state
  let branch = $state('main');
  let configPath = $state('config');
  let pollInterval = $state('60');
  let autoPush = $state(true);
  let autoPull = $state(true);

  let saving = $state(false);

  const stepIndex = $derived(STEPS.indexOf(step));
  const canGoBack = $derived(stepIndex > 0);

  // Load SSH key status on mount
  $effect(() => {
    loadSSHKey();
  });

  async function loadSSHKey() {
    const result = await api<{ exists: boolean; publicKey: string; path: string }>('/gitops/ssh-key');
    if (result.data) {
      sshKey = result.data;
    }
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
      await navigator.clipboard.writeText(sshKey.publicKey);
      copied = true;
      setTimeout(() => { copied = false; }, 2000);
    } catch {
      saltState.addNotification({ message: 'Failed to copy to clipboard', type: 'error' });
    }
  }

  async function testConnection() {
    testing = true;
    testResult = null;
    const result = await apiPost<{ success: boolean; error?: string }>('/gitops/test-connection', { repoUrl });
    testing = false;
    if (result.data) {
      testResult = result.data;
    } else if (result.error) {
      testResult = { success: false, error: result.error.error };
    }
  }

  function goBack() {
    const idx = STEPS.indexOf(step);
    if (idx > 0) step = STEPS[idx - 1];
  }

  function goNext() {
    const idx = STEPS.indexOf(step);
    if (idx < STEPS.length - 1) step = STEPS[idx + 1];
  }

  async function saveAndStart() {
    saving = true;
    const configs: [string, string][] = [
      ['GITOPS_REPO_URL', repoUrl],
      ['GITOPS_BRANCH', branch],
      ['GITOPS_PATH', configPath],
      ['GITOPS_POLL_INTERVAL_S', pollInterval],
      ['GITOPS_AUTO_PUSH', String(autoPush)],
      ['GITOPS_AUTO_PULL', String(autoPull)],
    ];

    const errors: string[] = [];
    for (const [envVar, value] of configs) {
      const result = await apiPut(`/config/gitops/${envVar}`, { value });
      if (result.error) {
        errors.push(`${envVar}: ${result.error.error}`);
      }
    }

    saving = false;

    if (errors.length > 0) {
      saltState.addNotification({ message: errors.join('; '), type: 'error' });
    } else {
      saltState.addNotification({ message: 'GitOps configuration saved', type: 'success' });
      await invalidateAll();
    }
  }
</script>

<div class="wizard">
  <!-- Step indicators -->
  <div class="step-bar">
    {#each STEPS as s, i}
      <button
        class="step-indicator"
        class:active={step === s}
        class:done={i < stepIndex}
        onclick={() => { if (i <= stepIndex) step = s; }}
        disabled={i > stepIndex}
      >
        <span class="step-num">{i + 1}</span>
        <span class="step-label">
          {s === 'ssh-key' ? 'SSH Key' : s === 'repository' ? 'Repository' : 'Settings'}
        </span>
      </button>
      {#if i < STEPS.length - 1}
        <div class="step-line" class:done={i < stepIndex}></div>
      {/if}
    {/each}
  </div>

  <!-- Step content -->
  <div class="step-content">
    {#if step === 'ssh-key'}
      <div transition:slide={{ duration: 200 }}>
        <h3>SSH Key</h3>
        <p class="step-desc">An SSH key allows this device to authenticate with your git host (GitHub, GitLab, etc.) without a password.</p>

        {#if sshKey.exists}
          <div class="key-display">
            <p class="field-label">Public Key</p>
            <div class="key-box">
              <code>{sshKey.publicKey}</code>
            </div>
            <div class="key-actions">
              <button class="btn secondary" onclick={copyPublicKey}>
                {copied ? 'Copied!' : 'Copy Public Key'}
              </button>
              <a
                class="btn secondary"
                href="https://github.com/settings/ssh/new"
                target="_blank"
                rel="noopener"
              >
                Add to GitHub
              </a>
              <a
                class="btn secondary"
                href="https://gitlab.com/-/user_settings/ssh_keys"
                target="_blank"
                rel="noopener"
              >
                Add to GitLab
              </a>
            </div>
            <p class="help-text">Copy the public key above and add it to your git host's SSH key settings.</p>
          </div>

          <div class="step-nav">
            <button class="btn secondary" onclick={generateKey} disabled={generatingKey}>
              {generatingKey ? 'Generating...' : 'Regenerate Key'}
            </button>
            <button class="btn primary" onclick={goNext}>
              Next: Repository
            </button>
          </div>
        {:else}
          <p class="help-text">No SSH key found at <code>{sshKey.path}</code>. Generate one to get started.</p>
          <div class="step-nav">
            <button class="btn primary" onclick={generateKey} disabled={generatingKey}>
              {generatingKey ? 'Generating...' : 'Generate SSH Key'}
            </button>
          </div>
        {/if}
      </div>

    {:else if step === 'repository'}
      <div transition:slide={{ duration: 200 }}>
        <h3>Repository</h3>
        <p class="step-desc">
          Enter the SSH URL of your git repository. If you haven't created one yet,
          <a href="https://github.com/new" target="_blank" rel="noopener">create a new repository on GitHub</a>
          or your preferred git host, then paste the SSH URL below.
        </p>

        <div class="form-field">
          <label for="repo-url">Repository URL (SSH)</label>
          <input
            id="repo-url"
            type="text"
            bind:value={repoUrl}
            placeholder="git@github.com:your-org/your-device-config.git"
          />
        </div>

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

        <div class="step-nav">
          <button class="btn secondary" onclick={goBack}>Back</button>
          <button class="btn secondary" onclick={testConnection} disabled={testing || !repoUrl}>
            {testing ? 'Testing...' : 'Test Connection'}
          </button>
          <button class="btn primary" onclick={goNext} disabled={!repoUrl}>
            Next: Settings
          </button>
        </div>
      </div>

    {:else if step === 'settings'}
      <div transition:slide={{ duration: 200 }}>
        <h3>Settings</h3>
        <p class="step-desc">Configure sync behavior. The defaults work well for most setups.</p>

        <div class="form-field">
          <label for="cfg-branch">Branch</label>
          <input id="cfg-branch" type="text" bind:value={branch} />
        </div>

        <div class="form-field">
          <label for="cfg-path">Config Path</label>
          <p class="field-desc">Directory within the repo for manifest files</p>
          <input id="cfg-path" type="text" bind:value={configPath} />
        </div>

        <div class="form-field">
          <label for="cfg-poll">Poll Interval (seconds)</label>
          <input id="cfg-poll" type="text" bind:value={pollInterval} />
        </div>

        <div class="toggle-row">
          <span class="field-label-inline">Auto Push Changes</span>
          <button
            type="button"
            class="toggle-switch"
            class:on={autoPush}
            onclick={() => { autoPush = !autoPush; }}
          >
            <span class="toggle-knob"></span>
          </button>
        </div>

        <div class="toggle-row">
          <span class="field-label-inline">Auto Pull Changes</span>
          <button
            type="button"
            class="toggle-switch"
            class:on={autoPull}
            onclick={() => { autoPull = !autoPull; }}
          >
            <span class="toggle-knob"></span>
          </button>
        </div>

        <div class="step-nav">
          <button class="btn secondary" onclick={goBack}>Back</button>
          <button class="btn primary" onclick={saveAndStart} disabled={saving}>
            {saving ? 'Saving...' : 'Save & Start'}
          </button>
        </div>
      </div>
    {/if}
  </div>
</div>

<style lang="scss">
  .wizard {
    margin-top: 0.5rem;
  }

  .step-bar {
    display: flex;
    align-items: center;
    gap: 0;
    margin-bottom: 1.5rem;
  }

  .step-indicator {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    background: none;
    border: none;
    cursor: pointer;
    padding: 0.375rem 0;
    color: var(--theme-text-muted);
    font-size: 0.8125rem;
    font-family: inherit;
    transition: color 0.2s;

    &.active {
      color: var(--theme-primary);
      .step-num {
        background: var(--theme-primary);
        color: white;
      }
    }

    &.done {
      color: var(--badge-green-text);
      .step-num {
        background: var(--badge-green-bg);
        color: var(--badge-green-text);
      }
    }

    &:disabled {
      cursor: default;
      opacity: 0.5;
    }
  }

  .step-num {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.5rem;
    height: 1.5rem;
    border-radius: 50%;
    font-size: 0.75rem;
    font-weight: 600;
    background: var(--theme-border);
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .step-line {
    flex: 1;
    height: 2px;
    background: var(--theme-border);
    margin: 0 0.5rem;
    transition: background 0.2s;

    &.done {
      background: var(--badge-green-text);
    }
  }

  .step-content {
    h3 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 0.375rem;
    }
  }

  .step-desc {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 1.25rem;
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
    margin-bottom: 1.25rem;
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
    margin-bottom: 1rem;

    &.success {
      background: var(--badge-green-bg);
      color: var(--badge-green-text);
    }

    &.fail {
      background: color-mix(in srgb, var(--color-red-500, #ef4444) 10%, transparent);
      color: var(--color-red-500, #ef4444);
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

  .step-nav {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-top: 1.5rem;
    padding-top: 1rem;
    border-top: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
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
      margin-left: auto;
    }

    &.secondary {
      background: var(--theme-surface);
      color: var(--theme-text);
      border: 1px solid var(--theme-border);
    }
  }

  .help-text {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    margin: 0.5rem 0 0;

    code {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.7rem;
      background: var(--theme-surface);
      padding: 0.1rem 0.3rem;
      border-radius: 3px;
    }
  }
</style>
