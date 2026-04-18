<script lang="ts">
	import { Pane, Splitpanes } from 'svelte-splitpanes';
	import { createWorkspaceLayout } from '$lib/plc/workspace-layout.svelte';

	const layout = createWorkspaceLayout();

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
								<div class="panel-body placeholder">
									Variables · Tasks · Programs list goes here.
								</div>
							</section>
						</Pane>
					{/if}
					<Pane minSize={20}>
						<section class="panel">
							<header class="panel-header">Editor</header>
							<div class="panel-body placeholder">
								Editor tabs + active editor (code / ladder / task form) go here.
							</div>
						</section>
					</Pane>
					{#if layout.rightOpen}
						<Pane size={layout.rightSize} minSize={10}>
							<section class="panel">
								<header class="panel-header">Inspector</header>
								<div class="panel-body placeholder">
									Live values + config for the selected item go here.
								</div>
							</section>
						</Pane>
					{/if}
				</Splitpanes>
			</Pane>
			{#if layout.bottomOpen}
				<Pane size={layout.bottomSize} minSize={8}>
					<section class="panel">
						<header class="panel-header">Output</header>
						<div class="panel-body placeholder">Logs · Transpile output · Problems.</div>
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
	}

	.placeholder {
		color: var(--theme-text-muted);
		font-size: 0.875rem;
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
