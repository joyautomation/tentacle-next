<script lang="ts">
	import { onMount } from 'svelte';
	import type { PlcVariableConfig, PlcTaskConfig, ProgramListItem } from '$lib/types/plc';
	import { api } from '$lib/api/client';
	import {
		watchVariable,
		liveValuesVersion,
		getLiveValue
	} from '$lib/plc/live-values.svelte';
	import {
		startTaskStats,
		taskStatsVersion,
		getTaskStats
	} from '$lib/plc/task-stats.svelte';
	import {
		workspaceSelection,
		workspaceVariableDrafts,
		workspaceReferences,
		workspaceOutput,
		type ReferenceSite
	} from '../workspace-state.svelte';
	import { Link } from '@joyautomation/salt/icons';
	import ValueTree from '$lib/components/ValueTree.svelte';

	async function findReferences(name: string, kind: 'program' | 'variable') {
		workspaceReferences.setLoading(name, kind);
		workspaceOutput.setTab('references');
		const res = await api<ReferenceSite[]>(
			`/plcs/plc/references?name=${encodeURIComponent(name)}&kind=${kind}`
		);
		if (res.error) {
			workspaceReferences.setError(name, kind, res.error.error);
			return;
		}
		workspaceReferences.setResult(name, kind, res.data ?? []);
	}

	function isStruct(v: unknown): boolean {
		return v !== null && typeof v === 'object';
	}

	type Props = {
		variables: Record<string, PlcVariableConfig>;
		tasks: Record<string, PlcTaskConfig>;
		programs: ProgramListItem[];
	};

	let { variables, tasks, programs }: Props = $props();

	let now = $state(Date.now());

	onMount(() => {
		const tick = setInterval(() => {
			now = Date.now();
		}, 1000);
		const stopStats = startTaskStats();
		return () => {
			clearInterval(tick);
			stopStats();
		};
	});

	const selection = $derived(workspaceSelection.current);

	// Watch whichever variable is currently selected; swap on change.
	$effect(() => {
		if (selection?.kind !== 'variable') return;
		const stop = watchVariable(selection.id);
		return stop;
	});

	const selectedVariable = $derived.by(() => {
		if (selection?.kind !== 'variable') return null;
		void liveValuesVersion();
		const persisted = variables[selection.id];
		const draft = workspaceVariableDrafts.get(selection.id);
		const config = draft
			? {
					...(persisted ?? {}),
					datatype: draft.datatype,
					direction: draft.direction,
					description: draft.description ?? persisted?.description,
					default: draft.default
				} as PlcVariableConfig
			: persisted;
		return {
			name: selection.id,
			config,
			live: getLiveValue(selection.id) ?? null
		};
	});

	const selectedTask = $derived.by(() => {
		if (selection?.kind !== 'task') return null;
		return tasks[selection.id] ?? null;
	});

	const selectedTaskStats = $derived.by(() => {
		if (selection?.kind !== 'task') return null;
		void taskStatsVersion();
		return getTaskStats(selection.id) ?? null;
	});

	function formatMicros(us: number): string {
		if (!Number.isFinite(us) || us <= 0) return '—';
		if (us < 1) return `${us.toFixed(2)} µs`;
		if (us < 1000) return `${us.toFixed(1)} µs`;
		const ms = us / 1000;
		if (ms < 100) return `${ms.toFixed(2)} ms`;
		return `${ms.toFixed(1)} ms`;
	}

	function headroomPct(stats: { p99Us: number; scanRateMs: number }): number | null {
		if (!stats.scanRateMs) return null;
		const scanUs = stats.scanRateMs * 1000;
		if (scanUs <= 0) return null;
		return Math.max(0, Math.min(100, 100 * (1 - stats.p99Us / scanUs)));
	}

	const selectedProgram = $derived.by(() => {
		if (selection?.kind !== 'program') return null;
		return programs.find((p) => p.name === selection.id) ?? null;
	});


	function formatValue(val: unknown): string {
		if (val === null || val === undefined) return '—';
		if (typeof val === 'number') {
			return Number.isInteger(val) ? String(val) : val.toFixed(3);
		}
		if (typeof val === 'boolean') return val ? 'true' : 'false';
		if (typeof val === 'string') return val;
		try {
			return JSON.stringify(val);
		} catch {
			return String(val);
		}
	}

	function formatAge(ts: unknown): string {
		void now;
		if (ts == null) return '';
		const tsMs =
			typeof ts === 'number'
				? ts < 1e12
					? ts * 1000
					: ts
				: typeof ts === 'string'
					? new Date(ts).getTime()
					: NaN;
		if (!Number.isFinite(tsMs)) return '';
		const diff = now - tsMs;
		if (diff < 1000) return 'now';
		if (diff < 60_000) return `${Math.floor(diff / 1000)}s`;
		if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`;
		if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`;
		return `${Math.floor(diff / 86_400_000)}d`;
	}

	function formatTimestamp(ts: number): string {
		if (!ts) return '—';
		return new Date(ts * 1000).toLocaleString();
	}

	function qualityClass(q: string | undefined): string {
		if (!q) return 'unknown';
		return q.toLowerCase();
	}
