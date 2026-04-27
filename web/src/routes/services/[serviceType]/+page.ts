import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { FleetModule } from '$lib/types/fleet';

interface Service {
  serviceType: string;
  moduleId: string;
  version: string | null;
  metadata: Record<string, unknown> | null;
  startedAt: string | number;
  enabled: boolean;
}

export const load: PageLoad = async ({ params, url }) => {
  const { serviceType } = params;
  const target = url.searchParams.get('target');

  // Remote mode: read the named edge tentacle's desired services from gitops
  // via mantle. We can't show live runtime data (no per-module heartbeats
  // for remote nodes yet), so this drives the Overview's identity + the
  // running/enabled toggle only.
  if (target) {
    const [group, node] = target.split('/', 2);
    if (!group || !node) {
      return {
        serviceType,
        target,
        remoteGroup: '',
        remoteNode: '',
        remoteModule: null,
        instances: [],
        graphqlConnected: false,
        error: 'Invalid target',
        storeForwardStatus: null,
      };
    }
    const result = await api<{ services: FleetModule[] }>(
      `/fleet/nodes/${encodeURIComponent(group)}/${encodeURIComponent(node)}/services`,
    );
    const remoteModule =
      (result.data?.services ?? []).find((m) => m.id === serviceType) ?? null;
    return {
      serviceType,
      target,
      remoteGroup: group,
      remoteNode: node,
      remoteModule,
      instances: [],
      graphqlConnected: false,
      error: result.error?.error ?? null,
      storeForwardStatus: null,
    };
  }

  try {
    const result = await api<Service[]>('/services');

    if (result.error) {
      return {
        serviceType,
        target: null,
        remoteGroup: '',
        remoteNode: '',
        remoteModule: null,
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
      target: null,
      remoteGroup: '',
      remoteNode: '',
      remoteModule: null,
      instances,
      graphqlConnected: true,
      error: null,
      storeForwardStatus: null,
    };
  } catch (e) {
    return {
      serviceType,
      target: null,
      remoteGroup: '',
      remoteNode: '',
      remoteModule: null,
      instances: [],
      graphqlConnected: false,
      error: e instanceof Error ? e.message : 'Failed to connect',
      storeForwardStatus: null,
    };
  }
};
