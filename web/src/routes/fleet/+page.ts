import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { FleetNode } from '$lib/types/fleet';

export const load: PageLoad = async () => {
	const result = await api<FleetNode[] | null>('/sparkplug-host/nodes');

	if (result.error) {
		return {
			nodes: [] as FleetNode[],
			error: result.error.error,
		};
	}

	const nodes = (result.data ?? []).slice().sort((a, b) =>
		`${a.groupId}/${a.nodeId}`.localeCompare(`${b.groupId}/${b.nodeId}`)
	);

	return {
		nodes,
		error: null as string | null,
	};
};
