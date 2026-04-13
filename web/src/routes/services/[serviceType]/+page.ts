import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface Service {
  serviceType: string;
  moduleId: string;
  version: string | null;
  metadata: Record<string, unknown> | null;
  startedAt: string | number;
  enabled: boolean;
}

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  try {
    const result = await api<Service[]>('/services');

    if (result.error) {
      return {
        serviceType,
        instances: [],
        graphqlConnected: false,
        error: result.error.error,
        storeForwardStatus: null,
      };
    }

    const instances = (result.data ?? []).filter(
      (s) => s.serviceType === serviceType
    );

    return {
      serviceType,
      instances,
      graphqlConnected: true,
      error: null,
      storeForwardStatus: null,
    };
  } catch (e) {
    return {
      serviceType,
      instances: [],
      graphqlConnected: false,
      error: e instanceof Error ? e.message : 'Failed to connect',
      storeForwardStatus: null,
    };
  }
};
