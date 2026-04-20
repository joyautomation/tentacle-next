<script lang="ts">
	import ProgramEditor from './ProgramEditor.svelte';
	import Tabs, { type TabItem } from '$lib/components/Tabs.svelte';
	import { workspaceTabs, workspaceSelection } from '../workspace-state.svelte';

	type Props = {
		variableNames: string[];
	};

	let { variableNames }: Props = $props();

	type EditorTab = TabItem & { language: string };

	const tabs = $derived<EditorTab[]>(
		workspaceTabs.list.map((t) => ({
			id: t.name,
			label: t.name,
			language: t.language
		}))
	);

	function activate(name: string) {
		workspaceTabs.activate(name);
		const tab = workspaceTabs.list.find((t) => t.name === name);
		if (tab) workspaceSelection.select('program', name);
	}

	function close(e: MouseEvent | KeyboardEvent, name: string) {
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
	<Tabs
		{tabs}
		active={workspaceTabs.active}
		onChange={activate}
		size="sm"
		ariaLabel="Open editors"
	>
		{#snippet tab({ tab }: { tab: EditorTab; active: boolean })}
			<span class="badge">{languageLabel(tab.language)}</span>
			<span class="name">{tab.label}</span>
			{#if workspaceTabs.dirty[tab.id]}
				<span class="dirty" title="Unsaved changes">●</span>
			{/if}
			<span
				class="close"
				role="button"
				tabindex="-1"
				aria-label={`Close ${tab.id}`}
				onclick={(e) => close(e, tab.id)}
				onkeydown={(e) => {
					if (e.key === 'Enter' || e.key === ' ') close(e, tab.id);
				}}
			>
				×
			</span>
		{/snippet}
	</Tabs>

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

	.badge {
		padding: 0.0625rem 0.3125rem;
		font-size: 0.625rem;
		font-weight: 600;
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		border-radius: 0.1875rem;
		font-family: var(--font-mono, monospace);
		text-transform: none;
		letter-spacing: 0;
	}

	.name {
		font-family: var(--font-mono, monospace);
		text-transform: none;
		letter-spacing: 0;
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
