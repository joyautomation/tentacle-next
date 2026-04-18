import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

export interface ProgramListItem {
  name: string;
  language: string;
  updatedAt: number;
  updatedBy?: string;
}

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  if (serviceType !== 'plc') {
    return { programs: [] as ProgramListItem[], serviceType, error: null };
  }

  try {
    const result = await api<ProgramListItem[]>('/plcs/plc/programs');

    return {
      programs: (!result.error ? result.data : []) as ProgramListItem[],
      serviceType,
      error: result.error?.error ?? null,
    };
  } catch (e) {
    return {
      programs: [] as ProgramListItem[],
      serviceType,
      error: e instanceof Error ? e.message : 'Failed to fetch programs',
    };
  }
};
