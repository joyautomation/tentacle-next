<script lang="ts">
	import { api, apiPut, apiPost, apiDelete } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { onMount } from 'svelte';
	import { state as saltState } from '@joyautomation/salt';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import DirtyIcon from '$lib/components/DirtyIcon.svelte';
	import { workspaceTabs } from '../workspace-state.svelte';
	import type { PlcTest, PlcTestResult } from '$lib/types/plc';

	type Props = {
		tabId: string;
		name: string;
		isNew?: boolean;
		variableNames?: string[];
		initialSource?: string;
		onDirtyChange?: (dirty: boolean) => void;
	};

	let { tabId, name, isNew = false, variableNames = [], initialSource, onDirtyChange }: Props = $props();

	const STARTER_SOURCE = `# Unit test — runs against the live engine.
# Use assert_eq / assert_true / assert_near / assert_raises.

def test_example():
    assert_eq(1 + 1, 2)

test_example()
`;

	let loaded = $state<PlcTest | null>(null);
	let loading = $state(!isNew);
	let error = $state<string | null>(null);

	let editValue = $state(isNew ? (initialSource ?? STARTER_SOURCE) : '');
	let description = $state('');
	let newName = $state('');

	let saving = $state(false);
	let deleting = $state(false);
	let running = $state(false);
	let result = $state<PlcTestResult | null>(null);

	async function load() {
		if (isNew) {
			loading = false;
			return;
		}
		loading = true;
		const res = await api<PlcTest>(`/plcs/plc/tests/${encodeURIComponent(name)}`);
		if (res.error) {
			error = res.error.error;
			loading = false;
			return;
		}
		loaded = res.data ?? null;
		editValue = loaded?.source ?? '';
		description = loaded?.description ?? '';
		result = loaded?.lastResult ?? null;
		loading = false;
	}

	onMount(load);

	function deriveName(source: string): string {
		// Prefer the first def; otherwise leave the user-typed newName.
		const match = source.match(/^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)/m);
		if (match) return match[1];
		return newName;
	}

	$effect(() => {
		if (!isNew) return;
		const derived = deriveName(editValue);
		if (derived && derived !== newName) {
			newName = derived;
			workspaceTabs.setTabLabel(tabId, derived);
		}
	});

	const dirty = $derived.by(() => {
		if (isNew) return editValue.trim().length > 0;
		if (!loaded) return false;
		if (editValue !== (loaded.source ?? '')) return true;
		if ((description ?? '') !== (loaded.description ?? '')) return true;
		return false;
	});

	$effect(() => {
		workspaceTabs.setDirty(tabId, dirty);
		onDirtyChange?.(dirty);
	});

	const effectiveName = $derived(isNew ? newName : name);
	const canSave = $derived(!!effectiveName && editValue.trim().length > 0 && !saving);

	async function save() {
		if (!canSave) return;
		saving = true;
		try {
			const body: Record<string, unknown> = {
				name: effectiveName,
				description: description.trim() || undefined,
				source: editValue
			};
			const res = await apiPut(
				`/plcs/plc/tests/${encodeURIComponent(isNew ? effectiveName : name)}`,
				body
			);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Test "${effectiveName}" saved`, type: 'success' });
			if (isNew) {
				workspaceTabs.renameTab(tabId, effectiveName);
			}
			await invalidateAll();
			await load();
		} finally {
			saving = false;
		}
	}

	async function run() {
		if (isNew || !loaded) {
			saltState.addNotification({ message: 'Save the test before running', type: 'info' });
			return;
		}
		running = true;
		try {
			const res = await apiPost<PlcTestResult>(
				`/plcs/plc/tests/${encodeURIComponent(name)}/run`
			);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			result = res.data ?? null;
			await invalidateAll();
		} finally {
			running = false;
		}
	}

	function revert() {
		if (!loaded) return;
		editValue = loaded.source ?? '';
		description = loaded.description ?? '';
	}

	async function del() {
		if (!confirm(`Delete test "${name}"? This cannot be undone.`)) return;
		deleting = true;
		try {
			const res = await apiDelete(`/plcs/plc/tests/${encodeURIComponent(name)}`);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Test "${name}" deleted`, type: 'success' });
			workspaceTabs.close(tabId);
			await invalidateAll();
		} finally {
			deleting = false;
		}
	}
</script>

