import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type {
	PlcConfig,
	PlcVariableConfig,
	PlcTaskConfig,
	PlcTemplate,
	ProgramListItem,
	TestListItem
} from '$lib/types/plc';

export interface LogEntry {
	timestamp: string;
	level: string;
	message: string;
	serviceType: string;
	moduleId: string;
	logger: string | null;
}

export interface WorkspaceLoadData {
	serviceType: string;
	variables: Record<string, PlcVariableConfig>;
	tasks: Record<string, PlcTaskConfig>;
	programs: ProgramListItem[];
	tests: TestListItem[];
	templates: PlcTemplate[];
	plcConfig: PlcConfig | null;
	initialLogs: LogEntry[];
	error: string | null;
}

export const load: PageLoad = async ({ params }): Promise<WorkspaceLoadData> => {
	const { serviceType } = params;

	const empty: WorkspaceLoadData = {
		serviceType,
		variables: {},
		tasks: {},
		programs: [],
		tests: [],
		templates: [],
		plcConfig: null,
		initialLogs: [],
		error: null
	};

	if (serviceType !== 'plc') return empty;

	const [configResult, tasksResult, programsResult, testsResult, logsResult, templatesResult] = await Promise.all([
		api<PlcConfig>('/plcs/plc/config'),
		api<Record<string, PlcTaskConfig>>('/plcs/plc/tasks'),
		api<ProgramListItem[]>('/plcs/plc/programs'),
		api<TestListItem[]>('/plcs/plc/tests'),
		api<LogEntry[]>(`/services/${serviceType}/logs`),
		api<PlcTemplate[]>('/plcs/plc/templates')
	]);

	return {
		serviceType,
		variables: configResult.data?.variables ?? {},
		tasks: tasksResult.data ?? {},
		programs: programsResult.data ?? [],
		tests: testsResult.data ?? [],
		templates: templatesResult.data ?? [],
		plcConfig: configResult.data ?? null,
		initialLogs: logsResult.data ?? [],
		error:
			configResult.error?.error ??
			tasksResult.error?.error ??
			programsResult.error?.error ??
			null
	};
};
