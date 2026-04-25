// Mirror of internal/gitops/repostore.go sanitize: replace any char outside
// [A-Za-z0-9_-] with '_'. Keep in lockstep — server-side name must match
// what mantle accepts so bare-repo creation lands at the right path.
export function sanitizeSegment(s: string): string {
  return s.replace(/[^A-Za-z0-9_-]/g, '_');
}

export function mantleRepoName(group: string, node: string): string {
  return `${sanitizeSegment(group)}--${sanitizeSegment(node)}`;
}

export function mantleRepoUrl(mantleUrl: string, group: string, node: string): string {
  const base = (mantleUrl || '').replace(/\/+$/, '');
  return `${base}/git/${mantleRepoName(group, node)}.git`;
}
