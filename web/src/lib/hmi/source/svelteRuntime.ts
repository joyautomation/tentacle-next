/** Compile Svelte source in the browser and mount it inside an arbitrary
 * target element.
 *
 * Svelte's compiled output is ESM that imports from `svelte/internal/client`
 * (and friends). We can't `import()` a blob URL with bare specifiers, so we
 * rewrite the import statements into `const` lookups against a small registry
 * pre-populated with the host page's already-loaded svelte modules. The
 * rewritten code is then evaluated as a `new Function(...)` body, which
 * returns the component constructor for `mount()` to consume.
 */

import { compile } from 'svelte/compiler';
import * as svelteRuntime from 'svelte';
import * as svelteInternalClient from 'svelte/internal/client';
import * as svelteDiscloseVersion from 'svelte/internal/disclose-version';
import * as svelteFlagsLegacy from 'svelte/internal/flags/legacy';

type Module = Record<string, unknown> & { default?: unknown };

// Pre-bundle every svelte sub-module the compiler can emit. Missing entries
// would 404 at runtime since the browser can't resolve bare specifiers.
const registry: Record<string, Module> = {
  'svelte': svelteRuntime as Module,
  'svelte/internal/client': svelteInternalClient as Module,
  'svelte/internal/disclose-version': svelteDiscloseVersion as Module,
  'svelte/internal/flags/legacy': svelteFlagsLegacy as Module,
};

function ensureModule(path: string): Module {
  return registry[path] ?? (registry[path] = {});
}

export interface CompileError {
  message: string;
  line?: number;
  column?: number;
}

export interface CompileResult {
  ok: true;
  Component: unknown;
}

export interface CompileFailure {
  ok: false;
  error: CompileError;
}

/** Compile the user's Svelte source into a mountable component constructor. */
export async function compileComponent(
  source: string,
): Promise<CompileResult | CompileFailure> {
  let js: string;
  try {
    const result = compile(source, {
      generate: 'client',
      dev: false,
      css: 'injected',
      filename: 'UserComponent.svelte',
      runes: true,
    });
    js = result.js.code;
  } catch (e: any) {
    return {
      ok: false,
      error: {
        message: e?.message ?? String(e),
        line: e?.position?.[0]?.line ?? e?.start?.line,
        column: e?.position?.[0]?.column ?? e?.start?.column,
      },
    };
  }

  // Stub any module the compiler references that we didn't statically
  // pre-bundle so the rewritten code still runs (returns an empty object).
  const importPaths = new Set<string>();
  const importRe = /from\s*['"]([^'"]+)['"]/g;
  let m: RegExpExecArray | null;
  while ((m = importRe.exec(js))) importPaths.add(m[1]);
  for (const p of importPaths) ensureModule(p);

  // Rewrite all import statements into local const lookups against the
  // registry, and convert the default export into a `return`.
  let code = js
    .replace(
      /import\s*\*\s*as\s+(\w+)\s+from\s*['"]([^'"]+)['"];?/g,
      (_: string, ns: string, path: string) =>
        `const ${ns} = __resolve(${JSON.stringify(path)});`,
    )
    .replace(
      /import\s*\{([^}]+)\}\s*from\s*['"]([^'"]+)['"];?/g,
      (_: string, names: string, path: string) =>
        `const {${names}} = __resolve(${JSON.stringify(path)});`,
    )
    .replace(
      /import\s+(\w+)\s+from\s*['"]([^'"]+)['"];?/g,
      (_: string, name: string, path: string) =>
        `const ${name} = __resolve(${JSON.stringify(path)}).default;`,
    )
    .replace(/import\s*['"]([^'"]+)['"];?/g, (_: string, path: string) =>
      `__resolve(${JSON.stringify(path)});`,
    );

  let exportName: string | null = null;
  code = code.replace(
    /export\s+default\s+function\s+(\w+)/,
    (_: string, name: string) => {
      exportName = name;
      return `function ${name}`;
    },
  );
  if (!exportName) {
    code = code.replace(
      /export\s+default\s+(\w+);?/,
      (_: string, name: string) => {
        exportName = name;
        return '';
      },
    );
  }
  if (!exportName) {
    return {
      ok: false,
      error: { message: 'Compiled output had no default export.' },
    };
  }

  const resolver = (path: string): Module => {
    const mod = registry[path];
    if (!mod) throw new Error(`Module not loaded: ${path}`);
    return mod;
  };

  try {
    // eslint-disable-next-line @typescript-eslint/no-implied-eval
    const factory = new Function('__resolve', `${code}\nreturn ${exportName};`);
    const Component = factory(resolver);
    return { ok: true, Component };
  } catch (e: any) {
    return {
      ok: false,
      error: { message: e?.message ?? String(e) },
    };
  }
}

/** Mount a previously compiled component on `target` with `props`. Returns
 * an unmount function. */
export function mountComponent(
  Component: unknown,
  target: HTMLElement,
  props: Record<string, unknown>,
): () => void {
  // svelte's `mount` is typed loosely on third-party constructors; we trust it.
  const instance = (svelteRuntime as any).mount(Component, { target, props });
  return () => {
    try {
      (svelteRuntime as any).unmount(instance);
    } catch {
      // ignore — component may have already torn down
    }
  };
}
