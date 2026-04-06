<script lang="ts">
	import type { DeadBandConfig } from '$lib/types/gateway';
	import { AdjustmentsHorizontal, XMark } from '@joyautomation/salt/icons';

	type FilterDef = { label: string; value: string; placeholder: string; oninput: (value: string) => void };

	let {
		filters,
		showEnableRbe = true,
		onBatchApply,
		onBatchClear,
		onBatchMqttEnable,
		onBatchMqttDisable
	}: {
		filters: FilterDef[];
		showEnableRbe?: boolean;
		onBatchApply: (db: DeadBandConfig) => void;
		onBatchClear: () => void;
		onBatchMqttEnable?: () => void;
		onBatchMqttDisable?: () => void;
	} = $props();

	let batchOpen = $state(false);
	let deadband = $state(1.0);
	let minTime: number | null = $state(null);
	let maxTime: number | null = $state(null);
	let enableRbe = $state(true);

	function handleApply() {
		const db: DeadBandConfig = {
			value: deadband,
			...(minTime != null ? { minTime } : {}),
			...(maxTime != null ? { maxTime } : {}),
			...(showEnableRbe && !enableRbe ? { disableRBE: true } : {}),
		};
		onBatchApply(db);
	}
</script>

<div class="tab-toolbar">
	{#each filters as f}
		<div class="filter-group">
			<span class="filter-label">{f.label}</span>
			<div class="filter-wrap">
				<input
					type="text"
					value={f.value}
					oninput={(e) => f.oninput((e.target as HTMLInputElement).value)}
					placeholder={f.placeholder}
					class="tab-filter"
				/>
				{#if f.value}
					<button class="filter-clear" onclick={() => f.oninput('')} aria-label="Clear filter">
						<XMark size="0.75rem" />
					</button>
				{/if}
			</div>
		</div>
	{/each}
	{#if onBatchMqttEnable || onBatchMqttDisable}
		<div class="batch-mqtt-group">
			{#if onBatchMqttEnable}
				<button class="batch-mqtt-btn mqtt-enable" onclick={onBatchMqttEnable}>MQTT All</button>
			{/if}
			{#if onBatchMqttDisable}
				<button class="batch-mqtt-btn mqtt-disable" onclick={onBatchMqttDisable}>MQTT None</button>
			{/if}
		</div>
	{/if}
	<div class="batch-dropdown">
		<button class="batch-toggle-btn" class:open={batchOpen} onclick={() => batchOpen = !batchOpen}>
			<AdjustmentsHorizontal size="0.875rem" /> Batch RBE
		</button>
		{#if batchOpen}
			<div class="batch-panel">
				<div class="batch-panel-row">
					<div class="batch-group">
						<span class="batch-group-label">Deadband</span>
						<input type="number" value={deadband} oninput={(e) => deadband = parseFloat((e.target as HTMLInputElement).value) || 0} step="0.1" min="0" class="batch-input" />
					</div>
					<div class="batch-group">
						<span class="batch-group-label">Min (ms)</span>
						<input type="number" value={minTime ?? ''} onchange={(e) => { const v = (e.target as HTMLInputElement).value; minTime = v ? parseInt(v) : null; }} step="100" min="0" placeholder="—" class="batch-input" />
					</div>
					<div class="batch-group">
						<span class="batch-group-label">Max (ms)</span>
						<input type="number" value={maxTime ?? ''} onchange={(e) => { const v = (e.target as HTMLInputElement).value; maxTime = v ? parseInt(v) : null; }} step="100" min="0" placeholder="—" class="batch-input" />
					</div>
				</div>
				{#if showEnableRbe}
					<label class="batch-toggle-field">
						<input type="checkbox" checked={enableRbe} onchange={(e) => enableRbe = (e.target as HTMLInputElement).checked} />
						Enable RBE
					</label>
				{/if}
				<div class="batch-actions">
					<button class="batch-btn" onclick={handleApply}>Apply to filtered</button>
					<button class="batch-btn" onclick={onBatchClear}>Clear to default</button>
				</div>
			</div>
		{/if}
	</div>
</div>

<style lang="scss">
	@use './tab-toolbar';
</style>
