<script lang="ts">
  interface Props {
    label?: string;
    value?: unknown;
    min?: number;
    max?: number;
    units?: string;
    color?: string;
  }

  let { label = '', value, min = 0, max = 100, units = '', color = 'var(--theme-accent, #3b82f6)' }: Props = $props();

  const numeric = $derived.by(() => {
    if (value == null) return null;
    const n = typeof value === 'number' ? value : Number(value);
    return Number.isFinite(n) ? n : null;
  });

  const pct = $derived.by(() => {
    if (numeric == null) return 0;
    const range = max - min;
    if (range <= 0) return 0;
    return Math.max(0, Math.min(100, ((numeric - min) / range) * 100));
  });
</script>

<div class="bar-widget">
  {#if label}<div class="label">{label}</div>{/if}
  <div class="track">
    <div class="fill" style:width="{pct}%" style:background={color}></div>
  </div>
  <div class="readout">
    <span>{numeric != null ? numeric.toFixed(1) : '—'}{units ? ` ${units}` : ''}</span>
    <span class="muted">{min}–{max}</span>
  </div>
</div>

<style lang="scss">
  .bar-widget {
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0.5rem 0.75rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text);
    overflow: hidden;
  }
  .label {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    white-space: nowrap;
    text-overflow: ellipsis;
    overflow: hidden;
  }
  .track {
    flex: 1;
    min-height: 0.5rem;
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-sm, 4px);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    transition: width 0.2s ease-out;
  }
  .readout {
    display: flex;
    justify-content: space-between;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
  }
  .muted {
    color: var(--theme-text-muted);
  }
</style>
