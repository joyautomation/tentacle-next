<script lang="ts">
	import type { GatewayUdtTemplateMember, DeadBandConfig } from '$lib/types/gateway';
	import { slide } from 'svelte/transition';
	import { PencilSquare } from '@joyautomation/salt/icons';
	import InlineEditCell from './InlineEditCell.svelte';
	import TabToolbar from './TabToolbar.svelte';

	let {
		members,
		excludedMembers,
		deviceDeadband,
		editingCell,
		editDraft,
		onToggleMember,
		onStartEdit,
		onCancelEdit,
		onSetDeadband,
		onClearDeadband,
		onBatchApply,
		onBatchClear,
		onBatchMqttEnable,
		onBatchMqttDisable,
		dirtyMembers
	}: {
		members: GatewayUdtTemplateMember[];
		excludedMembers: Set<string>;
		deviceDeadband: DeadBandConfig | null;
		dirtyMembers: Set<string>;
		editingCell: string | null;
		editDraft: string;
		onToggleMember: (memberName: string) => void;
		onStartEdit: (key: string, value: number) => void;
		onCancelEdit: () => void;
		onSetDeadband: (memberName: string, field: 'value' | 'minTime' | 'maxTime', val: number | null) => void;
		onClearDeadband: (memberName: string) => void;
		onBatchApply: (memberNames: string[], db: DeadBandConfig) => void;
		onBatchClear: (memberNames: string[]) => void;
		onBatchMqttEnable: (memberNames: string[]) => void;
		onBatchMqttDisable: (memberNames: string[]) => void;
	} = $props();

	// Local filter state
	let filter = $state('');

	const filteredMembers = $derived.by(() => {
		const q = filter?.toLowerCase() ?? '';
		if (!q) return members;
		return members.filter(m => m.name.toLowerCase().includes(q));
	});

	// Filtered analog non-excluded member names for batch
	const filteredBatchMemberNames = $derived.by((): string[] => {
		return filteredMembers
			.filter(m => m.datatype === 'number' && !excludedMembers.has(m.name))
			.map(m => m.name);
	});

	function handleBatchApply(db: DeadBandConfig) {
		onBatchApply(filteredBatchMemberNames, db);
	}

	function handleBatchClear() {
		onBatchClear(filteredBatchMemberNames);
	}

	function getFilteredMemberNames(): string[] {
		return filteredMembers.map(m => m.name);
	}

	function handleBatchMqttEnable() {
		onBatchMqttEnable(getFilteredMemberNames());
	}

	function handleBatchMqttDisable() {
		onBatchMqttDisable(getFilteredMemberNames());
	}
</script>

<TabToolbar
	filters={[
		{ label: 'Member Filter', value: filter, placeholder: 'Filter members...', oninput: (v) => filter = v },
	]}
	showEnableRbe={false}
	onBatchApply={handleBatchApply}
	onBatchClear={handleBatchClear}
	onBatchMqttEnable={handleBatchMqttEnable}
	onBatchMqttDisable={handleBatchMqttDisable}
/>

