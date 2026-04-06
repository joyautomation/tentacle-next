<script lang="ts">
	import type { BrowseCache, DeadBandConfig } from '$lib/types/gateway';
	import { mapDatatype, type RbeState } from './utils';
	import { slide } from 'svelte/transition';
	import { PencilSquare } from '@joyautomation/salt/icons';
	import InlineEditCell from './InlineEditCell.svelte';
	import TabToolbar from './TabToolbar.svelte';

	let {
		deviceId,
		browseCache,
		deviceDeadband,
		checkedAtomicTags,
		rbeOverrides,
		editingCell,
		editDraft,
		onToggleTag,
		onStartEdit,
		onCancelEdit,
		onSetRbeMode,
		onToggleRbe,
		onUpdateRbeDeadband,
		onBatchApply,
		onBatchClear,
		onBatchMqttEnable,
		onBatchMqttDisable,
		dirtyAtomicKeys
	}: {
		deviceId: string;
		browseCache: BrowseCache | null;
		deviceDeadband: DeadBandConfig | null;
		checkedAtomicTags: Set<string>;
		rbeOverrides: Map<string, RbeState>;
		dirtyAtomicKeys: Set<string>;
		editingCell: string | null;
		editDraft: string;
		onToggleTag: (deviceId: string, tag: string) => void;
		onStartEdit: (key: string, value: number) => void;
		onCancelEdit: () => void;
		onSetRbeMode: (key: string, mode: 'default' | 'custom') => void;
		onToggleRbe: (key: string) => void;
		onUpdateRbeDeadband: (
			key: string,
			field: 'value' | 'minTime' | 'maxTime',
			val: number | null
		) => void;
		onBatchApply: (keys: string[], db: DeadBandConfig) => void;
		onBatchClear: (keys: string[]) => void;
		onBatchMqttEnable: (keys: string[]) => void;
		onBatchMqttDisable: (keys: string[]) => void;
	} = $props();

	// Local filter/sort state
	let filter = $state('');

	type SortCol = 'tag' | 'type' | 'mqtt' | 'rbe' | 'deadband' | 'minTime' | 'maxTime' | 'status';
	let sortCol: SortCol = $state('tag');
	let sortAsc = $state(true);

	function toggleSort(col: SortCol) {
		if (sortCol === col) { sortAsc = !sortAsc; }
		else { sortCol = col; sortAsc = true; }
	}

	const atomicItems = $derived.by(
		(): { tag: string; name: string; datatype: string; protocolType: string }[] => {
			if (!browseCache) return [];
			const structTags = browseCache.structTags ?? {};
			const q = filter?.toLowerCase() ?? '';
			const filtered = browseCache.items
				.filter((item) => {
					if (structTags[item.tag]) return false;
					if (item.tag.includes('.')) return false;
					if (
						q &&
						!item.name.toLowerCase().includes(q) &&
						!item.tag.toLowerCase().includes(q)
					)
						return false;
					return true;
				})
				.map((item) => ({ ...item, datatype: mapDatatype(item.datatype) }));

			const dir = sortAsc ? 1 : -1;
			return filtered.sort((a, b) => {
				const keyA = `${deviceId}::${a.tag}`;
				const keyB = `${deviceId}::${b.tag}`;
				const pubA = checkedAtomicTags.has(keyA);
				const pubB = checkedAtomicTags.has(keyB);
				const rbeA = rbeOverrides.get(keyA);
				const rbeB = rbeOverrides.get(keyB);
				switch (sortCol) {
					case 'tag': return dir * a.tag.localeCompare(b.tag);
					case 'type': return dir * a.datatype.localeCompare(b.datatype);
					case 'mqtt': return dir * (Number(pubA) - Number(pubB));
					case 'rbe': return dir * (Number(!!rbeA?.disableRBE) - Number(!!rbeB?.disableRBE));
					case 'deadband': return dir * ((rbeA?.deadband?.value ?? 0) - (rbeB?.deadband?.value ?? 0));
					case 'minTime': return dir * ((rbeA?.deadband?.minTime ?? 0) - (rbeB?.deadband?.minTime ?? 0));
					case 'maxTime': return dir * ((rbeA?.deadband?.maxTime ?? 0) - (rbeB?.deadband?.maxTime ?? 0));
					case 'status': {
						const rank = (pub: boolean, analog: boolean, hasCustom: boolean) =>
							!pub ? 0 : !analog ? 1 : hasCustom ? 3 : 2;
						return dir * (rank(pubA, a.datatype === 'number', rbeA?.mode === 'custom') - rank(pubB, b.datatype === 'number', rbeB?.mode === 'custom'));
					}
					default: return dir * a.tag.localeCompare(b.tag);
				}
			});
		}
	);

	function getFilteredBatchKeys(): string[] {
		return atomicItems
			.filter(item => item.datatype === 'number' && checkedAtomicTags.has(`${deviceId}::${item.tag}`))
			.map(item => `${deviceId}::${item.tag}`);
	}

	function handleBatchApply(db: DeadBandConfig) {
		onBatchApply(getFilteredBatchKeys(), db);
	}

	function handleBatchClear() {
		onBatchClear(getFilteredBatchKeys());
	}

	function getFilteredTagKeys(): string[] {
		return atomicItems.map(item => `${deviceId}::${item.tag}`);
	}

	function handleBatchMqttEnable() {
		onBatchMqttEnable(getFilteredTagKeys());
	}

	function handleBatchMqttDisable() {
		onBatchMqttDisable(getFilteredTagKeys());
	}
