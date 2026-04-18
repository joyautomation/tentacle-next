import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { PlcConfig } from '$lib/types/plc';

export interface ProgramListItem {
  name: string;
  language: string;
  updatedAt: number;
  updatedBy?: string;
}

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  if (serviceType !== 'plc') {
    return { programs: [] as ProgramListItem[], serviceType, variableNames: [] as string[], error: null };
  }

  try {
    const [programsResult, configResult] = await Promise.all([
      api<ProgramListItem[]>('/plcs/plc/programs'),
      api<PlcConfig>('/plcs/plc/config'),
    ]);

    const variableNames = configResult.data
      ? Object.keys(configResult.data.variables ?? {}).sort()
      : [];

    return {
      programs: (!programsResult.error ? programsResult.data : []) as ProgramListItem[],
      serviceType,
      variableNames,
      error: programsResult.error?.error ?? null,
    };
  } catch (e) {
    return {
      programs: [] as ProgramListItem[],
      serviceType,
      variableNames: [] as string[],
      error: e instanceof Error ? e.message : 'Failed to fetch programs',
    };
  }
};
