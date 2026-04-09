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

    // Pre-fetch store-forward status for MQTT service page
    let storeForwardStatus = null;
    if (serviceType === 'mqtt') {
      try {
        const sfResult = await api<unknown>('/mqtt/store-forward');
        storeForwardStatus = sfResult.data ?? null;
      } catch {
        /* ignore */
      }
    }

    return {
      serviceType,
      instances,
      graphqlConnected: true,
      error: null,
      storeForwardStatus,
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
