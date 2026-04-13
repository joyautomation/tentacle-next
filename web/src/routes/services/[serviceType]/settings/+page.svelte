<script lang="ts">
  import type { PageData } from './$types';
  import type { FieldDef } from './+page';
  import { page } from '$app/stores';
  import { invalidateAll } from '$app/navigation';
  import { state as saltState } from '@joyautomation/salt';
  import { apiPut } from '$lib/api/client';
  import { slide } from 'svelte/transition';
  import GitOpsSettings from '$lib/components/GitOpsSettings.svelte';

  // Svelte action: auto-resize textarea to fit content on mount
  function autoResize(node: HTMLTextAreaElement) {
    node.style.height = 'auto';
    node.style.height = node.scrollHeight + 'px';
  }

  let { data }: { data: PageData } = $props();

  const serviceType = $derived($page.params.serviceType ?? '');

  type ConfigEntry = { moduleId: string; envVar: string; value: string };

  const schema = $derived((data?.schema ?? []) as FieldDef[]);
  const configEntries = $derived((data?.config ?? []) as ConfigEntry[]);

  // Build lookup from envVar → current value
  const configByEnvVar = $derived(
    Object.fromEntries(configEntries.map(e => [e.envVar, e.value]))
  );

  // Local form values (editable)
  let formValues: Record<string, string> = $state({});

  // Sync from server data on load
  $effect(() => {
    const vals: Record<string, string> = {};
    for (const field of schema) {
      vals[field.envVar] = configByEnvVar[field.envVar] ?? field.default ?? '';
    }
    formValues = vals;
  });

  // Group fields by schema group metadata
  type FieldGroup = { name: string; groupOrder: number; fields: FieldDef[] };
  const fieldGroups = $derived.by((): FieldGroup[] => {
    const groupMap = new Map<string, FieldGroup>();
    for (const field of schema) {
      const key = field.group || 'General';
      if (!groupMap.has(key)) {
        groupMap.set(key, { name: key, groupOrder: field.groupOrder, fields: [] });
      }
      groupMap.get(key)!.fields.push(field);
    }
    // Sort groups by groupOrder, fields by sortOrder
    const groups = [...groupMap.values()].sort((a, b) => a.groupOrder - b.groupOrder);
    for (const g of groups) {
      g.fields.sort((a, b) => a.sortOrder - b.sortOrder);
    }
    return groups;
  });

  // Determine if a field is visible (supports negated dependsOn with "!" prefix)
  function isFieldVisible(field: FieldDef): boolean {
    if (field.dependsOn) {
      if (field.dependsOn.startsWith('!')) {
        return formValues[field.dependsOn.slice(1)] !== 'true';
      }
      return formValues[field.dependsOn] === 'true';
    }
    return true;
  }

  // Check if any field in a group is visible
  function isGroupVisible(group: FieldGroup): boolean {
    return group.fields.some(isFieldVisible);
  }

  // When a textarea field's dependsOn becomes true and the value is empty,
  // seed it from related simple-mode fields (e.g. generate a Caddyfile).
  function seedTextareaDefaults() {
    for (const field of schema) {
      if (field.type === 'textarea' && isFieldVisible(field) && !formValues[field.envVar]) {
        // Build a default from sibling fields that are now hidden
        const hiddenFields = schema.filter(f => f.type !== 'boolean' && f.type !== 'textarea' && !isFieldVisible(f));
        if (hiddenFields.length > 0 && field.envVar === 'CADDY_CADDYFILE') {
          const domain = formValues['CADDY_DOMAIN'] || ':80';
          const port = formValues['CADDY_UPSTREAM_PORT'] || '4000';
          formValues[field.envVar] = `${domain} {\n\treverse_proxy localhost:${port}\n}\n`;
        }
      }
    }
  }

  // Track toggleable field enabled state separately — value may be empty while toggle is on
  let toggleStates: Record<string, boolean> = $state({});

  // Initialize toggle states from existing config values
  $effect(() => {
    const states: Record<string, boolean> = {};
    for (const field of schema) {
      if (field.toggleable) {
        states[field.envVar] = !!configByEnvVar[field.envVar];
      }
    }
    toggleStates = states;
  });

  function isToggleOn(field: FieldDef): boolean {
    return toggleStates[field.envVar] ?? false;
  }

  function handleToggle(field: FieldDef, on: boolean) {
    toggleStates[field.envVar] = on;
    if (!on) {
      formValues[field.envVar] = '';
    }
  }

  let saving = $state(false);

  async function handleSave() {
    saving = true;
    const errors: string[] = [];

    for (const field of schema) {
      const value = formValues[field.envVar] ?? '';
      const result = await apiPut(`/config/${serviceType}/${field.envVar}`, { value });
      if (result.error) {
        errors.push(`${field.label}: ${result.error.error}`);
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

{#if serviceType === 'gitops'}
  <GitOpsSettings config={configEntries} schema={schema} />
{:else}
  <div class="settings-page">
    {#if data.error}
      <div class="error-box">
        <p>{data.error}</p>
      </div>
    {/if}

    {#if fieldGroups.length > 0}
      <form onsubmit={(e) => { e.preventDefault(); handleSave(); }}>
        {#each fieldGroups as group}
          {#if isGroupVisible(group)}
          <section class="form-group" transition:slide={{ duration: 200 }}>
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
                        onclick={() => { formValues[field.envVar] = formValues[field.envVar] === 'true' ? 'false' : 'true'; seedTextareaDefaults(); }}
                      >
                        <span class="toggle-knob"></span>
                      </button>
                    </div>
                  {:else if field.type === 'textarea'}
                    <label class="field-label">{field.label}</label>
                    <textarea
                      class="auto-resize"
                      value={formValues[field.envVar]}
                      oninput={(e) => { const ta = e.target as HTMLTextAreaElement; formValues[field.envVar] = ta.value; ta.style.height = 'auto'; ta.style.height = ta.scrollHeight + 'px'; }}
                      use:autoResize
                    ></textarea>
                  {:else if field.toggleable}
                    <div class="toggle-row">
                      <span class="field-label">{field.toggleLabel ?? field.label}</span>
                      <button
                        type="button"
                        class="toggle-switch"
                        class:on={isToggleOn(field)}
                        onclick={() => handleToggle(field, !isToggleOn(field))}
                      >
                        <span class="toggle-knob"></span>
                      </button>
                    </div>
                    {#if isToggleOn(field)}
                      <div class="toggle-body" transition:slide={{ duration: 200 }}>
                        <label class="field-label">{field.label}</label>
                        <input
                          type={field.type === 'password' ? 'password' : 'text'}
                          value={formValues[field.envVar]}
                          oninput={(e) => { formValues[field.envVar] = (e.target as HTMLInputElement).value; }}
                        />
                      </div>
                    {/if}
                  {:else}
                    <label class="field-label">{field.label}</label>
                    <input
                      type={field.type === 'password' ? 'password' : 'text'}
                      value={formValues[field.envVar]}
                      oninput={(e) => { formValues[field.envVar] = (e.target as HTMLInputElement).value; }}
                    />
                  {/if}
                </div>
              {/if}
            {/each}
          </section>
          {/if}
        {/each}

        <button type="submit" class="save-btn" disabled={saving}>
          {saving ? 'Saving...' : 'Save All'}
        </button>
      </form>
    {:else if !data.error}
      <div class="empty-state">
        <p>No configuration found. Start the service to populate config from environment variables.</p>
      </div>
    {/if}
  </div>
{/if}

<style lang="scss">
  .settings-page {
    padding: 1.5rem;
    max-width: 700px;
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

  textarea,
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

  textarea.auto-resize {
    resize: vertical;
    line-height: 1.5;
    min-height: 4rem;
    overflow: hidden;
  }

  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    cursor: pointer;

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

  .toggle-body {
    margin-top: 0.5rem;
    padding-left: 0.25rem;
  }

  .save-btn {
    margin-top: 1rem;
    padding: 0.5rem 1.25rem;
    font-size: 0.875rem;
    font-weight: 600;
    color: white;
    background: var(--theme-primary);
    border: none;
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover:not(:disabled) {
      background: var(--theme-primary-hover);
    }

    &:disabled {
      opacity: 0.6;
      cursor: not-allowed;
    }
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
