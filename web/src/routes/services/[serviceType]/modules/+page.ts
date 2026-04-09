import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface ModuleInfo {
  moduleId: string;
  repo: string;
  description: string;
  category: string;
  runtime: string;
  requiredConfig?: { envVar: string; description: string; default?: string; required: boolean }[];
}

interface ServiceStatus {
  moduleId: string;
  installedVersions: string[];
  activeVersion: string;
  systemdState: string;
  reconcileState: string;
  runtime: string;
  category: string;
  repo: string;
  updatedAt: number;
}

export const load: PageLoad = async () => {
  try {
    const [modulesResult, statusesResult] = await Promise.all([
      api<ModuleInfo[]>('/orchestrator/modules'),
      api<ServiceStatus[]>('/orchestrator/service-statuses'),
    ]);

    return {
      modules: modulesResult.data ?? [],
      statuses: statusesResult.data ?? [],
      error: modulesResult.error?.error ?? statusesResult.error?.error ?? null,
    };
  } catch (e) {
    return {
      modules: [],
      statuses: [],
      error: e instanceof Error ? e.message : 'Failed to fetch modules',
    };
  }
};
