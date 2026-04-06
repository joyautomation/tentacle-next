import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface ConfigEntry {
  key: string;
  envVar: string;
  value: string;
  moduleId: string;
}

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  try {
    const result = await api<ConfigEntry[]>(`/config/${serviceType}`);

    return {
      serviceType,
      config: result.data ?? [],
      error: result.error?.error ?? null,
    };
  } catch (e) {
    return {
      serviceType,
      config: [],
      error: e instanceof Error ? e.message : 'Failed to load config',
    };
  }
};
