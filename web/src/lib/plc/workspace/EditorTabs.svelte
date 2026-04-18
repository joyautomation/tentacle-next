<script lang="ts">
	import ProgramEditor from './ProgramEditor.svelte';
	import { workspaceTabs, workspaceSelection } from '../workspace-state.svelte';

	type Props = {
		variableNames: string[];
	};

	let { variableNames }: Props = $props();

	function activate(name: string) {
		workspaceTabs.activate(name);
		const tab = workspaceTabs.list.find((t) => t.name === name);
		if (tab) workspaceSelection.select('program', name);
	}

	function close(e: MouseEvent, name: string) {
		e.stopPropagation();
		workspaceTabs.close(name);
	}

	function languageLabel(lang: string): string {
		if (lang === 'starlark') return 'PY';
		if (lang === 'st' || lang === 'structured-text') return 'ST';
		if (lang === 'ladder') return 'LD';
		return lang.slice(0, 2).toUpperCase();
	}
</script>

<div class="editor-tabs">
	<div class="tab-strip" role="tablist">
		{#each workspaceTabs.list as tab (tab.name)}
			<button
				type="button"
				role="tab"
				class="tab"
				class:active={workspaceTabs.active === tab.name}
				aria-selected={workspaceTabs.active === tab.name}
				onclick={() => activate(tab.name)}
			>
				<span class="badge">{languageLabel(tab.language)}</span>
				<span class="name">{tab.name}</span>
				{#if workspaceTabs.dirty[tab.name]}
					<span class="dirty" title="Unsaved changes">●</span>
				{/if}
				<span
					class="close"
					role="button"
					tabindex="-1"
					aria-label={`Close ${tab.name}`}
					onclick={(e) => close(e, tab.name)}
					onkeydown={(e) => {
						if (e.key === 'Enter' || e.key === ' ') {
							e.stopPropagation();
							workspaceTabs.close(tab.name);
						}
					}}
				>
					×
				</span>
			</button>
		{/each}
	</div>

	<div class="tab-content">
		{#each workspaceTabs.list as tab (tab.name)}
			<div class="editor-host" class:hidden={workspaceTabs.active !== tab.name}>
				<ProgramEditor
					name={tab.name}
					{variableNames}
					onDirtyChange={(d) => workspaceTabs.setDirty(tab.name, d)}
				/>
			</div>
		{/each}
	</div>
</div>

<style lang="scss">
	.editor-tabs {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
	}

	.tab-strip {
		display: flex;
		overflow-x: auto;
		background: var(--theme-background);
		border-bottom: 1px solid var(--theme-border);
		flex-shrink: 0;
	}

	.tab {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.375rem 0.625rem;
		background: transparent;
		border: none;
		border-right: 1px solid var(--theme-border);
		cursor: pointer;
		color: var(--theme-text-muted);
		font-size: 0.8125rem;
		white-space: nowrap;
		max-width: 16rem;
		flex-shrink: 0;

		&:hover {
			color: var(--theme-text);
			background: var(--theme-surface);
		}

		&.active {
			color: var(--theme-text);
			background: var(--theme-surface);
			box-shadow: inset 0 2px 0 var(--theme-primary);
		}
	}

	.badge {
		padding: 0.0625rem 0.3125rem;
		font-size: 0.625rem;
		font-weight: 600;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		border-radius: 0.1875rem;
		font-family: var(--font-mono, monospace);
	}

	.name {
		font-family: var(--font-mono, monospace);
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.dirty {
		color: var(--theme-warning, var(--theme-primary));
		font-size: 0.625rem;
	}

	.close {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 1rem;
		height: 1rem;
		color: var(--theme-text-muted);
		border-radius: 0.1875rem;
		font-size: 1rem;
		line-height: 1;

		&:hover {
			color: var(--theme-text);
			background: var(--theme-border);
		}
	}

	.tab-content {
		flex: 1;
		min-height: 0;
		position: relative;
	}

	.editor-host {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;

		&.hidden {
			display: none;
		}
	}
</style>