</script>

<TabToolbar
	filters={[
		{ label: 'Tag Filter', value: filter, placeholder: 'Filter tags...', oninput: (v) => filter = v },
	]}
	onBatchApply={handleBatchApply}
	onBatchClear={handleBatchClear}
	onBatchMqttEnable={handleBatchMqttEnable}
	onBatchMqttDisable={handleBatchMqttDisable}
/>

<div class="tc-scroll">
{#if !browseCache || atomicItems.length === 0}
	<div class="empty-state">
		<p>
			{browseCache
				? 'No atomic tags match the current filter.'
				: 'No browse data. Click Refresh to discover tags.'}
		</p>
	</div>
{:else}
	<table class="tpl-table">
		<thead>
			<tr>
				<th style="width: 20%" class="sortable" class:sorted={sortCol === 'tag'} onclick={() => toggleSort('tag')}>Tag <span class="sort-arrow">{sortCol === 'tag' ? (sortAsc ? '▲' : '▼') : ''}</span></th>
				<th style="width: 7%" class="sortable" class:sorted={sortCol === 'type'} onclick={() => toggleSort('type')}>Type <span class="sort-arrow">{sortCol === 'type' ? (sortAsc ? '▲' : '▼') : ''}</span></th>
				<th style="width: 7%" class="sortable" class:sorted={sortCol === 'mqtt'} onclick={() => toggleSort('mqtt')}>MQTT <span class="sort-arrow">{sortCol === 'mqtt' ? (sortAsc ? '▲' : '▼') : ''}</span></th>
				<th style="width: 8%" class="sortable" class:sorted={sortCol === 'rbe'} onclick={() => toggleSort('rbe')}>RBE <span class="sort-arrow">{sortCol === 'rbe' ? (sortAsc ? '▲' : '▼') : ''}</span></th>
				<th style="width: 12%" class="sortable" class:sorted={sortCol === 'deadband'} onclick={() => toggleSort('deadband')}>Deadband <span class="sort-arrow">{sortCol === 'deadband' ? (sortAsc ? '▲' : '▼') : ''}</span></th>
				<th style="width: 11%" class="sortable" class:sorted={sortCol === 'minTime'} onclick={() => toggleSort('minTime')}>Min (ms) <span class="sort-arrow">{sortCol === 'minTime' ? (sortAsc ? '▲' : '▼') : ''}</span></th>
				<th style="width: 11%" class="sortable" class:sorted={sortCol === 'maxTime'} onclick={() => toggleSort('maxTime')}>Max (ms) <span class="sort-arrow">{sortCol === 'maxTime' ? (sortAsc ? '▲' : '▼') : ''}</span></th>
				<th style="width: 10%" class="sortable" class:sorted={sortCol === 'status'} onclick={() => toggleSort('status')}>Status <span class="sort-arrow">{sortCol === 'status' ? (sortAsc ? '▲' : '▼') : ''}</span></th>
				<th style="width: 8%"></th>
			</tr>
		</thead>
		<tbody>
			{#each atomicItems as item (item.tag)}
				{@const key = `${deviceId}::${item.tag}`}
				{@const published = checkedAtomicTags.has(key)}
				{@const isAnalog = item.datatype === 'number'}
				{@const rbe = rbeOverrides.get(key) ?? { mode: 'default' }}
				{@const valKey = `atomic::${key}::value`}
				{@const minKey = `atomic::${key}::minTime`}
				{@const maxKey = `atomic::${key}::maxTime`}
				{@const hasOverride = rbeOverrides.has(key)}
				{@const hasCustom = rbe.mode === 'custom'}
				{@const isDisabled = !!rbe.disableRBE}
				<tr class:row-override={hasOverride} class:row-dirty={dirtyAtomicKeys.has(key)}>
					<td data-label="Tag">
						{#if dirtyAtomicKeys.has(key)}<span class="dirty-icon" title="Unsaved changes" transition:slide|local={{ axis: 'x', duration: 150 }}><PencilSquare size="1rem" /></span>{/if}
						<span class="mono item-name">{item.tag}</span>
					</td>
					<td data-label="Type">
						<span
							class="type-badge"
							class:type-number={isAnalog}
							class:type-bool={item.datatype === 'boolean'}
						>
							{item.datatype}
						</span>
					</td>
					<td data-label="MQTT">
						<label class="toggle-switch">
							<input
								type="checkbox"
								checked={published}
								onchange={() => onToggleTag(deviceId, item.tag)}
							/>
							<span class="toggle-track"></span>
						</label>
					</td>
					<td data-label="RBE">
						{#if isAnalog && published}
							<button
								class="rbe-toggle"
								class:rbe-overridden={hasOverride}
								onclick={() => onToggleRbe(key)}
							>
								{isDisabled ? 'disabled' : 'enabled'}
							</button>
						{/if}
					</td>
					<td data-label="Deadband">
						{#if isAnalog}
							<InlineEditCell
								editing={editingCell === valKey}
								draft={editDraft}
								onCommit={(val) => onUpdateRbeDeadband(key, 'value', val)}
								onCancel={onCancelEdit}
							>
								<button
									class="il-value"
									class:overridden={hasCustom}
									class:inherited-device={!hasCustom && deviceDeadband != null}
									onclick={() =>
										onStartEdit(
											valKey,
											rbe.deadband?.value ?? deviceDeadband?.value ?? 0
										)}
								>
									{#if hasCustom}
										{rbe.deadband?.value}
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
						{#if isAnalog}
							<InlineEditCell
								editing={editingCell === minKey}
								draft={editDraft}
								onCommit={(val) => onUpdateRbeDeadband(key, 'minTime', val || null)}
								onCancel={onCancelEdit}
							>
								<button
									class="il-value il-time"
									class:overridden={hasCustom}
									class:inherited-device={!hasCustom && deviceDeadband?.minTime != null}
									onclick={() =>
										onStartEdit(
											minKey,
											rbe.deadband?.minTime ?? deviceDeadband?.minTime ?? 0
										)}
								>
									{#if hasCustom && rbe.deadband?.minTime != null}
										{rbe.deadband.minTime}
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
						{#if isAnalog}
							<InlineEditCell
								editing={editingCell === maxKey}
								draft={editDraft}
								onCommit={(val) => onUpdateRbeDeadband(key, 'maxTime', val || null)}
								onCancel={onCancelEdit}
							>
								<button
									class="il-value il-time"
									class:overridden={hasCustom}
									class:inherited-device={!hasCustom && deviceDeadband?.maxTime != null}
									onclick={() =>
										onStartEdit(
											maxKey,
											rbe.deadband?.maxTime ?? deviceDeadband?.maxTime ?? 0
										)}
								>
									{#if hasCustom && rbe.deadband?.maxTime != null}
										{rbe.deadband.maxTime}
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
					<td data-label="Status">
						{#if !published}
							<span class="rbe-na">disabled</span>
						{:else if !isAnalog}
							<span class="rbe-na"></span>
						{:else if hasCustom}
							<span class="status-override">override</span>
						{:else if deviceDeadband}
							<a class="status-inherited link" href="/services/gateway/devices" title="Inherited from device">inherited</a>
						{:else}
							<span class="rbe-na"></span>
						{/if}
					</td>
					<td class="td-actions">
						{#if isAnalog && published && hasCustom}
							<button class="action-btn" onclick={() => onSetRbeMode(key, 'default')}
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

<style lang="scss">
	@use './tag-table';

	.tc-scroll { flex: 1; overflow: auto; }

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

	@media (max-width: 640px) {
		.tpl-table td[data-label='Tag'] {
			width: 100%;
			&::before { display: none; }
		}
	}
</style>
