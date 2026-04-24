/** Build a single <style> block for an HMI class library.
 *
 * `classes` is `{ [className]: cssBody }` where cssBody is the contents of
 * the rule (no surrounding braces). The output wraps each entry in a
 * selector. When `prefix` is non-empty the selector becomes
 * `.<prefix>__<name>`; otherwise just `.<name>` (app-wide).
 *
 * The user's CSS body is emitted verbatim. Component-private classes get
 * a unique prefix so they cannot collide across instances.
 */
export function compileScopedCss(
  classes: Record<string, string> | undefined,
  prefix: string,
  mode: 'compound' | 'descendant' = 'compound',
): string {
  if (!classes) return '';
  const parts: string[] = [];
  for (const name of Object.keys(classes).sort()) {
    const safeName = name.replace(/[^a-zA-Z0-9_-]/g, '');
    if (!safeName) continue;
    const body = (classes[name] ?? '').trim();
    if (!body) continue;
    // Prevent breaking out of the inline <style> block via </style>.
    const safeBody = body.replace(/<\/(style)/gi, '<\\/$1');
    const selector = !prefix
      ? `.${safeName}`
      : mode === 'descendant'
        ? `.${prefix} .${safeName}`
        : `.${prefix}__${safeName}`;
    parts.push(`${selector} {\n  ${safeBody.replace(/\n/g, '\n  ')}\n}`);
  }
  return parts.join('\n\n');
}

/** Resolve a list of class names from `widget.props.$classes` to actual
 * CSS class names that match the emitted selectors. Component classes
 * win over app classes when names collide. */
export function resolveClassList(
  names: string[] | undefined,
  appClasses: Record<string, string> | undefined,
  componentClasses: Record<string, string> | undefined,
  componentPrefix: string,
): string {
  if (!names || names.length === 0) return '';
  const out: string[] = [];
  for (const n of names) {
    if (componentClasses && componentClasses[n] !== undefined) {
      out.push(`${componentPrefix}__${n}`);
    } else if (appClasses && appClasses[n] !== undefined) {
      out.push(n);
    }
    // Unknown names are dropped silently — they may have been deleted.
  }
  return out.join(' ');
}
