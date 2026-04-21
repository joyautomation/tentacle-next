<script lang="ts">
	import * as d3 from 'd3';

	type Props = {
		value: unknown;
		label?: string;
		maxWidth?: number;
	};

	let { value, label = 'value', maxWidth = 320 }: Props = $props();

	type TreeDatum = {
		name: string;
		display: string;
		kind: 'root' | 'branch' | 'leaf';
		children?: TreeDatum[];
	};

	function formatLeaf(v: unknown): string {
		if (v === null) return 'null';
		if (v === undefined) return '—';
		if (typeof v === 'number') return Number.isInteger(v) ? String(v) : v.toFixed(3);
		if (typeof v === 'boolean') return String(v);
		if (typeof v === 'string') return `"${v}"`;
		return String(v);
	}

	function typeLabel(v: unknown): string {
		if (v === null) return 'null';
		if (Array.isArray(v)) return `array[${v.length}]`;
		if (typeof v === 'object') {
			const t = (v as Record<string, unknown>)._type;
			return typeof t === 'string' ? t : 'object';
		}
		return typeof v;
	}

	function build(name: string, v: unknown, depth: number, kind: 'root' | 'branch' | 'leaf' = 'leaf'): TreeDatum {
		if (v !== null && typeof v === 'object' && depth < 4) {
			const entries = Array.isArray(v)
				? v.map((item, i) => [String(i), item] as const)
				: Object.entries(v as Record<string, unknown>);
			return {
				name,
				display: kind === 'root' ? `${name}: ${typeLabel(v)}` : `${name}: ${typeLabel(v)}`,
				kind: kind === 'root' ? 'root' : 'branch',
				children: entries.map(([k, val]) => build(k, val, depth + 1))
			};
		}
		return { name, display: `${name}: ${formatLeaf(v)}`, kind: 'leaf' };
	}

	const tree = $derived(build(label, value, 0, 'root'));

	let svgEl: SVGSVGElement | null = $state(null);

	$effect(() => {
		void tree;
		void maxWidth;
		if (!svgEl) return;

		const root = d3.hierarchy<TreeDatum>(tree);

		const marginTop = 6;
		const marginRight = 8;
		const marginBottom = 6;
		const marginLeft = 12;

		const dx = 18;
		const dy = Math.max(70, (maxWidth - marginRight - marginLeft) / Math.max(1, 1 + root.height));

		const layout = d3.tree<TreeDatum>().nodeSize([dx, dy]);
		const diagonal = d3
			.linkHorizontal<d3.HierarchyLink<TreeDatum>, d3.HierarchyNode<TreeDatum>>()
			.x((d) => d.y!)
			.y((d) => d.x!);

		layout(root);

		let left = root as d3.HierarchyNode<TreeDatum>;
		let right = root as d3.HierarchyNode<TreeDatum>;
		root.each((node) => {
			if (node.x! < left.x!) left = node;
			if (node.x! > right.x!) right = node;
		});

		const height = Math.max(40, right.x! - left.x! + marginTop + marginBottom);
		const width = (root.height + 1) * dy + marginLeft + marginRight;

		const svg = d3.select(svgEl);
		svg.selectAll('*').remove();
		svg
			.attr('width', width)
			.attr('height', height)
			.attr('viewBox', [-marginLeft, left.x! - marginTop, width, height].join(' '))
			.attr('style', 'max-width: 100%; height: auto; font: 11px var(--font-mono, monospace);');

		svg
			.append('g')
			.attr('fill', 'none')
			.attr('stroke', 'currentColor')
			.attr('stroke-opacity', 0.25)
			.attr('stroke-width', 1.25)
			.selectAll('path')
			.data(root.links())
			.join('path')
			.attr('d', diagonal as unknown as (d: d3.HierarchyLink<TreeDatum>) => string);

		const nodes = svg
			.append('g')
			.selectAll('g')
			.data(root.descendants())
			.join('g')
			.attr('transform', (d) => `translate(${d.y},${d.x})`);

		nodes
			.append('circle')
			.attr('r', (d) => (d.data.kind === 'root' ? 3.5 : 2.5))
			.attr('fill', (d) => {
				if (d.data.kind === 'root') return 'var(--theme-primary)';
				if (d.data.kind === 'branch') return 'var(--theme-text)';
				return 'var(--theme-text-muted)';
			});

		nodes
			.append('text')
			.attr('dy', '0.31em')
			.attr('x', 6)
			.attr('text-anchor', 'start')
			.attr('fill', 'var(--theme-text)')
			.attr('font-weight', (d) => (d.data.kind === 'root' ? 600 : 400))
			.text((d) => d.data.display)
			.attr('paint-order', 'stroke')
			.attr('stroke', 'var(--theme-background)')
			.attr('stroke-width', 3)
			.attr('stroke-linejoin', 'round');
	});
</script>

<div class="value-tree">
	<svg bind:this={svgEl}></svg>
</div>

<style lang="scss">
	.value-tree {
		overflow-x: auto;
		color: var(--theme-text);
	}
</style>
