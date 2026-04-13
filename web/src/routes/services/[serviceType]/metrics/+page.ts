import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface MqttTemplateMember {
  name: string;
  datatype: string;
  description: string | null;
  templateRef: string | null;
  isArray: boolean | null;
}

interface MqttTemplateInfo {
  name: string;
  version: string | null;
  members: MqttTemplateMember[];
}

interface MqttMetricInfo {
  name: string;
  sparkplugType: string;
  value: unknown;
  moduleId: string;
  datatype: string;
  templateRef: string | null;
  lastUpdated: number | null;
}

interface MqttMetricsResponse {
  metrics: MqttMetricInfo[];
  templates: MqttTemplateInfo[];
  deviceId: string;
  timestamp: string;
}

export const load: PageLoad = async () => {
  try {
    // Race against a 3s timeout so navigation doesn't block for the full
    // 10s bus timeout when the MQTT module is unresponsive.
    // The SSE stream will populate data once the module comes back.
    const timeout = new Promise<null>(r => setTimeout(() => r(null), 3000));
    const result = await Promise.race([
      api<MqttMetricsResponse>('/mqtt/metrics'),
      timeout,
    ]);

    if (result === null) {
      return {
        metrics: [],
        templates: [],
        deviceId: '',
        error: 'MQTT module is not responding. Check that the broker is reachable and the service is running.',
        moduleUnavailable: true,
      };
    }

    if (result.error) {
      return {
        metrics: [],
        templates: [],
        deviceId: '',
        error: result.error.error,
        moduleUnavailable: result.error.status === 502,
      };
    }

    return {
      metrics: result.data?.metrics ?? [],
      templates: result.data?.templates ?? [],
      deviceId: result.data?.deviceId ?? '',
      error: null,
      moduleUnavailable: false,
    };
  } catch (e) {
    return {
      metrics: [],
      templates: [],
      deviceId: '',
      error: e instanceof Error ? e.message : 'Failed to fetch metrics',
      moduleUnavailable: false,
    };
  }
};
