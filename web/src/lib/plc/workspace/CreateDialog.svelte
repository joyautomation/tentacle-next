<script lang="ts">
	import { apiPut } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import { XMark } from '@joyautomation/salt/icons';
	import { workspaceSelection, workspaceTabs } from '../workspace-state.svelte';
	import type { PlcTaskConfig, PlcTemplate, ProgramListItem } from '$lib/types/plc';

	type Kind = 'variable' | 'task' | 'program';

	type Props = {
		kind: Kind;
		programs: ProgramListItem[];
		templates?: PlcTemplate[];
		onClose: () => void;
	};

	let { kind, programs, templates = [], onClose }: Props = $props();

	let saving = $state(false);

	let progName = $state('');
	let progDescription = $state('');
	let progLanguage = $state<'starlark' | 'st' | 'ladder'>('starlark');

	let taskName = $state('');
	let taskProgramRef = $state('');
	let taskEntryFn = $state('main');
	let taskScanRate = $state(100);
	let taskEnabled = $state(true);

	let varName = $state('');
	let varDatatype = $state<string>('number');
	let varDirection = $state<'internal' | 'output' | 'input'>('internal');
	let varDefault = $state('');

	const templateByName = $derived.by(() => {
		const m: Record<string, PlcTemplate> = {};
		for (const t of templates) m[t.name] = t;
		return m;
	});
	const selectedTemplate = $derived(templateByName[varDatatype] ?? null);

	function isPrimitiveDatatype(dt: string): boolean {
		return dt === 'number' || dt === 'boolean' || dt === 'string';
	}

	function fieldZero(type: string): unknown {
		if (type.endsWith('[]')) return [];
		if (type.endsWith('{}')) return {};
		if (type === 'bool' || type === 'boolean') return false;
		if (type === 'string' || type === 'bytes') return '';
		if (type === 'number') return 0;
		return null;
	}

	const title = $derived(
		kind === 'program' ? 'New Function' : kind === 'task' ? 'New Task' : 'New Variable'
	);

	function defaultBody(lang: 'starlark' | 'st' | 'ladder', fn: string): string {
		if (lang === 'st') return '';
		// Annotated template advertises the signature-from-annotations
		// feature on first use — users see right away that `: type` and
		// `-> type` are understood.
		return `def ${fn}():\n    pass\n`;
	}

	const canSubmit = $derived.by(() => {
		if (saving) return false;
		if (kind === 'program') return progName.trim() !== '';
		if (kind === 'task') return taskName.trim() !== '' && taskProgramRef !== '' && taskScanRate > 0;
		return varName.trim() !== '';
	});

	function parseDefault(dt: string, raw: string): unknown {
		const trimmed = raw.trim();
		if (dt === 'boolean') return trimmed === 'true' || trimmed === '1';
		if (dt === 'string') return trimmed;
		if (dt === 'number') {
			const n = Number(trimmed);
			return Number.isFinite(n) ? n : 0;
		}
		return null;
	}

	function templateDefault(tpl: PlcTemplate): Record<string, unknown> {
		const out: Record<string, unknown> = {};
		for (const f of tpl.fields) {
			out[f.name] = f.default !== undefined ? f.default : fieldZero(f.type);
		}
		return out;
	}

	async function submit() {
		if (!canSubmit) return;
		saving = true;
		try {
			if (kind === 'program') {
				const name = progName.trim();
				const source = progLanguage === 'st' ? '' : defaultBody(progLanguage, name);
				// Signature metadata is derived server-side from the function
				// body's annotations (`def foo(x: int) -> bool:`); the
				// client doesn't send a signature at all.
				const body: Record<string, unknown> = {
					name,
					description: progDescription.trim() || undefined,
					language: progLanguage,
					source,
					updatedBy: 'gui'
				};
				if (progLanguage === 'st') body.stSource = '';
				const res = await apiPut(`/plcs/plc/programs/${encodeURIComponent(name)}`, body);
				if (res.error) {
					saltState.addNotification({ message: res.error.error, type: 'error' });
					return;
				}
				saltState.addNotification({ message: `Program "${name}" created`, type: 'success' });
				await invalidateAll();
				workspaceTabs.open({ name, kind: 'program', language: progLanguage });
				workspaceSelection.select('program', name);
				onClose();
			} else if (kind === 'task') {
				const name = taskName.trim();
				const body: PlcTaskConfig = {
					name,
					scanRateMs: taskScanRate,
					programRef: taskProgramRef,
					entryFn: taskEntryFn.trim() || 'main',
					enabled: taskEnabled
				};
				const res = await apiPut(`/plcs/plc/tasks/${encodeURIComponent(name)}`, body);
				if (res.error) {
					saltState.addNotification({ message: res.error.error, type: 'error' });
					return;
				}
				saltState.addNotification({ message: `Task "${name}" created`, type: 'success' });
				await invalidateAll();
				workspaceSelection.select('task', name);
				onClose();
			} else {
				const name = varName.trim();
				const def = selectedTemplate
					? templateDefault(selectedTemplate)
					: parseDefault(varDatatype, varDefault);
				const body = {
					id: name,
					datatype: varDatatype,
					direction: varDirection,
					default: def
				};
				const res = await apiPut(`/plcs/plc/variables/${encodeURIComponent(name)}`, body);
				if (res.error) {
					saltState.addNotification({ message: res.error.error, type: 'error' });
					return;
				}
				saltState.addNotification({ message: `Variable "${name}" created`, type: 'success' });
				await invalidateAll();
				workspaceTabs.open({ name, kind: 'variable' });
				workspaceSelection.select('variable', name);
				onClose();
			}
		} finally {
			saving = false;
		}
	}

	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') onClose();
		if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) submit();
	}
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="backdrop"
	onclick={(e) => {
		if (e.target === e.currentTarget) onClose();
	}}
	onkeydown={onKeydown}
