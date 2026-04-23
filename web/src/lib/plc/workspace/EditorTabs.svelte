<script lang="ts">
	import ProgramEditor from './ProgramEditor.svelte';
	import VariableEditor from './VariableEditor.svelte';
	import TaskEditor from './TaskEditor.svelte';
	import TestEditor from './TestEditor.svelte';
	import TypeEditor from './TypeEditor.svelte';
	import Tabs, { type TabItem } from '$lib/components/Tabs.svelte';
	import { workspaceTabs, workspaceSelection } from '../workspace-state.svelte';
	import type { EditorTabKind } from '../workspace-state.svelte';
	import type {
		PlcConfig,
		PlcTaskConfig,
		PlcTemplate,
		ProgramListItem,
		TestListItem
	} from '$lib/types/plc';
	import { XMark } from '@joyautomation/salt/icons';
	import DirtyIcon from '$lib/components/DirtyIcon.svelte';

	type Props = {
		variableNames: string[];
		plcConfig: PlcConfig | null;
		templates: PlcTemplate[];
		tasks: Record<string, PlcTaskConfig>;
		programs: ProgramListItem[];
		tests: TestListItem[];
	};

	let { variableNames, plcConfig, templates, tasks, programs, tests }: Props = $props();

	type EditorTab = TabItem & { kind: EditorTabKind; language?: string };

	const tabs = $derived<EditorTab[]>(
		workspaceTabs.list.map((t) => ({
			id: t.id,
			label: t.isNew ? t.name || 'Untitled' : t.name,
			kind: t.kind,
			language: t.language
		}))
	);

	function activate(id: string) {
		workspaceTabs.activate(id);
		const tab = workspaceTabs.list.find((t) => t.id === id);
		if (!tab) return;
		// Unsaved tabs don't have a real key yet; leaving the current
		// selection alone is correct — otherwise the navigator would lose
		// focus on whatever the user was looking at.
		if (tab.isNew) return;
		workspaceSelection.select(tab.kind, tab.name);
	}

	function close(e: MouseEvent | KeyboardEvent, id: string) {
		e.stopPropagation();
		workspaceTabs.close(id);
	}

	function badgeLabel(tab: EditorTab): string {
		if (tab.kind === 'variable') return 'VAR';
		if (tab.kind === 'task') return 'TASK';
		if (tab.kind === 'test') return 'TEST';
		if (tab.kind === 'type') return 'TYPE';
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
			<span
				class="badge"
				class:var-badge={tab.kind === 'variable'}
				class:task-badge={tab.kind === 'task'}
				class:test-badge={tab.kind === 'test'}
				class:type-badge={tab.kind === 'type'}
			>{badgeLabel(tab)}</span>
			<span class="name">{tab.label}</span>
			{#if workspaceTabs.dirty[tab.id]}
				<DirtyIcon size="0.875rem" />
			{/if}
			<span
				class="close"
				role="button"
				tabindex="-1"
				aria-label={`Close ${tab.label}`}
				onclick={(e) => close(e, tab.id)}
				onkeydown={(e) => {
					if (e.key === 'Enter' || e.key === ' ') close(e, tab.id);
				}}
			>
				<XMark size="0.75rem" />
			</span>
		{/snippet}
	</Tabs>

	<div class="tab-content">
		{#each workspaceTabs.list as tab (tab.id)}
			<div class="editor-host" class:hidden={workspaceTabs.active !== tab.id}>
				{#if tab.kind === 'variable'}
					<VariableEditor tabId={tab.id} name={tab.name} {plcConfig} {templates} />
				{:else if tab.kind === 'task'}
					<TaskEditor
						tabId={tab.id}
						name={tab.name}
						{tasks}
						{programs}
						onDirtyChange={(d) => workspaceTabs.setDirty(tab.id, d)}
					/>
				{:else if tab.kind === 'test'}
					<TestEditor
						tabId={tab.id}
						name={tab.name}
						isNew={tab.isNew ?? false}
						initialSource={tab.initialSource}
						{variableNames}
						{tests}
						{programs}
						onDirtyChange={(d) => workspaceTabs.setDirty(tab.id, d)}
					/>
				{:else if tab.kind === 'type'}
					<TypeEditor
						tabId={tab.id}
						name={tab.name}
						{templates}
						{plcConfig}
						isNew={tab.isNew ?? false}
					/>
				{:else}
					<ProgramEditor
						tabId={tab.id}
						name={tab.name}
						{variableNames}
						{programs}
						isNew={tab.isNew ?? false}
						initialLanguage={tab.language}
						onDirtyChange={(d) => workspaceTabs.setDirty(tab.id, d)}
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

		&.task-badge {
			color: var(--theme-text);
			background: color-mix(in srgb, var(--theme-text) 12%, transparent);
		}

		&.test-badge {
			color: var(--theme-success, #10b981);
			background: color-mix(in srgb, var(--theme-success, #10b981) 14%, transparent);
		}

		&.type-badge {
			color: var(--theme-primary);
			background: color-mix(in srgb, var(--theme-primary) 14%, transparent);
		}
	}

	.name {
		font-family: var(--font-mono, monospace);
		text-transform: none;
		letter-spacing: 0;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.close {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 1rem;
		height: 1rem;
		color: var(--theme-text-muted);
		border-radius: 0.1875rem;
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
