import type { PageLoad } from './$types';
import { api } from '$lib/api/client';
import type { FleetNode } from '$lib/types/fleet';

export const load: PageLoad = async ({ params }) => {
	const { group, node } = params;

	// We pull the inventory list and find this specific node so we can show
	// online/offline + last-seen on the per-node page. If the node is unknown
	// to mantle (no NBIRTH yet), we still render the page so the operator can
	// pre-author config — they'll see an "unseen" warning.
	const result = await api<FleetNode[] | null>('/sparkplug-host/nodes');

	if (result.error) {
		return {
			group,
			node,
			fleetNode: null as FleetNode | null,
			error: result.error.error,
		};
	}

	const fleetNode =
		(result.data ?? []).find((n) => n.groupId === group && n.nodeId === node) ?? null;

	return {
		group,
		node,
		fleetNode,
		error: null as string | null,
	};
};
