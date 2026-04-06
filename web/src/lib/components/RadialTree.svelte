<script lang="ts">
  import * as d3 from 'd3';

  type HierarchyNode = {
    name: string;
    children?: HierarchyNode[];
    value?: number;
  };

  let { data }: { data: HierarchyNode } = $props();

  let container: HTMLDivElement;
  let prevDataJson = '';

  $effect(() => {
    if (!container) return;
    const currentData = data;
    const json = JSON.stringify(currentData);
    if (json === prevDataJson) return;
    prevDataJson = json;

    d3.select(container).selectAll('*').interrupt().remove();

    if (!currentData?.children?.length) return;

    const width = 928;
    const height = width;
    const cx = width * 0.5;
    const cy = height * 0.5;
    const radius = Math.min(width, height) / 2 - 30;

    const tree = d3
      .tree<HierarchyNode>()
      .size([2 * Math.PI, radius])
      .separation((a, b) => (a.parent === b.parent ? 1 : 2) / a.depth);

    const root = tree(
      d3
        .hierarchy(currentData)
        .sort((a, b) => d3.ascending(a.data.name, b.data.name))
    );

    const svg = d3
      .select(container)
      .append('svg')
      .attr('width', width)
      .attr('height', height)
      .attr('viewBox', [-cx, -cy, width, height].join(' '))
      .style('width', '100%')
      .style('height', 'auto')
      .style('font', "10px 'IBM Plex Mono', monospace");

    // Links
    svg
      .append('g')
      .attr('fill', 'none')
      .style('stroke', 'var(--theme-text-muted)')
      .attr('stroke-opacity', 0.4)
      .attr('stroke-width', 1.5)
      .selectAll('path')
      .data(root.links())
      .join('path')
      .attr(
        'd',
        d3
          .linkRadial<any, any>()
          .angle((d: any) => d.x)
          .radius((d: any) => d.y) as any
      );

    // Nodes
    svg
      .append('g')
      .selectAll('circle')
      .data(root.descendants())
      .join('circle')
      .attr(
        'transform',
        (d) =>
          `rotate(${(d.x * 180) / Math.PI - 90}) translate(${d.y},0)`
      )
      .attr('fill', (d) =>
        d.children ? 'var(--theme-text)' : 'var(--theme-text-muted)'
      )
      .attr('r', 2.5);

    // Labels
    svg
      .append('g')
      .attr('stroke-linejoin', 'round')
      .attr('stroke-width', 3)
      .selectAll('text')
      .data(root.descendants())
      .join('text')
      .attr(
        'transform',
        (d) =>
          `rotate(${(d.x * 180) / Math.PI - 90}) translate(${d.y},0) rotate(${d.x >= Math.PI ? 180 : 0})`
      )
      .attr('dy', '0.31em')
      .attr('x', (d) => (d.x < Math.PI === !d.children ? 6 : -6))
      .attr('text-anchor', (d) =>
        d.x < Math.PI === !d.children ? 'start' : 'end'
      )
      .attr('paint-order', 'stroke')
      .style('stroke', 'var(--theme-bg, var(--color-gray-950, #0a0a0a))')
      .style('fill', 'var(--theme-text)')
      .text((d) => d.data.name);
  });
</script>

<div class="radial-tree-container" bind:this={container}></div>

<style>
  .radial-tree-container {
    width: 100%;
    max-height: calc(100vh - 12rem);
    aspect-ratio: 1;
    margin: 0 auto;
  }
</style>
