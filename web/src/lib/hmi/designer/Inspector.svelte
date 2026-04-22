<script lang="ts">
  import type { HmiWidget, HmiBinding, HmiUdtMember, HmiComponentConfig } from '$lib/types/hmi';
  import { schemaByType, childLayoutFields } from '../widgetSchema';
  import VariablePicker from './VariablePicker.svelte';
  import UdtMemberPicker from './UdtMemberPicker.svelte';

  interface Props {
    widget: HmiWidget | null;
    onChange: (widget: HmiWidget) => void;
    onDelete: () => void;
    /** When set, binding pickers offer UDT members instead of variables.
     * Used by the component editor. */
    udtMembers?: HmiUdtMember[];
    /** Available components for the `component` field type. */
    components?: HmiComponentConfig[];
    /** True when the selected widget lives inside a container (its position is
     * determined by flex flow, not x/y). Hides Geometry; shows Layout. */
    parentIsContainer?: boolean;
  }

  let { widget, onChange, onDelete, udtMembers, components, parentIsContainer = false }: Props = $props();

  const schema = $derived(widget ? schemaByType[widget.type] : null);
  const udtMode = $derived(!!udtMembers);

  let varPickerOpen = $state(false);
  let memberPickerOpen = $state(false);
  let pickerSlot = $state<string | null>(null);

  function setProp(key: string, value: unknown) {
    if (!widget) return;
    onChange({ ...widget, props: { ...(widget.props ?? {}), [key]: value } });
  }

  function setGeom(key: 'x' | 'y' | 'w' | 'h', value: number) {
    if (!widget) return;
    onChange({ ...widget, [key]: Math.max(key === 'w' ? 40 : key === 'h' ? 24 : 0, value) });
  }

  function openPicker(slot: string) {
    pickerSlot = slot;
    if (udtMode) memberPickerOpen = true;
    else varPickerOpen = true;
  }

  function onPickVariable(gateway: string, variable: string) {
    if (!widget || !pickerSlot) return;
    const binding: HmiBinding = { kind: 'variable', gateway, variable };
    onChange({ ...widget, bindings: { ...(widget.bindings ?? {}), [pickerSlot]: binding } });
    closePicker();
  }

  function onPickMember(member: string) {
    if (!widget || !pickerSlot) return;
    const binding: HmiBinding = { kind: 'udtMember', member };
    onChange({ ...widget, bindings: { ...(widget.bindings ?? {}), [pickerSlot]: binding } });
    closePicker();
  }

  function closePicker() {
    varPickerOpen = false;
    memberPickerOpen = false;
    pickerSlot = null;
  }

  function clearBinding(slot: string) {
    if (!widget) return;
    const next = { ...(widget.bindings ?? {}) };
    delete next[slot];
    onChange({ ...widget, bindings: next });
  }

  function bindingLabel(b?: HmiBinding): string {
    if (!b) return 'Not bound';
    if (b.kind === 'variable') return `${b.gateway ?? '?'} / ${b.variable ?? '?'}`;
    if (b.kind === 'udtMember') {
      if (b.gateway) return `${b.gateway}/${b.udtVariable ?? '?'}.${b.member ?? '?'}`;
      return `member: ${b.member ?? '?'}`;
    }
    return 'Not bound';
  }
</script>

