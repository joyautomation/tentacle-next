<script lang="ts">
	import * as d3 from 'd3';
	import { apiPut } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import { slide } from 'svelte/transition';
	import type { PlcConfig, PlcTemplate, PlcTemplateField } from '$lib/types/plc';

	type Props = {
		template: PlcTemplate;
		templates: PlcTemplate[];
		plcConfig: PlcConfig | null;
	};

	let { template, templates, plcConfig }: Props = $props();

	const templateByName = $derived.by(() => {
		const m: Record<string, PlcTemplate> = {};
		for (const t of templates) m[t.name] = t;
		return m;
	});

	const affectedVariables = $derived.by(() => {
		if (!plcConfig?.variables) return [];
		const out: string[] = [];
		for (const v of Object.values(plcConfig.variables)) {
			const base = v.datatype.replace(/\[\]$/, '').replace(/\{\}$/, '');
			if (base === template.name) out.push(v.id);
		}
		return out.sort();
	});

	let description = $state('');
	let fields = $state<PlcTemplateField[]>([]);
	let selectedIdx = $state<number | null>(null);
	let saving = $state(false);
	let expanded = $state(false);

	// Reseed when the template prop changes (e.g. user switched variables).
	let lastSeededFor = '';
	$effect(() => {
		const key = `${template.name}::${template.updatedAt ?? 0}`;
		if (key === lastSeededFor) return;
		lastSeededFor = key;
		description = template.description ?? '';
		fields = template.fields.map((f) => ({ ...f }));
		selectedIdx = null;
	});

	const isDirty = $derived.by(() => {
		if ((description ?? '') !== (template.description ?? '')) return true;
		return JSON.stringify(fields) !== JSON.stringify(template.fields);
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
		const others = templates.filter((t) => t.name !== template.name).map((t) => t.name);
		return [...prims, ...others];
	}

	function addField() {
		let i = fields.length + 1;
		let name = `field${i}`;
		while (fields.some((f) => f.name === name)) {
			i++;
			name = `field${i}`;
		}
		fields = [...fields, { name, type: 'number', default: 0 }];
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

	function revert() {
		description = template.description ?? '';
		fields = template.fields.map((f) => ({ ...f }));
		selectedIdx = null;
	}

	async function save() {
		saving = true;
		try {
			const body: PlcTemplate = {
				name: template.name,
				description: description.trim() || undefined,
				tags: template.tags,
				fields,
				methods: template.methods,
				updatedBy: 'gui'
			};
			const res = await apiPut(`/plcs/plc/templates/${encodeURIComponent(template.name)}`, body);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({
				message: `Saved template "${template.name}"`,
				type: 'success'
			});
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	// ---- Tidy tree rendering -------------------------------------------------

	type TreeDatum = {
		name: string;
		type?: string;
		kind: 'root' | 'field' | 'nested-field';
		fieldIdx?: number;
		children?: TreeDatum[];
	};

	function buildTree(): TreeDatum {
		const visit = (name: string, type: string | undefined, depth: number): TreeDatum[] => {
			const base = (type ?? '').replace(/\[\]$/, '').replace(/\{\}$/, '');
			const nested = templateByName[base];
			if (!nested || depth > 3) return [];
			return nested.fields.map((nf) => ({
				name: nf.name,
				type: nf.type,
				kind: 'nested-field' as const,
				children: visit(nf.name, nf.type, depth + 1)
			}));
		};
		return {
			name: template.name,
			kind: 'root',
			children: fields.map((f, i) => ({
				name: f.name,
				type: f.type,
				kind: 'field' as const,
				fieldIdx: i,
				children: visit(f.name, f.type, 1)
			}))
		};
	}

	let svgEl: SVGSVGElement | null = $state(null);

	$effect(() => {
		// Re-render on field/template change.
		void fields;
		void selectedIdx;
		void template.name;
		void expanded;
		if (!svgEl) return;

		const data = buildTree();
		const root = d3.hierarchy<TreeDatum>(data);

		const width = 560;
		const marginTop = 10;
		const marginRight = 120;
		const marginBottom = 10;
		const marginLeft = 40;

		const dx = 22;
		const dy = (width - marginRight - marginLeft) / (1 + root.height);

		const tree = d3.tree<TreeDatum>().nodeSize([dx, dy]);
		const diagonal = d3
			.linkHorizontal<d3.HierarchyLink<TreeDatum>, d3.HierarchyNode<TreeDatum>>()
			.x((d) => d.y!)
			.y((d) => d.x!);

		tree(root);

		let left = root as d3.HierarchyNode<TreeDatum>;
		let right = root as d3.HierarchyNode<TreeDatum>;
		root.each((node) => {
			if (node.x! < left.x!) left = node;
			if (node.x! > right.x!) right = node;
		});

		const height = Math.max(80, right.x! - left.x! + marginTop + marginBottom);

		const svg = d3.select(svgEl);
		svg.selectAll('*').remove();
		svg
			.attr('width', width)
			.attr('height', height)
			.attr('viewBox', [-marginLeft, left.x! - marginTop, width, height].join(' '))
			.attr('style', 'max-width: 100%; height: auto; font: 11px var(--font-mono, monospace);');

		const gLink = svg
			.append('g')
			.attr('fill', 'none')
			.attr('stroke', 'currentColor')
			.attr('stroke-opacity', 0.25)
			.attr('stroke-width', 1.25);

		gLink
			.selectAll('path')
			.data(root.links())
			.join('path')
			.attr('d', diagonal as unknown as (d: d3.HierarchyLink<TreeDatum>) => string);

		const gNode = svg
			.append('g')
			.attr('cursor', 'pointer')
			.attr('pointer-events', 'all');

		const nodes = gNode
			.selectAll('g')
			.data(root.descendants())
			.join('g')
			.attr('transform', (d) => `translate(${d.y},${d.x})`);

		nodes
			.append('circle')
			.attr('r', (d) => (d.data.kind === 'root' ? 4 : 3))
			.attr('fill', (d) => {
				if (d.data.kind === 'root') return 'var(--theme-primary)';
				if (d.data.kind === 'field' && d.data.fieldIdx === selectedIdx)
					return 'var(--theme-primary)';
				if (d.data.kind === 'nested-field') return 'var(--theme-text-muted)';
				return 'var(--theme-text)';
			})
			.attr('stroke', (d) =>
				d.data.kind === 'field' && d.data.fieldIdx === selectedIdx
					? 'var(--theme-primary)'
					: 'none'
			)
			.attr('stroke-width', 2);

		nodes
			.append('text')
			.attr('dy', '0.31em')
			.attr('x', 8)
			.attr('text-anchor', 'start')
			.attr('fill', 'var(--theme-text)')
			.attr('font-weight', (d) =>
				d.data.kind === 'root' || (d.data.kind === 'field' && d.data.fieldIdx === selectedIdx)
					? 600
					: 400
			)
			.text((d) => {
				if (d.data.kind === 'root') return d.data.name;
				return `${d.data.name}: ${d.data.type ?? ''}`;
			})
			.attr('paint-order', 'stroke')
			.attr('stroke', 'var(--theme-background)')
			.attr('stroke-width', 3)
			.attr('stroke-linejoin', 'round');

		nodes.on('click', (_event, d) => {
			if (d.data.kind === 'field' && d.data.fieldIdx !== undefined) {
				selectedIdx = d.data.fieldIdx;
			} else if (d.data.kind === 'root') {
				selectedIdx = null;
			}
		});
	});
</script>

<div class="tpl-def" class:collapsed={!expanded}>
	<header class="tpl-head">
		<button
			type="button"
			class="toggle"
			onclick={() => (expanded = !expanded)}
			aria-expanded={expanded}
			aria-label={expanded ? 'Collapse template definition' : 'Expand template definition'}
		>
			<span class="chevron" class:open={expanded}>▸</span>
			<span class="badge">Template</span>
			<span class="name">{template.name}</span>
			{#if isDirty}<span class="dirty-dot" title="Unsaved template changes">●</span>{/if}
			<span class="field-count">{fields.length} {fields.length === 1 ? 'field' : 'fields'}</span>
		</button>
		{#if expanded}
			<div class="actions">
				<button class="btn" onclick={revert} disabled={!isDirty || saving}>Revert</button>
				<button class="btn primary" onclick={save} disabled={!isDirty || saving}>
					{saving ? 'Saving…' : 'Save template'}
				</button>
			</div>
		{/if}
	</header>

	{#if expanded}
		<div class="tpl-body" transition:slide={{ duration: 150 }}>
			{#if isDirty && affectedVariables.length > 0}
				<div class="warning" transition:slide={{ duration: 150 }}>
					<strong>Heads up:</strong>
					Editing this template will affect
					{affectedVariables.length}
					{affectedVariables.length === 1 ? 'variable' : 'variables'}:
					<span class="affected">
						{#each affectedVariables as v, i (v)}
							<code>{v}</code>{i < affectedVariables.length - 1 ? ', ' : ''}
						{/each}
					</span>
				</div>
			{/if}

			<label class="field">
				<span>Description</span>
				<input type="text" bind:value={description} class="input" placeholder="(optional)" />
			</label>

			<div class="tree-wrap">
				<svg bind:this={svgEl}></svg>
			</div>

	<div class="tree-actions">
		<button class="btn small" onclick={addField}>+ Add field</button>
		{#if selectedIdx !== null}
			<button class="btn small" onclick={() => moveField(selectedIdx!, -1)} disabled={selectedIdx === 0}>↑ Up</button>
			<button class="btn small" onclick={() => moveField(selectedIdx!, 1)} disabled={selectedIdx === fields.length - 1}>↓ Down</button>
			<button class="btn small danger" onclick={() => removeField(selectedIdx!)}>× Remove</button>
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
						oninput={(e) => updateField(selectedIdx!, { name: (e.currentTarget as HTMLInputElement).value })}
					/>
				</label>
				<label class="field">
					<span>Type</span>
					<input
						type="text"
						class="input"
						list="tpl-type-options"
						value={f.type}
						oninput={(e) => updateField(selectedIdx!, { type: (e.currentTarget as HTMLInputElement).value })}
					/>
					<datalist id="tpl-type-options">
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
								onchange={(e) => onDefaultRaw(selectedIdx!, (e.currentTarget as HTMLSelectElement).value)}
							>
								<option value="false">false</option>
								<option value="true">true</option>
							</select>
						{:else}
							<input
								type={f.type === 'number' ? 'number' : 'text'}
								class="input"
								value={defaultDisplay(f)}
								oninput={(e) => onDefaultRaw(selectedIdx!, (e.currentTarget as HTMLInputElement).value)}
								step="any"
							/>
						{/if}
					{:else}
						<input class="input" value={defaultDisplay(f)} readonly title="Set in Starlark" />
					{/if}
				</label>
			</div>
			<label class="field">
				<span>Description</span>
				<input
					type="text"
					class="input"
					value={f.description ?? ''}
					oninput={(e) => updateField(selectedIdx!, { description: (e.currentTarget as HTMLInputElement).value })}
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
	.tpl-def {
		border: 1px solid var(--theme-border);
		border-radius: 0.375rem;
		padding: 0.75rem;
		background: color-mix(in srgb, var(--theme-surface) 60%, transparent);
		display: flex;
		flex-direction: column;
		gap: 0.625rem;

		&.collapsed {
			padding: 0.375rem 0.75rem;
		}
	}

	.tpl-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
	}

	.toggle {
		display: flex;
		align-items: baseline;
		gap: 0.5rem;
		flex: 1;
		min-width: 0;
		padding: 0.125rem 0;
		background: transparent;
		border: 0;
		cursor: pointer;
		color: inherit;
		text-align: left;

		&:hover .name {
			color: var(--theme-primary);
		}

		.name {
			font-family: var(--font-mono, monospace);
			font-weight: 600;
			color: var(--theme-text);
		}

		.dirty-dot {
			color: var(--theme-warning, var(--theme-primary));
			font-size: 0.75rem;
		}

		.field-count {
			margin-left: auto;
			font-size: 0.6875rem;
			color: var(--theme-text-muted);
		}
	}

	.chevron {
		display: inline-block;
		color: var(--theme-text-muted);
		font-size: 0.6875rem;
		transition: transform 0.12s ease;
		transform-origin: center;
		width: 0.75rem;

		&.open {
			transform: rotate(90deg);
		}
	}

	.tpl-body {
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
	}

	.badge {
		padding: 0.0625rem 0.375rem;
		font-size: 0.625rem;
		font-weight: 600;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		border-radius: 0.1875rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
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

	.tree-actions {
		display: flex;
		gap: 0.375rem;
		flex-wrap: wrap;
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

		&:focus {
			outline: none;
			border-color: var(--theme-primary);
		}
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

	.hint {
		margin: 0;
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		font-style: italic;
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
</style>
