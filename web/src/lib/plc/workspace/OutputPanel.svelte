<script lang="ts">
	import LogViewer from '$lib/components/LogViewer.svelte';
	import ProblemsView from '$lib/plc/workspace/ProblemsView.svelte';
	import Tabs, { type TabItem } from '$lib/components/Tabs.svelte';
	import { workspaceDiagnostics } from '$lib/plc/workspace-state.svelte';
	import { ChevronDown } from '@joyautomation/salt/icons';

	type Props = {
		serviceType: string;
		initialLogs: import('svelte').ComponentProps<typeof LogViewer>['initialLogs'];
		onCollapse?: () => void;
	};

	let { serviceType, initialLogs, onCollapse }: Props = $props();

	type TabId = 'problems' | 'logs';
	let activeTab = $state<TabId>('problems');

	const problemCount = $derived(workspaceDiagnostics.total);
	const errorCount = $derived(workspaceDiagnostics.errorCount);

	const tabs = $derived<TabItem[]>([
		{ id: 'problems', label: 'Problems' },
		{ id: 'logs', label: 'Logs' }
	]);
</script>

<section class="panel">
	<header class="panel-header">
		<Tabs
			{tabs}
			active={activeTab}
			onChange={(id) => (activeTab = id as TabId)}
			size="sm"
			ariaLabel="Output panel"
		>
			{#snippet tab({ tab }: { tab: TabItem; active: boolean })}
				<span>{tab.label}</span>
				{#if tab.id === 'problems' && problemCount > 0}
					<span class="badge" class:error={errorCount > 0}>{problemCount}</span>
				{/if}
			{/snippet}
			{#snippet trailing()}
				{#if onCollapse}
					<button
						type="button"
						class="collapse-btn"
						onclick={onCollapse}
						title="Hide output"
						aria-label="Hide output"
					>
						<ChevronDown size="0.875rem" />
					</button>
				{/if}
			{/snippet}
		</Tabs>
	</header>
	<div class="panel-body" class:with-padding={activeTab === 'logs'}>
		{#if activeTab === 'problems'}
			<ProblemsView />
		{:else}
			<LogViewer {serviceType} {initialLogs} />
		{/if}
	</div>
</section>

<style lang="scss">
	.panel {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
		background: var(--theme-background);
	}

	.panel-header {
		background: var(--theme-surface);
	}

	.badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 1.25rem;
		height: 1rem;
		padding: 0 0.375rem;
		font-size: 0.6875rem;
		font-weight: 700;
		line-height: 1;
		color: var(--theme-text);
		background: var(--theme-border);
		border-radius: 0.625rem;
		letter-spacing: 0;

		&.error {
			color: white;
			background: var(--theme-danger, #e14545);
		}
	}

	.collapse-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 1.5rem;
		height: 1.5rem;
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
		overflow: hidden;
		display: flex;
		flex-direction: column;

		&.with-padding {
			padding: 0.5rem 0.75rem;
		}
	}
</style>
