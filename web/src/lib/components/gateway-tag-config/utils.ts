import type { DeadBandConfig } from '$lib/types/gateway';

// ── Shared types ──

export type RbeState = {
	mode: 'default' | 'custom';
	deadband?: DeadBandConfig;
	disableRBE?: boolean;
};

export type InstanceInfo = {
	id: string;
	deviceId: string;
	tag: string;
	published: boolean;
};

export type ActiveSection =
	| { kind: 'template'; templateName: string }
	| { kind: 'atomic'; deviceId: string };

// ── Datatype mapper ──
// Maps raw PLC datatypes (DINT, REAL, BOOL, etc.) to normalized types.
export function mapDatatype(d: string): string {
	const lower = d.toLowerCase();
	if (lower === 'bool' || lower === 'boolean') return 'boolean';
	if (
		[
			'dint',
			'int',
			'sint',
			'lint',
			'real',
			'lreal',
			'udint',
			'uint',
			'usint',
			'ulint',
			'number'
		].includes(lower)
	)
		return 'number';
	if (lower === 'string') return 'string';
	if (lower === 'struct') return 'struct';
	return 'number';
}
