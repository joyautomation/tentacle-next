<script lang="ts">
	import { apiPut, apiDelete } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import { slide } from 'svelte/transition';
	import type { PlcTemplate, PlcTemplateField, PlcTemplateMethod } from '$lib/types/plc';
	import { workspaceTabs } from '../workspace-state.svelte';

	type Props = {
		name: string;
		templates: PlcTemplate[];
	};

	let { name, templates }: Props = $props();

	const current = $derived(templates.find((t) => t.name === name) ?? null);
	const otherTemplateNames = $derived(templates.filter((t) => t.name !== name).map((t) => t.name));

	let description = $state('');
	let fields = $state<PlcTemplateField[]>([]);
	let methods = $state<PlcTemplateMethod[]>([]);
	let saving = $state(false);
	let deleting = $state(false);

	function fieldZero(type: string): unknown {
		if (type.endsWith('[]')) return [];
		if (type.endsWith('{}')) return {};
		if (type === 'bool' || type === 'boolean') return false;
		if (type === 'string' || type === 'bytes') return '';
		if (type === 'number') return 0;
		return null;
	}

	function cloneFields(src: PlcTemplateField[]): PlcTemplateField[] {
		return src.map((f) => ({ ...f }));
	}

	function cloneMethods(src: PlcTemplateMethod[]): PlcTemplateMethod[] {
		return src.map((m) => ({ ...m, function: { ...m.function } }));
	}

	let lastLoadedFor = '';
	$effect(() => {
		const key = `${name}::${current?.updatedAt ?? 0}`;
		if (!current || key === lastLoadedFor) return;
		lastLoadedFor = key;
		description = current.description ?? '';
		fields = cloneFields(current.fields);
		methods = cloneMethods(current.methods ?? []);
	});

	const isDirty = $derived.by(() => {
		if (!current) return false;
		if ((description ?? '') !== (current.description ?? '')) return true;
		if (JSON.stringify(fields) !== JSON.stringify(current.fields)) return true;
		if (JSON.stringify(methods) !== JSON.stringify(current.methods ?? [])) return true;
		return false;
	});

	$effect(() => {
		workspaceTabs.setDirty(name, isDirty);
	});

	function addField() {
		const base = 'field';
		let i = fields.length + 1;
		let candidate = `${base}${i}`;
		while (fields.some((f) => f.name === candidate)) {
			i++;
			candidate = `${base}${i}`;
		}
		fields = [...fields, { name: candidate, type: 'number', default: 0 }];
	}

	function removeField(idx: number) {
		fields = fields.filter((_, i) => i !== idx);
	}

	function moveField(idx: number, delta: number) {
		const next = [...fields];
		const j = idx + delta;
		if (j < 0 || j >= next.length) return;
		[next[idx], next[j]] = [next[j], next[idx]];
		fields = next;
	}

	function onTypeChange(idx: number, newType: string) {
		const f = fields[idx];
		const next = [...fields];
		next[idx] = { ...f, type: newType, default: fieldZero(newType) };
		fields = next;
	}

	function onFieldDefault(idx: number, raw: string) {
		const f = fields[idx];
		let val: unknown = raw;
		if (f.type === 'number') val = parseFloat(raw);
		else if (f.type === 'boolean' || f.type === 'bool') val = raw === 'true';
		const next = [...fields];
		next[idx] = { ...f, default: val };
		fields = next;
	}

	async function save() {
		if (!current) return;
		saving = true;
		try {
			const body: PlcTemplate = {
				name,
				description: description.trim() || undefined,
				tags: current.tags,
				fields,
				methods: methods.length > 0 ? methods : undefined,
				updatedBy: 'gui'
			};
			const res = await apiPut(`/plcs/plc/templates/${encodeURIComponent(name)}`, body);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Saved template "${name}"`, type: 'success' });
			workspaceTabs.clearDirty(name);
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function revert() {
		lastLoadedFor = '';
		fields = fields;
	}

	async function del() {
		if (!current) return;
		if (!confirm(`Delete template "${name}"? Any variable currently using it will break.`)) return;
		deleting = true;
		try {
			const res = await apiDelete(`/plcs/plc/templates/${encodeURIComponent(name)}`);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Deleted "${name}"`, type: 'success' });
			workspaceTabs.close(name);
			await invalidateAll();
		} finally {
			deleting = false;
		}
	}

	function typeOptions(): string[] {
		const prims = ['number', 'boolean', 'string', 'bytes'];
		return [...prims, ...otherTemplateNames];
	}

	function defaultInputValue(f: PlcTemplateField): string {
		const d = f.default;
		if (d === undefined || d === null) return '';
		if (typeof d === 'object') return JSON.stringify(d);
		return String(d);
	}

	function isEditableDefaultType(t: string): boolean {
		return t === 'number' || t === 'boolean' || t === 'bool' || t === 'string' || t === 'bytes';
	}