</script>

<div class="inspector">
	{#if selectedVariable}
		{@const v = selectedVariable}
		<div class="section">
			<div class="label-row">
				<div class="label">Variable</div>
				<button
					type="button"
					class="refs-btn"
					onclick={() => findReferences(v.name, 'variable')}
					title="Find references"
				>
					<Link size="0.75rem" /> Find references
				</button>
			</div>
			<div class="title">{v.name}</div>
		</div>
		<div class="value-block">
			{#if v.live && isStruct(v.live.value)}
				<ValueTree value={v.live.value} label={v.config?.datatype ?? v.name} />
			{:else}
				<div class="value-big" class:good={v.live?.quality?.toLowerCase() === 'good'}>
					{v.live ? formatValue(v.live.value) : '—'}
				</div>
			{/if}
			<div class="value-meta">
				<span class={`quality ${qualityClass(v.live?.quality)}`}>
					{v.live?.quality ?? 'no data'}
				</span>
				{#if v.live?.lastUpdated}
					<span class="age">{formatAge(v.live.lastUpdated)} ago</span>
				{/if}
			</div>
		</div>
		{#if v.config}
			<div class="section">
				<div class="field">
					<span class="k">Datatype</span>
					<span class="val">{v.config.datatype}</span>
				</div>
				<div class="field">
					<span class="k">Direction</span>
					<span class="val">{v.config.direction}</span>
				</div>
				{#if v.config.description}
					<div class="field">
						<span class="k">Description</span>
						<span class="val">{v.config.description}</span>
					</div>
				{/if}
				{#if v.config.default !== undefined && v.config.default !== null}
					{#if isStruct(v.config.default)}
						<div class="field stacked">
							<span class="k">Default</span>
							<ValueTree value={v.config.default} label={v.config.datatype} />
						</div>
					{:else}
						<div class="field">
							<span class="k">Default</span>
							<span class="val">{formatValue(v.config.default)}</span>
						</div>
					{/if}
				{/if}
				{#if v.config.source}
					<div class="field">
						<span class="k">Source</span>
						<span class="val">{v.config.source.protocol} · {v.config.source.deviceId}</span>
					</div>
					<div class="field">
						<span class="k">Tag</span>
						<span class="val">{v.config.source.tag}</span>
					</div>
				{/if}
				{#if v.config.deadband}
					<div class="field">
						<span class="k">Deadband</span>
						<span class="val">{v.config.deadband.value}</span>
					</div>
				{/if}
			</div>
		{:else}
			<div class="section hint">No config found for this variable.</div>
		{/if}
	{:else if selectedTask}
		<div class="section">
			<div class="label">Task</div>
			<div class="title">{selectedTask.name}</div>
		</div>
		<div class="section">
			{#if selectedTask.description}
				<div class="field">
					<span class="k">Description</span>
					<span class="val">{selectedTask.description}</span>
				</div>
			{/if}
			<div class="field">
				<span class="k">Scan rate</span>
				<span class="val">{selectedTask.scanRateMs} ms</span>
			</div>
			<div class="field">
				<span class="k">Function</span>
				<span class="val">{selectedTask.programRef || '—'}</span>
			</div>
			{#if selectedTask.entryFn && selectedTask.entryFn !== 'main'}
				<div class="field">
					<span class="k">Entry</span>
					<span class="val">{selectedTask.entryFn}()</span>
				</div>
			{/if}
			<div class="field">
				<span class="k">Enabled</span>
				<span class="val" class:muted={!selectedTask.enabled}>
					{selectedTask.enabled ? 'yes' : 'no'}
				</span>
			</div>
		</div>
		{#if selectedTaskStats}
			{@const s = selectedTaskStats}
			{@const headroom = headroomPct(s)}
			<div class="section">
				<div class="label">Scan time ({s.samples} samples)</div>
				<div class="stat-grid">
					<div class="stat">
						<span class="stat-k">p50</span>
						<span class="stat-v">{formatMicros(s.p50Us)}</span>
					</div>
					<div class="stat">
						<span class="stat-k">p95</span>
						<span class="stat-v">{formatMicros(s.p95Us)}</span>
					</div>
					<div class="stat">
						<span class="stat-k">p99</span>
						<span class="stat-v">{formatMicros(s.p99Us)}</span>
					</div>
					<div class="stat">
						<span class="stat-k">max</span>
						<span class="stat-v">{formatMicros(s.maxUs)}</span>
					</div>
					<div class="stat">
						<span class="stat-k">mean</span>
						<span class="stat-v">{formatMicros(s.meanUs)}</span>
					</div>
					<div class="stat">
						<span class="stat-k">last</span>
						<span class="stat-v">{formatMicros(s.lastUs)}</span>
					</div>
				</div>
				<div class="field">
					<span class="k">Runs</span>
					<span class="val">{s.totalRuns.toLocaleString()}</span>
				</div>
				{#if s.totalErrors > 0}
					<div class="field">
						<span class="k">Errors</span>
						<span class="val err">{s.totalErrors.toLocaleString()}</span>
					</div>
				{/if}
				<div class="field">
					<span class="k">Effective</span>
					<span class="val">{s.effectiveHz > 0 ? `${s.effectiveHz.toFixed(1)} Hz` : '—'}</span>
				</div>
				{#if headroom !== null}
					<div class="field">
						<span class="k">Headroom</span>
						<span
							class="val"
							class:warn={headroom < 20}
							class:err={headroom < 5}
						>{headroom.toFixed(0)}%</span>
					</div>
				{/if}
				{#if s.lastError}
					<div class="field">
						<span class="k">Last error</span>
						<span class="val err">{s.lastError}</span>
					</div>
				{/if}
			</div>
		{/if}
	{:else if selectedProgram}
		<div class="section">
			<div class="label-row">
				<div class="label">Function</div>
				<button
					type="button"
					class="refs-btn"
					onclick={() => findReferences(selectedProgram.name, 'program')}
					title="Find references"
				>
					<Link size="0.75rem" /> Find references
				</button>
			</div>
			<div class="title">{selectedProgram.name}</div>
		</div>
		<div class="section">
			{#if selectedProgram.description}
				<div class="field">
					<span class="k">Description</span>
					<span class="val">{selectedProgram.description}</span>
				</div>
			{/if}
			<div class="field">
				<span class="k">Language</span>
				<span class="val">{selectedProgram.language}</span>
			</div>
			<div class="field">
				<span class="k">Updated</span>
				<span class="val">{formatTimestamp(selectedProgram.updatedAt)}</span>
			</div>
			{#if selectedProgram.updatedBy}
				<div class="field">
					<span class="k">By</span>
					<span class="val">{selectedProgram.updatedBy}</span>
				</div>
			{/if}
		</div>
		{#if selectedProgram.signature?.params?.length || selectedProgram.signature?.returns}
			<div class="section">
				<div class="label">Signature</div>
				{#if selectedProgram.signature.params?.length}
					{#each selectedProgram.signature.params as p (p.name)}
						<div class="field">
							<span class="k">{p.name}</span>
							<span class="val">
								{p.type}{#if p.description} — {p.description}{/if}
							</span>
						</div>
					{/each}
				{/if}
				{#if selectedProgram.signature.returns}
					<div class="field">
						<span class="k">→ returns</span>
						<span class="val">
							{selectedProgram.signature.returns.type}{#if selectedProgram.signature.returns.description} — {selectedProgram.signature.returns.description}{/if}
						</span>
					</div>
				{/if}
			</div>
		{/if}
	{:else}
		<div class="empty">
			<div class="empty-title">Nothing selected</div>
			<div class="empty-hint">
				Pick a variable, task, or function from the Navigator to see details here.
			</div>
		</div>
	{/if}
</div>

<style lang="scss">
	.inspector {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		overflow-y: auto;
	}

	.section {
		padding: 0.625rem 0.75rem;
		border-bottom: 1px solid var(--theme-border);

		&.hint {
			color: var(--theme-text-muted);
			font-size: 0.75rem;
			font-style: italic;
		}
	}

	.label {
		font-size: 0.6875rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-text-muted);
		margin-bottom: 0.25rem;
	}

	.label-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
	}

	.refs-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		padding: 0.125rem 0.375rem;
		background: transparent;
		border: 1px solid var(--theme-border);
		border-radius: 0.1875rem;
		color: var(--theme-text-muted);
		font-size: 0.6875rem;
		cursor: pointer;
		transition: color 0.12s ease, background 0.12s ease, border-color 0.12s ease;

		&:hover {
			color: var(--theme-primary);
			border-color: var(--theme-primary);
			background: color-mix(in srgb, var(--theme-primary) 8%, transparent);
		}
	}

	.title {
		font-family: var(--font-mono, monospace);
		font-size: 0.9375rem;
		font-weight: 600;
		color: var(--theme-text);
		word-break: break-all;
	}

	.value-block {
		padding: 0.75rem;
		border-bottom: 1px solid var(--theme-border);
		background: var(--theme-surface);
	}

	.value-big {
		font-family: var(--font-mono, monospace);
		font-size: 1.5rem;
		font-weight: 600;
		color: var(--theme-text-muted);
		word-break: break-all;

		&.good {
			color: var(--theme-text);
		}
	}

	.value-meta {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin-top: 0.375rem;
		font-size: 0.75rem;
	}

	.quality {
		padding: 0.0625rem 0.375rem;
		border-radius: 0.1875rem;
		text-transform: uppercase;
		font-size: 0.625rem;
		font-weight: 600;
		letter-spacing: 0.04em;

		&.good {
			color: var(--theme-success, #2a7);
			background: color-mix(in srgb, var(--theme-success, #2a7) 14%, transparent);
		}

		&.bad {
			color: var(--theme-danger, #c33);
			background: color-mix(in srgb, var(--theme-danger, #c33) 14%, transparent);
		}

		&.unknown {
			color: var(--theme-text-muted);
			background: var(--theme-surface);
		}
	}

	.age {
		color: var(--theme-text-muted);
	}

	.field {
		display: flex;
		align-items: baseline;
		gap: 0.5rem;
		padding: 0.1875rem 0;
		font-size: 0.8125rem;

		&.stacked {
			flex-direction: column;
			align-items: stretch;
			gap: 0.25rem;
		}

		.k {
			flex-shrink: 0;
			min-width: 5rem;
			color: var(--theme-text-muted);
			font-size: 0.75rem;
		}

		.val {
			color: var(--theme-text);
			word-break: break-word;
			font-family: var(--font-mono, monospace);
			font-size: 0.8125rem;

			&.muted {
				color: var(--theme-text-muted);
			}

			&.warn {
				color: var(--theme-warning, #e0b050);
			}

			&.err {
				color: var(--theme-danger, #c33);
			}
		}
	}

	.stat-grid {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 0.25rem 0.5rem;
		margin-bottom: 0.5rem;
	}

	.stat {
		display: flex;
		flex-direction: column;
		padding: 0.25rem 0.375rem;
		background: var(--theme-surface);
		border-radius: 0.1875rem;
		border: 1px solid var(--theme-border);
	}

	.stat-k {
		font-size: 0.625rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-text-muted);
	}

	.stat-v {
		font-family: var(--font-mono, monospace);
		font-size: 0.8125rem;
		font-weight: 600;
		color: var(--theme-text);
	}

	.empty {
		padding: 1rem 0.875rem;
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
	}

	.empty-title {
		font-size: 0.8125rem;
		color: var(--theme-text);
	}

	.empty-hint {
		color: var(--theme-text-muted);
		font-size: 0.75rem;
		line-height: 1.45;
	}

	.hint {
		color: var(--theme-text-muted);
		font-size: 0.75rem;
		font-style: italic;
	}
</style>
