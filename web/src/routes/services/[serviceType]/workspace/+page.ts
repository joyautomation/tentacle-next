import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type {
	PlcConfig,
	PlcVariableConfig,
	PlcTaskConfig,
	PlcTemplate,
	ProgramListItem
} from '$lib/types/plc';
import type { GatewayConfig, BrowseCache, GatewayBrowseState } from '$lib/types/gateway';

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
	templates: PlcTemplate[];
	plcConfig: PlcConfig | null;
	gatewayConfig: GatewayConfig | null;
	browseCaches: BrowseCache[];
	browseStates: GatewayBrowseState[];
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
		templates: [],
		plcConfig: null,
		gatewayConfig: null,
		browseCaches: [],
		browseStates: [],
		initialLogs: [],
		error: null
	};

	if (serviceType !== 'plc') return empty;

	const [configResult, tasksResult, programsResult, logsResult, templatesResult, gatewayResult, browseStatesResult] = await Promise.all([
		api<PlcConfig>('/plcs/plc/config'),
		api<Record<string, PlcTaskConfig>>('/plcs/plc/tasks'),
		api<ProgramListItem[]>('/plcs/plc/programs'),
		api<LogEntry[]>(`/services/${serviceType}/logs`),
		api<PlcTemplate[]>('/plcs/plc/templates'),
		api<GatewayConfig>('/gateways/gateway'),
		api<GatewayBrowseState[]>('/gateways/browse-states')
	]);

	const gatewayConfig = gatewayResult.data ?? null;

	const browseCaches: BrowseCache[] = [];
	if (gatewayConfig?.devices) {
		const cacheResults = await Promise.allSettled(
			gatewayConfig.devices.map(async (device) => {
				const cacheResult = await api<BrowseCache>(`/gateways/gateway/browse-cache/${device.deviceId}`);
				return cacheResult.data ?? null;
			})
		);
		for (const r of cacheResults) {
			if (r.status === 'fulfilled' && r.value) browseCaches.push(r.value);
		}
	}

	return {
		serviceType,
		variables: configResult.data?.variables ?? {},
		tasks: tasksResult.data ?? {},
		programs: programsResult.data ?? [],
		templates: templatesResult.data ?? [],
		plcConfig: configResult.data ?? null,
		gatewayConfig,
		browseCaches,
		browseStates: browseStatesResult.data ?? [],
		initialLogs: logsResult.data ?? [],
		error:
			configResult.error?.error ??
			tasksResult.error?.error ??
			programsResult.error?.error ??
			null
	};
};
