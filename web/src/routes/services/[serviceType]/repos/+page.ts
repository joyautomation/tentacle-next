import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface RepoTree {
  name: string;
  files: string[];
  error?: string;
}

export const load: PageLoad = async () => {
  const result = await api<{ repos: RepoTree[] }>('/gitops/tree');
  if (result.error) {
    return { repos: [] as RepoTree[], error: result.error.error };
  }
  return { repos: result.data?.repos ?? [], error: null };
};
