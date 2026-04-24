/** Tiny markup tools for HMI Svelte source.
 *
 * The editor only stores markup — a leading `<script>` block is auto-
 * injected at compile time. To make the preview a drop target for class
 * chips we tag every element open tag with `data-hmi-el="N"` so the drop
 * handler can map a DOM element back to its position in the source.
 *
 * The parser here is intentionally small: it walks the source character
 * by character, respecting `"…"`, `'…'`, and `{…}` so it does not get
 * confused by Svelte expressions or quoted attribute values. It does NOT
 * try to understand block tags (`{#if}`, `{#each}`) — those don't start
 * with `<`, so they are simply skipped over by the outer loop.
 */

export interface ElementSpan {
  tagName: string;
  /** Index of the leading `<`. */
  openStart: number;
  /** Index of the closing `>` of the open tag (inclusive). */
  openEnd: number;
  /** True if the tag closed itself (`<br />` style). */
  selfClosing: boolean;
}

/** Enumerate every element open tag in source order. */
export function findElementOpenTags(source: string): ElementSpan[] {
  const result: ElementSpan[] = [];
  let i = 0;
  while (i < source.length) {
    const c = source[i];
    if (c === '<') {
      // Skip comments outright.
      if (source.startsWith('<!--', i)) {
        const end = source.indexOf('-->', i + 4);
        i = end < 0 ? source.length : end + 3;
        continue;
      }
      // Skip CDATA / DOCTYPE if anyone writes one.
      if (source[i + 1] === '!' || source[i + 1] === '?') {
        const end = source.indexOf('>', i + 2);
        i = end < 0 ? source.length : end + 1;
        continue;
      }
      // Closing tag — skip ahead to its `>`.
      if (source[i + 1] === '/') {
        const end = source.indexOf('>', i + 2);
        i = end < 0 ? source.length : end + 1;
        continue;
      }
      // Opening tag (or component).
      const nameMatch = source.slice(i + 1).match(/^([a-zA-Z][a-zA-Z0-9-]*)/);
      if (!nameMatch) {
        i++;
        continue;
      }
      const tagName = nameMatch[1];
      let j = i + 1 + nameMatch[1].length;
      let inQuote: '"' | "'" | null = null;
      let braceDepth = 0;
      while (j < source.length) {
        const cj = source[j];
        if (inQuote) {
          if (cj === inQuote) inQuote = null;
        } else if (braceDepth > 0) {
          if (cj === '{') braceDepth++;
          else if (cj === '}') braceDepth--;
        } else {
          if (cj === '"' || cj === "'") inQuote = cj;
          else if (cj === '{') braceDepth++;
          else if (cj === '>') break;
        }
        j++;
      }
      if (j >= source.length) break;
      const selfClosing = source[j - 1] === '/';
      result.push({ tagName, openStart: i, openEnd: j, selfClosing });
      i = j + 1;
      continue;
    }
    i++;
  }
  return result;
}

/** Inject `data-hmi-el="N"` on every element open tag so the preview can
 * map clicks back to source positions. Returns the augmented markup. */
export function injectMarkers(source: string): string {
  const tags = findElementOpenTags(source);
  if (tags.length === 0) return source;
  // Splice from the back so earlier offsets stay valid.
  let out = source;
  for (let n = tags.length - 1; n >= 0; n--) {
    const t = tags[n];
    // Skip if a marker is already present (idempotent — useful when the
    // same source is re-augmented across recompiles).
    const tagText = out.slice(t.openStart, t.openEnd + 1);
    if (/\bdata-hmi-el\s*=/.test(tagText)) continue;
    // Insert just before the closing `>` (or before `/>` for self-closing).
    const insertAt = t.selfClosing ? t.openEnd - 1 : t.openEnd;
    out = out.slice(0, insertAt) + ` data-hmi-el="${n}"` + out.slice(insertAt);
  }
  return out;
}

/** Append a class name to the Nth element's `class="…"` attribute. If no
 * `class` attribute exists, one is created. Returns the modified source.
 * Returns null when `idx` is out of range. */
