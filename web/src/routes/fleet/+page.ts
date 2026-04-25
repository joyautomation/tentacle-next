import type { PageLoad } from './$types';
import { api } from '$lib/api/client';

export interface FleetDevice {
	deviceId: string;
	online: boolean;
	lastSeen: number;
	metricCount: number;
}

export interface FleetNode {
	groupId: string;
	nodeId: string;
	online: boolean;
	lastSeen: number;
	firstSeen: number;
	bdSeq: number;
	devices: Record<string, FleetDevice> | null;
	metricCount: number;
	verbs?: string[];
}

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
