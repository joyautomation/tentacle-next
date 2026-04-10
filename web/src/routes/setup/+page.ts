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

export const load: PageLoad = async () => {
  const [desiredResult, statusResult, mqttConfigResult] = await Promise.all([
    api<DesiredService[]>('/orchestrator/desired-services'),
    api<ServiceStatus[]>('/orchestrator/service-statuses'),
    api<ConfigEntry[]>('/config/mqtt'),
  ]);

  return {
    desiredServices: desiredResult.data ?? [],
    serviceStatuses: statusResult.data ?? [],
    mqttConfig: mqttConfigResult.data ?? [],
  };
};
