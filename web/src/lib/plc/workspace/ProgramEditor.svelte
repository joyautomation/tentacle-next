<script lang="ts">
	import { api, apiPut, apiDelete } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { onMount } from 'svelte';
	import { state as saltState } from '@joyautomation/salt';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import DirtyIcon from '$lib/components/DirtyIcon.svelte';
	import {
		workspaceDiagnostics,
		workspaceTabs,
		workspaceView,
		type DiagnosticSeverity
	} from '$lib/plc/workspace-state.svelte';
	import { startLiveValues, liveValuesVersion, liveValuesSnapshot } from '$lib/plc/live-values.svelte';

	function lspSeverityToDiagnosticSeverity(sev: number | undefined): DiagnosticSeverity {
		if (sev === 2) return 'warning';
		if (sev === 3) return 'info';
		if (sev === 4) return 'hint';
		return 'error';
	}

	interface PlcProgramKV {
		name: string;
		language: string;
		source: string;
		stSource?: string;
		updatedAt: number;
		updatedBy?: string;
	}

	type Props = {
		tabId: string;
		name: string;
		variableNames: string[];
		onDirtyChange?: (dirty: boolean) => void;
	};

	let { tabId, name, variableNames, onDirtyChange }: Props = $props();

	let loading = $state(false);
	let saving = $state(false);
	let deleting = $state(false);
	let error = $state<string | null>(null);
	let serverSource = $state('');
	let serverStSource = $state('');
	let language = $state<string>('starlark');
	let draftSource = $state('');
	let draftStSource = $state('');

	const dirty = $derived(
		draftSource !== serverSource || draftStSource !== serverStSource
	);

	$effect(() => {
		onDirtyChange?.(dirty);
	});

	const editLanguage = $derived.by<'python' | 'starlark' | 'st'>(() => {
		if (language === 'st') return 'st';
		return 'starlark';
	});

	const editValue = $derived(language === 'st' ? draftStSource : draftSource);

	const showInlineValues = $derived(workspaceView.showInlineValues);
	const liveValuesMap = $derived.by(() => {
		void liveValuesVersion();
		return liveValuesSnapshot();
	});

	onMount(() => {
		const stop = startLiveValues();
		return () => stop();
	});

	$effect(() => {
		if (!name) return;
		void load(name);
	});

	async function load(n: string) {
		loading = true;
		error = null;
		const result = await api<PlcProgramKV>(`/plcs/plc/programs/${encodeURIComponent(n)}`);
		loading = false;
		if (result.error) {
			error = result.error.error;
			return;
		}
		const full = result.data;
		language = full.language;
		serverSource = full.source ?? '';
		serverStSource = full.stSource ?? '';
		draftSource = serverSource;
		draftStSource = serverStSource;
	}

	function onEditorChange(val: string) {
		if (language === 'st') {
			draftStSource = val;
		} else {
			draftSource = val;
		}
	}

	async function save() {
		if (!dirty || saving) return;
		saving = true;
		try {
			const body: Record<string, unknown> = {
				name,
				language,
				source: draftSource,
				updatedBy: 'gui'
			};
			if (language === 'st') {
				body.stSource = draftStSource;
			}
			const result = await apiPut(`/plcs/plc/programs/${encodeURIComponent(name)}`, body);
			if (result.error) {
				saltState.addNotification({ message: result.error.error, type: 'error' });
				return;
			}
			serverSource = draftSource;
			serverStSource = draftStSource;
			saltState.addNotification({ message: `Function "${name}" saved`, type: 'success' });
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function revert() {
		draftSource = serverSource;
		draftStSource = serverStSource;
	}

	async function del() {
		if (!confirm(`Delete function "${name}"? This cannot be undone.`)) return;
		deleting = true;
		try {
			const res = await apiDelete(`/plcs/plc/programs/${encodeURIComponent(name)}`);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			saltState.addNotification({ message: `Function "${name}" deleted`, type: 'success' });
			workspaceTabs.close(tabId);
			await invalidateAll();
		} finally {
			deleting = false;
		}
	}
</script>

<div class="program-editor">
	<div class="ed-header">
		<div class="left">
			<span class="prog-name">{name}</span>
			<span class="lang-badge">{language}</span>
			{#if dirty}
				<DirtyIcon size="0.875rem" />
			{/if}
		</div>
		<div class="right">
			<button
				type="button"
				class="btn toggle"
				class:active={showInlineValues}
				onclick={() => workspaceView.toggleInlineValues()}
				title={showInlineValues ? 'Hide inline values' : 'Show live variable values inline'}
			>
				{showInlineValues ? 'Hide Values' : 'Show Values'}
			</button>
			{#if dirty}
				<button type="button" class="btn subtle" onclick={revert} disabled={saving}>
					Revert
				</button>
			{/if}
			<button type="button" class="btn primary" onclick={save} disabled={!dirty || saving}>
				{saving ? 'Saving…' : 'Save'}
			</button>
			<button
				type="button"
				class="btn danger"
				onclick={del}
				disabled={deleting || saving}
				title="Delete function"
			>
				{deleting ? 'Deleting…' : 'Delete'}
			</button>
		</div>
	</div>

	<div class="ed-body">
		{#if loading}
			<div class="status">Loading…</div>
		{:else if error}
			<div class="status error">{error}</div>
		{:else if language === 'ladder'}
			<div class="status">
				Ladder editing isn't wired into the workspace yet.
				<a href="/services/plc/programs">Open in the Functions tab</a> to edit visually.
			</div>
		{:else}
			<div class="editor-wrap">
				<CodeEditor
					value={editValue}
					language={editLanguage}
					onchange={onEditorChange}
					{variableNames}
					enableVariableDrop
					flush
					enableLint
					useLSP
					lspUri={`tentacle-plc://programs/${encodeURIComponent(name)}.${editLanguage === 'st' ? 'st' : 'star'}`}
					{showInlineValues}
					liveValues={liveValuesMap}
					onDiagnostics={(uri, diags) => {
						workspaceDiagnostics.set(
							uri,
							diags.map((d) => ({
								severity: lspSeverityToDiagnosticSeverity(d.severity),
								message: d.message,
								startLine: d.range.start.line,
								startCol: d.range.start.character,
								endLine: d.range.end.line,
								endCol: d.range.end.character,
								source: d.source
							}))
						);
					}}
				/>
			</div>
		{/if}
	</div>
</div>

<style lang="scss">
	.program-editor {
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

	.prog-name {
		font-family: var(--font-mono, monospace);
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--theme-text);
	}

	.lang-badge {
		padding: 0.0625rem 0.375rem;
		font-size: 0.625rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
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

		&.toggle {
			color: var(--theme-text-muted);
			font-size: 0.75rem;

			&.active {
				color: var(--theme-primary);
				border-color: var(--theme-primary);
				background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
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
		display: flex;
		flex-direction: column;
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
		font-size: 0.875rem;

		&.error {
			color: var(--theme-danger, #c33);
		}

		a {
			color: var(--theme-primary);
		}
	}
</style>
