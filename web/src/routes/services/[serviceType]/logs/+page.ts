import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
  serviceType: string;
  moduleId: string;
  logger: string | null;
}

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  try {
    const result = await api<LogEntry[]>(`/services/${serviceType}/logs`);

    return {
      serviceType,
      initialLogs: result.data ?? [],
      error: result.error?.error ?? null,
    };
  } catch (e) {
    return {
      serviceType,
      initialLogs: [],
      error: e instanceof Error ? e.message : 'Failed to load logs',
    };
  }
};
