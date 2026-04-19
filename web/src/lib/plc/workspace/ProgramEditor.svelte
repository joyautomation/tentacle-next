<script lang="ts">
	import { api, apiPut } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import CodeEditor from '$lib/components/CodeEditor.svelte';

	interface PlcProgramKV {
		name: string;
		language: string;
		source: string;
		stSource?: string;
		updatedAt: number;
		updatedBy?: string;
	}

	type Props = {
		name: string;
		variableNames: string[];
		onDirtyChange?: (dirty: boolean) => void;
	};

	let { name, variableNames, onDirtyChange }: Props = $props();

	let loading = $state(false);
	let saving = $state(false);
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
			saltState.addNotification({ message: `Program "${name}" saved`, type: 'success' });
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function revert() {
		draftSource = serverSource;
		draftStSource = serverStSource;
	}
</script>

<div class="program-editor">
	<div class="ed-header">
		<div class="left">
			<span class="prog-name">{name}</span>
			<span class="lang-badge">{language}</span>
			{#if dirty}
				<span class="dirty-dot" title="Unsaved changes">●</span>
			{/if}
		</div>
		<div class="right">
			{#if dirty}
				<button type="button" class="btn subtle" onclick={revert} disabled={saving}>
					Revert
				</button>
			{/if}
			<button type="button" class="btn primary" onclick={save} disabled={!dirty || saving}>
				{saving ? 'Saving…' : 'Save'}
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
				<a href="/services/plc/programs">Open in the Programs tab</a> to edit visually.
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

	.dirty-dot {
		color: var(--theme-warning, var(--theme-primary));
		font-size: 0.75rem;
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