<div class="tc-scroll">
<table class="tpl-table">
	<thead>
		<tr>
			<th style="width: 25%">Member</th>
			<th style="width: 8%">Type</th>
			<th style="width: 10%">MQTT</th>
			<th style="width: 10%" title="Report by exception">RBE</th>
			<th style="width: 12%">Deadband</th>
			<th style="width: 12%">Min (ms)</th>
			<th style="width: 12%">Max (ms)</th>
			<th style="width: 9%">Actions</th>
		</tr>
	</thead>
	<tbody>
		{#each filteredMembers as member (member.name)}
			{@const excluded = excludedMembers.has(member.name)}
			{@const isAnalog = member.datatype === 'number'}
			{@const dbCellKey = 'tpl::db::' + member.name}
			{@const minCellKey = 'tpl::min::' + member.name}
			{@const maxCellKey = 'tpl::max::' + member.name}
			{@const hasDeadband = member.defaultDeadband != null}
			<tr class:row-dirty={dirtyMembers.has(member.name)}>
				<td data-label="Member">
					{#if dirtyMembers.has(member.name)}<span class="dirty-icon" title="Unsaved changes" transition:slide|local={{ axis: 'x', duration: 150 }}><PencilSquare size="1rem" /></span>{/if}
					<span class="mono item-name">{member.name}</span>
				</td>
				<td data-label="Type">
					<span class="type-badge" class:type-number={isAnalog} class:type-bool={!isAnalog}>
						{member.datatype}
					</span>
				</td>
				<td data-label="MQTT">
					<label class="toggle-switch">
						<input
							type="checkbox"
							checked={!excluded}
							onchange={() => onToggleMember(member.name)}
						/>
						<span class="toggle-track"></span>
					</label>
				</td>
				<td data-label="RBE">
					{#if excluded}
						<span class="rbe-na">disabled</span>
					{:else if isAnalog}
						{#if hasDeadband}
							<span class="status-override">override</span>
						{:else if deviceDeadband}
							<a class="status-inherited link" href="/services/gateway/devices" title="Inherited from device">inherited</a>
						{:else}
							<span class="rbe-na"></span>
						{/if}
					{:else}
						<span class="rbe-na"></span>
					{/if}
				</td>
				<td data-label="Deadband">
					{#if isAnalog && !excluded}
						<InlineEditCell
							editing={editingCell === dbCellKey}
							draft={editDraft}
							onCommit={(val) => onSetDeadband(member.name, 'value', val)}
							onCancel={onCancelEdit}
						>
							<button
								class="il-value"
								class:overridden={hasDeadband}
								class:inherited-device={!hasDeadband && deviceDeadband != null}
								onclick={() =>
									onStartEdit(dbCellKey, member.defaultDeadband?.value ?? deviceDeadband?.value ?? 0)}
							>
								{#if hasDeadband}
									{member.defaultDeadband?.value}
								{:else if deviceDeadband}
									<span class="device-inherit">{deviceDeadband.value}</span>
								{:else}
									—
								{/if}
							</button>
						</InlineEditCell>
					{:else}
						<span class="rbe-na"></span>
					{/if}
				</td>
				<td data-label="Min (ms)">
					{#if isAnalog && !excluded}
						<InlineEditCell
							editing={editingCell === minCellKey}
							draft={editDraft}
							onCommit={(val) => onSetDeadband(member.name, 'minTime', val || null)}
							onCancel={onCancelEdit}
						>
							<button
								class="il-value"
								class:overridden={hasDeadband && member.defaultDeadband?.minTime != null}
								class:inherited-device={!hasDeadband && deviceDeadband?.minTime != null}
								onclick={() =>
									onStartEdit(minCellKey, member.defaultDeadband?.minTime ?? deviceDeadband?.minTime ?? 0)}
							>
								{#if hasDeadband && member.defaultDeadband?.minTime != null}
									{member.defaultDeadband.minTime}
								{:else if deviceDeadband?.minTime != null}
									<span class="device-inherit">{deviceDeadband.minTime}</span>
								{:else}
									—
								{/if}
							</button>
						</InlineEditCell>
					{:else}
						<span class="rbe-na"></span>
					{/if}
				</td>
				<td data-label="Max (ms)">
					{#if isAnalog && !excluded}
						<InlineEditCell
							editing={editingCell === maxCellKey}
							draft={editDraft}
							onCommit={(val) => onSetDeadband(member.name, 'maxTime', val || null)}
							onCancel={onCancelEdit}
						>
							<button
								class="il-value"
								class:overridden={hasDeadband && member.defaultDeadband?.maxTime != null}
								class:inherited-device={!hasDeadband && deviceDeadband?.maxTime != null}
								onclick={() =>
									onStartEdit(maxCellKey, member.defaultDeadband?.maxTime ?? deviceDeadband?.maxTime ?? 0)}
							>
								{#if hasDeadband && member.defaultDeadband?.maxTime != null}
									{member.defaultDeadband.maxTime}
								{:else if deviceDeadband?.maxTime != null}
									<span class="device-inherit">{deviceDeadband.maxTime}</span>
								{:else}
									—
								{/if}
							</button>
						</InlineEditCell>
					{:else}
						<span class="rbe-na"></span>
					{/if}
				</td>
				<td class="td-actions">
					{#if isAnalog && !excluded && hasDeadband}
						<button class="action-btn" onclick={() => onClearDeadband(member.name)}>
							revert
						</button>
					{/if}
				</td>
			</tr>
		{/each}
	</tbody>
</table>
</div>

<style lang="scss">
	@use './tag-table';

	.tc-scroll { flex: 1; overflow: auto; }

	@media (max-width: 640px) {
		.tpl-table td[data-label='Member'] {
			width: 100%;
			&::before { display: none; }
		}
	}
</style>
