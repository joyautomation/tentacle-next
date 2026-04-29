import type { LayoutLoad } from './$types';
import { api } from '$lib/api/client';
import type { FleetNode } from '$lib/types/fleet';

// When configuring a remote tentacle (?target=group/node), fetch that node's
// fleet entry so the banner can show edge↔mantle sync status. The root
// +layout listens for GitOpsApplied SSE and calls invalidateAll(), which
// reruns this load and refreshes the badge after each apply.
export const load: LayoutLoad = async ({ url }) => {
	const target = url.searchParams.get('target');
	if (!target) return { fleetNode: null as FleetNode | null };

	const [group, node] = target.split('/', 2);
	if (!group || !node) return { fleetNode: null };

	const result = await api<FleetNode[] | null>('/fleet/nodes');
	const fleetNode =
		(result.data ?? []).find((n) => n.groupId === group && n.nodeId === node) ?? null;
	return { fleetNode };
};
