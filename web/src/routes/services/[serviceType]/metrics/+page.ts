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
    const result = await api<MqttMetricsResponse>('/mqtt/metrics');

    if (result.error) {
      return {
        metrics: [],
        templates: [],
        deviceId: '',
        error: result.error.error,
      };
    }

    return {
      metrics: result.data?.metrics ?? [],
      templates: result.data?.templates ?? [],
      deviceId: result.data?.deviceId ?? '',
      error: null,
    };
  } catch (e) {
    return {
      metrics: [],
      templates: [],
      deviceId: '',
      error: e instanceof Error ? e.message : 'Failed to fetch metrics',
    };
  }
};
