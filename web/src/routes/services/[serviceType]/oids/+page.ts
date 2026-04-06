import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface Variable {
  variableId: string;
  value: unknown;
  datatype: string;
  quality: string;
  moduleId: string;
  deviceId: string | null;
  lastUpdated: string;
  source: string | null;
}

interface Service {
  serviceType: string;
  moduleId: string;
  metadata: Record<string, unknown> | null;
}

interface ActiveDevice {
  deviceId: string;
  host: string;
  port: number;
  oidCount: number;
  version: string;
}

export const load: PageLoad = async () => {
  try {
    const [variablesResult, servicesResult] = await Promise.all([
      api<Variable[]>('/variables?moduleId=snmp'),
      api<Service[]>('/services'),
    ]);

    if (variablesResult.error) {
      return {
        variables: [],
        deviceInfo: {} as Record<string, ActiveDevice>,
        error: variablesResult.error.error,
      };
    }

    // Extract device connection info from SNMP heartbeat metadata
    const deviceInfo: Record<string, ActiveDevice> = {};
    const snmpServices = (servicesResult.data ?? []).filter(s => s.serviceType === 'snmp');
    for (const svc of snmpServices) {
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
      variables: variablesResult.data ?? [],
      deviceInfo,
      error: null,
    };
  } catch (e) {
    return {
      variables: [],
      deviceInfo: {} as Record<string, ActiveDevice>,
      error: e instanceof Error ? e.message : 'Failed to fetch OIDs',
    };
  }
};
