import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface ConfigField {
	envVar: string;
	description: string;
	required?: boolean;
	default?: string;
}

interface ModuleRegistryInfo {
	moduleId: string;
	repo: string;
	description: string;
	category: string;
	runtime: string;
	experimental?: boolean;
	requiredConfig?: ConfigField[];
}

interface ModuleVersionInfo {
	moduleId: string;
	installedVersions: string[];
	latestVersion: string | null;
	activeVersion: string | null;
}

interface DesiredService {
	moduleId: string;
	version: string;
	running: boolean;
}

interface ServiceStatus {
	moduleId: string;
	installedVersions: string[];
	activeVersion: string | null;
	systemdState: string;
	reconcileState: string;
	lastError: string | null;
	runtime: string;
	category: string;
	repo: string;
}

export const load: PageLoad = async ({ params }) => {
	const { moduleId } = params;

	try {
		const [modulesResult, versionsResult, internetResult, desiredResult, statusesResult, configResult] =
			await Promise.all([
				api<ModuleRegistryInfo[]>('/orchestrator/modules'),
				api<ModuleVersionInfo>(`/orchestrator/modules/${moduleId}/versions`),
				api<boolean>('/orchestrator/internet'),
				api<DesiredService[]>('/orchestrator/desired-services'),
				api<ServiceStatus[]>('/orchestrator/service-statuses'),
				api<{ moduleId: string; envVar: string; value: string }[]>(`/config/${moduleId}`),
			]);

		const firstError =
			modulesResult.error?.error ??
			versionsResult.error?.error ??
			internetResult.error?.error ??
			desiredResult.error?.error ??
			statusesResult.error?.error ??
			null;

		if (firstError) {
			return {
				moduleId,
				module: null,
				versions: null,
				online: false,
				desiredService: null,
				serviceStatus: null,
				existingConfig: [],
				error: firstError,
			};
		}

		const module =
			(modulesResult.data ?? []).find((m) => m.moduleId === moduleId) ?? null;

		const desiredService =
			(desiredResult.data ?? []).find((d) => d.moduleId === moduleId) ?? null;

		const serviceStatus =
			(statusesResult.data ?? []).find((s) => s.moduleId === moduleId) ?? null;

		return {
			moduleId,
			module,
			versions: versionsResult.data ?? null,
			online: internetResult.data ?? false,
			desiredService,
			serviceStatus,
			existingConfig: configResult.data ?? [],
			error: null,
		};
	} catch (e) {
		return {
			moduleId,
			module: null,
			versions: null,
			online: false,
			desiredService: null,
			serviceStatus: null,
			existingConfig: [],
			error: e instanceof Error ? e.message : 'Failed to connect',
		};
	}
};
