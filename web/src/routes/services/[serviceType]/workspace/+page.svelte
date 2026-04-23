<script lang="ts">
	import { Pane, Splitpanes } from 'svelte-splitpanes';
	import { createWorkspaceLayout } from '$lib/plc/workspace-layout.svelte';
	import {
		workspaceSelection,
		workspaceTabs,
		workspaceEditorSaves
	} from '$lib/plc/workspace-state.svelte';
	import Navigator from '$lib/plc/workspace/Navigator.svelte';
	import Inspector from '$lib/plc/workspace/Inspector.svelte';
	import EditorTabs from '$lib/plc/workspace/EditorTabs.svelte';
	import CreateDialog from '$lib/plc/workspace/CreateDialog.svelte';
	import OutputPanel from '$lib/plc/workspace/OutputPanel.svelte';
	import { ChevronLeft, ChevronRight, ChevronUp } from '@joyautomation/salt/icons';
	import { apiPost } from '$lib/api/client';
	import { invalidateAll } from '$app/navigation';
	import { state as saltState } from '@joyautomation/salt';
	import type { PlcTestResult } from '$lib/types/plc';
	import type { WorkspaceLoadData } from './+page';

	let { data }: { data: WorkspaceLoadData } = $props();

	const layout = createWorkspaceLayout();

	const selection = $derived(workspaceSelection.current);

	const variableNames = $derived(Object.keys(data.variables).sort());

	const programsByName = $derived.by(() => {
		const map: Record<string, string> = {};
		for (const p of data.programs) map[p.name] = p.language;
		return map;
	});

	$effect(() => {
		if (selection?.kind !== 'program') return;
		const lang = programsByName[selection.id];
		if (!lang) return;
		workspaceTabs.open({ name: selection.id, kind: 'program', language: lang });
	});

	$effect(() => {
		if (selection?.kind !== 'variable') return;
		if (!(selection.id in data.variables)) return;
		workspaceTabs.open({ name: selection.id, kind: 'variable' });
	});

	$effect(() => {
		if (selection?.kind !== 'task') return;
		if (!(selection.id in data.tasks)) return;
		workspaceTabs.open({ name: selection.id, kind: 'task' });
	});

	$effect(() => {
		if (selection?.kind !== 'test') return;
		if (!data.tests.some((t) => t.name === selection.id)) return;
		workspaceTabs.open({ name: selection.id, kind: 'test' });
	});

	// Ctrl/Cmd+S saves the active tab's draft. Each editor registers its
	// own save handler in workspaceEditorSaves; the handler no-ops when
	// there's nothing to persist (saving=false, clean, invalid, etc.).
	function onKeydown(e: KeyboardEvent) {
		if (e.key !== 's' || !(e.ctrlKey || e.metaKey) || e.altKey) return;
		const active = workspaceTabs.active;
		if (!active) return;
		e.preventDefault();
		workspaceEditorSaves.invoke(active);
	}

	let createKind = $state<'variable' | 'task' | null>(null);
	let runningAllTests = $state(false);

	async function runAllTests() {
		runningAllTests = true;
		try {
			const res = await apiPost<PlcTestResult[]>('/plcs/plc/tests/run');
			if (res.error) {
				saltState.addNotification({ message: res.error.error, type: 'error' });
				return;
			}
			const total = res.data?.length ?? 0;
			const passed = res.data?.filter((r) => r.status === 'pass').length ?? 0;
			saltState.addNotification({
				message: `${passed}/${total} tests passed`,
				type: passed === total ? 'success' : 'error'
			});
			await invalidateAll();
		} finally {
			runningAllTests = false;
		}
	}

	function toggleLeft() {
		layout.leftOpen = !layout.leftOpen;
	}
	function toggleRight() {
		layout.rightOpen = !layout.rightOpen;
	}
	function toggleBottom() {
		layout.bottomOpen = !layout.bottomOpen;
	}

	function onMainResize(sizes: { size: number }[]) {
		// sizes align with rendered panes: [left?, center, right?]
		let i = 0;
		if (layout.leftOpen) {
			layout.leftSize = sizes[i].size;
			i++;
		}
		// center (skip)
		i++;
		if (layout.rightOpen) {
			layout.rightSize = sizes[i].size;
		}
	}

	function onOuterResize(sizes: { size: number }[]) {
		// [main, bottom?]
		if (layout.bottomOpen && sizes[1]) {
			layout.bottomSize = sizes[1].size;
		}
	}