>
	<div class="dialog" role="dialog" aria-modal="true" aria-label={title}>
		<header class="dlg-head">
			<h3>{title}</h3>
			<button type="button" class="close" onclick={onClose} aria-label="Close"><XMark size="1rem" /></button>
		</header>

		<form
			class="dlg-body"
			onsubmit={(e) => {
				e.preventDefault();
				submit();
			}}
		>
			{#if kind === 'program'}
				<label class="field">
					<span>Name</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						type="text"
						bind:value={progName}
						placeholder="scan_cycle"
						required
						autofocus
					/>
				</label>
				<label class="field">
					<span>Description</span>
					<input
						type="text"
						bind:value={progDescription}
						placeholder="What does this function do?"
					/>
				</label>
				<label class="field">
					<span>Language</span>
					<select bind:value={progLanguage}>
						<option value="starlark">Starlark</option>
						<option value="st">Structured Text</option>
						<option value="ladder">Ladder</option>
					</select>
				</label>
				{#if progLanguage === 'starlark'}
					<p class="sig-hint">
						Parameter and return types are derived from annotations in the
						function body &mdash;
						<code>def name(x: int, y: str) -&gt; bool:</code>
					</p>
				{/if}
			{:else if kind === 'task'}
				<label class="field">
					<span>Name</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						type="text"
						bind:value={taskName}
						placeholder="FastScan"
						required
						autofocus
					/>
				</label>
				<label class="field">
					<span>Function</span>
					<select bind:value={taskProgramRef} required>
						<option value="" disabled>Select a function…</option>
						{#each programs as p (p.name)}
							<option value={p.name}>{p.name}</option>
						{/each}
					</select>
				</label>
				<label class="field">
					<span>Entry function</span>
					<input
						type="text"
						bind:value={taskEntryFn}
						placeholder="main"
					/>
				</label>
				<label class="field">
					<span>Scan rate (ms)</span>
					<input type="number" bind:value={taskScanRate} min="1" required />
				</label>
				<label class="field inline">
					<input type="checkbox" bind:checked={taskEnabled} />
					<span>Enabled</span>
				</label>
			{:else}
				<label class="field">
					<span>Name</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input type="text" bind:value={varName} placeholder="tank_level" required autofocus />
				</label>
				<label class="field">
					<span>Datatype</span>
					<select bind:value={varDatatype}>
						<optgroup label="Primitives">
							<option value="number">number</option>
							<option value="boolean">boolean</option>
							<option value="string">string</option>
						</optgroup>
						{#if templates.length > 0}
							<optgroup label="Templates">
								{#each templates as tmpl (tmpl.name)}
									<option value={tmpl.name}>{tmpl.name}</option>
								{/each}
							</optgroup>
						{/if}
					</select>
				</label>
				<label class="field">
					<span>Direction</span>
					<select bind:value={varDirection}>
						<option value="internal">internal</option>
						<option value="output">output</option>
						<option value="input">input</option>
					</select>
				</label>
				{#if isPrimitiveDatatype(varDatatype)}
					<label class="field">
						<span>Default</span>
						<input
							type="text"
							bind:value={varDefault}
							placeholder={varDatatype === 'boolean' ? 'false' : '0'}
						/>
					</label>
				{:else if selectedTemplate}
					<p class="template-hint">
						Uses template defaults. Edit field values after creating.
					</p>
				{/if}
			{/if}

			<div class="actions">
				<button type="button" class="btn subtle" onclick={onClose}>Cancel</button>
				<button type="submit" class="btn primary" disabled={!canSubmit}>
					{saving ? 'Creating…' : 'Create'}
				</button>
			</div>
		</form>
	</div>
</div>

<style lang="scss">
	.backdrop {
		position: fixed;
		inset: 0;
		background: color-mix(in srgb, black 45%, transparent);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 50;
		padding: 1rem;
	}

	.dialog {
		width: 100%;
		max-width: 26rem;
		background: var(--theme-background);
		border: 1px solid var(--theme-border);
		border-radius: 0.5rem;
		box-shadow: 0 12px 40px rgba(0, 0, 0, 0.3);
		display: flex;
		flex-direction: column;
		max-height: calc(100vh - 2rem);
	}

	.dlg-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.75rem 1rem;
		border-bottom: 1px solid var(--theme-border);

		h3 {
			margin: 0;
			font-size: 0.9375rem;
			color: var(--theme-text);
		}
	}

	.close {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 1.75rem;
		height: 1.75rem;
		background: transparent;
		border: none;
		color: var(--theme-text-muted);
		line-height: 1;
		cursor: pointer;
		border-radius: 0.25rem;

		&:hover {
			color: var(--theme-text);
			background: var(--theme-surface);
		}
	}

	.dlg-body {
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		overflow-y: auto;
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 0.3125rem;
		font-size: 0.8125rem;
		color: var(--theme-text);

		&.inline {
			flex-direction: row;
			align-items: center;
			gap: 0.5rem;
		}

		span {
			font-size: 0.75rem;
			color: var(--theme-text-muted);
			font-weight: 500;
		}

		input[type='text'],
		input[type='number'],
		select {
			padding: 0.375rem 0.5rem;
			font-size: 0.8125rem;
			background: var(--theme-background);
			color: var(--theme-text);
			border: 1px solid var(--theme-border);
			border-radius: 0.3125rem;

			&:focus {
				outline: none;
				border-color: var(--theme-primary);
			}
		}
	}

	.template-hint {
		margin: 0;
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		font-style: italic;
	}

	.sig-hint {
		margin: 0;
		padding: 0.5rem 0.625rem;
		font-size: 0.75rem;
		color: var(--theme-text-muted);
		background: var(--theme-surface);
		border: 1px solid var(--theme-border);
		border-radius: 0.3125rem;
		line-height: 1.4;

		code {
			font-family: 'IBM Plex Mono', monospace;
			font-size: 0.6875rem;
			color: var(--theme-text);
		}
	}

	.actions {
		display: flex;
		gap: 0.5rem;
		justify-content: flex-end;
		margin-top: 0.25rem;
	}

	.btn {
		padding: 0.375rem 0.875rem;
		font-size: 0.8125rem;
		border-radius: 0.3125rem;
		border: 1px solid var(--theme-border);
		background: transparent;
		color: var(--theme-text);
		cursor: pointer;

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
</style>
