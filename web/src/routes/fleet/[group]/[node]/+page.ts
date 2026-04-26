import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { FleetNode, FleetModule } from '$lib/types/fleet';

export const load: PageLoad = async ({ params }) => {
	const { group, node } = params;

	const [nodesResult, servicesResult] = await Promise.all([
		api<FleetNode[] | null>('/sparkplug-host/nodes'),
		api<{ services: FleetModule[] }>(
			`/fleet/nodes/${encodeURIComponent(group)}/${encodeURIComponent(node)}/services`,
		),
	]);

	const fleetNode =
		(nodesResult.data ?? []).find((n) => n.groupId === group && n.nodeId === node) ?? null;

	return {
		group,
		node,
		fleetNode,
		services: servicesResult.data?.services ?? [],
		servicesError: servicesResult.error?.error ?? null,
		error: nodesResult.error?.error ?? null,
	};
};