<div class="test-editor">
	<div class="ed-header">
		<div class="left">
			{#if result}
				<span
					class="status-dot"
					class:pass={result.status === 'pass'}
					class:fail={result.status === 'fail'}
					class:error={result.status === 'error'}
					title={result.message ?? ''}
				></span>
			{/if}
			<span class="test-name">{effectiveName || 'Untitled'}</span>
			<span class="kind-badge">Test</span>
			{#if dirty}
				<DirtyIcon size="0.875rem" />
			{/if}
			{#if result}
				<span class="result-meta" class:fail={result.status !== 'pass'}>
					{result.status} · {result.durationMs}ms
				</span>
			{/if}
		</div>
		<div class="right">
			<button
				type="button"
				class="btn test"
				onclick={run}
				disabled={isNew || dirty || running}
				title={isNew
					? 'Save the test first'
					: dirty
						? 'Save first — run executes the persisted source'
						: 'Run this test'}
			>
				{running ? 'Running…' : 'Run'}
			</button>
			{#if dirty && !isNew}
				<button type="button" class="btn subtle" onclick={revert} disabled={saving}>
					Revert
				</button>
			{/if}
			<button type="button" class="btn primary" onclick={save} disabled={!canSave}>
				{saving ? 'Saving…' : isNew ? 'Create' : 'Save'}
			</button>
			{#if !isNew}
				<button
					type="button"
					class="btn danger"
					onclick={del}
					disabled={deleting || saving}
					title="Delete test"
				>
					{deleting ? 'Deleting…' : 'Delete'}
				</button>
			{/if}
		</div>
	</div>

	<div class="ed-body">
		{#if loading}
			<div class="status">Loading…</div>
		{:else if error}
			<div class="status error">{error}</div>
		{:else}
			<div class="editor-wrap">
				<CodeEditor
					value={editValue}
					language="starlark-test"
					onchange={(v) => (editValue = v)}
					{variableNames}
					useLSP
					lspUri={`tentacle-plc://tests/${encodeURIComponent(effectiveName || tabId)}.star`}
					flush
				/>
			</div>
			{#if result}
				<div class="results-panel" class:fail={result.status !== 'pass'}>
					<div class="results-header">
						<span
							class="status-dot"
							class:pass={result.status === 'pass'}
							class:fail={result.status === 'fail'}
							class:error={result.status === 'error'}
						></span>
						<span class="results-title">
							{result.status === 'pass' ? 'Passed' : result.status === 'fail' ? 'Failed' : 'Error'}
						</span>
						<span class="results-duration">{result.durationMs}ms</span>
					</div>
					{#if result.message}
						<pre class="results-message">{result.message}</pre>
					{/if}
					{#if result.logs && result.logs.length > 0}
						<div class="results-logs-header">Logs</div>
						<pre class="results-logs">{result.logs.join('\n')}</pre>
					{/if}
				</div>
			{/if}
		{/if}
	</div>
</div>

<style lang="scss">
	.test-editor {
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

	.test-name {
		font-family: var(--font-mono, monospace);
		font-size: 0.875rem;
		color: var(--theme-text);
	}

	.kind-badge {
		padding: 0.0625rem 0.3125rem;
		font-size: 0.625rem;
		font-weight: 600;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		border-radius: 0.1875rem;
		text-transform: uppercase;
	}

	.result-meta {
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.04em;

		&.fail {
			color: var(--theme-danger, #ef4444);
		}
	}

	.status-dot {
		flex-shrink: 0;
		display: inline-block;
		width: 0.5rem;
		height: 0.5rem;
		border-radius: 50%;
		background: var(--theme-border);

		&.pass {
			background: var(--theme-success, #10b981);
		}
		&.fail {
			background: var(--theme-danger, #ef4444);
		}
		&.error {
			background: var(--theme-warning, #f59e0b);
		}
	}

	.btn {
		padding: 0.25rem 0.625rem;
		font-size: 0.75rem;
		font-weight: 500;
		border: 1px solid var(--theme-border);
		border-radius: 0.25rem;
		background: var(--theme-background);
		color: var(--theme-text);
		cursor: pointer;

		&:disabled {
			opacity: 0.5;
			cursor: not-allowed;
		}

		&:hover:not(:disabled) {
			background: var(--theme-surface);
		}

		&.primary {
			background: var(--theme-primary);
			color: var(--theme-primary-contrast, white);
			border-color: var(--theme-primary);
		}

		&.danger {
			color: var(--theme-danger, #ef4444);
			border-color: color-mix(in srgb, var(--theme-danger, #ef4444) 40%, var(--theme-border));

			&:hover:not(:disabled) {
				background: color-mix(in srgb, var(--theme-danger, #ef4444) 12%, transparent);
			}
		}

		&.test {
			color: var(--theme-success, #10b981);
			border-color: color-mix(in srgb, var(--theme-success, #10b981) 40%, var(--theme-border));

			&:hover:not(:disabled) {
				background: color-mix(in srgb, var(--theme-success, #10b981) 12%, transparent);
			}
		}
	}

	.ed-body {
		flex: 1;
		display: flex;
		flex-direction: column;
		min-height: 0;
	}

	.editor-wrap {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}

	.status {
		padding: 1rem;
		color: var(--theme-text-muted);

		&.error {
			color: var(--theme-danger, #ef4444);
		}
	}

	.results-panel {
		border-top: 1px solid var(--theme-border);
		background: var(--theme-surface);
		padding: 0.5rem 0.75rem;
		max-height: 14rem;
		overflow-y: auto;

		&.fail {
			background: color-mix(in srgb, var(--theme-danger, #ef4444) 6%, var(--theme-surface));
		}
	}

	.results-header {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.8125rem;
		font-weight: 600;
	}

	.results-title {
		flex: 1;
	}

	.results-duration {
		color: var(--theme-text-muted);
		font-size: 0.75rem;
	}

	.results-message {
		margin: 0.375rem 0 0 0;
		padding: 0.5rem;
		font-family: var(--font-mono, monospace);
		font-size: 0.75rem;
		color: var(--theme-text);
		background: var(--theme-background);
		border-radius: 0.25rem;
		white-space: pre-wrap;
		word-break: break-word;
	}

	.results-logs-header {
		margin-top: 0.5rem;
		font-size: 0.6875rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-text-muted);
	}

	.results-logs {
		margin: 0.25rem 0 0 0;
		padding: 0.5rem;
		font-family: var(--font-mono, monospace);
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		background: var(--theme-background);
		border-radius: 0.25rem;
		white-space: pre-wrap;
	}
</style>
