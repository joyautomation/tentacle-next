<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { apiPut, apiDelete } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import { slide } from 'svelte/transition';
	import type { GatewayConfig, GatewayDevice } from '$lib/types/gateway';
	import {
		workspaceTabs,
		workspaceEditorSaves,
		workspaceSelection
	} from '../workspace-state.svelte';
	import DirtyIcon from '$lib/components/DirtyIcon.svelte';

	type Props = {
		tabId: string;
		name: string; // deviceId, or '' for a new source
		gatewayConfig: GatewayConfig | null;
		isNew?: boolean;
	};

	let { tabId, name, gatewayConfig, isNew = false }: Props = $props();

	// The canonical device record from the loaded gateway config. When the
	// user saves, invalidateAll refetches the config and this re-derives.
	const existing = $derived<GatewayDevice | null>(
		gatewayConfig?.devices?.find((d) => d.deviceId === name) ?? null
	);

	const allProtocols = [
		{ value: 'ethernetip', label: 'EtherNet/IP', defaultScanRate: 1000 },
		{ value: 'opcua', label: 'OPC UA', defaultScanRate: 1000 },
		{ value: 'snmp', label: 'SNMP', defaultScanRate: 5000 },
		{ value: 'modbus', label: 'Modbus TCP', defaultScanRate: 1000 },
		{ value: 'plc', label: 'PLC', defaultScanRate: 1000 }
	] as const;

	const protocolLabels: Record<string, string> = Object.fromEntries(
		allProtocols.map((p) => [p.value, p.label])
	);

	const availableProtocols = $derived(
		gatewayConfig?.availableProtocols?.length
			? allProtocols.filter((p) => gatewayConfig!.availableProtocols!.includes(p.value))
			: []
	);

	const defaultProtocol = $derived(
		gatewayConfig?.availableProtocols?.[0] ?? 'ethernetip'
	);

	// Editable fields — single set used for both create and edit flows.
	let pendingDeviceId = $state('');
	let protocol = $state<string>('ethernetip');
	let host = $state('');
	let port = $state('');
	let slot = $state('');
	let endpointUrl = $state('');
	let snmpVersion = $state('2c');
	let community = $state('public');
	let unitId = $state('1');
	let scanRate = $state('');
	let deadbandValue = $state('');
	let deadbandMinTime = $state('');
	let deadbandMaxTime = $state('');
	let disableRBE = $state(false);

	let saving = $state(false);
	let deleting = $state(false);

	// Reseed local state when the underlying device changes (save/remote
	// update) or when the tab first binds to a new deviceId.
	let lastSeededFor = '';
	$effect(() => {
		if (isNew) {
			if (lastSeededFor === '__new__') return;
			lastSeededFor = '__new__';
			pendingDeviceId = '';
			protocol = defaultProtocol;
			host = '';
			port = '';
			slot = '';
			endpointUrl = '';
			snmpVersion = '2c';
			community = 'public';
			unitId = '1';
			scanRate = '';
			deadbandValue = '';
			deadbandMinTime = '';
			deadbandMaxTime = '';
			disableRBE = false;
			return;
		}
		if (!existing) return;
		const key = `${existing.deviceId}::${gatewayConfig?.updatedAt ?? ''}`;
		if (key === lastSeededFor) return;
		lastSeededFor = key;
		pendingDeviceId = existing.deviceId;
		protocol = existing.protocol;
		host = existing.host ?? '';
		port = existing.port != null ? String(existing.port) : '';
		slot = existing.slot != null ? String(existing.slot) : '';
		endpointUrl = existing.endpointUrl ?? '';
		snmpVersion = existing.version ?? '2c';
		community = existing.community ?? 'public';
		unitId = existing.unitId != null ? String(existing.unitId) : '1';
		scanRate = existing.scanRate != null ? String(existing.scanRate) : '';
		deadbandValue = existing.deadband?.value != null ? String(existing.deadband.value) : '';
		deadbandMinTime =
			existing.deadband?.minTime != null ? String(existing.deadband.minTime) : '';
		deadbandMaxTime =
			existing.deadband?.maxTime != null ? String(existing.deadband.maxTime) : '';
		disableRBE = existing.disableRBE ?? false;
	});

	const variableCount = $derived.by(() => {
		if (!gatewayConfig || !existing) return 0;
		const vars = gatewayConfig.variables?.filter((v) => v.deviceId === existing.deviceId) ?? [];
		const udts = gatewayConfig.udtVariables?.filter((v) => v.deviceId === existing.deviceId) ?? [];
		return vars.length + udts.length;
	});

	const isAutoManaged = $derived(existing?.autoManaged === true);

	function buildBody(): Record<string, unknown> {
		const effectiveId = (pendingDeviceId || name).trim();
		const body: Record<string, unknown> = {
			deviceId: effectiveId,
			protocol
		};
		if (protocol !== 'opcua' && protocol !== 'plc' && host) body.host = host;
		if (port) body.port = parseInt(port, 10);
		if (protocol === 'ethernetip' && slot) body.slot = parseInt(slot, 10);
		if (protocol === 'opcua' && endpointUrl) body.endpointUrl = endpointUrl;
		if (protocol === 'snmp') {
			body.version = snmpVersion;
			body.community = community;
		}
		if (protocol === 'modbus' && unitId) body.unitId = parseInt(unitId, 10);
		if (scanRate) body.scanRate = parseInt(scanRate, 10);
		if (disableRBE) {
			body.disableRBE = true;
		} else if (deadbandValue) {
			const db: Record<string, unknown> = { value: parseFloat(deadbandValue) };
			if (deadbandMinTime) db.minTime = parseInt(deadbandMinTime, 10);
			if (deadbandMaxTime) db.maxTime = parseInt(deadbandMaxTime, 10);
			body.deadband = db;
		}
		return body;
	}

	const isDirty = $derived.by(() => {
		if (isNew) {
			return (
				pendingDeviceId.trim() !== '' ||
				host !== '' ||
				port !== '' ||
				slot !== '' ||
				endpointUrl !== '' ||
				scanRate !== '' ||
				deadbandValue !== '' ||
				disableRBE
			);
		}
		if (!existing) return false;
		const cur = JSON.stringify({
			host: existing.host ?? '',
			port: existing.port ?? null,
			slot: existing.slot ?? null,
			endpointUrl: existing.endpointUrl ?? '',
			snmpVersion: existing.version ?? '2c',
			community: existing.community ?? 'public',
			unitId: existing.unitId ?? 1,
			scanRate: existing.scanRate ?? null,
			deadbandValue: existing.deadband?.value ?? null,
			deadbandMinTime: existing.deadband?.minTime ?? null,
			deadbandMaxTime: existing.deadband?.maxTime ?? null,
			disableRBE: existing.disableRBE ?? false
		});
		const next = JSON.stringify({
			host,
			port: port ? parseInt(port, 10) : null,
			slot: slot ? parseInt(slot, 10) : null,
			endpointUrl,
			snmpVersion,
			community,
			unitId: unitId ? parseInt(unitId, 10) : 1,
			scanRate: scanRate ? parseInt(scanRate, 10) : null,
			deadbandValue: deadbandValue ? parseFloat(deadbandValue) : null,
			deadbandMinTime: deadbandMinTime ? parseInt(deadbandMinTime, 10) : null,
			deadbandMaxTime: deadbandMaxTime ? parseInt(deadbandMaxTime, 10) : null,
			disableRBE
		});
		return cur !== next;
	});

	$effect(() => {
		workspaceTabs.setDirty(tabId, isDirty);
	});

	const canSave = $derived.by(() => {
		if (saving || isAutoManaged && !isDirty) return false;
		if (isNew) {
			const id = pendingDeviceId.trim();
			if (!id) return false;
			if (gatewayConfig?.devices?.some((d) => d.deviceId === id)) return false;
			if (availableProtocols.length === 0) return false;
		}
		return isDirty || isNew;
	});

	onMount(() => workspaceEditorSaves.register(tabId, save));
	onDestroy(() => workspaceEditorSaves.unregister(tabId));

	async function save() {
		if (!canSave) return;
		saving = true;
		try {
			const body = buildBody();
			const res = await apiPut('/gateways/gateway/devices', body);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			const id = body.deviceId as string;
			saltState.addNotification({ message: `Saved source "${id}"`, type: 'success' });
			if (isNew) {
				workspaceTabs.renameTab(tabId, id);
				workspaceSelection.select('source', id);
			}
			workspaceTabs.clearDirty(tabId);
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function revert() {
		lastSeededFor = '';
		// Re-run the seeding effect by touching a reactive dep.
		const tmp = protocol;
		protocol = tmp;
	}

	async function del() {
		if (!existing) return;
		if (isAutoManaged) {
			saltState.addNotification({
				message: 'Auto-managed sources cannot be deleted from here.',
				type: 'error'
			});
			return;
		}
		const msg =
			variableCount > 0
				? `Delete source "${existing.deviceId}"? This will also remove ${variableCount} variable(s) and browse cache for this device.`
				: `Delete source "${existing.deviceId}"? This cannot be undone.`;
		if (!confirm(msg)) return;
		deleting = true;
		try {
			const res = await apiDelete(
				`/gateways/gateway/devices/${encodeURIComponent(existing.deviceId)}`
			);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({
				message: `Deleted source "${existing.deviceId}"`,
				type: 'success'
			});
			workspaceTabs.close(tabId);
			await invalidateAll();
		} finally {
			deleting = false;
		}
	}
</script>

<div class="source-editor">
	<header class="se-head">
		<div class="left">
			<span class="kind-badge">Source</span>
			<span class="name">{pendingDeviceId || name || '(new source)'}</span>
			{#if existing}
				<span class="proto-badge">{protocolLabels[existing.protocol] ?? existing.protocol}</span>
			{/if}
			{#if isDirty}
				<DirtyIcon size="0.875rem" />
			{/if}
		</div>
		<div class="right">
			{#if isDirty && !isNew}
				<button type="button" class="btn subtle" onclick={revert} disabled={saving}>Revert</button>
			{/if}
			<button type="button" class="btn primary" onclick={save} disabled={!canSave}>
				{saving ? 'Saving…' : isNew ? 'Create' : 'Save'}
			</button>
			{#if !isNew && existing && !isAutoManaged}
				<button
					type="button"
					class="btn danger"
					onclick={del}
					disabled={deleting || saving}
					title={variableCount > 0
						? `Will also remove ${variableCount} variable(s)`
						: 'Delete source'}
				>
					{deleting ? 'Deleting…' : 'Delete'}
				</button>
			{/if}
		</div>
	</header>

	{#if !isNew && !existing}
		<div class="se-body">
			<div class="status">Source "{name}" not found.</div>
		</div>
	{:else}
		<div class="se-body">
			{#if isNew}
				<div class="section">
					<div class="section-label">Identity</div>
					<div class="grid">
						<label class="field">
							<span>Device ID</span>
							<input
								type="text"
								class="input"
								bind:value={pendingDeviceId}
								placeholder="e.g. plc-1"
							/>
						</label>
						<label class="field">
							<span>Protocol</span>
							{#if availableProtocols.length === 0}
								<p class="hint error">
									No protocol modules connected. Start a protocol service (EtherNet/IP,
									OPC UA, SNMP, or Modbus) to add a source.
								</p>
							{:else}
								<select class="input" bind:value={protocol}>
									{#each availableProtocols as p (p.value)}
										<option value={p.value}>{p.label}</option>
									{/each}
								</select>
							{/if}
						</label>
					</div>
				</div>
			{/if}

			{#if isAutoManaged}
				<div class="info-note">
					This source is auto-managed by a protocol module. Connection details are not
					user-editable; you can still adjust RBE / deadband below.
				</div>
			{/if}

			{#if !isAutoManaged}
				<div class="section" transition:slide={{ duration: 150 }}>
					<div class="section-label">Connection</div>
					<div class="grid">
						{#if protocol === 'plc'}
							<p class="hint muted">
								The PLC publishes directly on the internal bus — no connection details
								needed.
							</p>
						{:else if protocol === 'opcua'}
							<label class="field wide">
								<span>Endpoint URL</span>
								<input
									type="text"
									class="input"
									bind:value={endpointUrl}
									placeholder="opc.tcp://192.168.1.50:4840"
								/>
							</label>
						{:else}
							<label class="field">
								<span>Host</span>
								<input
									type="text"
									class="input"
									bind:value={host}
									placeholder="192.168.1.100"
								/>
							</label>
							<label class="field">
								<span>Port</span>
								<input
									type="text"
									class="input"
									bind:value={port}
									placeholder={protocol === 'ethernetip'
										? '44818'
										: protocol === 'snmp'
											? '161'
											: '502'}
								/>
							</label>
							{#if protocol === 'ethernetip'}
								<label class="field">
									<span>Slot</span>
									<input type="text" class="input" bind:value={slot} placeholder="0" />
								</label>
							{/if}
							{#if protocol === 'snmp'}
								<label class="field">
									<span>SNMP Version</span>
									<select class="input" bind:value={snmpVersion}>
										<option value="1">v1</option>
										<option value="2c">v2c</option>
										<option value="3">v3</option>
									</select>
								</label>
								<label class="field">
									<span>Community</span>
									<input
										type="text"
										class="input"
										bind:value={community}
										placeholder="public"
									/>
								</label>
							{/if}
							{#if protocol === 'modbus'}
								<label class="field">
									<span>Unit ID</span>
									<input type="text" class="input" bind:value={unitId} placeholder="1" />
								</label>
							{/if}
						{/if}
					</div>
				</div>
			{/if}

			<div class="section">
				<div class="section-label">Polling</div>
				<div class="grid">
					<label class="field">
						<span>Scan rate (ms)</span>
						<input
							type="number"
							class="input"
							bind:value={scanRate}
							placeholder={String(
								allProtocols.find((p) => p.value === protocol)?.defaultScanRate ?? 1000
							)}
							min="100"
							step="100"
						/>
					</label>
				</div>
			</div>

			<div class="section">
				<div class="section-label">RBE / Deadband</div>
				<label class="checkbox-label">
					<input type="checkbox" bind:checked={disableRBE} />
					<span>Disable RBE (publish every scan)</span>
				</label>
				{#if !disableRBE}
					<div class="grid" transition:slide={{ duration: 150 }}>
						<label class="field">
							<span>Deadband</span>
							<input
								type="number"
								class="input"
								bind:value={deadbandValue}
								placeholder="0"
								min="0"
								step="0.1"
							/>
						</label>
						<label class="field">
							<span>Min time (ms)</span>
							<input
								type="number"
								class="input"
								bind:value={deadbandMinTime}
								placeholder="none"
								min="0"
								step="100"
							/>
						</label>
						<label class="field">
							<span>Max time (ms)</span>
							<input
								type="number"
								class="input"
								bind:value={deadbandMaxTime}
								placeholder="none"
								min="0"
								step="1000"
							/>
						</label>
					</div>
				{/if}
			</div>

			{#if !isNew && existing}
				<div class="section meta-row">
					<span>{variableCount} variable(s) bound to this source</span>
				</div>
			{/if}
		</div>
	{/if}
</div>

<style lang="scss">
	.source-editor {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		background: var(--theme-background);
	}

	.se-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		padding: 0.375rem 0.625rem;
		border-bottom: 1px solid var(--theme-border);
		background: var(--theme-surface);
	}

	.left,
	.right {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.name {
		font-family: var(--font-mono, monospace);
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--theme-text);
	}

	.kind-badge {
		padding: 0.0625rem 0.375rem;
		font-size: 0.625rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		border-radius: 0.1875rem;
	}

	.proto-badge {
		padding: 0.0625rem 0.375rem;
		font-size: 0.625rem;
		font-weight: 600;
		color: var(--theme-text-muted);
		background: var(--theme-surface);
		border: 1px solid var(--theme-border);
		border-radius: 0.1875rem;
		font-family: var(--font-mono, monospace);
	}

	.btn {
		padding: 0.3125rem 0.75rem;
		font-size: 0.8125rem;
		font-weight: 500;
		border: 1px solid var(--theme-border);
		border-radius: 0.3125rem;
		background: transparent;
		color: var(--theme-text);
		cursor: pointer;

		&:hover:not(:disabled) {
			border-color: var(--theme-text-muted);
		}

		&.primary {
			background: var(--theme-primary);
			color: var(--theme-on-primary, white);
			border-color: var(--theme-primary);

			&:hover:not(:disabled) {
				opacity: 0.9;
			}
		}

		&.subtle {
			color: var(--theme-text-muted);
		}

		&.danger {
			color: var(--theme-error, #e5484d);
			border-color: color-mix(in srgb, var(--theme-error, #e5484d) 40%, transparent);

			&:hover:not(:disabled) {
				background: color-mix(in srgb, var(--theme-error, #e5484d) 12%, transparent);
				border-color: var(--theme-error, #e5484d);
			}
		}

		&:disabled {
			opacity: 0.5;
			cursor: not-allowed;
		}
	}

	.se-body {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.status {
		padding: 1rem;
		color: var(--theme-text-muted);
		font-size: 0.875rem;
	}

	.section {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding: 0.75rem;
		border: 1px solid var(--theme-border);
		border-radius: 0.375rem;
		background: color-mix(in srgb, var(--theme-surface) 60%, transparent);
	}

	.section-label {
		font-size: 0.6875rem;
		color: var(--theme-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		font-weight: 600;
	}

	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
		gap: 0.625rem;
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 0.3125rem;
		font-size: 0.8125rem;
		color: var(--theme-text);

		&.wide {
			grid-column: 1 / -1;
		}

		> span {
			font-size: 0.75rem;
			color: var(--theme-text-muted);
			font-weight: 500;
		}
	}

	.input {
		padding: 0.375rem 0.5rem;
		font-size: 0.8125rem;
		background: var(--theme-background);
		color: var(--theme-text);
		border: 1px solid var(--theme-border);
		border-radius: 0.25rem;
		font-family: inherit;

		&:focus {
			outline: none;
			border-color: var(--theme-primary);
		}
	}

	.hint {
		margin: 0;
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		font-style: italic;

		&.error {
			color: var(--theme-error, #e5484d);
			font-style: normal;
		}

		&.muted {
			grid-column: 1 / -1;
		}
	}

	.info-note {
		padding: 0.5rem 0.625rem;
		font-size: 0.8125rem;
		color: var(--theme-text-muted);
		background: color-mix(in srgb, var(--theme-primary) 8%, transparent);
		border: 1px solid color-mix(in srgb, var(--theme-primary) 30%, var(--theme-border));
		border-radius: 0.3125rem;
	}

	.checkbox-label {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.8125rem;
		color: var(--theme-text);
		cursor: pointer;

		input[type='checkbox'] {
			width: 1rem;
			height: 1rem;
			cursor: pointer;
		}
	}

	.meta-row {
		flex-direction: row;
		justify-content: flex-end;
		color: var(--theme-text-muted);
		font-size: 0.75rem;
		background: transparent;
		border: 0;
		padding: 0.25rem 0.5rem;
	}
</style>
