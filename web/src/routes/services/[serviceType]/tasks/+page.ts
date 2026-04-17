import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

export interface PlcTaskConfigKV {
  name: string;
  description?: string;
  scanRateMs: number;
  programRef: string;
  enabled: boolean;
}

export interface ProgramListItem {
  name: string;
  language: string;
  updatedAt: number;
  updatedBy?: string;
}

export const load: PageLoad = async ({ params }) => {
  const { serviceType } = params;

  if (serviceType !== 'plc') {
    return { tasks: {} as Record<string, PlcTaskConfigKV>, programs: [] as ProgramListItem[], serviceType, error: null };
  }

  try {
    const [tasksResult, programsResult] = await Promise.all([
      api<Record<string, PlcTaskConfigKV>>('/plcs/plc/tasks'),
      api<ProgramListItem[]>('/plcs/plc/programs'),
    ]);

    const error = tasksResult.error?.error ?? programsResult.error?.error ?? null;

    return {
      tasks: (!tasksResult.error ? tasksResult.data : {}) as Record<string, PlcTaskConfigKV>,
      programs: (!programsResult.error ? programsResult.data : []) as ProgramListItem[],
      serviceType,
      error,
    };
  } catch (e) {
    return {
      tasks: {} as Record<string, PlcTaskConfigKV>,
      programs: [] as ProgramListItem[],
      serviceType,
      error: e instanceof Error ? e.message : 'Failed to fetch tasks',
    };
  }
};
