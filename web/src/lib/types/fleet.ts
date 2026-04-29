export interface FleetDevice {
	deviceId: string;
	online: boolean;
	lastSeen: number;
	metricCount: number;
}

export interface FleetModule {
	id: string;
	version?: string;
	running: boolean;
}

export type FleetSyncStatus = 'synced' | 'syncing' | 'empty' | 'unknown';

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
	modules?: FleetModule[];
	modulesError?: string;
	gitopsCommitSHA?: string;
	repoHead?: string;
	syncStatus?: FleetSyncStatus;
}
