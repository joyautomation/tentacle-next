<script lang="ts">
	import TidyTreeView, { type TidyNode } from './TidyTreeView.svelte';

	type Props = {
		value: unknown;
		label?: string;
		secondary?: unknown;
		onSelect?: (path: (string | number)[], leafType: LeafType) => void;
		selectedPath?: (string | number)[] | null;
	};

	let {
		value,
		label = 'value',
		secondary,
		onSelect,
		selectedPath = null
	}: Props = $props();

	type LeafType = 'number' | 'boolean' | 'string' | 'null' | 'complex';

	type ValueNode = TidyNode & {
		keyName: string;
		raw: unknown;
		leafType: LeafType;
		children?: ValueNode[];
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

	function getAtPath(obj: unknown, path: (string | number)[]): unknown {
		let cur: unknown = obj;
		for (const k of path) {
			if (cur === null || cur === undefined) return undefined;
			if (typeof cur !== 'object') return undefined;
			cur = (cur as Record<string | number, unknown>)[k];
		}
		return cur;
	}

	function leafDisplay(v: unknown): string {
		if (v === null) return 'null';
		if (v === undefined) return '—';
		if (typeof v === 'string') return `"${v}"`;
		if (typeof v === 'number') return Number.isInteger(v) ? String(v) : v.toFixed(3);
		if (typeof v === 'boolean') return String(v);
		return String(v);
	}

	function build(
		name: string,
		v: unknown,
		path: (string | number)[],
		depth: number,
		kind: 'root' | 'branch' | 'leaf' = 'leaf'
	): ValueNode {
		const id = path.length === 0 ? '$root' : path.join('/');
		if (v !== null && typeof v === 'object' && depth < 4) {
			const entries = Array.isArray(v)
				? v.map((item, i) => [i, item] as [string | number, unknown])
				: Object.entries(v as Record<string, unknown>).filter(([k]) => k !== '_type');
			const resolvedKind = kind === 'root' ? 'root' : 'branch';
			return {
				id,
				label: resolvedKind === 'root' ? name : `${name}: ${typeBadge(v)}`,
				kind: resolvedKind,
				path,
				keyName: name,
				raw: v,
				leafType: 'complex',
				children: entries.map(([k, val]) => build(String(k), val, [...path, k], depth + 1))
			};
		}
		return {
			id,
			label: `${name}: ${leafDisplay(v)}`,
			kind: 'leaf',
			path,
			selectable: !!onSelect,
			keyName: name,
			raw: v,
			leafType: leafTypeOf(v)
		};
	}

	const tree = $derived(build(label, value, [], 0, 'root'));

	function handleSelect(path: (string | number)[], node: TidyNode) {
		const leafType = (node as ValueNode).leafType;
		onSelect?.(path, leafType);
	}
</script>

<TidyTreeView
	root={tree}
	{selectedPath}
	onSelect={onSelect ? handleSelect : undefined}
>
	{#snippet content(args: { node: TidyNode; selected: boolean })}
		{@const node = args.node}
		{@const vn = node as ValueNode}
		{#if vn.kind === 'root'}
			<span class="label">{vn.keyName}</span>
		{:else if vn.kind === 'branch'}
			<span class="label">
				<span class="key">{vn.keyName}</span>:
				<em>{typeBadge(vn.raw)}</em>
			</span>
		{:else}
			<span class="leaf-label">
				<span class="key">{vn.keyName}</span>:
				<span class="leaf-val">{leafDisplay(vn.raw)}</span>
				{#if secondary !== undefined}
					<span class="leaf-sep">/</span>
					<span class="leaf-val secondary">{leafDisplay(getAtPath(secondary, vn.path))}</span>
				{/if}
			</span>
		{/if}
	{/snippet}
</TidyTreeView>

<style lang="scss">
	.label em {
		font-style: normal;
		color: var(--theme-text-muted);
	}

	.leaf-label {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
	}

	.leaf-val {
		color: var(--theme-primary);
	}

	.leaf-val.secondary {
		color: var(--theme-text-muted);
	}

	.leaf-sep {
		color: var(--theme-text-muted);
		opacity: 0.5;
	}

	.key {
		color: var(--theme-text);
	}
</style>
