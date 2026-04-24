<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { listHmiUdts, listHmiApps, createHmiComponent } from '$lib/api/hmi';
  import type { HmiAppConfig, HmiUdtTemplate } from '$lib/types/hmi';

  let udts = $state<HmiUdtTemplate[]>([]);
  let apps = $state<HmiAppConfig[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let expanded = $state<Record<string, boolean>>({});
  /** Which UDT a create-component dialog is open for. */
  let creatingFor = $state<string | null>(null);
  let targetAppId = $state<string>('');
  let componentName = $state<string>('');
  let busy = $state(false);

  async function refresh() {
    loading = true;
    error = null;
    const [u, a] = await Promise.all([listHmiUdts(), listHmiApps()]);
    if (u.error) error = u.error.error;
    else udts = u.data ?? [];
    if (a.data) apps = a.data;
    if (!targetAppId && apps.length > 0) targetAppId = apps[0].appId;
    loading = false;
  }

  function toggle(name: string) {
    expanded[name] = !expanded[name];
  }

  function openCreate(udtName: string) {
    creatingFor = udtName;
    componentName = udtName;
    if (!targetAppId && apps.length > 0) targetAppId = apps[0].appId;
  }

  function closeCreate() {
    creatingFor = null;
    componentName = '';
  }

  async function handleCreate() {
    if (!creatingFor || !targetAppId || !componentName.trim()) return;
    busy = true;
    const r = await createHmiComponent(targetAppId, {
      name: componentName.trim(),
      udtTemplate: creatingFor,
    });
    busy = false;
    if (r.error) {
      error = r.error.error;
      return;
    }
    const appId = targetAppId;
    closeCreate();
    // Jump to the designer — component editor is phase 2.
    goto(`/hmi/designer/${encodeURIComponent(appId)}`);
  }

  onMount(refresh);
</script>

<svelte:head>
  <title>UDT Browser · HMI</title>
</svelte:head>

<section class="page">
  <p class="subtitle">
    User-Defined Types declared across all gateways. Build a reusable HMI
    component for any UDT type — one definition renders per instance.
  </p>

  {#if error}
    <div class="banner error">{error}</div>
  {/if}

  {#if loading}
    <p class="muted">Loading…</p>
  {:else if udts.length === 0}
    <div class="empty">
      <p>No UDTs are defined yet.</p>
      <p class="muted">Define UDT templates and instances in a gateway's config, then they'll appear here.</p>
    </div>
  {:else}
    <ul class="udt-list">
      {#each udts as udt (udt.name)}
        {@const isOpen = expanded[udt.name]}
        <li class="udt-card">
          <button class="udt-header" onclick={() => toggle(udt.name)}>
            <div>
              <span class="udt-name">{udt.name}</span>
              {#if udt.version}<span class="udt-version">v{udt.version}</span>{/if}
            </div>
            <div class="udt-counts">
              <span>{udt.members.length} members</span>
              <span>·</span>
              <span>{udt.instances.length} instances</span>
              <span>·</span>
              <span>{udt.gateways.length} gateway{udt.gateways.length === 1 ? '' : 's'}</span>
              <span class="chevron" class:open={isOpen}>▸</span>
            </div>
          </button>
          {#if isOpen}
            <div class="udt-body">
              <div class="columns">
                <div class="col">
                  <h3>Members</h3>
                  <ul class="members">
                    {#each udt.members as m}
                      <li>
                        <span class="m-name">{m.name}</span>
                        <span class="m-type">{m.templateRef ? `@${m.templateRef}` : m.datatype}</span>
                      </li>
                    {/each}
                  </ul>
                </div>
                <div class="col">
                  <h3>Instances</h3>
                  {#if udt.instances.length === 0}
                    <p class="muted small">No gateway has created an instance of this UDT yet.</p>
                  {:else}
                    <ul class="instances">
                      {#each udt.instances as i}
                        <li>
                          <span class="i-tag">{i.tag}</span>
                          <span class="muted small">@ {i.gatewayId} / {i.deviceId}</span>
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </div>
              </div>
              <div class="actions">
                <button class="action-btn" onclick={() => openCreate(udt.name)} disabled={apps.length === 0}>
                  Create component for this UDT
                </button>
                {#if apps.length === 0}
                  <span class="muted small">Create an HMI app first.</span>
                {/if}
              </div>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

{#if creatingFor}
  <div class="dialog-backdrop" role="button" tabindex="-1" onclick={closeCreate} onkeydown={(e) => e.key === 'Escape' && closeCreate()}>
    <div class="dialog" role="dialog" aria-modal="true" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
      <h2>Create component for <code>{creatingFor}</code></h2>
      <label>
        <span>In app</span>
        <select bind:value={targetAppId}>
          {#each apps as app (app.appId)}
            <option value={app.appId}>{app.name} ({app.appId})</option>
          {/each}
        </select>
      </label>
      <label>
        <span>Component name</span>
        <input type="text" bind:value={componentName} placeholder="e.g. PumpCard" />
      </label>
      <div class="dialog-actions">
        <button onclick={closeCreate} disabled={busy}>Cancel</button>
        <button class="primary" onclick={handleCreate} disabled={busy || !targetAppId || !componentName.trim()}>
          {busy ? 'Creating…' : 'Create'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style lang="scss">
  .page { max-width: 64rem; margin: 0 auto; padding: 2rem 1.5rem; }
  .subtitle { margin: 0 0 1.5rem; max-width: 42rem; color: var(--theme-text-muted); font-size: 0.9375rem; }
  .banner.error {
    margin-bottom: 1rem;
    padding: 0.75rem 1rem;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.4);
    border-radius: var(--rounded-md);
    color: #ef4444;
  }
  .empty {
    padding: 3rem 1rem;
    text-align: center;
    border: 1px dashed var(--theme-border);
    border-radius: var(--rounded-lg);
  }
  .udt-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.75rem; }
  .udt-card {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
  }
  .udt-header {
    width: 100%;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.875rem 1.125rem;
    background: transparent;
    border: none;
    color: var(--theme-text);
    font-family: inherit;
    font-size: inherit;
    cursor: pointer;
    text-align: left;
    &:hover { background: var(--theme-background); }
  }
  .udt-name { font-weight: 600; font-size: 1.0625rem; font-family: 'IBM Plex Mono', monospace; }
  .udt-version { margin-left: 0.5rem; color: var(--theme-text-muted); font-size: 0.8125rem; }
  .udt-counts { display: flex; gap: 0.5rem; color: var(--theme-text-muted); font-size: 0.875rem; align-items: center; }
  .chevron { margin-left: 0.5rem; transition: transform 0.15s; }
  .chevron.open { transform: rotate(90deg); }
  .udt-body { padding: 0 1.125rem 1rem; border-top: 1px solid var(--theme-border); }
  .columns { display: grid; grid-template-columns: 1fr 1fr; gap: 1.5rem; margin: 1rem 0; }
  @media (max-width: 640px) { .columns { grid-template-columns: 1fr; } }
  .col h3 { margin: 0 0 0.5rem; font-size: 0.875rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--theme-text-muted); }
  .members, .instances {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .members li, .instances li {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    padding: 0.375rem 0.5rem;
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.8125rem;
  }
  .m-type, .i-tag { color: var(--theme-text-muted); }
  .i-tag { color: var(--theme-text); }
  .actions { display: flex; align-items: center; gap: 0.75rem; padding-top: 0.5rem; }
  .action-btn {
    padding: 0.5rem 1rem;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    background: var(--theme-text);
    color: var(--theme-background);
    cursor: pointer;
    font-family: inherit;
    font-weight: 600;
    &:disabled { opacity: 0.5; cursor: not-allowed; }
  }
  .muted { color: var(--theme-text-muted); }
  .small { font-size: 0.8125rem; }

  .dialog-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }
  .dialog {
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1.25rem 1.5rem;
    width: min(28rem, calc(100% - 2rem));
    display: flex;
    flex-direction: column;
    gap: 1rem;
    h2 { margin: 0; font-size: 1.125rem; color: var(--theme-text); code { font-family: 'IBM Plex Mono', monospace; color: var(--theme-text); } }
    label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.875rem; color: var(--theme-text-muted); }
    input, select {
      padding: 0.5rem 0.75rem;
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-md);
      background: var(--theme-surface);
      color: var(--theme-text);
      font-family: inherit;
    }
    .dialog-actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
      button {
        padding: 0.5rem 1rem;
        border: 1px solid var(--theme-border);
        border-radius: var(--rounded-md);
        background: var(--theme-surface);
        color: var(--theme-text);
        cursor: pointer;
        font-family: inherit;
        &:disabled { opacity: 0.5; cursor: not-allowed; }
        &.primary { background: var(--theme-text); color: var(--theme-background); }
      }
    }
  }
</style>
