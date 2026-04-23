<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { apiPut, apiDelete } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import { slide } from 'svelte/transition';
	import type { PlcConfig, PlcTemplate, PlcTemplateField } from '$lib/types/plc';
	import { Plus, Trash, ArrowUp, ArrowDown } from '@joyautomation/salt/icons';
	import TidyTreeView, { type TidyNode } from '$lib/components/TidyTreeView.svelte';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import DirtyIcon from '$lib/components/DirtyIcon.svelte';
	import {
		workspaceTabs,
		workspaceEditorSaves,
		workspaceSelection
	} from '../workspace-state.svelte';

	type Props = {
		tabId: string;
		name: string;
		templates: PlcTemplate[];
		plcConfig: PlcConfig | null;
		isNew?: boolean;
	};

	let { tabId, name, templates, plcConfig, isNew = false }: Props = $props();

	const existing = $derived(templates.find((t) => t.name === name) ?? null);

	// For unsaved tabs we stage the eventual name separately; the tab id is
	// synthetic until the first save promotes it via renameTab.
	let pendingName = $state('');
	let description = $state('');
	let fields = $state<PlcTemplateField[]>([]);
	let selectedIdx = $state<number | null>(null);
	let saving = $state(false);
	let deleting = $state(false);
	let view = $state<'form' | 'json'>('form');
	let jsonDraft = $state('');
	let jsonError = $state<string | null>(null);

	const templateByName = $derived.by(() => {
		const m: Record<string, PlcTemplate> = {};
		for (const t of templates) m[t.name] = t;
		return m;
	});

	const affectedVariables = $derived.by(() => {
		if (!plcConfig?.variables || !existing) return [] as string[];
		const out: string[] = [];
		for (const v of Object.values(plcConfig.variables)) {
			const base = v.datatype.replace(/\[\]$/, '').replace(/\{\}$/, '');
			if (base === existing.name) out.push(v.id);
		}
		return out.sort();
	});

	function fieldZero(type: string): unknown {
		if (type.endsWith('[]')) return [];
		if (type.endsWith('{}')) return {};
		if (type === 'bool' || type === 'boolean') return false;
		if (type === 'string' || type === 'bytes') return '';
		if (type === 'number') return 0;
		return null;
	}

	function isPrimitive(t: string): boolean {
		const base = t.replace(/\[\]$/, '').replace(/\{\}$/, '');
		return base === 'number' || base === 'boolean' || base === 'bool' || base === 'string' || base === 'bytes';
	}

	function typeOptions(): string[] {
		const prims = ['number', 'boolean', 'string', 'bytes'];
		const selfName = existing?.name ?? pendingName;
		const others = templates.filter((t) => t.name !== selfName).map((t) => t.name);
		return [...prims, ...others];
	}

	// Reseed when the underlying template changes (save/remote update) or
	// when switching between types.
	let lastSeededFor = '';
	$effect(() => {
		if (isNew) {
			// Seed once; don't clobber the user's in-progress input.
			if (lastSeededFor === '__new__') return;
			lastSeededFor = '__new__';
			pendingName = name;
			description = '';
			fields = [];
			selectedIdx = null;
			return;
		}
		if (!existing) return;
		const key = `${existing.name}::${existing.updatedAt ?? 0}`;
		if (key === lastSeededFor) return;
		lastSeededFor = key;
		pendingName = existing.name;
		description = existing.description ?? '';
		fields = existing.fields.map((f) => ({ ...f }));
		selectedIdx = null;
	});

	// Build the object we'd send to the API.
	function buildBody(): PlcTemplate {
		return {
			name: (pendingName || name).trim(),
			description: description.trim() || undefined,
			tags: existing?.tags,
			fields,
			methods: existing?.methods,
			updatedBy: 'gui'
		};
	}

	// JSON view stays mirror-bound: switching to JSON serializes the form;
	// typing JSON back re-parses and updates form state on each keystroke.
	function formToJson(): string {
		const body = buildBody();
		const out: Record<string, unknown> = {
			name: body.name,
			fields: body.fields
		};
		if (body.description) out.description = body.description;
		if (body.tags && body.tags.length > 0) out.tags = body.tags;
		if (body.methods && body.methods.length > 0) out.methods = body.methods;
		return JSON.stringify(out, null, 2);
	}

	function syncFromJson(raw: string) {
		jsonDraft = raw;
		try {
			const parsed = JSON.parse(raw);
			if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
				throw new Error('Root must be an object.');
			}
			if (typeof parsed.name !== 'string') {
				throw new Error('`name` must be a string.');
			}
			if (!Array.isArray(parsed.fields)) {
				throw new Error('`fields` must be an array.');
			}
			for (const [i, f] of parsed.fields.entries()) {
				if (!f || typeof f !== 'object') {
					throw new Error(`fields[${i}] must be an object.`);
				}
				if (typeof f.name !== 'string' || typeof f.type !== 'string') {
					throw new Error(`fields[${i}] needs string \`name\` and \`type\`.`);
				}
			}
			jsonError = null;
			pendingName = parsed.name;
			description = typeof parsed.description === 'string' ? parsed.description : '';
			fields = parsed.fields.map((f: PlcTemplateField) => ({ ...f }));
		} catch (err) {
			jsonError = err instanceof Error ? err.message : String(err);
		}
	}

	function switchView(next: 'form' | 'json') {
		if (next === view) return;
		if (next === 'json') {
			jsonDraft = formToJson();
			jsonError = null;
		}
		view = next;
	}

	const isDirty = $derived.by(() => {
		if (isNew) {
			return pendingName.trim() !== '' || description !== '' || fields.length > 0;
		}
		if (!existing) return false;
		if ((pendingName ?? '') !== existing.name) return true;
		if ((description ?? '') !== (existing.description ?? '')) return true;
		return JSON.stringify(fields) !== JSON.stringify(existing.fields);
	});

	$effect(() => {
		workspaceTabs.setDirty(tabId, isDirty);
	});

	const canSave = $derived.by(() => {
		if (saving || jsonError) return false;
		const n = (pendingName || '').trim();
		if (!n) return false;
		if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(n)) return false;
		// Don't silently overwrite a different existing type.
		if (isNew && templateByName[n]) return false;
		return isDirty;
	});

	function fieldClashesName(): boolean {
		const seen = new Set<string>();
		for (const f of fields) {
			if (!f.name) return true;
			if (seen.has(f.name)) return true;
			seen.add(f.name);
		}
		return false;
	}

	onMount(() => workspaceEditorSaves.register(tabId, save));
	onDestroy(() => workspaceEditorSaves.unregister(tabId));

	async function save() {
		if (!canSave) return;
		if (fieldClashesName()) {
			saltState.addNotification({
				message: 'Field names must be unique and non-empty.',
				type: 'error'
			});
			return;
		}
		saving = true;
		try {
			const body = buildBody();
			const res = await apiPut(
				`/plcs/plc/templates/${encodeURIComponent(body.name)}`,
				body
			);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Saved type "${body.name}"`, type: 'success' });
			if (isNew) {
				workspaceTabs.renameTab(tabId, body.name);
				workspaceSelection.select('type', body.name);
			}
			workspaceTabs.clearDirty(tabId);
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function revert() {
		lastSeededFor = '';
		if (isNew) {
			pendingName = '';
			description = '';
			fields = [];
			selectedIdx = null;
		} else if (existing) {
			pendingName = existing.name;
			description = existing.description ?? '';
			fields = existing.fields.map((f) => ({ ...f }));
			selectedIdx = null;
		}
	}

	async function del() {
		if (!existing) return;
		if (affectedVariables.length > 0) {
			saltState.addNotification({
				message: `Cannot delete: ${affectedVariables.length} variable(s) still use this type.`,
				type: 'error'
			});
			return;
		}
		if (!confirm(`Delete type "${existing.name}"? This cannot be undone.`)) return;
		deleting = true;
		try {
			const res = await apiDelete(`/plcs/plc/templates/${encodeURIComponent(existing.name)}`);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Deleted type "${existing.name}"`, type: 'success' });
			workspaceTabs.close(tabId);
			await invalidateAll();
		} finally {
			deleting = false;
		}
	}

	function addField() {
		let i = fields.length + 1;
		let newName = `field${i}`;
		while (fields.some((f) => f.name === newName)) {
			i++;
			newName = `field${i}`;
		}
		fields = [...fields, { name: newName, type: 'number', default: 0 }];
		selectedIdx = fields.length - 1;
	}

	function removeField(idx: number) {
		fields = fields.filter((_, i) => i !== idx);
		if (selectedIdx === idx) selectedIdx = null;
		else if (selectedIdx !== null && idx < selectedIdx) selectedIdx = selectedIdx - 1;
	}

	function moveField(idx: number, delta: number) {
		const j = idx + delta;
		if (j < 0 || j >= fields.length) return;
		const next = [...fields];
		[next[idx], next[j]] = [next[j], next[idx]];
		fields = next;
		if (selectedIdx === idx) selectedIdx = j;
		else if (selectedIdx === j) selectedIdx = idx;
	}

	function updateField(idx: number, patch: Partial<PlcTemplateField>) {
		const next = [...fields];
		next[idx] = { ...next[idx], ...patch };
		if (patch.type !== undefined && patch.default === undefined) {
			next[idx].default = fieldZero(patch.type);
		}
		fields = next;
	}

	function onDefaultRaw(idx: number, raw: string) {
		const f = fields[idx];
		let val: unknown = raw;
		if (f.type === 'number') val = parseFloat(raw);
		else if (f.type === 'boolean' || f.type === 'bool') val = raw === 'true';
		updateField(idx, { default: val });
	}

	function defaultDisplay(f: PlcTemplateField): string {
		const d = f.default;
		if (d === undefined || d === null) return '';
		if (typeof d === 'object') return JSON.stringify(d);
		return String(d);
	}

	type SchemaNode = TidyNode & {
		typeLabel?: string;
		children?: SchemaNode[];
	};

	const tree = $derived.by<SchemaNode>(() => {
		const visit = (
			nodeName: string,
			type: string | undefined,
			path: (string | number)[],
			depth: number
		): SchemaNode[] => {
			const base = (type ?? '').replace(/\[\]$/, '').replace(/\{\}$/, '');
			const nested = templateByName[base];
			if (!nested || depth > 3) return [];
			return nested.fields.map((nf) => ({
				id: [...path, nf.name].join('/'),
				label: `${nf.name}: ${nf.type}`,
				kind: 'leaf',
				path: [...path, nf.name],
				typeLabel: nf.type,
				children: visit(nf.name, nf.type, [...path, nf.name], depth + 1)
			}));
		};
		return {
			id: '$root',
			label: pendingName || name || '(unnamed)',
			kind: 'root',
			path: [],
			children: fields.map((f, i) => {
				const nested = visit(f.name, f.type, [i], 1);
				return {
					id: `field:${i}`,
					label: `${f.name}: ${f.type}`,
					kind: nested.length > 0 ? 'branch' : 'leaf',
					path: [i],
					selectable: true,
					typeLabel: f.type,
					children: nested
				};
			})
		};
	});

	const selectedPath = $derived(selectedIdx !== null ? [selectedIdx] : null);

	function handleSelect(path: (string | number)[]) {
		const k = path[0];
		if (typeof k === 'number') selectedIdx = k;
	}
