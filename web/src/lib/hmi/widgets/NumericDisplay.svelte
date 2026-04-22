<script lang="ts">
  interface Props {
    label?: string;
    value?: unknown;
    units?: string;
    precision?: number;
  }

  let { label = '', value, units = '', precision = 2 }: Props = $props();

  const display = $derived.by(() => {
    if (value == null) return '—';
    const n = typeof value === 'number' ? value : Number(value);
    if (Number.isFinite(n)) return n.toFixed(precision);
    return String(value);
  });
</script>

<div class="numeric">
  {#if label}
    <div class="label">{label}</div>
  {/if}
  <div class="value">
    <span class="number">{display}</span>
    {#if units}<span class="units">{units}</span>{/if}
  </div>
</div>

<style lang="scss">
  .numeric {
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    justify-content: center;
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
    margin-bottom: 0.125rem;
    white-space: nowrap;
    text-overflow: ellipsis;
    overflow: hidden;
  }
  .value {
    display: flex;
    align-items: baseline;
    gap: 0.25rem;
    font-family: 'IBM Plex Mono', monospace;
  }
  .number {
    font-size: 1.5rem;
    font-weight: 600;
  }
  .units {
    font-size: 0.875rem;
    color: var(--theme-text-muted);
  }
</style>
