import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

export interface ModuleInfo {
  moduleId: string;
  repo: string;
  description: string;
  category: string;
  runtime: string;
  requiredConfig?: { envVar: string; description: string; default?: string; required: boolean }[];
}

export interface ServiceStatus {
  moduleId: string;
  installedVersions: string[];
  activeVersion: string;
  systemdState: string;
  reconcileState: string;
  runtime: string;
  category: string;
  repo: string;
  updatedAt: number;
  lastError?: string;
}

export interface DesiredService {
  moduleId: string;
  version: string;
  running: boolean;
  updatedAt: number;
}

export const load: PageLoad = async () => {
  try {
    const [modulesResult, statusesResult, desiredResult] = await Promise.all([
      api<ModuleInfo[]>('/orchestrator/modules'),
      api<ServiceStatus[]>('/orchestrator/service-statuses'),
      api<DesiredService[]>('/orchestrator/desired-services'),
    ]);

    return {
      modules: modulesResult.data ?? [],
      statuses: statusesResult.data ?? [],
      desired: desiredResult.data ?? [],
      error: modulesResult.error?.error ?? statusesResult.error?.error ?? desiredResult.error?.error ?? null,
    };
  } catch (e) {
    return {
      modules: [],
      statuses: [],
      desired: [],
      error: e instanceof Error ? e.message : 'Failed to fetch modules',
    };
  }
};