<aside class="inspector">
  {#if !widget || !schema}
    <p class="muted">Select a widget to edit.</p>
  {:else}
    <header class="header">
      <div>
        <div class="kind">{schema.label}</div>
        <div class="id">{widget.id}</div>
      </div>
      <button class="del" onclick={onDelete} title="Delete widget">Delete</button>
    </header>

    {#if parentIsContainer}
      <section class="section">
        <h4>Layout (in container)</h4>
        {#each childLayoutFields as field (field.key)}
          {@const value = (widget.props ?? {})[field.key]}
          <label class="field">
            <span>{field.label}</span>
            {#if field.type === 'select'}
              <select value={value as string ?? ''} onchange={(e) => setProp(field.key, e.currentTarget.value)}>
                {#each field.options ?? [] as opt}
                  <option value={opt}>{opt || '(default)'}</option>
                {/each}
              </select>
            {:else if field.type === 'number'}
              <input
                type="number"
                step={field.step ?? 'any'}
                value={value as number ?? ''}
                oninput={(e) => {
                  const v = e.currentTarget.value;
                  setProp(field.key, v === '' ? undefined : Number(v));
                }}
              />
            {:else}
              <input
                type="text"
                placeholder={field.placeholder ?? ''}
                value={value as string ?? ''}
                oninput={(e) => setProp(field.key, e.currentTarget.value || undefined)}
              />
            {/if}
          </label>
        {/each}
        <div class="hint">
          Intrinsic size W={widget.w} H={widget.h}. Used as fallback when basis/grow isn't set.
        </div>
        <div class="grid">
          <label>W<input type="number" value={widget.w} oninput={(e) => setGeom('w', Number(e.currentTarget.value))} /></label>
          <label>H<input type="number" value={widget.h} oninput={(e) => setGeom('h', Number(e.currentTarget.value))} /></label>
        </div>
      </section>
    {:else}
      <section class="section">
        <h4>Geometry</h4>
        <div class="grid">
          <label>X<input type="number" value={widget.x} oninput={(e) => setGeom('x', Number(e.currentTarget.value))} /></label>
          <label>Y<input type="number" value={widget.y} oninput={(e) => setGeom('y', Number(e.currentTarget.value))} /></label>
          <label>W<input type="number" value={widget.w} oninput={(e) => setGeom('w', Number(e.currentTarget.value))} /></label>
          <label>H<input type="number" value={widget.h} oninput={(e) => setGeom('h', Number(e.currentTarget.value))} /></label>
        </div>
      </section>
    {/if}

    {#if schema.propFields.length > 0}
      <section class="section">
        <h4>Properties</h4>
        {#each schema.propFields as field (field.key)}
          {@const value = (widget.props ?? {})[field.key]}
          <label class="field">
            <span>{field.label}</span>
            {#if field.type === 'select'}
              <select value={value as string ?? ''} onchange={(e) => setProp(field.key, e.currentTarget.value)}>
                {#each field.options ?? [] as opt}
                  <option value={opt}>{opt}</option>
                {/each}
              </select>
            {:else if field.type === 'number'}
              <input
                type="number"
                step={field.step ?? 'any'}
                value={value as number ?? 0}
                oninput={(e) => setProp(field.key, Number(e.currentTarget.value))}
              />
            {:else if field.type === 'component'}
              <select value={value as string ?? ''} onchange={(e) => setProp(field.key, e.currentTarget.value)}>
                <option value="">— pick a component —</option>
                {#each components ?? [] as c (c.componentId)}
                  <option value={c.componentId}>{c.name} ({c.componentId}{c.udtTemplate ? ` · ${c.udtTemplate}` : ''})</option>
                {/each}
              </select>
            {:else}
              <input
                type="text"
                placeholder={field.placeholder ?? ''}
                value={value as string ?? ''}
                oninput={(e) => setProp(field.key, e.currentTarget.value)}
              />
            {/if}
          </label>
        {/each}
      </section>
    {/if}

    {#if schema.bindingSlots.length > 0}
      <section class="section">
        <h4>Bindings</h4>
        {#each schema.bindingSlots as slot (slot.key)}
          {@const b = (widget.bindings ?? {})[slot.key]}
          <div class="binding">
            <div class="binding-label">{slot.label}</div>
            <div class="binding-value" class:bound={!!b}>{bindingLabel(b)}</div>
            <div class="binding-actions">
              <button onclick={() => openPicker(slot.key)}>{b ? 'Change' : 'Bind'}</button>
              {#if b}<button class="ghost" onclick={() => clearBinding(slot.key)}>Clear</button>{/if}
            </div>
          </div>
        {/each}
      </section>
    {/if}
  {/if}
</aside>

<VariablePicker open={varPickerOpen} onClose={closePicker} onPick={onPickVariable} />
<UdtMemberPicker open={memberPickerOpen} members={udtMembers ?? []} onClose={closePicker} onPick={onPickMember} />

<style lang="scss">
  .inspector {
    width: 18rem;
    flex-shrink: 0;
    padding: 1rem;
    border-left: 1px solid var(--theme-border);
    background: var(--theme-surface);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  .muted { color: var(--theme-text-muted); margin: 0; font-size: 0.875rem; }
  .hint {
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    margin: 0.5rem 0;
    line-height: 1.4;
  }
  .header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 0.5rem;
  }
  .kind { color: var(--theme-text); font-size: 0.875rem; font-weight: 600; }
  .id { color: var(--theme-text-muted); font-family: 'IBM Plex Mono', monospace; font-size: 0.75rem; }
  .del {
    background: transparent;
    border: 1px solid var(--theme-border);
    color: var(--theme-text-muted);
    padding: 0.25rem 0.5rem;
    border-radius: var(--rounded-sm, 4px);
    font-size: 0.75rem;
    cursor: pointer;
    &:hover { color: #ef4444; border-color: #ef4444; }
  }
  .section h4 {
    margin: 0 0 0.5rem;
    font-size: 0.6875rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--theme-text-muted);
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.5rem;
    label { display: flex; flex-direction: column; gap: 0.125rem; font-size: 0.75rem; color: var(--theme-text-muted); }
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    margin-bottom: 0.5rem;
  }
  input, select {
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    color: var(--theme-text);
    padding: 0.375rem 0.5rem;
    font-family: inherit;
    font-size: 0.8125rem;
    &:focus { outline: none; border-color: var(--theme-text); }
  }
  .binding {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0.5rem;
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    margin-bottom: 0.5rem;
  }
  .binding-label { font-size: 0.75rem; color: var(--theme-text-muted); }
  .binding-value {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    &.bound { color: var(--theme-text); }
  }
  .binding-actions {
    display: flex;
    gap: 0.25rem;
    button {
      background: var(--theme-surface);
      border: 1px solid var(--theme-border);
      color: var(--theme-text);
      padding: 0.25rem 0.5rem;
      border-radius: var(--rounded-sm, 4px);
      font-size: 0.75rem;
      cursor: pointer;
      &:hover { border-color: var(--theme-text); }
      &.ghost { color: var(--theme-text-muted); }
    }
  }
</style>
