<script lang="ts">
	import { api, apiPut, apiPost, apiDelete } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { onMount, onDestroy } from 'svelte';
	import { state as saltState } from '@joyautomation/salt';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import DirtyIcon from '$lib/components/DirtyIcon.svelte';
	import {
		workspaceDiagnostics,
		workspaceTabs,
		workspaceSelection,
		workspaceView,
		workspaceEditorGotos,
		type DiagnosticSeverity
	} from '$lib/plc/workspace-state.svelte';
	import { startLiveValues, liveValuesVersion, liveValuesSnapshot } from '$lib/plc/live-values.svelte';
	import type { ProgramListItem } from '$lib/types/plc';

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
		pendingSource?: string;
		pendingStSource?: string;
		pendingLanguage?: string;
		pendingUpdatedAt?: number;
		pendingUpdatedBy?: string;
	}

	type Props = {
		tabId: string;
		name: string;
		variableNames: string[];
		programs?: ProgramListItem[];
		isNew?: boolean;
		initialLanguage?: string;
		onDirtyChange?: (dirty: boolean) => void;
	};

	let {
		tabId,
		name,
		variableNames,
		programs = [],
		isNew = false,
		initialLanguage = 'starlark',
		onDirtyChange
	}: Props = $props();

	// Placeholder body seeded into a brand-new tab. The user edits the def
	// name in-place — pendingName is derived from the def header below.
	const NEW_PROGRAM_PLACEHOLDER = 'def new_function():\n    pass\n';

	let loading = $state(false);
	let saving = $state(false);
	let assembling = $state(false);
	let cancelling = $state(false);
	let deleting = $state(false);
	let error = $state<string | null>(null);
	let serverSource = $state(isNew ? NEW_PROGRAM_PLACEHOLDER : '');
	let serverStSource = $state('');
	// Pending = persisted uncommitted edit ("save"). Draft = in-memory edit
	// buffer that may or may not have been saved as pending yet.
	let pendingSource = $state<string | null>(null);
	let pendingStSource = $state<string | null>(null);
	let pendingUpdatedAt = $state<number>(0);
	let pendingUpdatedBy = $state<string>('');
	let language = $state<string>(isNew ? initialLanguage : 'starlark');
	let draftSource = $state(isNew ? NEW_PROGRAM_PLACEHOLDER : '');
	let draftStSource = $state('');

	// When a live program exists, "save" stores a pending edit rather than
	// overwriting the running code. Assemble promotes pending → live,
	// Cancel discards it. New programs (isNew) skip this entirely.
	const hasPending = $derived(pendingSource !== null);
	const effectiveSavedSource = $derived(pendingSource ?? serverSource);
	const effectiveSavedStSource = $derived(pendingStSource ?? serverStSource);

	const dirty = $derived(
		isNew ||
			draftSource !== effectiveSavedSource ||
			draftStSource !== effectiveSavedStSource
	);

	// Show the diff view when there's something to compare against the live
	// version — either a saved pending edit or an in-flight draft.
	const showDiff = $derived(
		!isNew && (hasPending || draftSource !== serverSource || draftStSource !== serverStSource)
	);

	// Name derived from the first `def` header in the current source.
	// Drives the tab label for unsaved tabs and the rename check for saved
	// ones — editing the def is the user's way of naming the function.
	function extractDefName(src: string): string {
		const m = src.match(/^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(/m);
		return m?.[1] ?? '';
	}

	const pendingName = $derived.by(() => {
		if (language !== 'starlark') return name;
		return extractDefName(draftSource);
	});

	const nameIsValid = $derived(/^[A-Za-z_][A-Za-z0-9_]*$/.test(pendingName));

	const nameCollision = $derived.by(() => {
		if (!pendingName) return false;
		if (!isNew && pendingName === name) return false;
		return programs.some((p) => p.name === pendingName);
	});

	// Effective program name for LSP / display: the saved name while it
	// still matches the source, otherwise whatever the user is typing.
	const effectiveName = $derived(pendingName || name || 'untitled');

	const lspUri = $derived(
		`tentacle-plc://programs/${encodeURIComponent(effectiveName)}.${language === 'st' ? 'st' : 'star'}`
	);

	// Keep the tab label in sync with the pending name so the tab strip
	// shows the function's evolving identity as the user types.
	$effect(() => {
		if (!isNew) return;
		workspaceTabs.setTabLabel(tabId, pendingName || 'Untitled');
	});

	const errorCount = $derived.by(() => {
		const diags = workspaceDiagnostics.byUri[lspUri];
		if (!diags) return 0;
		let n = 0;
		for (const d of diags) if (d.severity === 'error') n++;
		return n;
	});

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

	onDestroy(() => {
		workspaceEditorGotos.unregister(tabId);
	});

	$effect(() => {
		if (isNew) return;
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
		language = full.pendingLanguage || full.language;
		serverSource = full.source ?? '';
		serverStSource = full.stSource ?? '';
		if (full.pendingSource) {
			pendingSource = full.pendingSource;
			pendingStSource = full.pendingStSource ?? '';
			pendingUpdatedAt = full.pendingUpdatedAt ?? 0;
			pendingUpdatedBy = full.pendingUpdatedBy ?? '';
			draftSource = pendingSource;
			draftStSource = pendingStSource ?? '';
		} else {
			pendingSource = null;
			pendingStSource = null;
			pendingUpdatedAt = 0;
			pendingUpdatedBy = '';
			draftSource = serverSource;
			draftStSource = serverStSource;
		}
	}

	function onEditorChange(val: string) {
		if (language === 'st') {
			draftStSource = val;
		} else {
			draftSource = val;
		}
	}

	// saveBlockedReason returns a short explanation when save is disabled.
	// Used as a tooltip so the user knows why the button isn't clickable.
	const saveBlockedReason = $derived.by(() => {
		if (language === 'starlark') {
			if (!pendingName) return 'Add a def header to name this function';
			if (!nameIsValid) return `"${pendingName}" is not a valid identifier`;
			if (nameCollision) return `A program named "${pendingName}" already exists`;
		}
		if (errorCount > 0) return `Fix ${errorCount} error${errorCount === 1 ? '' : 's'} before saving`;
		return undefined;
	});

	const canSave = $derived.by(() => {
		if (!dirty || saving) return false;
		if (errorCount > 0) return false;
		if (language === 'starlark') {
			if (!nameIsValid) return false;
			if (nameCollision) return false;
		}
		return true;
	});

	async function save() {
		if (!canSave) return;
		saving = true;
		try {
			// New programs go live immediately (nothing to pend against).
			// Existing programs save to pending so the running engine keeps
			// executing the live version until the user assembles.
			if (isNew) {
				const urlName = pendingName;
				const bodyName = language === 'starlark' ? pendingName : name;
				const body: Record<string, unknown> = {
					name: bodyName,
					language,
					source: draftSource,
					updatedBy: 'gui'
				};
				if (language === 'st') body.stSource = draftStSource;
				const result = await apiPut(`/plcs/plc/programs/${encodeURIComponent(urlName)}`, body);
				if (result.error) {
					saltState.addNotification({ message: result.error.error, type: 'error' });
					return;
				}
				serverSource = draftSource;
				serverStSource = draftStSource;
				workspaceTabs.renameTab(tabId, bodyName);
				workspaceSelection.select('program', bodyName);
				saltState.addNotification({
					message: `Function "${bodyName}" created`,
					type: 'success'
				});
				await invalidateAll();
				return;
			}

			// Rename via def-header change is a structural edit and not
			// online-safe — force the user to assemble first, then rename.
			const bodyName = language === 'starlark' ? pendingName : name;
			if (bodyName !== name) {
				saltState.addNotification({
					message: 'Assemble or cancel the pending edit before renaming the function',
					type: 'error'
				});
				return;
			}

			const body: Record<string, unknown> = {
				source: draftSource,
				language,
				updatedBy: 'gui'
			};
			if (language === 'st') body.stSource = draftStSource;
			const result = await apiPut<PlcProgramKV>(
				`/plcs/plc/programs/${encodeURIComponent(name)}/pending`,
				body
			);
			if (result.error) {
				saltState.addNotification({ message: result.error.error, type: 'error' });
				return;
			}
			pendingSource = result.data.pendingSource ?? draftSource;
			pendingStSource = result.data.pendingStSource ?? draftStSource;
			pendingUpdatedAt = result.data.pendingUpdatedAt ?? Date.now();
			pendingUpdatedBy = result.data.pendingUpdatedBy ?? 'gui';
			saltState.addNotification({
				message: `Pending edit saved — assemble to commit`,
				type: 'success'
			});
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	async function assemble() {
		if (!hasPending || assembling) return;
		assembling = true;
		try {
			const res = await apiPost<PlcProgramKV>(
				`/plcs/plc/programs/${encodeURIComponent(name)}/assemble`
			);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			serverSource = res.data.source ?? '';
			serverStSource = res.data.stSource ?? '';
			pendingSource = null;
			pendingStSource = null;
			pendingUpdatedAt = 0;
			pendingUpdatedBy = '';
			draftSource = serverSource;
			draftStSource = serverStSource;
			saltState.addNotification({
				message: `Function "${name}" assembled — live engine updated`,
				type: 'success'
			});
			await invalidateAll();
		} finally {
			assembling = false;
		}
	}

	async function cancelPending() {
		if (!hasPending || cancelling) return;
		if (!confirm(`Discard pending edit for "${name}"? This cannot be undone.`)) return;
		cancelling = true;
		try {
			const res = await apiPost<PlcProgramKV>(
				`/plcs/plc/programs/${encodeURIComponent(name)}/cancel`
			);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			pendingSource = null;
			pendingStSource = null;
			pendingUpdatedAt = 0;
			pendingUpdatedBy = '';
			draftSource = serverSource;
			draftStSource = serverStSource;
			saltState.addNotification({
				message: `Pending edit discarded`,
				type: 'success'
			});
			await invalidateAll();
		} finally {
			cancelling = false;
		}
	}

	function revert() {
		draftSource = effectiveSavedSource;
		draftStSource = effectiveSavedStSource;
	}

	async function del() {
		// A never-saved tab just closes — nothing to delete server-side.
		if (isNew) {
			workspaceTabs.close(tabId);
			return;
		}
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
			<span class="prog-name" class:pending={isNew || pendingName !== name}>
				{effectiveName}
			</span>
			{#if !isNew && pendingName && pendingName !== name && nameIsValid && !nameCollision}
				<span class="rename-hint" title="Saving will rename the program">
					(will rename on save)
				</span>
			{/if}
			<span class="lang-badge">{language}</span>
			{#if hasPending}
				<span class="pending-badge" title="Uncommitted edit — assemble to apply to running engine">
					Pending edit
				</span>
			{/if}
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
			{#if dirty && !isNew}
				<button type="button" class="btn subtle" onclick={revert} disabled={saving}>
					Revert
				</button>
			{/if}
			<button
				type="button"
				class="btn primary"
				onclick={save}
				disabled={!canSave}
				title={saveBlockedReason}
			>
				{saving ? 'Saving…' : isNew ? 'Create' : hasPending ? 'Update Pending' : 'Save'}
			</button>
			{#if showDiff}
				<button
					type="button"
					class="btn assemble"
					onclick={assemble}
					disabled={!hasPending || assembling || saving || dirty}
					title={!hasPending
						? 'Save the pending edit first, then assemble to apply it'
						: dirty
							? 'Save pending edit before assembling'
							: 'Apply pending edit to the running engine'}
				>
					{assembling ? 'Assembling…' : 'Assemble'}
				</button>
				<button
					type="button"
					class="btn subtle"
					onclick={cancelPending}
					disabled={!hasPending || cancelling || assembling}
					title={hasPending
						? 'Discard pending edit, keep live version'
						: 'No pending edit to cancel'}
				>
					{cancelling ? 'Cancelling…' : 'Cancel Edit'}
				</button>
			{/if}
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
		{:else if showDiff}
			<div class="diff-wrap">
				<div class="diff-pane">
					<div class="pane-label pending">
						{hasPending ? 'Pending (saved)' : 'Pending (unsaved)'}
					</div>
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
							{lspUri}
							{showInlineValues}
							liveValues={liveValuesMap}
							onReady={(api) => workspaceEditorGotos.register(tabId, api.goto)}
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
				</div>
				<div class="diff-pane">
					<div class="pane-label live">Live (running)</div>
					<div class="editor-wrap">
						<CodeEditor
							value={language === 'st' ? serverStSource : serverSource}
							language={editLanguage}
							readonly
							flush
							{showInlineValues}
							liveValues={liveValuesMap}
						/>
					</div>
				</div>
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
					{lspUri}
					{showInlineValues}
					liveValues={liveValuesMap}
					onReady={(api) => workspaceEditorGotos.register(tabId, api.goto)}
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

		&.pending {
			color: var(--theme-text-muted);
			font-style: italic;
		}
	}

	.rename-hint {
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		font-style: italic;
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

	.pending-badge {
		padding: 0.0625rem 0.375rem;
		font-size: 0.625rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-warning, #d97706);
		background: color-mix(in srgb, var(--theme-warning, #d97706) 14%, transparent);
		border: 1px solid color-mix(in srgb, var(--theme-warning, #d97706) 40%, transparent);
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

		&.assemble {
			color: var(--theme-on-primary, white);
			background: var(--theme-success, #16a34a);
			border-color: var(--theme-success, #16a34a);

			&:hover:not(:disabled) {
				opacity: 0.9;
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

	.diff-wrap {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: row;
	}

	.diff-pane {
		flex: 1 1 0;
		min-width: 0;
		min-height: 0;
		display: flex;
		flex-direction: column;
		border-right: 1px solid var(--theme-border);

		&:last-child {
			border-right: none;
		}
	}

	.pane-label {
		padding: 0.25rem 0.625rem;
		font-size: 0.6875rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		border-bottom: 1px solid var(--theme-border);
		background: var(--theme-surface);
		color: var(--theme-text-muted);

		&.live {
			color: var(--theme-primary);
		}

		&.pending {
			color: var(--theme-warning, #d97706);
		}
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
