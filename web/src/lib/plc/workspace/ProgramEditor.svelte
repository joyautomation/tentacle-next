<script lang="ts">
	import { api, apiPut, apiPost, apiDelete } from '$lib/api/client';
	import { subscribe as subscribeSSE } from '$lib/api/subscribe';
	import { invalidateAll } from '$app/navigation';
	import { onMount, onDestroy } from 'svelte';
	import { fly } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
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
	let deleting = $state(false);
	let error = $state<string | null>(null);

	// Test-mode session state. When non-null, the live engine is running
	// the draft candidate; an auto-revert fires on error or timeout.
	type TestSessionInfo = { program: string; startedAt: number; expiresAt: number };
	let testSession = $state<TestSessionInfo | null>(null);
	let testStarting = $state(false);
	let testRemaining = $state(0);
	const TEST_TIMEOUT_SECONDS = 120;
	let serverSource = $state(isNew ? NEW_PROGRAM_PLACEHOLDER : '');
	let serverStSource = $state('');
	let language = $state<string>(isNew ? initialLanguage : 'starlark');
	let draftSource = $state(isNew ? NEW_PROGRAM_PLACEHOLDER : '');
	let draftStSource = $state('');

	const dirty = $derived(
		isNew || draftSource !== serverSource || draftStSource !== serverStSource
	);

	// Diff view appears whenever the draft differs from the running code —
	// the pane shows Live (readonly) next to the in-memory edits so the
	// user can review before saving.
	const showDiff = $derived(
		!isNew && (draftSource !== serverSource || draftStSource !== serverStSource)
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
		const stopEvents = isNew
			? () => {}
			: subscribeSSE<TestEventMsg>('/plcs/plc/programs/test/events', (ev) => {
					if (!ev || ev.program !== name) return;
					if (ev.reason === 'started') {
						// Session info comes back on the POST response; the
						// 'started' event is fan-out for other clients.
						return;
					}
					// Terminal events: session is over, surface to the user.
					testSession = null;
					testRemaining = 0;
					const msg =
						ev.reason === 'error'
							? `Test reverted: ${ev.error || 'runtime error'}`
							: ev.reason === 'timeout'
								? 'Test session expired — reverted to live source'
								: 'Test session stopped';
					saltState.addNotification({
						message: msg,
						type: ev.reason === 'error' ? 'error' : 'info'
					});
				});
		// Poll current status once so an active session survives navigation.
		if (!isNew) {
			void fetchTestStatus();
		}
		return () => {
			stop();
			stopEvents();
		};
	});

	type TestEventMsg = { program: string; reason: string; error?: string; at: number };

	async function fetchTestStatus() {
		const res = await api<{ session?: TestSessionInfo | null }>(
			`/plcs/plc/programs/${encodeURIComponent(name)}/test`
		);
		if (!res.error && res.data?.session) {
			testSession = res.data.session;
		}
	}

	// Countdown tick while a test session is active.
	$effect(() => {
		if (!testSession) {
			testRemaining = 0;
			return;
		}
		const tick = () => {
			testRemaining = Math.max(
				0,
				Math.ceil((testSession!.expiresAt - Date.now()) / 1000)
			);
		};
		tick();
		const id = window.setInterval(tick, 500);
		return () => window.clearInterval(id);
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
			// For Starlark, the stored key follows the def header. New tabs
			// POST to the derived name; saved tabs PUT to the old key and
			// ask the server to rename when the def has changed.
			const urlName = isNew ? pendingName : name;
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
			const renamed = !isNew && bodyName !== name;
			if (isNew || renamed) {
				workspaceTabs.renameTab(tabId, bodyName);
				workspaceSelection.select('program', bodyName);
			}
			const verb = isNew ? 'created' : renamed ? 'renamed' : 'saved';
			saltState.addNotification({
				message: `Function "${bodyName}" ${verb}`,
				type: 'success'
			});
			await invalidateAll();
		} finally {
			saving = false;
		}
	}

	function revert() {
		draftSource = serverSource;
		draftStSource = serverStSource;
	}

	// canTest gates the Test button: only Starlark (engine-level hot-swap),
	// must be dirty, must not have blocking errors, and no active session.
	const canTest = $derived.by(() => {
		if (isNew) return false;
		if (language !== 'starlark') return false;
		if (!dirty) return false;
		if (errorCount > 0) return false;
		if (testStarting) return false;
		if (testSession) return false;
		return true;
	});

	const testBlockedReason = $derived.by(() => {
		if (isNew) return 'Save first before testing';
		if (language !== 'starlark') return 'Test mode is Starlark-only for now';
		if (!dirty) return 'Make an edit to test';
		if (errorCount > 0) return `Fix ${errorCount} error${errorCount === 1 ? '' : 's'} before testing`;
		return undefined;
	});

	async function startTest() {
		if (!canTest) return;
		testStarting = true;
		try {
			const res = await apiPost<{ ok: boolean; session?: TestSessionInfo; error?: string }>(
				`/plcs/plc/programs/${encodeURIComponent(name)}/test`,
				{ source: draftSource, timeoutSeconds: TEST_TIMEOUT_SECONDS }
			);
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			if (res.data?.ok && res.data.session) {
				testSession = res.data.session;
				saltState.addNotification({
					message: `Test session started (${TEST_TIMEOUT_SECONDS}s watchdog)`,
					type: 'success'
				});
			} else if (res.data?.error) {
				saltState.addNotification({ message: res.data.error, type: 'error' });
			}
		} finally {
			testStarting = false;
		}
	}

	async function stopTest() {
		if (!testSession) return;
		const res = await apiPost<{ ok: boolean }>(
			`/plcs/plc/programs/${encodeURIComponent(name)}/test/stop`,
			{}
		);
		if (res.error) {
			saltState.addNotification({ message: res.error.error, type: 'error' });
			return;
		}
		// The SSE 'stopped' event will clear testSession — fall back in case
		// the event arrives late so the UI isn't stuck.
		testSession = null;
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
			{#if testSession}
				<span class="test-status" title="Test session auto-reverts on error or timeout">
					<span class="test-dot"></span>
					Testing · {testRemaining}s
				</span>
				<button type="button" class="btn warn" onclick={stopTest}>
					Stop Test
				</button>
			{:else if !isNew && language === 'starlark'}
				<button
					type="button"
					class="btn test"
					onclick={startTest}
					disabled={!canTest}
					title={testBlockedReason ?? 'Hot-swap the draft into the engine with auto-revert on error'}
				>
					{testStarting ? 'Starting…' : 'Test'}
				</button>
			{/if}
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
				{saving ? 'Saving…' : isNew ? 'Create' : 'Save'}
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
			<div class="diff-wrap" class:diff-active={showDiff}>
				<div class="diff-pane pending-pane">
					{#if showDiff}
						<div class="pane-label pending">
							{testSession ? 'Testing (candidate)' : 'Draft (unsaved)'}
						</div>
					{/if}
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
				{#if showDiff}
					<div
						class="diff-pane live-pane"
						in:fly={{ x: 60, duration: 240, easing: cubicOut }}
						out:fly={{ x: 60, duration: 180, easing: cubicOut }}
					>
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
				{/if}
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

		&.test {
			color: var(--theme-warning, #d97706);
			border-color: color-mix(in srgb, var(--theme-warning, #d97706) 40%, transparent);

			&:hover:not(:disabled) {
				background: color-mix(in srgb, var(--theme-warning, #d97706) 12%, transparent);
				border-color: var(--theme-warning, #d97706);
			}
		}

		&.warn {
			color: var(--theme-warning, #d97706);
			background: color-mix(in srgb, var(--theme-warning, #d97706) 14%, transparent);
			border-color: var(--theme-warning, #d97706);

			&:hover:not(:disabled) {
				background: color-mix(in srgb, var(--theme-warning, #d97706) 22%, transparent);
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
		overflow: hidden;
	}

	.diff-pane {
		min-width: 0;
		min-height: 0;
		display: flex;
		flex-direction: column;
		border-right: 1px solid var(--theme-border);

		&:last-child {
			border-right: none;
		}
	}

	// When the diff is hidden the pending pane takes the whole area.
	// When the diff opens, both panes split 50/50 and the live pane
	// flies in from the right. Transitioning flex-basis lets the pending
	// pane shrink smoothly rather than snapping to half-width.
	.pending-pane {
		flex: 1 1 100%;
		transition: flex-basis 240ms cubic-bezier(0.25, 0.1, 0.25, 1);
	}

	.diff-wrap.diff-active .pending-pane {
		flex-basis: 50%;
	}

	.live-pane {
		flex: 1 1 50%;
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

	.test-status {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		font-size: 0.75rem;
		font-weight: 600;
		color: var(--theme-warning, #d97706);
		padding: 0.1875rem 0.5rem;
		border-radius: 0.25rem;
		background: color-mix(in srgb, var(--theme-warning, #d97706) 12%, transparent);
	}

	.test-dot {
		width: 0.5rem;
		height: 0.5rem;
		border-radius: 50%;
		background: var(--theme-warning, #d97706);
		animation: pulse 1.4s ease-in-out infinite;
	}

	@keyframes pulse {
		0%, 100% { opacity: 1; transform: scale(1); }
		50% { opacity: 0.5; transform: scale(0.8); }
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
