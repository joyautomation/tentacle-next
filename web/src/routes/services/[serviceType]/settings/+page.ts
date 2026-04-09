import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface ConfigEntry {
  moduleId: string;
  envVar: string;
  value: string;
}

export interface FieldDef {
  envVar: string;
  default?: string;
  required?: boolean;
  type: string; // "string" | "number" | "boolean" | "password"
  label: string;
  group: string;
  groupOrder: number;
  sortOrder: number;
  toggleable?: boolean;
  toggleLabel?: string;
  dependsOn?: string;
}

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  try {
    const [configResult, schemaResult] = await Promise.all([
      api<ConfigEntry[]>(`/config/${serviceType}`),
      api<FieldDef[]>(`/config/${serviceType}/schema`),
    ]);

    return {
      serviceType,
      config: configResult.data ?? [],
      schema: schemaResult.data ?? [],
      error: configResult.error?.error ?? null,
    };
  } catch (e) {
    return {
      serviceType,
      config: [],
      schema: [],
      error: e instanceof Error ? e.message : 'Failed to load config',
    };
  }
};
