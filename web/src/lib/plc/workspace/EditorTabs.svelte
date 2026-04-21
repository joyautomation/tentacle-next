<script lang="ts">
	import ProgramEditor from './ProgramEditor.svelte';
	import VariableEditor from './VariableEditor.svelte';
	import Tabs, { type TabItem } from '$lib/components/Tabs.svelte';
	import { workspaceTabs, workspaceSelection } from '../workspace-state.svelte';
	import type { EditorTabKind } from '../workspace-state.svelte';
	import type { PlcConfig, PlcTemplate } from '$lib/types/plc';
	import { PencilSquare } from '@joyautomation/salt/icons';

	type Props = {
		variableNames: string[];
		plcConfig: PlcConfig | null;
		templates: PlcTemplate[];
	};

	let { variableNames, plcConfig, templates }: Props = $props();

	type EditorTab = TabItem & { kind: EditorTabKind; language?: string };

	const tabs = $derived<EditorTab[]>(
		workspaceTabs.list.map((t) => ({
			id: t.name,
			label: t.name,
			kind: t.kind,
			language: t.language
		}))
	);

	function activate(name: string) {
		workspaceTabs.activate(name);
		const tab = workspaceTabs.list.find((t) => t.name === name);
		if (!tab) return;
		if (tab.kind === 'program') workspaceSelection.select('program', name);
		else if (tab.kind === 'variable') workspaceSelection.select('variable', name);
	}

	function close(e: MouseEvent | KeyboardEvent, name: string) {
		e.stopPropagation();
		workspaceTabs.close(name);
	}

	function badgeLabel(tab: EditorTab): string {
		if (tab.kind === 'variable') return 'VAR';
		const lang = tab.language ?? '';
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
			<span class="badge" class:var-badge={tab.kind === 'variable'}>{badgeLabel(tab)}</span>
			<span class="name">{tab.label}</span>
			{#if workspaceTabs.dirty[tab.id]}
				<span class="dirty-icon" title="Unsaved changes"><PencilSquare size="0.875rem" /></span>
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
				{#if tab.kind === 'variable'}
					<VariableEditor name={tab.name} {plcConfig} {templates} />
				{:else}
					<ProgramEditor
						name={tab.name}
						{variableNames}
						onDirtyChange={(d) => workspaceTabs.setDirty(tab.name, d)}
					/>
				{/if}
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

		&.var-badge {
			color: var(--theme-text);
			background: color-mix(in srgb, var(--theme-text) 12%, transparent);
		}
	}

	.name {
		font-family: var(--font-mono, monospace);
		text-transform: none;
		letter-spacing: 0;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.dirty-icon {
		display: inline-flex;
		align-items: center;
		color: var(--theme-warning, #f59e0b);

		:global(svg) {
			flex-shrink: 0;
		}
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
