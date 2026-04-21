<script lang="ts" module>
	export type TidyNode = {
		id: string;
		label: string;
		kind: 'root' | 'branch' | 'leaf';
		path: (string | number)[];
		selectable?: boolean;
		children?: TidyNode[];
	};
</script>

<script lang="ts">
	import * as d3 from 'd3';
	import type { Snippet } from 'svelte';

	type Props = {
		root: TidyNode;
		selectedPath?: (string | number)[] | null;
		onSelect?: (path: (string | number)[], node: TidyNode) => void;
		nodeWidth?: number;
		rowHeight?: number;
		content?: Snippet<[{ node: TidyNode; selected: boolean }]>;
	};

	let {
		root,
		selectedPath = null,
		onSelect,
		nodeWidth = 140,
		rowHeight = 24,
		content
	}: Props = $props();

	const layout = $derived.by(() => {
		const hroot = d3.hierarchy<TidyNode>(root);
		d3.tree<TidyNode>().nodeSize([rowHeight, nodeWidth])(hroot);

		let minX = Infinity;
		let maxX = -Infinity;
		hroot.each((n) => {
			if (n.x! < minX) minX = n.x!;
			if (n.x! > maxX) maxX = n.x!;
		});

		const padT = 10;
		const padB = 10;
		const padL = 12;
		const padR = 20;

		const height = Math.max(40, maxX - minX + padT + padB);
		const width = (hroot.height + 1) * nodeWidth + padL + padR;
		const offsetX = -minX + padT;

		const nodes = hroot.descendants().map((n) => ({
			x: n.x! + offsetX,
			y: n.y! + padL,
			data: n.data
		}));

		const links = hroot.links().map((l) => ({
			d: `M${l.source.y! + padL},${l.source.x! + offsetX}C${(l.source.y! + l.target.y!) / 2 + padL},${l.source.x! + offsetX} ${(l.source.y! + l.target.y!) / 2 + padL},${l.target.x! + offsetX} ${l.target.y! + padL},${l.target.x! + offsetX}`,
			id: `${l.source.data.id}->${l.target.data.id}`
		}));

		return { nodes, links, width, height };
	});

	function pathsEqual(a: (string | number)[] | null, b: (string | number)[]): boolean {
		if (!a) return false;
		if (a.length !== b.length) return false;
		for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
		return true;
	}
</script>

<div class="tidy" style="width: {layout.width}px; height: {layout.height}px;">
	<svg width={layout.width} height={layout.height} aria-hidden="true">
		<g fill="none" stroke="currentColor" stroke-opacity="0.25" stroke-width="1.25">
			{#each layout.links as l (l.id)}
				<path d={l.d} />
			{/each}
		</g>
	</svg>

	{#each layout.nodes as n (n.data.id)}
		{@const selected = pathsEqual(selectedPath, n.data.path)}
		{@const clickable = !!(onSelect && n.data.selectable)}
		<div
			class="node"
			class:is-root={n.data.kind === 'root'}
			class:is-branch={n.data.kind === 'branch'}
			class:is-leaf={n.data.kind === 'leaf'}
			class:is-selectable={clickable}
			class:is-selected={selected}
			style="left: {n.y}px; top: {n.x}px;"
		>
			<span class="dot"></span>
			{#if clickable}
				<button type="button" class="node-btn" onclick={() => onSelect?.(n.data.path, n.data)}>
					{#if content}
						{@render content({ node: n.data, selected })}
					{:else}
						<span class="label">{n.data.label}</span>
					{/if}
				</button>
			{:else if content}
				<span class="node-static">
					{@render content({ node: n.data, selected })}
				</span>
			{:else}
				<span class="label">{n.data.label}</span>
			{/if}
		</div>
	{/each}
</div>

<style lang="scss">
	.tidy {
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

	.is-root .label {
		font-weight: 600;
	}

	.node-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		background: transparent;
		border: 1px solid transparent;
		border-radius: 0.1875rem;
		padding: 0.0625rem 0.3125rem;
		color: var(--theme-text);
		font: inherit;
		cursor: pointer;
		text-align: left;

		&:hover {
			border-color: var(--theme-border);
			background: color-mix(in srgb, var(--theme-primary) 8%, transparent);
		}
	}

	.is-selected .node-btn {
		border-color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
	}

	.is-selected .dot {
		background: var(--theme-primary);
	}

	.node-static {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
	}
</style>