</script>

<svelte:window onkeydown={onKeydown} />

<div class="workspace">
	<div class="split-root">
		{#if !layout.leftOpen}
			<button
				type="button"
				class="rail rail-left"
				onclick={toggleLeft}
				title="Show navigator"
				aria-label="Show navigator"
			>
				<ChevronRight size="0.875rem" />
				<span class="rail-label">Navigator</span>
			</button>
		{/if}
		<div class="split-area">
			<Splitpanes
				horizontal
				theme="plc-workspace"
				on:resized={(e) => onOuterResize(e.detail)}
			>
				<Pane minSize={30}>
					<Splitpanes theme="plc-workspace" on:resized={(e) => onMainResize(e.detail)}>
						{#if layout.leftOpen}
							<Pane size={layout.leftSize} minSize={10}>
								<section class="panel">
									<header class="panel-header">
										<span>Navigator</span>
										<button
											type="button"
											class="collapse-btn"
											onclick={toggleLeft}
											title="Hide navigator"
											aria-label="Hide navigator"
										>
											<ChevronLeft size="0.875rem" />
										</button>
									</header>
									<div class="panel-body no-pad">
										<Navigator
											variables={data.variables}
											tasks={data.tasks}
											programs={data.programs}
											tests={data.tests}
											onCreate={(kind) => (createKind = kind)}
											onRunAllTests={runAllTests}
											testsRunning={runningAllTests}
										/>
									</div>
								</section>
							</Pane>
						{/if}
						<Pane minSize={20}>
							<section class="panel">
								<header class="panel-header">Editor</header>
								<div class="panel-body no-pad">
									{#if workspaceTabs.list.length > 0}
										<EditorTabs
											{variableNames}
											plcConfig={data.plcConfig}
											templates={data.templates}
											tasks={data.tasks}
											programs={data.programs}
											tests={data.tests}
										/>
									{:else}
										<div class="placeholder-card muted">
											<div class="title">Nothing selected</div>
											<div class="hint">
												Pick a function from the Navigator to open it here.
											</div>
										</div>
									{/if}
								</div>
							</section>
						</Pane>
						{#if layout.rightOpen}
							<Pane size={layout.rightSize} minSize={10}>
								<section class="panel">
									<header class="panel-header">
										<span>Inspector</span>
										<button
											type="button"
											class="collapse-btn"
											onclick={toggleRight}
											title="Hide inspector"
											aria-label="Hide inspector"
										>
											<ChevronRight size="0.875rem" />
										</button>
									</header>
									<div class="panel-body no-pad">
										<Inspector
											variables={data.variables}
											tasks={data.tasks}
											programs={data.programs}
										/>
									</div>
								</section>
							</Pane>
						{/if}
					</Splitpanes>
				</Pane>
				{#if layout.bottomOpen}
					<Pane size={layout.bottomSize} minSize={8}>
						<OutputPanel
							serviceType={data.serviceType}
							initialLogs={data.initialLogs}
							onCollapse={toggleBottom}
						/>
					</Pane>
				{/if}
			</Splitpanes>
			{#if !layout.bottomOpen}
				<button
					type="button"
					class="rail rail-bottom"
					onclick={toggleBottom}
					title="Show output"
					aria-label="Show output"
				>
					<span class="rail-label-h">Output</span>
					<ChevronUp size="0.875rem" />
				</button>
			{/if}
		</div>
		{#if !layout.rightOpen}
			<button
				type="button"
				class="rail rail-right"
				onclick={toggleRight}
				title="Show inspector"
				aria-label="Show inspector"
			>
				<ChevronLeft size="0.875rem" />
				<span class="rail-label">Inspector</span>
			</button>
		{/if}
	</div>
</div>

{#if createKind}
	<CreateDialog
		kind={createKind}
		programs={data.programs}
		templates={data.templates}
		onClose={() => (createKind = null)}
	/>
{/if}

<style lang="scss">
	:global(.service-layout:has(> .workspace)) {
		height: calc(100vh - var(--header-height));
		min-height: 0;
		overflow: hidden;
	}

	.workspace {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-height: 0;
	}

	.split-root {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: row;
	}

	.split-area {
		flex: 1;
		min-width: 0;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}

	.rail {
		display: flex;
		align-items: center;
		justify-content: flex-start;
		gap: 0.5rem;
		padding: 0;
		background: var(--theme-surface);
		border: 0;
		color: var(--theme-text-muted);
		cursor: pointer;
		transition: color 0.12s ease, background 0.12s ease;

		&:hover {
			color: var(--theme-primary);
			background: color-mix(in srgb, var(--theme-primary) 8%, var(--theme-surface));
		}
	}

	.rail-left,
	.rail-right {
		flex-direction: column;
		width: 1.75rem;
		flex-shrink: 0;
		padding: 0.625rem 0;
	}

	.rail-left {
		border-right: 1px solid var(--theme-border);
	}

	.rail-right {
		border-left: 1px solid var(--theme-border);
	}

	.rail-bottom {
		flex-direction: row;
		justify-content: flex-end;
		height: 1.75rem;
		flex-shrink: 0;
		padding: 0 0.625rem;
		border-top: 1px solid var(--theme-border);
	}

	.rail-label {
		writing-mode: vertical-rl;
		transform: rotate(180deg);
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.rail-label-h {
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.panel {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		background: var(--theme-background);
	}

	.panel-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		min-height: 20px;
		padding: 0.5rem 0.75rem;
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--theme-text-muted);
		background: var(--theme-surface);
		border-bottom: 1px solid var(--theme-border);
	}

	.collapse-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 1.25rem;
		height: 1.25rem;
		padding: 0;
		background: transparent;
		border: 0;
		color: var(--theme-text-muted);
		cursor: pointer;
		border-radius: 0.1875rem;
		transition: color 0.12s ease, background 0.12s ease;

		&:hover {
			color: var(--theme-text);
			background: var(--theme-border);
		}
	}

	.panel-body {
		flex: 1;
		min-height: 0;
		overflow: auto;
		padding: 0.75rem;

		&.no-pad {
			padding: 0;
		}
	}

	.placeholder-card {
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.375rem;

		&.muted {
			color: var(--theme-text-muted);
		}

		.title {
			font-size: 1rem;
			font-weight: 600;
			font-family: var(--font-mono, monospace);
			color: var(--theme-text);
		}
	}

	.hint {
		color: var(--theme-text-muted);
		font-size: 0.75rem;
		font-style: italic;
	}

	:global(.splitpanes.plc-workspace .splitpanes__splitter) {
		background: var(--theme-border);
		position: relative;
		transition: background 0.15s ease;
	}

	:global(.splitpanes.plc-workspace .splitpanes__splitter:hover),
	:global(.splitpanes.plc-workspace .splitpanes__splitter.splitpanes__splitter--active) {
		background: var(--theme-primary);
	}

	:global(.splitpanes.plc-workspace.splitpanes--vertical > .splitpanes__splitter) {
		width: 4px;
		cursor: col-resize;
	}

	:global(.splitpanes.plc-workspace.splitpanes--horizontal > .splitpanes__splitter) {
		height: 4px;
		cursor: row-resize;
	}
</style>
