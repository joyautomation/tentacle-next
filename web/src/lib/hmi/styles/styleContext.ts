import { getContext, setContext } from 'svelte';
import { resolveClassList } from './cssScope';

export interface HmiStyleContext {
  /** App-wide classes (bare selectors like `.name`). */
  appClasses?: Record<string, string>;
  /** Active component scope, when rendering inside a `componentInstance`. */
  component?: {
    prefix: string;
    classes: Record<string, string>;
  };
}

const KEY = 'hmi-style';

export function setHmiStyleContext(ctx: HmiStyleContext): void {
  setContext(KEY, ctx);
}

export function getHmiStyleContext(): HmiStyleContext {
  return getContext(KEY) ?? {};
}

/** Resolve a widget's `$classes` against the active style context. Returns
 * a space-separated class string suitable for `class=` on a DOM element. */
export function widgetClassString(
  $classes: unknown,
  ctx: HmiStyleContext = getHmiStyleContext(),
): string {
  if (!Array.isArray($classes) || $classes.length === 0) return '';
  return resolveClassList(
    $classes as string[],
    ctx.appClasses,
    ctx.component?.classes,
    ctx.component?.prefix ?? '',
  );
}
