<script lang="ts">
  interface Props {
    label?: string;
    value?: unknown;
    /** Color when truthy. Falls back to a green theme accent. */
    onColor?: string;
    /** Color when falsy. */
    offColor?: string;
  }

  let {
    label = '',
    value,
    onColor = '#22c55e',
    offColor = 'var(--theme-border)',
  }: Props = $props();

  const isOn = $derived(!!value);
</script>

<div class="indicator">
  <div class="lamp" style:background={isOn ? onColor : offColor}></div>
  {#if label}<div class="label">{label}</div>{/if}
</div>

<style lang="scss">
  .indicator {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.25rem 0.5rem;
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text);
    overflow: hidden;
  }
  .lamp {
    flex-shrink: 0;
    width: 0.875rem;
    height: 0.875rem;
    border-radius: 50%;
    box-shadow: 0 0 0 2px var(--theme-background) inset;
    transition: background 0.15s;
  }
  .label {
    font-size: 0.875rem;
    white-space: nowrap;
    text-overflow: ellipsis;
    overflow: hidden;
  }
</style>
