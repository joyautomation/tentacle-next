<script lang="ts">
	import { onMount } from 'svelte';
	import { apiPut, apiDelete } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import { slide } from 'svelte/transition';
	import type {
		PlcConfig,
		PlcTemplate,
		PlcVariableConfig,
		PlcVariableSource
	} from '$lib/types/plc';
	import { workspaceTabs } from '../workspace-state.svelte';
	import { startLiveValues, liveValuesVersion, getLiveValue } from '../live-values.svelte';
	import TemplateDefinitionEditor from './TemplateDefinitionEditor.svelte';
	import ValueTree from '$lib/components/ValueTree.svelte';
	import { PencilSquare } from '@joyautomation/salt/icons';

	type Props = {
		name: string;
		plcConfig: PlcConfig | null;
		templates: PlcTemplate[];
	};

	let { name, plcConfig, templates }: Props = $props();

	const current = $derived(plcConfig?.variables?.[name] ?? null);

	const templateByName = $derived.by(() => {
		const m: Record<string, PlcTemplate> = {};
		for (const t of templates) m[t.name] = t;
		return m;
	});

	function isPrimitiveDatatype(dt: string): boolean {
		return dt === 'number' || dt === 'boolean' || dt === 'string';
	}

	function fieldZero(type: string): unknown {
		const base = type.replace(/\[\]$/, '').replace(/\{\}$/, '');
		if (type.endsWith('[]')) return [];
		if (type.endsWith('{}')) return {};
		if (base === 'bool' || base === 'boolean') return false;
		if (base === 'string') return '';
		if (base === 'bytes') return '';
		if (base === 'number') return 0;
		return null;
	}

	let datatype = $state('number');
	let direction = $state<'internal' | 'output' | 'input'>('internal');
	let primitiveDefault = $state<string>('0');
	let templateDefaults = $state<Record<string, unknown>>({});
	let description = $state('');

	const selectedTemplate = $derived(templateByName[datatype] ?? null);

	// Seed local state from current config when variable or config changes.
	let lastLoadedFor = '';
	$effect(() => {
		const key = `${name}::${plcConfig?.updatedAt ?? 0}`;
		if (!current || key === lastLoadedFor) return;
		lastLoadedFor = key;
		datatype = current.datatype;
		direction = (current.direction as 'internal' | 'output' | 'input') ?? 'internal';
		description = current.description ?? '';
		if (isPrimitiveDatatype(current.datatype)) {
			const d = current.default;
			primitiveDefault = current.datatype === 'boolean' ? (d ? 'true' : 'false') : String(d ?? '');
			templateDefaults = {};
		} else {
			primitiveDefault = '0';
			const tpl = templateByName[current.datatype];
			const d = (current.default && typeof current.default === 'object' ? current.default : {}) as Record<string, unknown>;
			const next: Record<string, unknown> = {};
			if (tpl) {
				for (const f of tpl.fields) {
					next[f.name] = d[f.name] !== undefined ? d[f.name] : (f.default !== undefined ? f.default : fieldZero(f.type));
				}
			}
			templateDefaults = next;
		}
	});

	// When user switches datatype, seed sensible defaults for the new type.
	let prevDatatype = $state('');
	$effect(() => {
		if (datatype === prevDatatype) return;
		const was = prevDatatype;
		prevDatatype = datatype;
		if (was === '') return; // initial load, skip
		if (isPrimitiveDatatype(datatype)) {
			primitiveDefault = datatype === 'boolean' ? 'false' : datatype === 'string' ? '' : '0';
			templateDefaults = {};
		} else {
			const tpl = templateByName[datatype];
			if (!tpl) return;
			const next: Record<string, unknown> = {};
			for (const f of tpl.fields) {
				next[f.name] = f.default !== undefined ? f.default : fieldZero(f.type);
			}
			templateDefaults = next;
		}
	});

	function buildDefault(): unknown {
		if (!isPrimitiveDatatype(datatype) && selectedTemplate) {
			return { _type: selectedTemplate.name, ...templateDefaults };
		}
		if (datatype === 'boolean') return primitiveDefault === 'true';
		if (datatype === 'string') return primitiveDefault;
		if (datatype === 'number') return parseFloat(primitiveDefault) || 0;
		return null;
	}

	function deepEqual(a: unknown, b: unknown): boolean {
		if (a === b) return true;
		if (a === null || b === null || a === undefined || b === undefined) return a === b;
		if (typeof a !== typeof b) return false;
		if (typeof a !== 'object') return false;
		if (Array.isArray(a) !== Array.isArray(b)) return false;
		if (Array.isArray(a)) {
			const ba = b as unknown[];
			if (a.length !== ba.length) return false;
			for (let i = 0; i < a.length; i++) if (!deepEqual(a[i], ba[i])) return false;
			return true;
		}
		const ao = a as Record<string, unknown>;
		const bo = b as Record<string, unknown>;
		const ak = Object.keys(ao);
		const bk = Object.keys(bo);
		if (ak.length !== bk.length) return false;
		for (const k of ak) if (!deepEqual(ao[k], bo[k])) return false;
		return true;
	}

	onMount(() => startLiveValues());

	const currentValues = $derived.by(() => {
		void liveValuesVersion();
		const out: Record<string, unknown> = {};
		if (!selectedTemplate) return out;
		for (const f of selectedTemplate.fields) {
			const lv = getLiveValue(`${name}.${f.name}`);
			out[f.name] = lv?.value;
		}
		return out;
	});

	let selectedLeafPath = $state<(string | number)[] | null>(null);
	let selectedLeafType = $state<'number' | 'boolean' | 'string' | 'null' | 'complex'>('string');

	const selectedFieldName = $derived.by(() => {
		if (!selectedLeafPath || selectedLeafPath.length === 0) return null;
		const k = selectedLeafPath[0];
		return typeof k === 'string' ? k : null;
	});

	const selectedFieldType = $derived.by(() => {
		if (!selectedFieldName || !selectedTemplate) return null;
		return selectedTemplate.fields.find((f) => f.name === selectedFieldName)?.type ?? null;
	});

	let currentInput = $state<string>('');
	let writing = $state(false);
	let lastSelectedKey = '';

	$effect(() => {
		if (!selectedFieldName) {
			lastSelectedKey = '';
			return;
		}
		const live = getLiveValue(`${name}.${selectedFieldName}`)?.value;
		const key = `${selectedFieldName}::${JSON.stringify(live)}`;
		if (key === lastSelectedKey) return;
		lastSelectedKey = key;
		if (typeof live === 'boolean') currentInput = live ? 'true' : 'false';
		else if (live === undefined || live === null) currentInput = '';
		else currentInput = String(live);
	});

	function defaultInputValue(): string {
		if (!selectedFieldName) return '';
		const v = templateDefaults[selectedFieldName];
		if (typeof v === 'boolean') return v ? 'true' : 'false';
		if (v === undefined || v === null) return '';
		return String(v);
	}

	function commitDefault(raw: string) {
		if (!selectedFieldName || selectedLeafType === 'complex') return;
		let parsed: unknown = raw;
		if (selectedLeafType === 'number') {
			const n = parseFloat(raw);
			parsed = Number.isFinite(n) ? n : 0;
		} else if (selectedLeafType === 'boolean') {
			parsed = raw === 'true';
		}
		templateDefaults = { ...templateDefaults, [selectedFieldName]: parsed };
	}

	async function writeCurrent() {
		if (!selectedFieldName || selectedLeafType === 'complex') return;
		let parsed: unknown = currentInput;
		if (selectedLeafType === 'number') {
			const n = parseFloat(currentInput);
			if (!Number.isFinite(n)) {
				saltState.addNotification({ message: 'Invalid number', type: 'error' });
				return;
			}
			parsed = n;
		} else if (selectedLeafType === 'boolean') {
			parsed = currentInput === 'true';
		}
		writing = true;
		try {
			const id = `${name}.${selectedFieldName}`;
			const res = await apiPut(`/variables/plc/${encodeURIComponent(id)}/value`, { value: parsed });
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Wrote ${id}`, type: 'success' });
		} finally {
			writing = false;
		}
	}

	const isDirty = $derived.by(() => {
		if (!current) return false;
		if (datatype !== current.datatype) return true;
		if (direction !== current.direction) return true;
		if ((description ?? '') !== (current.description ?? '')) return true;
		return !deepEqual(buildDefault(), current.default);
	});

	$effect(() => {
		workspaceTabs.setDirty(name, isDirty);
	});

	let saving = $state(false);
	let deleting = $state(false);

	async function save() {
		if (!current) return;
		saving = true;
		try {
			const body: Record<string, unknown> = {
				id: name,
				datatype,
				direction,
				default: buildDefault()
			};
			if (description.trim()) body.description = description.trim();
			if (current.source) body.source = current.source;
			if (current.deadband) body.deadband = current.deadband;
			if (current.disableRBE) body.disableRBE = current.disableRBE;
			const res = await apiPut(`/plcs/plc/variables/${encodeURIComponent(name)}`, body);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Saved "${name}"`, type: 'success' });
			workspaceTabs.clearDirty(name);
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function revert() {
		lastLoadedFor = ''; // force re-seed from current
		// Trigger re-read by nudging a state used in the seeding effect.
		const tmp = datatype;
		datatype = tmp;
	}

	async function del() {
		if (!current) return;
		if (!confirm(`Delete variable "${name}"? This cannot be undone.`)) return;
		deleting = true;
		try {
			const res = await apiDelete(`/plcs/plc/variables/${encodeURIComponent(name)}`);
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

	async function unlinkSource() {
		if (!current || !current.source) return;
		if (!confirm(`Unlink "${name}" from its scanner source? The variable will become internal.`)) return;
		saving = true;
		try {
			const body: Record<string, unknown> = {
				id: name,
				datatype,
				direction,
				default: buildDefault()
			};
			if (description.trim()) body.description = description.trim();
			const res = await apiPut(`/plcs/plc/variables/${encodeURIComponent(name)}`, body);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Unlinked "${name}"`, type: 'success' });
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function sourceSummary(s: PlcVariableSource): string {
		const parts = [s.protocol, s.deviceId, s.tag].filter(Boolean);
		return parts.join(' · ');
	}

</script>

<div class="variable-editor">
	{#if !current}
		<div class="empty">
			<p>Variable <code>{name}</code> not found.</p>
			<p class="hint">It may have been deleted. Close this tab.</p>
		</div>
	{:else}
		<header class="ve-head">
			<div class="title-row">
				<span class="name">{name}</span>
				{#if isDirty}
					<span class="dirty-icon" title="Unsaved changes"><PencilSquare size="1rem" /></span>
				{/if}
			</div>
			<div class="actions">
				<button class="btn" onclick={revert} disabled={!isDirty || saving}>Revert</button>
				<button class="btn primary" onclick={save} disabled={!isDirty || saving}>
					{saving ? 'Saving…' : 'Save'}
				</button>
				<button class="btn danger" onclick={del} disabled={deleting || saving} title="Delete variable">
					{deleting ? 'Deleting…' : 'Delete'}
				</button>
			</div>
		</header>

		<div class="body">
			<div class="form-row">
				<label class="field">
					<span>Direction</span>
					<select bind:value={direction} class="input">
						<option value="internal">internal</option>
						<option value="output">output</option>
						<option value="input">input</option>
					</select>
				</label>
				<label class="field">
					<span>Datatype</span>
					<select bind:value={datatype} class="input">
						<optgroup label="Primitives">
							<option value="number">number</option>
							<option value="boolean">boolean</option>
							<option value="string">string</option>
						</optgroup>
						{#if templates.length > 0}
							<optgroup label="Templates">
								{#each templates as tmpl (tmpl.name)}
									<option value={tmpl.name}>{tmpl.name}</option>
								{/each}
							</optgroup>
						{/if}
					</select>
				</label>
				{#if isPrimitiveDatatype(datatype)}
					<label class="field">
						<span>Default</span>
						{#if datatype === 'boolean'}
							<select bind:value={primitiveDefault} class="input">
								<option value="false">false</option>
								<option value="true">true</option>
							</select>
						{:else}
							<input type={datatype === 'number' ? 'number' : 'text'} bind:value={primitiveDefault} class="input" step="any" />
						{/if}
					</label>
				{/if}
			</div>

			<label class="field">
				<span>Description</span>
				<input type="text" bind:value={description} class="input" placeholder="(optional)" />
			</label>

			{#if selectedTemplate}
				<div class="template-block" transition:slide={{ duration: 150 }}>
					<div class="section-label">Instance defaults · default / current</div>
					<div class="tree-wrap">
						<ValueTree
							value={templateDefaults}
							secondary={currentValues}
							label={selectedTemplate.name}
							secondaryLabel="current"
							selectedPath={selectedLeafPath}
							onSelect={(path, leafType) => {
								selectedLeafPath = path;
								selectedLeafType = leafType;
							}}
						/>
					</div>

					{#if selectedFieldName}
						<div class="leaf-panel" transition:slide={{ duration: 150 }}>
							<div class="leaf-head">
								<span class="leaf-name">{selectedFieldName}</span>
								{#if selectedFieldType}<span class="leaf-type">{selectedFieldType}</span>{/if}
								<button class="btn small" onclick={() => (selectedLeafPath = null)} title="Close">×</button>
							</div>
							<div class="leaf-grid">
								<label class="field">
									<span>Default</span>
									{#if selectedLeafType === 'boolean'}
										<select
											class="input"
											value={defaultInputValue()}
											onchange={(e) => commitDefault((e.currentTarget as HTMLSelectElement).value)}
										>
											<option value="false">false</option>
											<option value="true">true</option>
										</select>
									{:else if selectedLeafType === 'number' || selectedLeafType === 'string'}
										<input
											class="input"
											type={selectedLeafType === 'number' ? 'number' : 'text'}
											value={defaultInputValue()}
											step="any"
											oninput={(e) => commitDefault((e.currentTarget as HTMLInputElement).value)}
										/>
									{:else}
										<span class="muted">not editable</span>
									{/if}
								</label>
								<label class="field">
									<span>Current</span>
									{#if selectedLeafType === 'boolean'}
										<select class="input" bind:value={currentInput}>
											<option value="false">false</option>
											<option value="true">true</option>
										</select>
									{:else if selectedLeafType === 'number' || selectedLeafType === 'string'}
										<input
											class="input"
											type={selectedLeafType === 'number' ? 'number' : 'text'}
											bind:value={currentInput}
											step="any"
										/>
									{:else}
										<span class="muted">not editable</span>
									{/if}
								</label>
								<div class="field">
									<span>&nbsp;</span>
									<button
										class="btn primary"
										onclick={writeCurrent}
										disabled={writing || selectedLeafType === 'complex' || selectedLeafType === 'null'}
									>
										{writing ? 'Writing…' : 'Write'}
									</button>
								</div>
							</div>
							<p class="hint">
								Default changes are staged — press <strong>Save</strong> above to commit them to the
								variable definition. Current value writes send immediately to the PLC.
							</p>
						</div>
					{/if}
				</div>

				<TemplateDefinitionEditor template={selectedTemplate} {templates} {plcConfig} />
			{/if}

			{#if current.source}
				<div class="source-block">
					<div class="source-head">
						<span class="source-badge">Source</span>
						<span class="source-summary">{sourceSummary(current.source)}</span>
					</div>
					<div class="source-meta">
						{#if current.source.cipType}<span>cipType: {current.source.cipType}</span>{/if}
						{#if current.source.functionCode !== undefined && current.source.functionCode !== null}<span>fc: {current.source.functionCode}</span>{/if}
						{#if current.source.modbusDatatype}<span>modbus: {current.source.modbusDatatype}</span>{/if}
						{#if current.source.byteOrder}<span>byteOrder: {current.source.byteOrder}</span>{/if}
						{#if current.source.address !== undefined && current.source.address !== null}<span>addr: {current.source.address}</span>{/if}
					</div>
					<div class="source-actions">
						<button class="btn small" onclick={unlinkSource} disabled={saving}>Unlink</button>
					</div>
				</div>
			{/if}
		</div>
	{/if}
</div>

<style lang="scss">
	.variable-editor {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		overflow: auto;
		background: var(--theme-background);
	}

	.ve-head {
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

		.name {
			font-family: var(--font-mono, monospace);
			font-size: 0.9375rem;
			font-weight: 600;
			color: var(--theme-text);
		}

		.dirty-icon {
			display: inline-flex;
			align-items: center;
			color: var(--theme-warning, #f59e0b);

			:global(svg) {
				flex-shrink: 0;
			}
		}
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

		&:disabled {
			opacity: 0.5;
			cursor: not-allowed;
		}
	}

	.body {
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.form-row {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
		gap: 0.75rem;
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 0.3125rem;
		font-size: 0.8125rem;
		color: var(--theme-text);

		span {
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

		&:focus {
			outline: none;
			border-color: var(--theme-primary);
		}
	}

	.template-block,
	.source-block {
		border: 1px solid var(--theme-border);
		border-radius: 0.375rem;
		padding: 0.75rem;
		background: color-mix(in srgb, var(--theme-surface) 60%, transparent);
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
	}

	.source-head {
		display: flex;
		align-items: baseline;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.section-label {
		font-size: 0.6875rem;
		color: var(--theme-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		font-weight: 600;
	}

	.source-badge {
		padding: 0.0625rem 0.375rem;
		font-size: 0.625rem;
		font-weight: 600;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		border-radius: 0.1875rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.source-summary {
		color: var(--theme-text-muted);
		font-size: 0.8125rem;
	}

	.source-summary {
		font-family: var(--font-mono, monospace);
		color: var(--theme-text);
	}

	.tree-wrap {
		overflow-x: auto;
		padding: 0.25rem 0;
	}

	.leaf-panel {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding: 0.625rem;
		border: 1px solid var(--theme-border);
		border-radius: 0.3125rem;
		background: var(--theme-background);
	}

	.leaf-head {
		display: flex;
		align-items: center;
		gap: 0.5rem;

		.leaf-name {
			font-family: var(--font-mono, monospace);
			font-weight: 600;
			color: var(--theme-text);
		}

		.leaf-type {
			padding: 0.0625rem 0.375rem;
			font-size: 0.6875rem;
			color: var(--theme-text-muted);
			background: color-mix(in srgb, var(--theme-text-muted) 12%, transparent);
			border-radius: 0.1875rem;
			font-family: var(--font-mono, monospace);
		}

		.btn.small {
			margin-left: auto;
			font-size: 0.875rem;
			line-height: 1;
			padding: 0.125rem 0.4375rem;
		}
	}

	.leaf-grid {
		display: grid;
		grid-template-columns: 1fr 1fr auto;
		gap: 0.625rem;
		align-items: end;
	}

	.muted {
		color: var(--theme-text-muted);
		font-size: 0.8125rem;
		padding: 0.375rem 0;
	}

	.hint {
		margin: 0;
		font-size: 0.75rem;
		color: var(--theme-text-muted);

		strong {
			color: var(--theme-text);
		}
	}

	.source-meta {
		display: flex;
		flex-wrap: wrap;
		gap: 0.625rem;
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		font-family: var(--font-mono, monospace);
	}

	.source-actions {
		display: flex;
		justify-content: flex-end;
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
