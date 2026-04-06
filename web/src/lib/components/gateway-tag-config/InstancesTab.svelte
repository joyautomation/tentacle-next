<script lang="ts">
	import type { GatewayUdtTemplateMember, DeadBandConfig } from '$lib/types/gateway';
	import type { InstanceInfo } from './utils';
	import { slide } from 'svelte/transition';
	import { ChevronRight, PencilSquare } from '@joyautomation/salt/icons';
	import InlineEditCell from './InlineEditCell.svelte';
	import TabToolbar from './TabToolbar.svelte';

	let {
		instances,
		analogMembers,
		expandedInstances,
		checkedInstances,
		workingOverrides,
		editingCell,
		editDraft,
		onTogglePublish,
		onToggleExpand,
		onStartEdit,
		onCancelEdit,
		onSetOverride,
		onClearOverride,
		onToggleRbe,
		getEffectiveRbe,
		getOverrideCount,
		onGoToDefaults,
		onBatchApplyFiltered,
		onBatchClearFiltered,
		onBatchMqttEnable,
		onBatchMqttDisable,
		dirtyInstanceKeys,
		dirtyInstanceMembers
	}: {
		instances: InstanceInfo[];
		analogMembers: GatewayUdtTemplateMember[];
		dirtyInstanceKeys: Set<string>;
		dirtyInstanceMembers: Map<string, Set<string>>;
		expandedInstances: Set<string>;
		checkedInstances: Set<string>;
		workingOverrides: Map<string, Record<string, DeadBandConfig>>;
		editingCell: string | null;
		editDraft: string;
		onTogglePublish: (deviceId: string, tag: string) => void;
		onToggleExpand: (id: string) => void;
		onStartEdit: (key: string, value: number) => void;
		onCancelEdit: () => void;
		onSetOverride: (instanceId: string, memberName: string, field: 'value' | 'minTime' | 'maxTime', val: number | null) => void;
		onClearOverride: (instanceId: string, memberName: string) => void;
		onToggleRbe: (instanceId: string, memberName: string) => void;
		getEffectiveRbe: (memberName: string, instanceId: string) => { deadband: DeadBandConfig; inherited: boolean };
		getOverrideCount: (instanceId: string) => number;
		onGoToDefaults: () => void;
		onBatchApplyFiltered: (instanceIds: string[], memberNames: string[], db: DeadBandConfig) => void;
		onBatchClearFiltered: (instanceIds: string[], memberNames: string[]) => void;
		onBatchMqttEnable: (keys: string[]) => void;
		onBatchMqttDisable: (keys: string[]) => void;
	} = $props();

	// Local filter state
	let instanceFilter = $state('');
	let memberFilter = $state('');

	const filteredInstances = $derived.by(() => {
		const q = instanceFilter?.toLowerCase() ?? '';
		if (!q) return instances;
		return instances.filter(i => i.tag.toLowerCase().includes(q));
	});

	const filteredAnalogMembers = $derived.by(() => {
		const q = memberFilter?.toLowerCase() ?? '';
		if (!q) return analogMembers;
		return analogMembers.filter(m => m.name.toLowerCase().includes(q));
	});

	function handleBatchApply(db: DeadBandConfig) {
		onBatchApplyFiltered(filteredInstances.map(i => i.id), filteredAnalogMembers.map(m => m.name), db);
	}

	function handleBatchClear() {
		onBatchClearFiltered(filteredInstances.map(i => i.id), filteredAnalogMembers.map(m => m.name));
	}

	function getFilteredInstanceKeys(): string[] {
		return filteredInstances.map(i => `${i.deviceId}::${i.tag}`);
	}

	function handleBatchMqttEnable() {
		onBatchMqttEnable(getFilteredInstanceKeys());
	}

	function handleBatchMqttDisable() {
		onBatchMqttDisable(getFilteredInstanceKeys());
	}
</script>

<TabToolbar
	filters={[
		{ label: 'Instance Filter', value: instanceFilter, placeholder: 'Filter instances...', oninput: (v) => instanceFilter = v },
		{ label: 'Member Filter', value: memberFilter, placeholder: 'Filter members...', oninput: (v) => memberFilter = v },
	]}
	onBatchApply={handleBatchApply}
	onBatchClear={handleBatchClear}
	onBatchMqttEnable={handleBatchMqttEnable}
	onBatchMqttDisable={handleBatchMqttDisable}
