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
  let prevValue = value;

  // Only sync from parent when the numeric value actually changes externally
  $effect.pre(() => {
    if (value !== prevValue) {
      text = hexDisplay(value);
      prevValue = value;
    }
  });

  function handleBlur() {
    const parsed = parseHex(text);
    // Only reformat if the text is empty or not a valid hex-looking string
    if (text.trim() === '') {
      text = hexDisplay(parsed);
    }
    prevValue = parsed;
    onchange(parsed);
  }
</script>

<input type="text" bind:value={text} {placeholder} onblur={handleBlur} />

<style>
  input {
    padding: 0.5rem;
    font-size: 0.8125rem;
    font-family: var(--font-mono);
    background: var(--theme-bg);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text);
    width: 100%;
    box-sizing: border-box;
  }
</style>
