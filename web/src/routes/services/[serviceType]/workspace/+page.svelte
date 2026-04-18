<script lang="ts">
	import { Pane, Splitpanes } from 'svelte-splitpanes';
	import { createWorkspaceLayout } from '$lib/plc/workspace-layout.svelte';
	import { workspaceSelection } from '$lib/plc/workspace-state.svelte';
	import Navigator from '$lib/plc/workspace/Navigator.svelte';
	import Inspector from '$lib/plc/workspace/Inspector.svelte';
	import ProgramEditor from '$lib/plc/workspace/ProgramEditor.svelte';
	import LogViewer from '$lib/components/LogViewer.svelte';
	import type { WorkspaceLoadData } from './+page';

	let { data }: { data: WorkspaceLoadData } = $props();

	const layout = createWorkspaceLayout();

	const selection = $derived(workspaceSelection.current);

	const variableNames = $derived(Object.keys(data.variables).sort());

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

<div class="workspace">
	<div class="toolbar">
		<div class="toolbar-group">
			<strong class="title">PLC Workspace</strong>
		</div>
		<div class="toolbar-group">
			<button
				type="button"
				class="toggle"
				class:active={layout.leftOpen}
				onclick={toggleLeft}
				title="Toggle navigator"
				aria-pressed={layout.leftOpen}
			>
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
					<rect x="3" y="4" width="6" height="16" rx="1" />
					<rect x="10" y="4" width="11" height="16" rx="1" opacity="0.35" />
				</svg>
				<span>Navigator</span>
			</button>
			<button
				type="button"
				class="toggle"
				class:active={layout.rightOpen}
				onclick={toggleRight}
				title="Toggle inspector"
				aria-pressed={layout.rightOpen}
			>
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
					<rect x="3" y="4" width="11" height="16" rx="1" opacity="0.35" />
					<rect x="15" y="4" width="6" height="16" rx="1" />
				</svg>
				<span>Inspector</span>
			</button>
			<button
				type="button"
				class="toggle"
				class:active={layout.bottomOpen}
				onclick={toggleBottom}
				title="Toggle output"
				aria-pressed={layout.bottomOpen}
			>
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
					<rect x="3" y="3" width="18" height="12" rx="1" opacity="0.35" />
					<rect x="3" y="16" width="18" height="5" rx="1" />
				</svg>
				<span>Output</span>
			</button>
		</div>
	</div>

	<div class="split-root">
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
								<header class="panel-header">Navigator</header>
								<div class="panel-body no-pad">
									<Navigator
										variables={data.variables}
										tasks={data.tasks}
										programs={data.programs}
									/>
								</div>
							</section>
						</Pane>
					{/if}
					<Pane minSize={20}>
						<section class="panel">
							<header class="panel-header">Editor</header>
							<div class="panel-body no-pad">
								{#if selection?.kind === 'program'}
									{#key selection.id}
										<ProgramEditor name={selection.id} {variableNames} />
									{/key}
								{:else if selection?.kind === 'task'}
									<div class="placeholder-card">
										<div class="label">Task</div>
										<div class="title">{selection.id}</div>
										<div class="hint">
											Task editing in the workspace is not yet wired up.
											<a href="/services/plc/tasks">Open in the Tasks tab</a>.
										</div>
									</div>
								{:else if selection?.kind === 'variable'}
									<div class="placeholder-card">
										<div class="label">Variable</div>
										<div class="title">{selection.id}</div>
										<div class="hint">
											Variable config editing will appear in the Inspector soon.
											For now, use the <a href="/services/plc/info">Variables tab</a>.
										</div>
									</div>
								{:else}
									<div class="placeholder-card muted">
										<div class="title">Nothing selected</div>
										<div class="hint">
											Pick a program from the Navigator to open it here.
										</div>
									</div>
								{/if}
							</div>
						</section>
					</Pane>
					{#if layout.rightOpen}
						<Pane size={layout.rightSize} minSize={10}>
							<section class="panel">
								<header class="panel-header">Inspector</header>
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
					<section class="panel">
						<header class="panel-header">Output · Logs</header>
						<div class="panel-body logs">
							<LogViewer serviceType={data.serviceType} initialLogs={data.initialLogs} />
						</div>
					</section>
				</Pane>
			{/if}
		</Splitpanes>
	</div>
</div>

<style lang="scss">
	.workspace {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-height: 0;
		height: calc(100vh - var(--header-height) - 6.5rem);
	}

	.toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.5rem 1rem;
		border-bottom: 1px solid var(--theme-border);
		background: var(--theme-surface);
	}

	.toolbar-group {
		display: flex;
		align-items: center;
		gap: 0.375rem;
	}

	.title {
		font-size: 0.875rem;
		color: var(--theme-text);
	}

	.toggle {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.375rem 0.625rem;
		font-size: 0.8125rem;
		color: var(--theme-text-muted);
		background: transparent;
		border: 1px solid var(--theme-border);
		border-radius: 0.375rem;
		cursor: pointer;
		transition: all 0.12s ease;

		&:hover {
			color: var(--theme-text);
			border-color: var(--theme-text-muted);
		}

		&.active {
			color: var(--theme-primary);
			border-color: var(--theme-primary);
			background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
		}

		svg {
			flex-shrink: 0;
		}
	}

	.split-root {
		flex: 1;
		min-height: 0;
		position: relative;
	}

	.panel {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		background: var(--theme-background);
	}

	.panel-header {
		padding: 0.5rem 0.75rem;
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--theme-text-muted);
		background: var(--theme-surface);
		border-bottom: 1px solid var(--theme-border);
	}

	.panel-body {
		flex: 1;
		min-height: 0;
		overflow: auto;
		padding: 0.75rem;

		&.no-pad {
			padding: 0;
		}

		&.logs {
			padding: 0.5rem 0.75rem;
			display: flex;
			flex-direction: column;
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

		.label {
			font-size: 0.6875rem;
			font-weight: 600;
			text-transform: uppercase;
			letter-spacing: 0.04em;
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

		a {
			color: var(--theme-primary);
		}
	}

	.label {
		font-size: 0.6875rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--theme-text-muted);
	}

	.title {
		font-size: 1rem;
		font-weight: 600;
		font-family: var(--font-mono, monospace);
		color: var(--theme-text);
		margin-bottom: 0.25rem;
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
