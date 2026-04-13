import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface DesiredService {
  moduleId: string;
  version: string;
  running: boolean;
}

interface ServiceStatus {
  moduleId: string;
  systemdState: string;
  reconcileState: string;
}

interface ConfigEntry {
  moduleId: string;
  envVar: string;
  value: string;
}

interface ModuleInfo {
  moduleId: string;
  experimental?: boolean;
}

export const load: PageLoad = async () => {
  const [desiredResult, statusResult, mqttConfigResult, gitopsConfigResult, modulesResult] = await Promise.all([
    api<DesiredService[]>('/orchestrator/desired-services'),
    api<ServiceStatus[]>('/orchestrator/service-statuses'),
    api<ConfigEntry[]>('/config/mqtt'),
    api<ConfigEntry[]>('/config/gitops'),
    api<ModuleInfo[]>('/orchestrator/modules'),
  ]);

  return {
    desiredServices: desiredResult.data ?? [],
    serviceStatuses: statusResult.data ?? [],
    mqttConfig: mqttConfigResult.data ?? [],
    gitopsConfig: gitopsConfigResult.data ?? [],
    modules: modulesResult.data ?? [],
  };
};
