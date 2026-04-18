import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { Variable, GatewayConfig, BrowseCache, GatewayBrowseState } from '$lib/types/gateway';
import type { PlcConfig } from '$lib/types/plc';

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  // Gateway: load gateway config + browse cache for unified variable view
  if (serviceType === 'gateway') {
    try {
      const result = await api<GatewayConfig>('/gateways/gateway');

      if (result.error) {
        return { variables: [], serviceType, gatewayConfig: null, browseCaches: [] as BrowseCache[], browseStates: [] as GatewayBrowseState[], plcConfig: null, error: result.error.error };
      }

      const config = result.data ?? null;

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
        variables: [],
        serviceType,
        gatewayConfig: config,
        browseCaches,
        browseStates,
        plcConfig: null,
        error: null,
      };
    } catch (e) {
      return {
        variables: [],
        serviceType,
        gatewayConfig: null,
        browseCaches: [] as BrowseCache[],
        browseStates: [] as GatewayBrowseState[],
        plcConfig: null,
        error: e instanceof Error ? e.message : 'Failed to fetch gateway config',
      };
    }
  }

  // PLC: load live variables + gateway config + browse caches for variable import
  if (serviceType === 'plc') {
    try {
      const [variablesResult, plcConfigResult, gatewayResult, browseStatesResult] = await Promise.all([
        api<Variable[]>('/variables'),
        api<PlcConfig>('/plcs/plc/config'),
        api<GatewayConfig>('/gateways/gateway'),
        api<GatewayBrowseState[]>('/gateways/browse-states'),
      ]);

      const gatewayConfig = gatewayResult.data ?? null;

      // Fetch browse caches for each gateway device
      const browseCaches: BrowseCache[] = [];
      if (gatewayConfig?.devices) {
        const cacheResults = await Promise.allSettled(
          gatewayConfig.devices.map(async (device) => {
            const cacheResult = await api<BrowseCache>(`/gateways/gateway/browse-cache/${device.deviceId}`);
            return cacheResult.data ?? null;
          })
        );
        for (const r of cacheResults) {
          if (r.status === 'fulfilled' && r.value) {
            browseCaches.push(r.value);
          }
        }
      }

      return {
        variables: variablesResult.data ?? [],
        serviceType,
        gatewayConfig,
        browseCaches,
        browseStates: browseStatesResult.data ?? [],
        plcConfig: plcConfigResult.data ?? null,
        error: variablesResult.error?.error ?? null,
      };
    } catch (e) {
      return {
        variables: [],
        serviceType,
        gatewayConfig: null,
        browseCaches: [] as BrowseCache[],
        browseStates: [] as GatewayBrowseState[],
        plcConfig: null,
        error: e instanceof Error ? e.message : 'Failed to fetch PLC data',
      };
    }
  }

  return { variables: [], serviceType, gatewayConfig: null, browseCaches: [] as BrowseCache[], browseStates: [] as GatewayBrowseState[], plcConfig: null, error: null };
};
