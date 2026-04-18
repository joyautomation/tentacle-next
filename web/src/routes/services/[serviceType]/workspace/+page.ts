import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type {
	PlcConfig,
	PlcVariableConfig,
	PlcTaskConfig,
	ProgramListItem
} from '$lib/types/plc';

export interface WorkspaceLoadData {
	serviceType: string;
	variables: Record<string, PlcVariableConfig>;
	tasks: Record<string, PlcTaskConfig>;
	programs: ProgramListItem[];
	error: string | null;
}

export const load: PageLoad = async ({ params }): Promise<WorkspaceLoadData> => {
	const { serviceType } = params;

	const empty: WorkspaceLoadData = {
		serviceType,
		variables: {},
		tasks: {},
		programs: [],
		error: null
	};

	if (serviceType !== 'plc') return empty;

	const [configResult, tasksResult, programsResult] = await Promise.all([
		api<PlcConfig>('/plcs/plc/config'),
		api<Record<string, PlcTaskConfig>>('/plcs/plc/tasks'),
		api<ProgramListItem[]>('/plcs/plc/programs')
	]);

	return {
		serviceType,
		variables: configResult.data?.variables ?? {},
		tasks: tasksResult.data ?? {},
		programs: programsResult.data ?? [],
		error:
			configResult.error?.error ??
			tasksResult.error?.error ??
			programsResult.error?.error ??
			null
	};
};
