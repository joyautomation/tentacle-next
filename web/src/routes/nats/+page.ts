import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

interface Service {
	serviceType: string;
	moduleId: string;
	uptime: number;
	metadata?: Record<string, unknown>;
}

export const load: PageLoad = async () => {
	const result = await api<Service[]>('/services');

	if (result.error) {
		return {
			services: [],
			error: result.error.error,
		};
	}

	return {
		services: result.data ?? [],
		error: null,
	};
};
