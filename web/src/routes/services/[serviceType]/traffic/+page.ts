import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface NatsTrafficEntry {
  timestamp: string;
  subject: string;
  size: number;
  payload: string;
}

export const load: PageLoad = async () => {
  try {
    const result = await api<NatsTrafficEntry[]>('/nats/traffic');

    return {
      initialTraffic: result.data ?? [],
      error: result.error?.error ?? null,
    };
  } catch (e) {
    return {
      initialTraffic: [],
      error: e instanceof Error ? e.message : 'Failed to load traffic',
    };
  }
};
