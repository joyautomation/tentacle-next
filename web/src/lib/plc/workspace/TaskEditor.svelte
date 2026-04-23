<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { apiPut, apiDelete } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import DirtyIcon from '$lib/components/DirtyIcon.svelte';
	import { workspaceTabs, workspaceEditorSaves } from '../workspace-state.svelte';
	import type { PlcTaskConfig, ProgramListItem } from '$lib/types/plc';

	type Props = {
		tabId: string;
		name: string;
		tasks: Record<string, PlcTaskConfig>;
		programs: ProgramListItem[];
		onDirtyChange?: (dirty: boolean) => void;
	};

	let { tabId, name, tasks, programs, onDirtyChange }: Props = $props();

	const current = $derived(tasks[name] ?? null);

	let description = $state('');
	let scanRateMs = $state(100);
	let programRef = $state('');
	let entryFn = $state('');
	let enabled = $state(true);

	let saving = $state(false);
	let deleting = $state(false);

	let lastLoadedFor = '';
	$effect(() => {
		if (!current) return;
		const key = `${current.name}::${current.scanRateMs}::${current.programRef}::${current.entryFn ?? ''}::${current.enabled}::${current.description ?? ''}`;
		if (key === lastLoadedFor) return;
		lastLoadedFor = key;
		description = current.description ?? '';
		scanRateMs = current.scanRateMs;
		programRef = current.programRef ?? '';
		entryFn = current.entryFn ?? '';
		enabled = !!current.enabled;
	});

	const dirty = $derived.by(() => {
		if (!current) return false;
		if ((description ?? '') !== (current.description ?? '')) return true;
		if (scanRateMs !== current.scanRateMs) return true;
		if (programRef !== (current.programRef ?? '')) return true;
		if ((entryFn ?? '') !== (current.entryFn ?? '')) return true;
		if (enabled !== !!current.enabled) return true;
		return false;
	});

	$effect(() => {
		workspaceTabs.setDirty(tabId, dirty);
		onDirtyChange?.(dirty);
	});

	const canSave = $derived(
		!!current && dirty && !saving && scanRateMs > 0 && programRef !== ''
	);

	onMount(() => workspaceEditorSaves.register(tabId, save));
	onDestroy(() => workspaceEditorSaves.unregister(tabId));

	async function save() {
		if (!canSave) return;
		saving = true;
		try {
			const body: Record<string, unknown> = {
				name,
				description: description.trim() || undefined,
				scanRateMs,
				programRef,
				enabled
			};
			if (entryFn.trim()) body.entryFn = entryFn.trim();
			const result = await apiPut(`/plcs/plc/tasks/${encodeURIComponent(name)}`, body);
			if (result.error) {
				saltState.addNotification({ message: result.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Task "${name}" saved`, type: 'success' });
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function revert() {
		if (!current) return;
		description = current.description ?? '';
		scanRateMs = current.scanRateMs;
		programRef = current.programRef ?? '';
		entryFn = current.entryFn ?? '';
		enabled = !!current.enabled;
	}

	async function del() {
		if (!confirm(`Delete task "${name}"? This cannot be undone.`)) return;
		deleting = true;
		try {
			const res = await apiDelete(`/plcs/plc/tasks/${encodeURIComponent(name)}`);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Task "${name}" deleted`, type: 'success' });
			workspaceTabs.close(tabId);
			await invalidateAll();
		} finally {
			deleting = false;
		}
	}
</script>

<div class="task-editor">
	<div class="ed-header">
		<div class="left">
			<span class="task-name">{name}</span>
			<span class="kind-badge">Task</span>
			{#if dirty}
				<DirtyIcon size="0.875rem" />
			{/if}
		</div>
		<div class="right">
			{#if dirty}
				<button type="button" class="btn subtle" onclick={revert} disabled={saving}>
					Revert
				</button>
			{/if}
			<button type="button" class="btn primary" onclick={save} disabled={!canSave}>
				{saving ? 'Saving…' : 'Save'}
			</button>
			<button
				type="button"
				class="btn danger"
				onclick={del}
				disabled={deleting || saving}
				title="Delete task"
			>
				{deleting ? 'Deleting…' : 'Delete'}
			</button>
		</div>
	</div>

	<div class="ed-body">
		{#if !current}
			<div class="status">Task "{name}" not found.</div>
		{:else}
			<div class="form">
				<div class="row">
					<label for="task-desc">Description</label>
					<input
						id="task-desc"
						type="text"
						bind:value={description}
						placeholder="optional"
					/>
				</div>

				<div class="row">
					<label for="task-rate">Scan rate (ms)</label>
					<input
						id="task-rate"
						type="number"
						min="1"
						bind:value={scanRateMs}
					/>
				</div>

				<div class="row">
					<label for="task-prog">Function</label>
					<select id="task-prog" bind:value={programRef}>
						<option value="" disabled>Select a function</option>
						{#each programs as prog (prog.name)}
							<option value={prog.name}>{prog.name} ({prog.language})</option>
						{/each}
					</select>
				</div>

				<div class="row">
					<label for="task-entry">Entry fn</label>
					<input
						id="task-entry"
						type="text"
						bind:value={entryFn}
						placeholder="main (default)"
					/>
				</div>

				<div class="row toggle-row">
					<label for="task-enabled">Enabled</label>
					<label class="switch">
						<input id="task-enabled" type="checkbox" bind:checked={enabled} />
						<span class="slider"></span>
						<span class="switch-label">{enabled ? 'yes' : 'no'}</span>
					</label>
				</div>
			</div>
		{/if}
	</div>
</div>

<style lang="scss">
	.task-editor {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
	}

	.ed-header {
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

	.task-name {
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
		color: var(--theme-text);
		background: color-mix(in srgb, var(--theme-text) 12%, transparent);
		border-radius: 0.1875rem;
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
		transition: all 0.12s ease;

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

	.ed-body {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
	}

	.status {
		padding: 1rem;
		color: var(--theme-text-muted);
		font-size: 0.875rem;
	}

	.form {
		padding: 1rem;
		max-width: 36rem;
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.row {
		display: flex;
		align-items: center;
		gap: 0.75rem;

		label {
			min-width: 8rem;
			font-size: 0.8125rem;
			color: var(--theme-text-muted);
			flex-shrink: 0;
		}

		input[type='text'],
		input[type='number'],
		select {
			flex: 1;
			padding: 0.375rem 0.5rem;
			font-size: 0.8125rem;
			border: 1px solid var(--theme-border);
			border-radius: 0.25rem;
			background: var(--theme-background);
			color: var(--theme-text);
			font-family: inherit;

			&:focus {
				outline: none;
				border-color: var(--theme-primary);
			}
		}

		select {
			cursor: pointer;
		}
	}

	.toggle-row {
		align-items: center;
	}

	.switch {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		cursor: pointer;

		input {
			position: absolute;
			opacity: 0;
			pointer-events: none;
		}
	}

	.slider {
		position: relative;
		width: 36px;
		height: 20px;
		background: var(--theme-border);
		border-radius: 20px;
		transition: background 0.2s;

		&::before {
			content: '';
			position: absolute;
			width: 14px;
			height: 14px;
			left: 3px;
			top: 3px;
			background: var(--theme-text);
			border-radius: 50%;
			transition: transform 0.2s;
		}
	}

	.switch input:checked + .slider {
		background: var(--theme-primary);
	}

	.switch input:checked + .slider::before {
		transform: translateX(16px);
		background: var(--theme-on-primary, white);
	}

	.switch-label {
		font-size: 0.75rem;
		color: var(--theme-text-muted);
	}
</style>