</script>

<div class="type-editor">
	<header class="te-head">
		<div class="left">
			<span class="kind-badge">Type</span>
			<span class="name">{pendingName || name || '(unnamed)'}</span>
			{#if isDirty}
				<DirtyIcon size="0.875rem" />
			{/if}
		</div>
		<div class="right">
			<div class="view-toggle" role="tablist" aria-label="Editor view">
				<button
					type="button"
					role="tab"
					class="view-btn"
					class:active={view === 'form'}
					aria-selected={view === 'form'}
					onclick={() => switchView('form')}
				>
					Form
				</button>
				<button
					type="button"
					role="tab"
					class="view-btn"
					class:active={view === 'json'}
					aria-selected={view === 'json'}
					onclick={() => switchView('json')}
				>
					JSON
				</button>
			</div>
			{#if isDirty}
				<button type="button" class="btn subtle" onclick={revert} disabled={saving}>
					Revert
				</button>
			{/if}
			<button type="button" class="btn primary" onclick={save} disabled={!canSave}>
				{saving ? 'Saving…' : 'Save'}
			</button>
			{#if !isNew && existing}
				<button
					type="button"
					class="btn danger"
					onclick={del}
					disabled={deleting || saving}
					title={affectedVariables.length > 0
						? `In use by ${affectedVariables.length} variable(s)`
						: 'Delete type'}
				>
					{deleting ? 'Deleting…' : 'Delete'}
				</button>
			{/if}
		</div>
	</header>

	{#if !isNew && !existing}
		<div class="te-body">
			<div class="status">Type "{name}" not found.</div>
		</div>
	{:else if view === 'json'}
		<div class="te-body json-body">
			<CodeEditor
				value={jsonDraft}
				language="json"
				onchange={syncFromJson}
			/>
			{#if jsonError}
				<div class="json-error" transition:slide={{ duration: 120 }}>
					<strong>Parse error:</strong> {jsonError}
				</div>
			{/if}
		</div>
	{:else}
		<div class="te-body">
			{#if isNew}
				<label class="field">
					<span>Name</span>
					<input
						type="text"
						class="input"
						bind:value={pendingName}
						placeholder="Motor"
					/>
					<span class="hint">
						Must be a valid identifier. Used as the datatype in variables.
					</span>
				</label>
			{/if}

			<label class="field">
				<span>Description</span>
				<input
					type="text"
					class="input"
					bind:value={description}
					placeholder="(optional)"
				/>
			</label>

			{#if isDirty && affectedVariables.length > 0}
				<div class="warning" transition:slide={{ duration: 150 }}>
					<strong>Heads up:</strong>
					Editing this type will affect
					{affectedVariables.length}
					{affectedVariables.length === 1 ? 'variable' : 'variables'}:
					<span class="affected">
						{#each affectedVariables as v, i (v)}
							<code>{v}</code>{i < affectedVariables.length - 1 ? ', ' : ''}
						{/each}
					</span>
				</div>
			{/if}

			<div class="section-label">Fields</div>
			<div class="tree-wrap">
				<TidyTreeView
					root={tree}
					{selectedPath}
					onSelect={handleSelect}
				>
					{#snippet content(args: { node: TidyNode; selected: boolean })}
						{@const sn = args.node as SchemaNode}
						{#if sn.kind === 'root'}
							<span class="tree-root">{sn.label}</span>
						{:else}
							<span class="schema-leaf">
								<span class="key">{sn.label.split(':')[0]}</span>:
								<em>{sn.typeLabel}</em>
							</span>
						{/if}
					{/snippet}
				</TidyTreeView>
			</div>

			<div class="tree-actions">
				<button class="btn small icon-text" onclick={addField}>
					<Plus size="0.875rem" /><span>Add field</span>
				</button>
				{#if selectedIdx !== null}
					<button
						class="btn small icon-text"
						onclick={() => moveField(selectedIdx!, -1)}
						disabled={selectedIdx === 0}
					>
						<ArrowUp size="0.875rem" /><span>Up</span>
					</button>
					<button
						class="btn small icon-text"
						onclick={() => moveField(selectedIdx!, 1)}
						disabled={selectedIdx === fields.length - 1}
					>
						<ArrowDown size="0.875rem" /><span>Down</span>
					</button>
					<button
						class="btn small danger icon-text"
						onclick={() => removeField(selectedIdx!)}
					>
						<Trash size="0.875rem" /><span>Remove</span>
					</button>
				{/if}
			</div>

			{#if selectedIdx !== null && fields[selectedIdx]}
				{@const f = fields[selectedIdx]}
				<div class="field-editor" transition:slide={{ duration: 150 }}>
					<div class="fe-grid">
						<label class="field">
							<span>Name</span>
							<input
								type="text"
								class="input"
								value={f.name}
								oninput={(e) =>
									updateField(selectedIdx!, {
										name: (e.currentTarget as HTMLInputElement).value
									})}
							/>
						</label>
						<label class="field">
							<span>Type</span>
							<input
								type="text"
								class="input"
								list="type-type-options"
								value={f.type}
								oninput={(e) =>
									updateField(selectedIdx!, {
										type: (e.currentTarget as HTMLInputElement).value
									})}
							/>
							<datalist id="type-type-options">
								{#each typeOptions() as t (t)}
									<option value={t}></option>
									<option value={`${t}[]`}></option>
									<option value={`${t}{}`}></option>
								{/each}
							</datalist>
						</label>
						<label class="field">
							<span>Default</span>
							{#if isPrimitive(f.type) && !f.type.endsWith('[]') && !f.type.endsWith('{}')}
								{#if f.type === 'boolean' || f.type === 'bool'}
									<select
										class="input"
										value={String(f.default ?? false)}
										onchange={(e) =>
											onDefaultRaw(
												selectedIdx!,
												(e.currentTarget as HTMLSelectElement).value
											)}
									>
										<option value="false">false</option>
										<option value="true">true</option>
									</select>
								{:else}
									<input
										type={f.type === 'number' ? 'number' : 'text'}
										class="input"
										value={defaultDisplay(f)}
										oninput={(e) =>
											onDefaultRaw(
												selectedIdx!,
												(e.currentTarget as HTMLInputElement).value
											)}
										step="any"
									/>
								{/if}
							{:else}
								<input
									class="input"
									value={defaultDisplay(f)}
									readonly
									title="Composite defaults live in nested instances"
								/>
							{/if}
						</label>
					</div>
					<label class="field">
						<span>Description</span>
						<input
							type="text"
							class="input"
							value={f.description ?? ''}
							oninput={(e) =>
								updateField(selectedIdx!, {
									description: (e.currentTarget as HTMLInputElement).value
								})}
							placeholder="(optional)"
						/>
					</label>
				</div>
			{:else}
				<p class="hint">Click a field in the tree to edit it, or add a new one.</p>
			{/if}
		</div>
	{/if}
</div>

<style lang="scss">
	.type-editor {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		background: var(--theme-background);
	}

	.te-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		padding: 0.375rem 0.625rem;
		border-bottom: 1px solid var(--theme-border);
		background: var(--theme-surface);
	}

	.left,
	.right {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.name {
		font-family: var(--font-mono, monospace);
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--theme-text);
	}

	.kind-badge {
		padding: 0.0625rem 0.375rem;
		font-size: 0.625rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		border-radius: 0.1875rem;
	}

	.view-toggle {
		display: inline-flex;
		border: 1px solid var(--theme-border);
		border-radius: 0.3125rem;
		overflow: hidden;
	}

	.view-btn {
		padding: 0.25rem 0.625rem;
		font-size: 0.75rem;
		background: transparent;
		color: var(--theme-text-muted);
		border: 0;
		cursor: pointer;

		&:hover {
			color: var(--theme-text);
			background: var(--theme-surface);
		}

		&.active {
			background: var(--theme-primary);
			color: var(--theme-on-primary, white);
		}

		& + & {
			border-left: 1px solid var(--theme-border);
		}
	}

	.btn {
		padding: 0.3125rem 0.75rem;
		font-size: 0.8125rem;
		font-weight: 500;
		border: 1px solid var(--theme-border);
		border-radius: 0.3125rem;
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

		&.subtle {
			color: var(--theme-text-muted);
		}

		&.danger {
			color: var(--theme-error, #e5484d);
			border-color: color-mix(in srgb, var(--theme-error, #e5484d) 40%, transparent);

			&:hover:not(:disabled) {
				background: color-mix(in srgb, var(--theme-error, #e5484d) 12%, transparent);
				border-color: var(--theme-error, #e5484d);
			}
		}

		&.small {
			padding: 0.1875rem 0.5rem;
			font-size: 0.75rem;
		}

		&.icon-text {
			display: inline-flex;
			align-items: center;
			gap: 0.3125rem;
		}

		&:disabled {
			opacity: 0.5;
			cursor: not-allowed;
		}
	}

	.te-body {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.json-body {
		padding: 0;
		gap: 0;
	}

	.json-error {
		padding: 0.5rem 0.75rem;
		font-size: 0.8125rem;
		color: var(--theme-error, #e5484d);
		background: color-mix(in srgb, var(--theme-error, #e5484d) 10%, transparent);
		border-top: 1px solid color-mix(in srgb, var(--theme-error, #e5484d) 40%, transparent);
	}

	.status {
		padding: 1rem;
		color: var(--theme-text-muted);
		font-size: 0.875rem;
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
		border-radius: 0.25rem;
		font-family: inherit;

		&:focus {
			outline: none;
			border-color: var(--theme-primary);
		}
	}

	.hint {
		margin: 0;
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		font-style: italic;
	}

	.section-label {
		font-size: 0.6875rem;
		color: var(--theme-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		font-weight: 600;
	}

	.warning {
		border: 1px solid color-mix(in srgb, var(--theme-warning, var(--theme-primary)) 40%, var(--theme-border));
		background: color-mix(in srgb, var(--theme-warning, var(--theme-primary)) 10%, transparent);
		color: var(--theme-text);
		padding: 0.5rem 0.625rem;
		border-radius: 0.3125rem;
		font-size: 0.8125rem;
		line-height: 1.4;

		strong {
			font-weight: 600;
		}

		.affected code {
			font-family: var(--font-mono, monospace);
			font-size: 0.75rem;
			padding: 0.0625rem 0.25rem;
			background: color-mix(in srgb, var(--theme-text) 8%, transparent);
			border-radius: 0.1875rem;
		}
	}

	.tree-wrap {
		overflow-x: auto;
		padding: 0.25rem 0;
		color: var(--theme-text);
	}

	.tree-root {
		font-family: var(--font-mono, monospace);
		font-weight: 600;
		color: var(--theme-text);
	}

	.schema-leaf {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;

		em {
			font-style: normal;
			color: var(--theme-text-muted);
		}

		.key {
			color: var(--theme-text);
			font-family: var(--font-mono, monospace);
		}
	}

	.tree-actions {
		display: flex;
		gap: 0.375rem;
		flex-wrap: wrap;
	}

	.field-editor {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding: 0.625rem;
		border: 1px solid var(--theme-border);
		border-radius: 0.3125rem;
		background: var(--theme-background);
	}

	.fe-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr));
		gap: 0.5rem;
	}
</style>