</script>

<div class="template-editor">
	{#if !current}
		<div class="empty">
			<p>Template <code>{name}</code> not found.</p>
			<p class="hint">It may have been deleted. Close this tab.</p>
		</div>
	{:else}
		<header class="te-head">
			<div class="title-row">
				<span class="badge">TEMPLATE</span>
				<span class="name">{name}</span>
				{#if isDirty}<span class="dirty-dot" title="Unsaved changes">●</span>{/if}
			</div>
			<div class="actions">
				<button class="btn" onclick={revert} disabled={!isDirty || saving}>Revert</button>
				<button class="btn primary" onclick={save} disabled={!isDirty || saving}>
					{saving ? 'Saving…' : 'Save'}
				</button>
				<button class="btn danger" onclick={del} disabled={deleting || saving}>
					{deleting ? 'Deleting…' : 'Delete'}
				</button>
			</div>
		</header>

		<div class="body">
			<label class="field">
				<span>Description</span>
				<input type="text" bind:value={description} class="input" placeholder="(optional)" />
			</label>

			<section class="section">
				<div class="section-head">
					<h4>Fields</h4>
					<button class="btn small" onclick={addField}>+ Add field</button>
				</div>
				{#if fields.length === 0}
					<p class="muted">No fields. Add one to get started.</p>
				{:else}
					<div class="fields-table">
						<div class="col-head">
							<span>Name</span>
							<span>Type</span>
							<span>Default</span>
							<span>Description</span>
							<span></span>
						</div>
						{#each fields as field, idx (idx)}
							<div class="row" transition:slide={{ duration: 120 }}>
								<input
									type="text"
									class="input"
									bind:value={field.name}
									placeholder="fieldName"
								/>
								<input
									type="text"
									class="input"
									list={`types-${idx}`}
									value={field.type}
									oninput={(e) => onTypeChange(idx, (e.currentTarget as HTMLInputElement).value)}
									placeholder="number"
								/>
								<datalist id={`types-${idx}`}>
									{#each typeOptions() as t}
										<option value={t}></option>
										<option value={`${t}[]`}></option>
										<option value={`${t}{}`}></option>
									{/each}
								</datalist>
								{#if isEditableDefaultType(field.type)}
									{#if field.type === 'boolean' || field.type === 'bool'}
										<select
											class="input"
											value={String(field.default ?? false)}
											onchange={(e) => onFieldDefault(idx, (e.currentTarget as HTMLSelectElement).value)}
										>
											<option value="false">false</option>
											<option value="true">true</option>
										</select>
									{:else}
										<input
											type={field.type === 'number' ? 'number' : 'text'}
											class="input"
											value={defaultInputValue(field)}
											oninput={(e) => onFieldDefault(idx, (e.currentTarget as HTMLInputElement).value)}
											step="any"
										/>
									{/if}
								{:else}
									<input class="input" value={defaultInputValue(field)} readonly title="Set in Starlark" />
								{/if}
								<input
									type="text"
									class="input"
									bind:value={field.description}
									placeholder="(optional)"
								/>
								<div class="row-actions">
									<button class="btn icon" onclick={() => moveField(idx, -1)} disabled={idx === 0} title="Move up">↑</button>
									<button class="btn icon" onclick={() => moveField(idx, 1)} disabled={idx === fields.length - 1} title="Move down">↓</button>
									<button class="btn icon danger" onclick={() => removeField(idx)} title="Remove">×</button>
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</section>

			{#if methods.length > 0}
				<section class="section">
					<div class="section-head">
						<h4>Methods</h4>
						<span class="muted small">(readonly for now)</span>
					</div>
					<div class="methods-list">
						{#each methods as m (m.name)}
							<div class="method-row">
								<span class="method-name">{m.name}</span>
								<span class="method-fn">{m.function.module}.{m.function.name}</span>
							</div>
						{/each}
					</div>
				</section>
			{/if}
		</div>
	{/if}
</div>

<style lang="scss">
	.template-editor {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		overflow: auto;
		background: var(--theme-background);
	}

	.te-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 0.625rem 1rem;
		border-bottom: 1px solid var(--theme-border);
		background: var(--theme-surface);
		position: sticky;
		top: 0;
		z-index: 1;
	}

	.title-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.badge {
		padding: 0.0625rem 0.375rem;
		font-size: 0.625rem;
		font-weight: 600;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		border-radius: 0.1875rem;
		letter-spacing: 0.04em;
	}

	.name {
		font-family: var(--font-mono, monospace);
		font-size: 0.9375rem;
		font-weight: 600;
		color: var(--theme-text);
	}

	.dirty-dot {
		color: var(--theme-warning, var(--theme-primary));
		font-size: 0.75rem;
	}

	.actions {
		display: flex;
		gap: 0.375rem;
	}

	.btn {
		padding: 0.3125rem 0.75rem;
		font-size: 0.8125rem;
		border-radius: 0.3125rem;
		border: 1px solid var(--theme-border);
		background: transparent;
		color: var(--theme-text);
		cursor: pointer;

		&:hover:not(:disabled) {
			border-color: var(--theme-text-muted);
		}

		&.primary {
			background: var(--theme-primary);
			color: var(--theme-on-primary, white);
			border-color: var(--theme-primary);

			&:hover:not(:disabled) {
				opacity: 0.9;
			}
		}

		&.danger {
			color: var(--theme-error, var(--theme-primary));
			border-color: color-mix(in srgb, var(--theme-error, var(--theme-primary)) 40%, var(--theme-border));

			&:hover:not(:disabled) {
				background: color-mix(in srgb, var(--theme-error, var(--theme-primary)) 10%, transparent);
			}
		}

		&.small {
			padding: 0.1875rem 0.5rem;
			font-size: 0.75rem;
		}

		&.icon {
			padding: 0.1875rem 0.375rem;
			font-size: 0.75rem;
			min-width: 1.5rem;
		}

		&:disabled {
			opacity: 0.5;
			cursor: not-allowed;
		}
	}

	.body {
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 0.3125rem;
		font-size: 0.8125rem;
		color: var(--theme-text);

		> span {
			font-size: 0.75rem;
			color: var(--theme-text-muted);
			font-weight: 500;
		}
	}

	.input {
		padding: 0.375rem 0.5rem;
		font-size: 0.8125rem;
		background: var(--theme-background);
		color: var(--theme-text);
		border: 1px solid var(--theme-border);
		border-radius: 0.3125rem;
		width: 100%;

		&:focus {
			outline: none;
			border-color: var(--theme-primary);
		}
	}

	.section {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.section-head {
		display: flex;
		align-items: baseline;
		gap: 0.625rem;

		h4 {
			margin: 0;
			font-size: 0.8125rem;
			font-weight: 600;
			text-transform: uppercase;
			letter-spacing: 0.05em;
			color: var(--theme-text-muted);
		}
	}

	.muted {
		color: var(--theme-text-muted);
		font-size: 0.8125rem;

		&.small {
			font-size: 0.6875rem;
		}
	}

	.fields-table {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
	}

	.col-head,
	.row {
		display: grid;
		grid-template-columns: 1fr 1fr 1fr 1.5fr auto;
		gap: 0.375rem;
		align-items: center;
	}

	.col-head {
		font-size: 0.6875rem;
		color: var(--theme-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.04em;
		padding: 0 0.25rem;
	}

	.row-actions {
		display: flex;
		gap: 0.1875rem;
	}

	.methods-list {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	.method-row {
		display: flex;
		gap: 0.75rem;
		padding: 0.375rem 0.5rem;
		background: color-mix(in srgb, var(--theme-surface) 60%, transparent);
		border: 1px solid var(--theme-border);
		border-radius: 0.25rem;
		font-family: var(--font-mono, monospace);
		font-size: 0.8125rem;

		.method-name {
			color: var(--theme-text);
			font-weight: 600;
		}

		.method-fn {
			color: var(--theme-text-muted);
		}
	}

	.empty {
		padding: 2rem;
		color: var(--theme-text-muted);

		code {
			font-family: var(--font-mono, monospace);
			color: var(--theme-text);
		}

		.hint {
			font-size: 0.8125rem;
			margin-top: 0.5rem;
		}
	}
</style>
