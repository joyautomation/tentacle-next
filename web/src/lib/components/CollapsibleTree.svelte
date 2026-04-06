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
    const marginTop = 10;
    const marginRight = 10;
    const marginBottom = 10;
    const marginLeft = 40;

    const root = d3.hierarchy(currentData) as d3.HierarchyNode<HierarchyNode> & {
      x0?: number; y0?: number; id?: number;
      _children?: d3.HierarchyNode<HierarchyNode>[] | null;
    };
    const dx = 20;
    const dy = (width - marginRight - marginLeft) / (1 + root.height);

    const tree = d3.tree<HierarchyNode>().nodeSize([dx, dy]);
    const diagonal = d3.linkHorizontal<any, any>().x((d: any) => d.y).y((d: any) => d.x);

    // Read theme colors from CSS custom properties
    const style = getComputedStyle(container);
    const isDark = style.getPropertyValue('--theme-surface').trim() !== '';

    const svg = d3
      .select(container)
      .append('svg')
      .attr('width', width)
      .attr('height', dx)
      .attr('viewBox', [-marginLeft, -marginTop, width, dx].join(' '))
      .style('max-width', '100%')
      .style('height', 'auto')
      .style('font', "10px 'IBM Plex Mono', monospace")
      .style('user-select', 'none');

    const gLink = svg
      .append('g')
      .attr('fill', 'none')
      .style('stroke', 'var(--theme-text-muted)')
      .attr('stroke-opacity', 0.4)
      .attr('stroke-width', 1.5);

    const gNode = svg
      .append('g')
      .attr('cursor', 'pointer')
      .attr('pointer-events', 'all');

    function update(event: any, source: any) {
      const duration = event?.altKey ? 2500 : 250;
      const nodes = root.descendants().reverse();
      const links = root.links();

      tree(root);

      let left = root;
      let right = root;
      root.eachBefore((node: any) => {
        if (node.x < (left as any).x) left = node;
        if (node.x > (right as any).x) right = node;
      });

      const height = (right as any).x - (left as any).x + marginTop + marginBottom;

      const transition = svg
        .transition()
        .duration(duration)
        .attr('height', height)
        .attr('viewBox', [-marginLeft, (left as any).x - marginTop, width, height].join(' '))
        .tween(
          'resize',
          window.ResizeObserver ? null : (() => () => svg.dispatch('toggle')) as any
        );

      const node = gNode.selectAll<SVGGElement, any>('g').data(nodes, (d: any) => d.id);

      const nodeEnter = node
        .enter()
        .append('g')
        .attr('transform', () => `translate(${source.y0},${source.x0})`)
        .attr('fill-opacity', 0)
        .attr('stroke-opacity', 0)
        .on('click', (event: MouseEvent, d: any) => {
          d.children = d.children ? null : d._children;
          update(event, d);
        });

      nodeEnter
        .append('circle')
        .attr('r', 2.5)
        .attr('fill', (d: any) => (d._children ? 'var(--theme-text)' : 'var(--theme-text-muted)'))
        .attr('stroke-width', 10);

      nodeEnter
        .append('text')
        .attr('dy', '0.31em')
        .attr('x', (d: any) => (d._children ? -6 : 6))
        .attr('text-anchor', (d: any) => (d._children ? 'end' : 'start'))
        .text((d: any) => d.data.name)
        .attr('stroke-linejoin', 'round')
        .attr('stroke-width', 3)
        .style('stroke', 'var(--theme-bg, var(--color-gray-950, #0a0a0a))')
        .style('fill', 'var(--theme-text)')
        .attr('paint-order', 'stroke');

      node
        .merge(nodeEnter as any)
        .transition(transition as any)
        .attr('transform', (d: any) => `translate(${d.y},${d.x})`)
        .attr('fill-opacity', 1)
        .attr('stroke-opacity', 1);

      node
        .exit()
        .transition(transition as any)
        .remove()
        .attr('transform', () => `translate(${source.y},${source.x})`)
        .attr('fill-opacity', 0)
        .attr('stroke-opacity', 0);

      const link = gLink.selectAll<SVGPathElement, any>('path').data(links, (d: any) => d.target.id);

      const linkEnter = link
        .enter()
        .append('path')
        .attr('d', () => {
          const o = { x: source.x0, y: source.y0 };
          return diagonal({ source: o, target: o } as any);
        });

      link
        .merge(linkEnter as any)
        .transition(transition as any)
        .attr('d', diagonal as any);

      link
        .exit()
        .transition(transition as any)
        .remove()
        .attr('d', () => {
          const o = { x: source.x, y: source.y };
          return diagonal({ source: o, target: o } as any);
        });

      root.eachBefore((d: any) => {
        d.x0 = d.x;
        d.y0 = d.y;
      });
    }

    // Initialize: assign IDs and collapse all except root children
    (root as any).x0 = dy / 2;
    (root as any).y0 = 0;
    root.descendants().forEach((d: any, i: number) => {
      d.id = i;
      d._children = d.children;
      if (d.depth > 0) d.children = null;
    });

    update(null, root);
  });
</script>

<div class="tree-container" bind:this={container}></div>

<style>
  .tree-container {
    width: 100%;
    overflow-x: auto;
  }
</style>