export function addClassToElement(
  source: string,
  idx: number,
  className: string,
): string | null {
  const tags = findElementOpenTags(source);
  const t = tags[idx];
  if (!t) return null;
  const tagText = source.slice(t.openStart, t.openEnd + 1);
  const classRe = /(\bclass\s*=\s*")([^"]*)(")/;
  const m = classRe.exec(tagText);
  let nextTag: string;
  if (m) {
    const existing = m[2].split(/\s+/).filter(Boolean);
    if (existing.includes(className)) return source; // already applied
    existing.push(className);
    nextTag =
      tagText.slice(0, m.index) +
      m[1] +
      existing.join(' ') +
      m[3] +
      tagText.slice(m.index + m[0].length);
  } else {
    // Insert ` class="X"` just before the closing `>` (or `/>`).
    const insertAt = t.selfClosing ? tagText.length - 2 : tagText.length - 1;
    nextTag =
      tagText.slice(0, insertAt) + ` class="${className}"` + tagText.slice(insertAt);
  }
  return source.slice(0, t.openStart) + nextTag + source.slice(t.openEnd + 1);
}

/** Merge inline `style="…"` declarations on the Nth element. New keys are
 * added; existing keys are overwritten; keys with `undefined` or empty
 * values are removed. If the tag has no `style` attribute, one is created.
 * Returns null when `idx` is out of range. */
export function setInlineStyleProps(
  source: string,
  idx: number,
  props: Record<string, string | undefined>,
): string | null {
  const tags = findElementOpenTags(source);
  const t = tags[idx];
  if (!t) return null;
  const tagText = source.slice(t.openStart, t.openEnd + 1);
  const styleRe = /(\bstyle\s*=\s*")([^"]*)(")/;
  const m = styleRe.exec(tagText);
  const map = new Map<string, string>();
  if (m) {
    for (const decl of m[2].split(';')) {
      const i = decl.indexOf(':');
      if (i < 0) continue;
      const k = decl.slice(0, i).trim();
      const v = decl.slice(i + 1).trim();
      if (k && v) map.set(k, v);
    }
  }
  for (const [k, v] of Object.entries(props)) {
    if (v === undefined || v === '') map.delete(k);
    else map.set(k, v);
  }
  const next = [...map.entries()].map(([k, v]) => `${k}: ${v}`).join('; ');
  let nextTag: string;
  if (m) {
    if (next === '') {
      // Drop the style="" attribute and a leading space.
      const before = tagText.slice(0, m.index).replace(/\s+$/, '');
      const after = tagText.slice(m.index + m[0].length);
      nextTag = before + (after.startsWith('>') || after.startsWith('/') ? '' : ' ') + after;
    } else {
      nextTag =
        tagText.slice(0, m.index) +
        m[1] + next + m[3] +
        tagText.slice(m.index + m[0].length);
    }
  } else {
    if (next === '') return source;
    const insertAt = t.selfClosing ? tagText.length - 2 : tagText.length - 1;
    nextTag = tagText.slice(0, insertAt) + ` style="${next}"` + tagText.slice(insertAt);
  }
  return source.slice(0, t.openStart) + nextTag + source.slice(t.openEnd + 1);
}

/** Parse the inline `style="…"` declarations on the Nth element into a
 * key→value map. Returns an empty map if no style attribute is present,
 * or null when `idx` is out of range. */
export function getInlineStyleProps(
  source: string,
  idx: number,
): Record<string, string> | null {
  const tags = findElementOpenTags(source);
  const t = tags[idx];
  if (!t) return null;
  const tagText = source.slice(t.openStart, t.openEnd + 1);
  const m = /\bstyle\s*=\s*"([^"]*)"/.exec(tagText);
  const out: Record<string, string> = {};
  if (!m) return out;
  for (const decl of m[1].split(';')) {
    const i = decl.indexOf(':');
    if (i < 0) continue;
    const k = decl.slice(0, i).trim();
    const v = decl.slice(i + 1).trim();
    if (k && v) out[k] = v;
  }
  return out;
}

/** Pull a leading `<script>…</script>` block out of source. Returns the
 * inner script text (trimmed) and the markup with the block removed. */
export function stripScriptBlock(source: string): { script: string; markup: string } {
  const m = source.match(/<script\b[^>]*>([\s\S]*?)<\/script>\s*/);
  if (!m || m.index === undefined) return { script: '', markup: source };
  return {
    script: m[1].trim(),
    markup: source.slice(0, m.index) + source.slice(m.index + m[0].length),
  };
}
