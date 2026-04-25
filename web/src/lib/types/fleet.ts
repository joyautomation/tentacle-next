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
