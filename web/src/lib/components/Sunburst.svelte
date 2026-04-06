<script lang="ts">
  import * as d3 from 'd3';

  export type HierarchyNode = {
    name: string;
    children?: HierarchyNode[];
    value?: number;
    displayValue?: string;
  };

  let { data }: { data: HierarchyNode } = $props();

  let container: HTMLDivElement;
  let prevDataJson = '';

  // Keep a reference to the latest data so click handlers can look up
  // fresh displayValues without requiring an SVG rebuild.
  let latestData: HierarchyNode = data;

  // Track selected leaf for live value updates
  let selectedLeafPath: string[] | null = null;
  let centerValueRef: d3.Selection<SVGTextElement, unknown, null, undefined> | null = null;

  // Look up a fresh displayValue by walking the latest data tree
  function lookupDisplayValue(pathParts: string[]): string {
    let node: HierarchyNode | undefined = latestData;
    for (const part of pathParts) {
      node = node?.children?.find(c => c.name === part);
      if (!node) return '';
    }
    return node?.displayValue ?? '';
  }

  $effect(() => {
    // Always update the latest data reference (cheap, no SVG work)
    latestData = data;

    if (!container) return;
    const currentData = data;

    // Only compare structural data — exclude displayValue which changes
    // with live variable updates. This prevents constant SVG rebuilds
    // that would wipe navigation/selection state.
    const json = JSON.stringify(currentData, (key, value) =>
      key === 'displayValue' ? undefined : value
    );
    if (json === prevDataJson) {
      // Structure unchanged — refresh selected leaf value if any
      if (selectedLeafPath && centerValueRef) {
        centerValueRef.text(lookupDisplayValue(selectedLeafPath));
      }
      return;
    }
    prevDataJson = json;
    selectedLeafPath = null;

    d3.select(container).selectAll('*').interrupt().remove();

    if (!currentData?.children?.length) return;

    const width = 928;
    const centerRadius = width * 0.12; // smaller center hole
    const maxRadius = width / 2 - 2;
    const visibleLevels = 2; // arcVisible shows 2 depth rings
    const ringWidth = (maxRadius - centerRadius) / visibleLevels;

    // Map partition y-coordinates to pixel radii
    // y=1 maps to centerRadius, y=3 maps to maxRadius
    function yScale(y: number) {
      return centerRadius + (y - 1) * ringWidth;
    }

    const hierarchy = d3
      .hierarchy(currentData)
      .sum((d) => d.value ?? 0)
      .sort((a, b) => (b.value ?? 0) - (a.value ?? 0));

    const root = d3
      .partition<HierarchyNode>()
      .size([2 * Math.PI, hierarchy.height + 1])(hierarchy);

    root.each((d) => ((d as any).current = d));

    // Find the best depth for color differentiation — drill down
    // through single-child levels until we find variety
    let colorDepth = 1;
    let node: d3.HierarchyNode<HierarchyNode> | undefined = root;
    while (node && node.children && node.children.length === 1 && node.children[0].children) {
      colorDepth++;
      node = node.children[0];
    }
    const colorNodes = root.descendants().filter(d => d.depth === colorDepth);
    const color = d3.scaleOrdinal(
      d3.quantize(d3.interpolateRainbow, Math.max(colorNodes.length, 1) + 1)
    );

    const arc = d3
      .arc<any>()
      .startAngle((d) => d.x0)
      .endAngle((d) => d.x1)
      .padAngle((d) => Math.min((d.x1 - d.x0) / 2, 0.005))
      .padRadius(centerRadius * 1.5)
      .innerRadius((d) => yScale(d.y0))
      .outerRadius((d) => Math.max(yScale(d.y0), yScale(d.y1) - 1));

    const svg = d3
      .select(container)
      .append('svg')
      .attr('viewBox', `${-width / 2} ${-width / 2} ${width} ${width}`)
      .style('width', '100%')
      .style('height', '100%')
      .style('font', "10px 'IBM Plex Mono', monospace");

    const path = svg
      .append('g')
      .selectAll('path')
      .data(root.descendants().slice(1))
      .join('path')
      .attr('fill', (d) => {
        let node = d;
        while (node.depth > colorDepth) node = node.parent!;
        return color(node.data.name);
      })
      .attr('fill-opacity', (d) =>
        arcVisible((d as any).current) ? (d.children ? 0.6 : 0.4) : 0
      )
      .attr('pointer-events', (d) =>
        arcVisible((d as any).current) ? 'auto' : 'none'
      )
      .style('cursor', (d) => arcVisible((d as any).current) ? 'pointer' : 'default')
      .attr('d', (d) => arc((d as any).current));

    path.on('click', (event: MouseEvent, d) => {
      if (d.children) {
        clearLeafSelection();
        clicked(event, d);
      } else {
        selectLeaf(d);
      }
    });

    const label = svg
      .append('g')
      .attr('pointer-events', 'none')
      .attr('text-anchor', 'middle')
      .style('user-select', 'none')
      .selectAll('text')
      .data(root.descendants().slice(1))
      .join('text')
      .attr('dy', '0.35em')
      .attr('fill-opacity', (d) => +labelVisible((d as any).current))
      .style('fill', 'var(--theme-text)')
      .attr('transform', (d) => labelTransform((d as any).current))
      .text((d) => d.data.name);

    const parent = svg
      .append('circle')
      .datum(root)
      .attr('r', centerRadius)
      .attr('fill', 'none')
      .attr('pointer-events', 'all')
      .style('cursor', 'pointer')
      .on('click', (event: MouseEvent, d) => {
        clearLeafSelection();
        clicked(event, d);
      });

    const centerNameLabel = svg
      .append('text')
      .attr('text-anchor', 'middle')
      .attr('dy', '0.35em')
      .style('fill', 'var(--theme-text-muted)')
      .style('font-size', '14px')
      .style('font-weight', '500')
      .style('pointer-events', 'none')
      .text(root.data.name);

    const centerValueLabel = svg
      .append('text')
      .attr('text-anchor', 'middle')
      .attr('dy', '1.5em')
      .style('fill', 'var(--theme-text)')
      .style('font-size', '18px')
      .style('font-weight', '600')
      .style('pointer-events', 'none')
      .text('');

    // Store ref for live value updates from the outer $effect
    centerValueRef = centerValueLabel;

    function selectLeaf(d: d3.HierarchyRectangularNode<HierarchyNode>) {
      // Build path from ancestors (skip root) and look up fresh value
      const pathParts = d.ancestors().map(a => a.data.name).reverse().slice(1);
      selectedLeafPath = pathParts;
      const freshValue = lookupDisplayValue(pathParts);

      centerNameLabel.attr('dy', '-0.3em').text(d.data.name);
      centerValueLabel.text(freshValue);
      path
        .attr('stroke', (p) => p === d ? 'var(--theme-text)' : 'none')
        .attr('stroke-width', (p) => p === d ? 2 : 0);
    }

    function clearLeafSelection() {
      selectedLeafPath = null;
      centerValueLabel.text('');
      path.attr('stroke', 'none').attr('stroke-width', 0);
    }

    function clicked(
      event: MouseEvent,
      p: d3.HierarchyRectangularNode<HierarchyNode>
    ) {
      parent.datum(p.parent || root);
      centerNameLabel.attr('dy', '0.35em').text(p.data.name);
      centerValueLabel.text('');

      root.each(
        (d) =>
          ((d as any).target = {
            x0:
              Math.max(0, Math.min(1, (d.x0 - p.x0) / (p.x1 - p.x0))) *
              2 *
              Math.PI,
            x1:
              Math.max(0, Math.min(1, (d.x1 - p.x0) / (p.x1 - p.x0))) *
              2 *
              Math.PI,
            y0: Math.max(0, d.y0 - p.depth),
            y1: Math.max(0, d.y1 - p.depth),
          })
      );

      const t = svg.transition().duration(event.altKey ? 7500 : 750);

      path
        .transition(t as any)
        .tween('data', (d) => {
          const i = d3.interpolate((d as any).current, (d as any).target);
          return (t: number) => ((d as any).current = i(t));
        })
        .filter(function (d) {
          return (
            +((this as Element).getAttribute('fill-opacity') ?? 0) ||
            arcVisible((d as any).target)
          );
        })
        .attr('fill-opacity', (d) =>
          arcVisible((d as any).target) ? (d.children ? 0.6 : 0.4) : 0
        )
        .attr('pointer-events', (d) =>
          arcVisible((d as any).target) ? 'auto' : 'none'
        )
        .style('cursor', (d) =>
          arcVisible((d as any).target) ? 'pointer' : 'default'
        )
        .attrTween('d', (d) => () => arc((d as any).current)!);

      label
        .filter(function (d) {
          return (
            +((this as Element).getAttribute('fill-opacity') ?? 0) ||
            labelVisible((d as any).target)
          );
        })
        .transition(t as any)
        .attr('fill-opacity', (d) => +labelVisible((d as any).target))
        .attrTween(
          'transform',
          (d) => () => labelTransform((d as any).current)
        );
    }

    function arcVisible(d: any) {
      return d.y1 <= 3 && d.y0 >= 1 && d.x1 > d.x0;
    }

    function labelVisible(d: any) {
      return (
        d.y1 <= 3 &&
        d.y0 >= 1 &&
        (d.y1 - d.y0) * (d.x1 - d.x0) > 0.03
      );
    }

    function labelTransform(d: any) {
      const x = ((d.x0 + d.x1) / 2) * (180 / Math.PI);
      const y = (yScale(d.y0) + yScale(d.y1)) / 2;
      return `rotate(${x - 90}) translate(${y},0) rotate(${x < 180 ? 0 : 180})`;
    }
  });
</script>

<div class="sunburst-container" bind:this={container}></div>

<style>
  .sunburst-container {
    width: 100%;
    max-height: calc(100vh - 12rem);
    aspect-ratio: 1;
    margin: 0 auto;
  }
</style>
