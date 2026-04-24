<script lang="ts">
  interface Props {
    text?: string;
    align?: 'left' | 'center' | 'right';
    weight?: 'normal' | 'bold';
    size?: 'sm' | 'md' | 'lg' | 'xl';
  }

  let { text = '', align = 'left', weight = 'normal', size = 'md' }: Props = $props();

  const sizeMap = { sm: '0.875rem', md: '1rem', lg: '1.25rem', xl: '1.75rem' };
</script>

<div
  class="label"
  style:text-align={align}
  style:font-weight={weight}
  style:font-size={sizeMap[size]}
>
  {text}
</div>

<style lang="scss">
  .label {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    color: var(--theme-text);
    padding: 0 0.25rem;
  }
</style>
