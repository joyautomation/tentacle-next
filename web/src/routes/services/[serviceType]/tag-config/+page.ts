import type { PageLoad } from './$types';
import { api, withTarget } from '$lib/api/client';
import type { GatewayConfig, BrowseCache, GatewayBrowseState } from '$lib/types/gateway';

export const load: PageLoad = async ({ params, url }) => {
  const { serviceType } = params;
  const target = url.searchParams.get('target') || null;

  if (serviceType === 'modbus') {
    try {
      const result = await api<GatewayConfig>(withTarget('/gateways/gateway', target));
      return {
        serviceType,
        target,
        gatewayConfig: result.data ?? null,
        browseCaches: [] as BrowseCache[],
        browseStates: [] as GatewayBrowseState[],
        error: result.error?.error ?? null,
      };
    } catch (e) {
      return {
        serviceType,
        target,
        gatewayConfig: null,
        browseCaches: [] as BrowseCache[],
        browseStates: [] as GatewayBrowseState[],
        error: e instanceof Error ? e.message : 'Failed to fetch gateway config',
      };
    }
  }

  if (serviceType !== 'gateway') {
    return { serviceType, target, gatewayConfig: null, browseCaches: [] as BrowseCache[], browseStates: [] as GatewayBrowseState[], error: null };
  }

  try {
    const result = await api<GatewayConfig>(withTarget('/gateways/gateway', target));

    if (result.error) {
      return { serviceType, target, gatewayConfig: null, browseCaches: [] as BrowseCache[], browseStates: [] as GatewayBrowseState[], error: result.error.error };
    }

    const config = result.data ?? null;

    // Browse cache + states are live runtime data on the local module.
    // In remote mode there's no per-target browse engine — skip the fetch
    // so the page still loads and the operator can see the gitops-managed
    // device list / variable selections without a stream of 404s.
    if (target) {
      return {
        serviceType,
        target,
        gatewayConfig: config,
        browseCaches: [] as BrowseCache[],
        browseStates: [] as GatewayBrowseState[],
        error: null,
      };
    }

    // Fetch browse caches and browse states in parallel
    const [browseCaches, browseStates] = await Promise.all([
      (async () => {
        const caches: BrowseCache[] = [];
        if (config?.devices) {
          const cacheResults = await Promise.allSettled(
            config.devices.map(async (device) => {
              const cacheResult = await api<BrowseCache>(`/gateways/gateway/browse-cache/${device.deviceId}`);
              return cacheResult.data ?? null;
            })
          );
          for (const r of cacheResults) {
            if (r.status === 'fulfilled' && r.value) {
              caches.push(r.value);
            }
          }
        }
        return caches;
      })(),
      (async () => {
        try {
          const statesResult = await api<GatewayBrowseState[]>('/gateways/browse-states');
          return statesResult.data ?? [];
        } catch {
          return [] as GatewayBrowseState[];
        }
      })(),
    ]);

    return {
      serviceType,
      target,
      gatewayConfig: config,
      browseCaches,
      browseStates,
      error: null,
    };
  } catch (e) {
    return {
      serviceType,
      target,
      gatewayConfig: null,
      browseCaches: [] as BrowseCache[],
      browseStates: [] as GatewayBrowseState[],
      error: e instanceof Error ? e.message : 'Failed to fetch gateway config',
    };
  }
};
