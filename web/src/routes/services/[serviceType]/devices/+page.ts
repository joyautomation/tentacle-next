import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { Variable, ActiveDevice, GatewayConfig } from '$lib/types/gateway';
import type { ControllerSubscription, NetworkState } from '$lib/types/profinet';

interface Service {
  serviceType: string;
  moduleId: string;
  metadata: Record<string, unknown> | null;
}

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  // PROFINET Controller: load subscriptions and network interfaces
  if (serviceType === 'profinetcontroller') {
    try {
      const [subsResult, ifacesResult] = await Promise.all([
        api<ControllerSubscription[]>('/profinetcontroller/subscriptions'),
        api<NetworkState>('/network/interfaces'),
      ]);

      return {
        serviceType,
        variables: [],
        deviceInfo: {} as Record<string, ActiveDevice>,
        gatewayConfig: null,
        profinetSubscriptions: subsResult.data ?? [],
        networkInterfaces: ifacesResult.data?.interfaces ?? [],
        error: subsResult.error?.error ?? null,
      };
    } catch (e) {
      return {
        serviceType,
        variables: [],
        deviceInfo: {} as Record<string, ActiveDevice>,
        gatewayConfig: null,
        profinetSubscriptions: [],
        networkInterfaces: [],
        error: e instanceof Error ? e.message : 'Failed to fetch PROFINET subscriptions',
      };
    }
  }

  // Gateway: load gateway config (includes availableProtocols from active services)
  if (serviceType === 'gateway') {
    try {
      const result = await api<GatewayConfig>('/gateways/gateway');

      if (result.error) {
        return {
          serviceType,
          variables: [],
          deviceInfo: {} as Record<string, ActiveDevice>,
          gatewayConfig: null,
          error: result.error.error,
        };
      }

      return {
        serviceType,
        variables: [],
        deviceInfo: {} as Record<string, ActiveDevice>,
        gatewayConfig: result.data ?? null,
        error: null,
      };
    } catch (e) {
      return {
        serviceType,
        variables: [],
        deviceInfo: {} as Record<string, ActiveDevice>,
        gatewayConfig: null,
        error: e instanceof Error ? e.message : 'Failed to fetch gateway config',
      };
    }
  }

  // EtherNet/IP: load variables and device info
  try {
    const [variablesResult, servicesResult] = await Promise.all([
      api<Variable[]>('/variables?moduleId=ethernetip'),
      api<Service[]>('/services'),
    ]);

    if (variablesResult.error) {
      return {
        serviceType,
        variables: [],
        deviceInfo: {} as Record<string, ActiveDevice>,
        gatewayConfig: null,
        error: variablesResult.error.error,
      };
    }

    // Extract device connection info from EIP heartbeat metadata
    const deviceInfo: Record<string, ActiveDevice> = {};
    const eipServices = (servicesResult.data ?? []).filter(s => s.serviceType === 'ethernetip');
    for (const svc of eipServices) {
      if (svc.metadata?.devices) {
        try {
          const devices: ActiveDevice[] = typeof svc.metadata.devices === 'string'
            ? JSON.parse(svc.metadata.devices)
            : svc.metadata.devices;
          for (const d of devices) {
            deviceInfo[d.deviceId] = d;
          }
        } catch { /* ignore parse errors */ }
      }
    }

    return {
      serviceType,
      variables: variablesResult.data ?? [],
      deviceInfo,
      gatewayConfig: null,
      error: null,
    };
  } catch (e) {
    return {
      serviceType,
      variables: [],
      deviceInfo: {} as Record<string, ActiveDevice>,
      gatewayConfig: null,
      error: e instanceof Error ? e.message : 'Failed to fetch variables',
    };
  }
};
