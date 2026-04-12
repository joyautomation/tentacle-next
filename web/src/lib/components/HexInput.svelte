<script lang="ts">
  interface Props {
    value: number;
    onchange: (value: number) => void;
    placeholder?: string;
  }

  let { value, onchange, placeholder = '0x0' }: Props = $props();

  function hexDisplay(n: number): string {
    return '0x' + n.toString(16).toUpperCase();
  }

  function parseHex(s: string): number {
    const trimmed = s.trim();
    if (trimmed === '' || trimmed === '0x' || trimmed === '0X') return 0;
    if (trimmed.startsWith('0x') || trimmed.startsWith('0X')) {
      const n = parseInt(trimmed, 16);
      return Number.isNaN(n) ? 0 : n;
    }
    const n = parseInt(trimmed);
    return Number.isNaN(n) ? 0 : n;
  }

  let text = $state(hexDisplay(value));

  // Sync from parent when value changes externally
  $effect(() => {
    text = hexDisplay(value);
  });

  function handleBlur() {
    const parsed = parseHex(text);
    text = hexDisplay(parsed);
    onchange(parsed);
  }
</script>

<input type="text" bind:value={text} {placeholder} onblur={handleBlur} />
