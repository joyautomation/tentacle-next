<script lang="ts">
	import * as d3 from 'd3';

	type Props = {
		value: unknown;
		label?: string;
		editable?: boolean;
		onChange?: (path: (string | number)[], newValue: unknown) => void;
	};

	let { value, label = 'value', editable = false, onChange }: Props = $props();

	type LeafType = 'number' | 'boolean' | 'string' | 'null' | 'complex';

	type TreeDatum = {
		name: string;
		kind: 'root' | 'branch' | 'leaf';
		path: (string | number)[];
		raw: unknown;
		leafType: LeafType;
		children?: TreeDatum[];
	};

	function leafTypeOf(v: unknown): LeafType {
		if (v === null || v === undefined) return 'null';
		if (typeof v === 'number') return 'number';
		if (typeof v === 'boolean') return 'boolean';
		if (typeof v === 'string') return 'string';
		return 'complex';
	}

	function typeBadge(v: unknown): string {
		if (v === null) return 'null';
		if (Array.isArray(v)) return `array[${v.length}]`;
		if (typeof v === 'object') {
			const t = (v as Record<string, unknown>)._type;
			return typeof t === 'string' ? t : 'object';
		}
		return typeof v;
	}

	function build(
		name: string,
		v: unknown,
		path: (string | number)[],
		depth: number,
		kind: 'root' | 'branch' | 'leaf' = 'leaf'
	): TreeDatum {
		if (v !== null && typeof v === 'object' && depth < 4) {
			const entries = Array.isArray(v)
				? v.map((item, i) => [i, item] as [string | number, unknown])
				: Object.entries(v as Record<string, unknown>).filter(([k]) => k !== '_type');
			return {
				name,
				kind: kind === 'root' ? 'root' : 'branch',
				path,
				raw: v,
				leafType: 'complex',
				children: entries.map(([k, val]) => build(String(k), val, [...path, k], depth + 1))
			};
		}
		return { name, kind: 'leaf', path, raw: v, leafType: leafTypeOf(v) };
	}

	const tree = $derived(build(label, value, [], 0, 'root'));

	const layout = $derived.by(() => {
		const root = d3.hierarchy<TreeDatum>(tree);
		const dx = 24;
		const dy = 140;
		d3.tree<TreeDatum>().nodeSize([dx, dy])(root);

		let minX = Infinity;
		let maxX = -Infinity;
		root.each((n) => {
			if (n.x! < minX) minX = n.x!;
			if (n.x! > maxX) maxX = n.x!;
		});

		const padT = 10;
		const padB = 10;
		const padL = 12;
		const padR = 20;

		const height = Math.max(40, maxX - minX + padT + padB);
		const width = (root.height + 1) * dy + padL + padR;
		const offsetX = -minX + padT;

		const nodes = root.descendants().map((n) => ({
			x: n.x! + offsetX,
			y: n.y! + padL,
			data: n.data
		}));

		const links = root.links().map((l) => {
			const sx = l.source.x! + offsetX;
			const sy = l.source.y! + padL;
			const tx = l.target.x! + offsetX;
			const ty = l.target.y! + padL;
			const mx = (sy + ty) / 2;
			return { d: `M${sy},${sx}C${mx},${sx} ${mx},${tx} ${ty},${tx}` };
		});

		return { nodes, links, width, height };
	});

	function leafDisplay(n: TreeDatum): string {
		const v = n.raw;
		if (v === null) return 'null';
		if (v === undefined) return '—';
		if (typeof v === 'string') return `"${v}"`;
		if (typeof v === 'number') return Number.isInteger(v) ? String(v) : v.toFixed(3);
		if (typeof v === 'boolean') return String(v);
		return String(v);
	}

	function commit(n: TreeDatum, raw: string) {
		if (!onChange) return;
		let parsed: unknown = raw;
		if (n.leafType === 'number') {
			const num = parseFloat(raw);
			parsed = Number.isFinite(num) ? num : 0;
		} else if (n.leafType === 'boolean') {
			parsed = raw === 'true';
		}
		onChange(n.path, parsed);
	}
</script>

<div class="vt" style="width: {layout.width}px; height: {layout.height}px;">
	<svg width={layout.width} height={layout.height} aria-hidden="true">
		<g fill="none" stroke="currentColor" stroke-opacity="0.25" stroke-width="1.25">
			{#each layout.links as l (l.d)}
				<path d={l.d} />
			{/each}
		</g>
	</svg>

	{#each layout.nodes as n (n.data.path.join('/') || 'root')}
		<div
			class="node"
			class:is-root={n.data.kind === 'root'}
			class:is-branch={n.data.kind === 'branch'}
			class:is-leaf={n.data.kind === 'leaf'}
			style="left: {n.y}px; top: {n.x}px;"
		>
			<span class="dot"></span>
			{#if n.data.kind === 'root'}
				<span class="label name">{n.data.name}</span>
			{:else if n.data.kind === 'branch'}
				<span class="label"
					><span class="key">{n.data.name}</span>: <em>{typeBadge(n.data.raw)}</em></span
				>
			{:else if editable && n.data.leafType === 'boolean'}
				<span class="label"><span class="key">{n.data.name}</span>:</span>
				<select
					class="leaf-input"
					value={String(n.data.raw)}
					onchange={(e) => commit(n.data, (e.currentTarget as HTMLSelectElement).value)}
				>
					<option value="false">false</option>
					<option value="true">true</option>
				</select>
			{:else if editable && (n.data.leafType === 'number' || n.data.leafType === 'string')}
				<span class="label"><span class="key">{n.data.name}</span>:</span>
				<input
					class="leaf-input"
					type={n.data.leafType === 'number' ? 'number' : 'text'}
					value={n.data.raw as string | number}
					step="any"
					oninput={(e) => commit(n.data, (e.currentTarget as HTMLInputElement).value)}
				/>
			{:else}
				<span class="label"
					><span class="key">{n.data.name}</span>: <span class="leaf-val">{leafDisplay(n.data)}</span></span
				>
			{/if}
		</div>
	{/each}
</div>

<style lang="scss">
	.vt {
		position: relative;
		color: var(--theme-text);
		font: 11px var(--font-mono, monospace);
	}

	svg {
		position: absolute;
		inset: 0;
		pointer-events: none;
	}

	.node {
		position: absolute;
		display: flex;
		align-items: center;
		gap: 0.3125rem;
		transform: translateY(-50%);
		white-space: nowrap;
	}

	.dot {
		display: inline-block;
		width: 5px;
		height: 5px;
		border-radius: 50%;
		background: var(--theme-text-muted);
		flex-shrink: 0;
	}

	.is-root .dot {
		background: var(--theme-primary);
		width: 7px;
		height: 7px;
	}

	.is-branch .dot {
		background: var(--theme-text);
		width: 6px;
		height: 6px;
	}

	.label {
		color: var(--theme-text);
	}

	.is-root .name {
		font-weight: 600;
	}

	.key {
		color: var(--theme-text);
	}

	.label em {
		font-style: normal;
		color: var(--theme-text-muted);
	}

	.leaf-val {
		color: var(--theme-primary);
	}

	.leaf-input {
		background: var(--theme-background);
		color: var(--theme-text);
		border: 1px solid var(--theme-border);
		border-radius: 0.1875rem;
		padding: 0.0625rem 0.3125rem;
		font: 11px var(--font-mono, monospace);
		width: 6rem;

		&:focus {
			outline: none;
			border-color: var(--theme-primary);
		}
	}
</style>
