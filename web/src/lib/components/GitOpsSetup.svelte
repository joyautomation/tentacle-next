<script lang="ts">
  import GitOpsConfigForm, { type GitOpsConfig } from './setup/GitOpsConfigForm.svelte';
  import { api, apiPut } from '$lib/api/client';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';

  let config = $state<GitOpsConfig>({
    repoUrl: '',
    branch: 'main',
    configPath: 'config',
    pollInterval: '60',
    autoPush: true,
    autoPull: true,
  });

  // Pre-populate form from stored env config so subsequent edits start from real values.
  $effect(() => { loadExistingConfig(); });

  async function loadExistingConfig() {
    const result = await api<Array<{ envVar: string; value: string }>>('/config/gitops');
    if (!result.data) return;
    const fieldMap: Record<string, keyof GitOpsConfig> = {
      GITOPS_REPO_URL: 'repoUrl',
      GITOPS_BRANCH: 'branch',
      GITOPS_PATH: 'configPath',
      GITOPS_POLL_INTERVAL_S: 'pollInterval',
      GITOPS_AUTO_PUSH: 'autoPush',
      GITOPS_AUTO_PULL: 'autoPull',
    };
    const next = { ...config };
    for (const entry of result.data) {
      const field = fieldMap[entry.envVar];
      if (!field) continue;
      if (field === 'autoPush' || field === 'autoPull') {
        next[field] = entry.value === 'true';
      } else {
        (next[field] as string) = entry.value;
      }
    }
    config = next;
  }

  async function commit() {
    const configs: [string, string | boolean][] = [
      ['GITOPS_REPO_URL', config.repoUrl],
      ['GITOPS_BRANCH', config.branch],
      ['GITOPS_PATH', config.configPath],
      ['GITOPS_POLL_INTERVAL_S', config.pollInterval],
      ['GITOPS_AUTO_PUSH', String(config.autoPush)],
      ['GITOPS_AUTO_PULL', String(config.autoPull)],
    ];
    const errors: string[] = [];
    for (const [envVar, value] of configs) {
      const result = await apiPut(`/config/gitops/${envVar}`, { value: String(value) });
      if (result.error) errors.push(`${envVar}: ${result.error.error}`);
    }
    // GitOps is typically already enabled (in needs_config) when this wizard renders,
    // but the PUT is idempotent so it's safe regardless.
    const enable = await apiPut('/orchestrator/desired-services/gitops', {
      version: 'latest',
      running: true,
    });
    if (enable.error) errors.push(`enable module: ${enable.error.error}`);

    if (errors.length > 0) {
      saltState.addNotification({ message: errors.join('; '), type: 'error' });
    } else {
      saltState.addNotification({ message: 'GitOps configuration saved — module starting', type: 'success' });
      await invalidateAll();
    }
  }
</script>

<GitOpsConfigForm
  {config}
  onchange={(c) => { config = c; }}
  onCommit={commit}
/>
