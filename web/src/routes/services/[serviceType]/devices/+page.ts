import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { Variable, ActiveDevice, GatewayConfig, DeviceCommStatus } from '$lib/types/gateway';
import type { ControllerSubscription, NetworkState } from '$lib/types/profinet';

interface Service {
  serviceType: string;
  moduleId: string;
  metadata: Record<string, unknown> | null;
}

// Map module serviceType -> gateway-config protocol string.
const SERVICE_TO_PROTOCOL: Record<string, string> = {
  modbus: 'modbus',
  opcua: 'opcua',
  ethernetip: 'ethernetip',
  snmp: 'snmp',
};

// Flatten per-device statuses from every scanner module heartbeat into
// `${protocol}:${deviceId}` -> status for easy UI lookup.
function collectDeviceStatuses(services: Service[]): Record<string, DeviceCommStatus> {
  const out: Record<string, DeviceCommStatus> = {};
  for (const svc of services) {
    const protocol = SERVICE_TO_PROTOCOL[svc.serviceType];
    if (!protocol) continue;
    const raw = svc.metadata?.deviceStatuses;
    if (!Array.isArray(raw)) continue;
    for (const s of raw as DeviceCommStatus[]) {
      if (!s || !s.deviceId) continue;
      out[`${protocol}:${s.deviceId}`] = s;
    }
  }
  return out;
}

export const load: PageLoad = async ({ params, depends }) => {
  const { serviceType } = params;

  // Allow the client to trigger re-runs via invalidate('app:gateway-devices').
  // Used for polling per-device comm status badges on the gateway sources page.
  depends('app:gateway-devices');

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

  // Gateway: load gateway config + per-device comm status from scanner heartbeats
  if (serviceType === 'gateway') {
    try {
      const [gwResult, servicesResult] = await Promise.all([
        api<GatewayConfig>('/gateways/gateway'),
        api<Service[]>('/services'),
      ]);

      if (gwResult.error) {
        return {
          serviceType,
          variables: [],
          deviceInfo: {} as Record<string, ActiveDevice>,
          deviceStatuses: {} as Record<string, DeviceCommStatus>,
          gatewayConfig: null,
          error: gwResult.error.error,
        };
      }

      const deviceStatuses: Record<string, DeviceCommStatus> =
        collectDeviceStatuses(servicesResult.data ?? []);

      return {
        serviceType,
        variables: [],
        deviceInfo: {} as Record<string, ActiveDevice>,
        deviceStatuses,
        gatewayConfig: gwResult.data ?? null,
        error: null,
      };
    } catch (e) {
      return {
        serviceType,
        variables: [],
        deviceInfo: {} as Record<string, ActiveDevice>,
        deviceStatuses: {} as Record<string, DeviceCommStatus>,
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