/>

<div class="inst-toolbar">
	<span class="inst-toolbar-count">{filteredInstances.length} instances</span>
	<div class="inst-legend">
		<span class="legend-dot"></span>
		<span class="legend-text">= override</span>
	</div>
</div>

<div class="tc-scroll">
	{#if filteredInstances.length === 0}
		<div class="empty-state">
			<p>{instanceFilter ? 'No instances match the current filter.' : 'No instances found for this template.'}</p>
		</div>
	{/if}
	{#each filteredInstances as inst (`${inst.deviceId}::${inst.tag}`)}
		{@const expanded = expandedInstances.has(inst.id)}
		{@const ovCount = getOverrideCount(inst.id)}
		{@const publishKey = `${inst.deviceId}::${inst.tag}`}
		{@const isPublished = checkedInstances.has(publishKey)}
		<div class="inst-row" class:row-dirty={dirtyInstanceKeys.has(publishKey)}>
			<!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
			<div class="inst-head" onclick={() => onToggleExpand(inst.id)}>
				{#if dirtyInstanceKeys.has(publishKey)}<span class="dirty-icon" title="Unsaved changes" transition:slide|local={{ axis: 'x', duration: 150 }}><PencilSquare size="1rem" /></span>{/if}
				<!-- svelte-ignore a11y_no_noninteractive_element_interactions a11y_click_events_have_key_events -->
				<label class="toggle-switch" onclick={(e: MouseEvent) => e.stopPropagation()}>
					<input
						type="checkbox"
						checked={isPublished}
						onchange={() => onTogglePublish(inst.deviceId, inst.tag)}
					/>
					<span class="toggle-track"></span>
				</label>
				<span
					class="inst-expand"
					class:open={expanded}
				>
					<ChevronRight size="0.75rem" />
				</span>
				<span class="inst-name mono">{inst.tag}</span>
				<span class="inst-device mono">{inst.deviceId}</span>
				{#if isPublished}
					<span class="inst-ov-badge" class:has-overrides={ovCount > 0}>
						{ovCount > 0 ? `${ovCount} override${ovCount > 1 ? 's' : ''}` : 'inherited'}
					</span>
				{:else}
					<span class="inst-unpublished">unpublished</span>
				{/if}
			</div>

			{#if expanded}
				<div class="inst-detail" transition:slide|local={{ duration: 150 }}>
					{#if filteredAnalogMembers.length === 0}
						<p class="inst-empty">{memberFilter ? 'No members match the current filter.' : 'No analog members for RBE configuration.'}</p>
					{:else}
						<table class="inst-table">
							<thead>
								<tr>
									<th style="width:20%">Member</th>
									<th style="width:10%">RBE</th>
									<th style="width:14%">Deadband</th>
									<th style="width:12%">Min (ms)</th>
									<th style="width:12%">Max (ms)</th>
									<th style="width:12%">Status</th>
									<th style="width:10%"></th>
								</tr>
							</thead>
							<tbody>
								{#each filteredAnalogMembers as member}
									{@const eff = getEffectiveRbe(member.name, inst.id)}
									{@const valKey = `inst::${inst.id}::${member.name}::value`}
									{@const minKey = `inst::${inst.id}::${member.name}::minTime`}
									{@const maxKey = `inst::${inst.id}::${member.name}::maxTime`}
									{@const memberDirty = dirtyInstanceMembers.get(inst.id)?.has(member.name) ?? false}
									<tr class:row-override={!eff.inherited} class:row-dirty={memberDirty}>
										<td data-label="Member">{#if memberDirty}<span class="dirty-icon" title="Unsaved changes" transition:slide|local={{ axis: 'x', duration: 150 }}><PencilSquare size="1rem" /></span>{/if}<span class="mono item-name">{member.name}</span></td>
										<td data-label="RBE">
											<button
												class="rbe-toggle"
												class:rbe-overridden={!eff.inherited}
												onclick={() => onToggleRbe(inst.id, member.name)}
											>
												{eff.deadband.disableRBE ? 'disabled' : 'enabled'}
											</button>
										</td>
										<td data-label="Deadband">
											<InlineEditCell
												editing={editingCell === valKey}
												draft={editDraft}
												onCommit={(val) => onSetOverride(inst.id, member.name, 'value', val)}
												onCancel={onCancelEdit}
											>
												<button
													class="il-value"
													class:inherited={eff.inherited}
													class:overridden={!eff.inherited}
													onclick={() => onStartEdit(valKey, eff.deadband.value)}
												>
													{eff.deadband.value}
												</button>
											</InlineEditCell>
										</td>
										<td data-label="Min (ms)">
											<InlineEditCell
												editing={editingCell === minKey}
												draft={editDraft}
												onCommit={(val) => onSetOverride(inst.id, member.name, 'minTime', val || null)}
												onCancel={onCancelEdit}
											>
												<button
													class="il-value il-time"
													class:inherited={eff.inherited}
													class:overridden={!eff.inherited && eff.deadband.minTime != null}
													onclick={() => onStartEdit(minKey, eff.deadband.minTime ?? 0)}
												>
													{eff.deadband.minTime != null ? eff.deadband.minTime : '—'}
												</button>
											</InlineEditCell>
										</td>
										<td data-label="Max (ms)">
											<InlineEditCell
												editing={editingCell === maxKey}
												draft={editDraft}
												onCommit={(val) => onSetOverride(inst.id, member.name, 'maxTime', val || null)}
												onCancel={onCancelEdit}
											>
												<button
													class="il-value il-time"
													class:inherited={eff.inherited}
													class:overridden={!eff.inherited && eff.deadband.maxTime != null}
													onclick={() => onStartEdit(maxKey, eff.deadband.maxTime ?? 0)}
												>
													{eff.deadband.maxTime != null ? eff.deadband.maxTime : '—'}
												</button>
											</InlineEditCell>
										</td>
										<td data-label="Status">
											{#if eff.inherited}
												<button class="status-inherited link" onclick={onGoToDefaults} title="View template defaults">inherited</button>
											{:else}
												<span class="status-override">override</span>
											{/if}
										</td>
										<td class="td-actions">
											{#if !eff.inherited}
												<button
													class="action-btn"
													onclick={() => onClearOverride(inst.id, member.name)}
													>revert</button
												>
											{/if}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					{/if}
				</div>
			{/if}
		</div>
	{/each}
</div>

<style lang="scss">
	@use './tag-table';

	.tc-scroll {
		flex: 1;
		overflow: auto;
	}

	.inst-toolbar {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.625rem 1.25rem;
		border-bottom: 1px solid var(--theme-border);
		background: var(--theme-surface);
	}
	.inst-toolbar-count {
		font-size: 0.6875rem;
		color: var(--theme-text-muted);
	}
	.inst-legend {
		margin-left: auto;
		font-size: 0.625rem;
		color: var(--theme-text-muted);
		display: flex;
		align-items: center;
		gap: 0.375rem;
	}
	.legend-dot {
		width: 8px;
		height: 8px;
		border-radius: 2px;
		background: #a78bfa;
	}
	.legend-text {
		font-family: 'IBM Plex Mono', monospace;
	}

	.dirty-icon {
		display: inline-flex; align-items: center; flex-shrink: 0;
		color: #f59e0b;
		vertical-align: middle; margin-right: 0.375rem;
		overflow: hidden;
		:global(svg) { flex-shrink: 0; }
	}

	.inst-row {
		border-bottom: 1px solid var(--theme-border);
	}
	.inst-head {
		display: flex;
		align-items: center;
		gap: 0.625rem;
		padding: 0.625rem 1.25rem;
		cursor: pointer;
		&:hover {
			background: color-mix(in srgb, var(--theme-text) 3%, transparent);
		}
	}
	.inst-expand {
		display: inline-flex;
		flex-shrink: 0;
		color: var(--theme-text-muted);
		transition: transform 0.15s ease;
		&.open {
			transform: rotate(90deg);
		}
	}
	.inst-name {
		font-weight: 600;
		font-size: 0.8125rem;
		color: var(--theme-text);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.inst-device {
		font-size: 0.5625rem;
		color: var(--theme-text-muted);
		background: color-mix(in srgb, var(--theme-text) 6%, transparent);
		padding: 0.1rem 0.4rem;
		border-radius: var(--rounded-sm);
	}
	.inst-ov-badge {
		margin-left: auto;
		font-size: 0.625rem;
		font-family: 'IBM Plex Mono', monospace;
		padding: 0.1rem 0.5rem;
		border-radius: var(--rounded-sm);
		white-space: nowrap;
		color: var(--theme-text-muted);
		&.has-overrides {
			background: rgba(167, 139, 250, 0.08);
			color: #a78bfa;
			border: 1px solid rgba(167, 139, 250, 0.25);
		}
	}
	.inst-unpublished {
		margin-left: auto;
		font-size: 0.625rem;
		font-family: 'IBM Plex Mono', monospace;
		color: var(--theme-text-muted);
		padding: 0.1rem 0.5rem;
	}

	.inst-detail {
		padding: 0 1.25rem 0.75rem 3.5rem;
	}
	.inst-table {
		width: 100%;
		border-collapse: separate;
		border-spacing: 0;
	}
	.inst-table th {
		text-align: left;
		font-size: 0.5625rem;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--theme-text-muted);
		padding: 0.375rem 0.625rem;
		border-bottom: 1px solid var(--theme-border);
	}
	.inst-table td {
		padding: 0.3rem 0.625rem;
		border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
		font-size: 0.8125rem;
	}
	.inst-table tr:hover td {
		background: color-mix(in srgb, var(--theme-text) 2%, transparent);
	}
	.inst-table tr.row-override td {
		background: rgba(167, 139, 250, 0.06);
	}
	.inst-table tr.row-override:hover td {
		background: rgba(167, 139, 250, 0.10);
	}
	.inst-empty {
		font-size: 0.8125rem;
		color: var(--theme-text-muted);
		padding: 0.5rem 0;
	}
	.rbe-toggle {
		font-size: 0.625rem;
		font-family: 'IBM Plex Mono', monospace;
		color: var(--theme-text-muted);
		cursor: pointer;
		border: none;
		background: none;
		border-radius: 0;
		padding: 0;
		&:hover { opacity: 0.8; }
		&.rbe-overridden {
			color: #a78bfa;
		}
	}

	// Mobile
	@media (max-width: 640px) {
		.inst-head {
			padding: 0.625rem 0.875rem;
			gap: 0.5rem;
		}
		.inst-device {
			display: none;
		}
		.inst-detail {
			padding: 0.25rem 0.875rem 0.75rem;
		}
		// Card layout for member table
		.inst-table thead {
			display: none;
		}
		.inst-table,
		.inst-table tbody {
			display: block;
		}
		.inst-table tr {
			display: grid;
			grid-template-columns: 1fr 1fr 1fr;
			gap: 0.25rem 0.5rem;
			padding: 0.5rem 0;
			border-bottom: 1px solid color-mix(in srgb, var(--theme-border) 50%, transparent);
			&:last-child { border-bottom: none; }
		}
		.inst-table td {
			display: flex;
			align-items: center;
			gap: 0.25rem;
			padding: 0.15rem 0;
			border: none;
			font-size: 0.75rem;
			&::before {
				content: attr(data-label);
				font-size: 0.5625rem;
				font-weight: 700;
				text-transform: uppercase;
				letter-spacing: 0.04em;
				color: var(--theme-text-muted);
				min-width: 3rem;
				flex-shrink: 0;
			}
			&[data-label='Member'] {
				grid-column: 1 / -1;
				&::before { display: none; }
			}
		}
		.td-actions {
			&::before { display: none; }
		}
		.il-input {
			width: 100%;
		}
		.bulk-bar {
			padding: 0.625rem 0.875rem;
		}
		.bulk-sep {
			display: none;
		}
		.bulk-btn {
			flex: 1;
			text-align: center;
			font-size: 0.75rem;
		}
		.batch-panel {
			position: fixed;
			bottom: 0;
			left: 0;
			right: 0;
			border-radius: var(--rounded-lg) var(--rounded-lg) 0 0;
			min-width: auto;
		}
	}
</style>
