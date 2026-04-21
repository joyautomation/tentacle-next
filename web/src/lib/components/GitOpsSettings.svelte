<script lang="ts">
  import { api, apiPost, apiPut } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { slide } from 'svelte/transition';
  import { CheckCircle, XCircle } from '@joyautomation/salt/icons';

  type ConfigEntry = { moduleId: string; envVar: string; value: string };
  interface FieldDef {
    envVar: string;
    default?: string;
    required?: boolean;
    type: string;
    label: string;
    group: string;
    groupOrder: number;
    sortOrder: number;
    toggleable?: boolean;
    toggleLabel?: string;
    dependsOn?: string;
  }

  let {
    config = [],
    schema = [],
  }: {
    config: ConfigEntry[];
    schema: FieldDef[];
  } = $props();

  // SSH key state
  let sshKey = $state({ exists: false, publicKey: '', path: '' });
  let generatingKey = $state(false);
  let copied = $state(false);

  // Connection test state
  let testing = $state(false);
  let testResult: { success: boolean; error?: string } | null = $state(null);

  // Config form
  const configByEnvVar = $derived(
    Object.fromEntries(config.map((e) => [e.envVar, e.value]))
  );

  let formValues: Record<string, string> = $state({});
  let saving = $state(false);

  $effect(() => {
    const vals: Record<string, string> = {};
    for (const field of schema) {
      vals[field.envVar] = configByEnvVar[field.envVar] ?? field.default ?? '';
    }
    formValues = vals;
  });

  // Group fields by schema group, excluding SSH Key group (handled separately)
  type FieldGroup = { name: string; groupOrder: number; fields: FieldDef[] };
  const fieldGroups = $derived.by((): FieldGroup[] => {
    const groupMap = new Map<string, FieldGroup>();
    for (const field of schema) {
      if (field.group === 'Authentication') continue; // SSH key handled separately
      const key = field.group || 'General';
      if (!groupMap.has(key)) {
        groupMap.set(key, { name: key, groupOrder: field.groupOrder, fields: [] });
      }
      groupMap.get(key)!.fields.push(field);
    }
    const groups = [...groupMap.values()].sort((a, b) => a.groupOrder - b.groupOrder);
    for (const g of groups) {
      g.fields.sort((a, b) => a.sortOrder - b.sortOrder);
    }
    return groups;
  });

  function isFieldVisible(field: FieldDef): boolean {
    if (field.dependsOn) {
      return formValues[field.dependsOn] === 'true';
    }
    return true;
  }

  // Load SSH key on mount
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
      saltState.addNotification({ message: 'SSH key regenerated', type: 'success' });
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
    const repoUrl = formValues['GITOPS_REPO_URL'] ?? '';
    const result = await apiPost<{ success: boolean; error?: string }>('/gitops/test-connection', { repoUrl });
    testing = false;
    if (result.data) {
      testResult = result.data;
    } else if (result.error) {
      testResult = { success: false, error: result.error.error };
    }
  }

  async function handleSave() {
    saving = true;
    const errors: string[] = [];

    for (const field of schema) {
      if (field.group === 'Authentication') continue; // SSH key path not edited here
      const value = formValues[field.envVar] ?? '';
      const result = await apiPut(`/config/gitops/${field.envVar}`, { value });
      if (result.error) {
        errors.push(`${field.label}: ${result.error.error}`);
      }
    }

    saving = false;

    if (errors.length > 0) {
      saltState.addNotification({ message: errors.join('; '), type: 'error' });
    } else {
      saltState.addNotification({ message: 'Settings saved. Module will restart with new config.', type: 'success' });
      await invalidateAll();
    }
  }
</script>

<div class="gitops-settings">
  <!-- SSH Key Section -->
  <section class="settings-section">
    <h2>SSH Key</h2>
    {#if sshKey.exists}
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
        <button class="btn danger-outline" onclick={generateKey} disabled={generatingKey}>
          {generatingKey ? 'Generating...' : 'Regenerate Key'}
        </button>
      </div>
      <p class="help-text">Regenerating will invalidate the current key. You'll need to update your git host.</p>
    {:else}
      <p class="help-text">No SSH key found. Generate one to authenticate with your git host.</p>
      <button class="btn primary" onclick={generateKey} disabled={generatingKey}>
        {generatingKey ? 'Generating...' : 'Generate SSH Key'}
      </button>
    {/if}
  </section>

  <!-- Config Form -->
  <form onsubmit={(e) => { e.preventDefault(); handleSave(); }}>
    {#each fieldGroups as group}
      <section class="settings-section">
        <h2>{group.name}</h2>
        {#each group.fields as field}
          {#if isFieldVisible(field)}
            <div class="form-field" transition:slide={{ duration: 200 }}>
              {#if field.type === 'boolean'}
                <div class="toggle-row">
                  <span class="field-label">{field.label}</span>
                  <button
                    type="button"
                    class="toggle-switch"
                    class:on={formValues[field.envVar] === 'true'}
                    onclick={() => { formValues[field.envVar] = formValues[field.envVar] === 'true' ? 'false' : 'true'; }}
                  >
                    <span class="toggle-knob"></span>
                  </button>
                </div>
              {:else}
                <label class="field-label">{field.label}</label>
                <input
                  type="text"
                  value={formValues[field.envVar]}
                  oninput={(e) => { formValues[field.envVar] = (e.target as HTMLInputElement).value; }}
                />
                {#if field.envVar === 'GITOPS_REPO_URL'}
                  <div class="field-actions">
                    <button
                      type="button"
                      class="btn secondary btn-sm"
                      onclick={testConnection}
                      disabled={testing || !formValues['GITOPS_REPO_URL']}
                    >
                      {testing ? 'Testing...' : 'Test Connection'}
                    </button>
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
                {/if}
              {/if}
            </div>
          {/if}
        {/each}
      </section>
    {/each}

    <button type="submit" class="btn primary save-btn" disabled={saving}>
      {saving ? 'Saving...' : 'Save Settings'}
    </button>
  </form>
</div>

<style lang="scss">
  .gitops-settings {
    padding: 1.5rem;
    max-width: 700px;
  }

  .settings-section {
    margin-bottom: 2rem;

    h2 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 0.75rem;
    }
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
  }

  .field-label {
    display: block;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    margin-bottom: 0.25rem;
  }

  .field-actions {
    margin-top: 0.375rem;
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

  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;

    .field-label {
      margin-bottom: 0;
    }
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

  .test-result {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.625rem 0.75rem;
    border-radius: var(--rounded-md);
    font-size: 0.8125rem;
    margin-top: 0.5rem;

    &.success {
      background: var(--badge-green-bg);
      color: var(--badge-green-text);
    }

    &.fail {
      background: color-mix(in srgb, var(--red-500, #ef4444) 10%, transparent);
      color: var(--red-500, #ef4444);
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

    &.danger-outline {
      background: transparent;
      color: var(--red-500, #ef4444);
      border: 1px solid var(--red-500, #ef4444);
    }

    &.btn-sm {
      padding: 0.375rem 0.75rem;
      font-size: 0.75rem;
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

  .save-btn {
    margin-top: 0.5rem;
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
