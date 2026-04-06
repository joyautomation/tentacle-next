<script lang="ts">
  import { onMount } from 'svelte';
  import * as d3 from 'd3';

  type TreeNode = {
    name: string;
    children?: TreeNode[];
    value?: number;
    displayValue?: string;
  };

  let { data }: { data: TreeNode } = $props();
  let container: HTMLDivElement;

  function countLeaves(node: TreeNode): number {
    if (!node.children?.length) return 1;
    return node.children.reduce((sum, c) => sum + countLeaves(c), 0);
  }

  function render() {
    if (!container || !data?.children?.length) return;

    d3.select(container).selectAll('*').remove();

    const grid = d3.select(container)
      .append('div')
      .attr('class', 'tidy-grid');

    // Flatten to get renderable groups
    // For groups with many children (e.g., template types with 10+ instances),
    // split into individual instance trees. For small groups, keep as one tree.
    const MAX_LEAVES_PER_TREE = 30;
    const groups: { title: string; node: TreeNode }[] = [];
    for (const topChild of (data.children ?? [])) {
      for (const group of (topChild.children ?? [])) {
        if (!group.children?.length && !group.value) continue;

        // Count total leaf nodes
        const leafCount = countLeaves(group);
        if (leafCount > MAX_LEAVES_PER_TREE && group.children) {
          // Too dense — split each child into its own tree
          for (const child of group.children) {
            groups.push({ title: child.name, node: child });
          }
        } else {
          groups.push({ title: group.name, node: group });
        }
      }
    }

    if (groups.length === 0) return;

    for (const group of groups) {
      const cell = grid.append('div').attr('class', 'tidy-cell');
      renderRadialTree(cell, group.title, group.node);
    }
  }

  function renderRadialTree(
    container: d3.Selection<HTMLDivElement, unknown, null, undefined>,
    title: string,
    root: TreeNode
  ) {
    // Title
    container.append('div')
      .attr('class', 'tidy-cell-title')
      .text(title);

    const hierarchy = d3.hierarchy(root)
      .sort((a, b) => d3.ascending(a.data.name, b.data.name));

    const nodeCount = hierarchy.descendants().length;
    // Scale size based on node count
    const size = Math.min(400, Math.max(200, nodeCount * 15 + 100));
    const radius = size / 2 - 40;

    const tree = d3.tree<TreeNode>()
      .size([2 * Math.PI, radius])
      .separation((a, b) => (a.parent === b.parent ? 1 : 2) / (a.depth || 1));

    const treeRoot = tree(hierarchy);

    const svg = container.append('svg')
      .attr('width', size)
      .attr('height', size)
      .attr('viewBox', `${-size / 2} ${-size / 2} ${size} ${size}`)
      .style('width', '100%')
      .style('height', 'auto')
      .style('font', "10px 'IBM Plex Mono', monospace");

    // Links
    svg.append('g')
      .attr('fill', 'none')
      .attr('stroke', 'var(--theme-text-muted)')
      .attr('stroke-opacity', 0.3)
      .attr('stroke-width', 1.5)
      .selectAll('path')
      .data(treeRoot.links())
      .join('path')
      .attr('d', d3.linkRadial<any, any>()
        .angle((d: any) => d.x)
        .radius((d: any) => d.y) as any);

    // Nodes
    svg.append('g')
      .selectAll('circle')
      .data(treeRoot.descendants())
      .join('circle')
      .attr('transform', d => `rotate(${(d.x * 180) / Math.PI - 90}) translate(${d.y},0)`)
      .attr('fill', d => d.children
        ? 'var(--color-teal-500, #14b8a6)'
        : 'var(--theme-text-muted)')
      .attr('r', d => d.children ? 3.5 : 2.5);

    // Labels
    svg.append('g')
      .attr('stroke-linejoin', 'round')
      .attr('stroke-width', 3)
      .selectAll('text')
      .data(treeRoot.descendants())
      .join('text')
      .attr('transform', d =>
        `rotate(${(d.x * 180) / Math.PI - 90}) translate(${d.y},0) rotate(${d.x >= Math.PI ? 180 : 0})`)
      .attr('dy', '0.31em')
      .attr('x', d => (d.x < Math.PI === !d.children ? 6 : -6))
      .attr('text-anchor', d => (d.x < Math.PI === !d.children ? 'start' : 'end'))
      .attr('paint-order', 'stroke')
      .style('stroke', 'var(--theme-background)')
      .style('fill', 'var(--theme-text)')
      .text(d => {
        if (d.data.displayValue) return `${d.data.name}: ${d.data.displayValue}`;
        return d.data.name;
      });
  }

  $effect(() => {
    data;
    if (container) render();
  });

  onMount(() => {
    render();
  });
</script>

<div class="tidy-container" bind:this={container}></div>

<style lang="scss">
  .tidy-container {
    width: 100%;

    :global(.tidy-grid) {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
      gap: 1rem;
    }

    :global(.tidy-cell) {
      background: var(--theme-surface);
      border: 1px solid var(--theme-border);
      border-radius: var(--rounded-lg);
      padding: 0.75rem;
      display: flex;
      flex-direction: column;
      align-items: center;
    }

    :global(.tidy-cell-title) {
      font-size: 0.75rem;
      font-weight: 600;
      font-family: 'IBM Plex Mono', monospace;
      color: var(--theme-text-muted);
      margin-bottom: 0.5rem;
      align-self: flex-start;
    }
  }
</style>
