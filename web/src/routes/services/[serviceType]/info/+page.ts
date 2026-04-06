import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { Variable, GatewayConfig, BrowseCache, GatewayBrowseState } from '$lib/types/gateway';

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  // Gateway: load gateway config + browse cache for unified variable view
  if (serviceType === 'gateway') {
    try {
      const result = await api<GatewayConfig>('/gateways/gateway');

      if (result.error) {
        return { variables: [], serviceType, gatewayConfig: null, browseCaches: [] as BrowseCache[], browseStates: [] as GatewayBrowseState[], error: result.error.error };
      }

      const config = result.data ?? null;

      // Fetch browse caches and browse states in parallel
      const [browseCaches, browseStates] = await Promise.all([
        // Browse cache for each device
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
        // Active browse states
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
        variables: [],
        serviceType,
        gatewayConfig: config,
        browseCaches,
        browseStates,
        error: null,
      };
    } catch (e) {
      return {
        variables: [],
        serviceType,
        gatewayConfig: null,
        browseCaches: [] as BrowseCache[],
        browseStates: [] as GatewayBrowseState[],
        error: e instanceof Error ? e.message : 'Failed to fetch gateway config',
      };
    }
  }

  // Only PLC uses this page for variables
  if (serviceType !== 'plc') {
    return { variables: [], serviceType, gatewayConfig: null, browseCaches: [] as BrowseCache[], browseStates: [] as GatewayBrowseState[], error: null };
  }

  try {
    const result = await api<Variable[]>('/variables');

    if (result.error) {
      return {
        variables: [],
        serviceType,
        gatewayConfig: null,
        browseCaches: [] as BrowseCache[],
        browseStates: [] as GatewayBrowseState[],
        error: result.error.error,
      };
    }

    return {
      variables: result.data ?? [],
      serviceType,
      gatewayConfig: null,
      browseCaches: [] as BrowseCache[],
      browseStates: [] as GatewayBrowseState[],
      error: null,
    };
  } catch (e) {
    return {
      variables: [],
      serviceType,
      gatewayConfig: null,
      browseCaches: [] as BrowseCache[],
      browseStates: [] as GatewayBrowseState[],
      error: e instanceof Error ? e.message : 'Failed to fetch variables',
    };
  }
};
