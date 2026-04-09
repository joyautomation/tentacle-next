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

export const load: PageLoad = async () => {
  const [desiredResult, statusResult] = await Promise.all([
    api<DesiredService[]>('/orchestrator/desired-services'),
    api<ServiceStatus[]>('/orchestrator/service-statuses'),
  ]);

  return {
    desiredServices: desiredResult.data ?? [],
    serviceStatuses: statusResult.data ?? [],
  };
};
