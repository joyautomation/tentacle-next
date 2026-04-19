<script lang="ts">
	import LogViewer from '$lib/components/LogViewer.svelte';
	import ProblemsView from '$lib/plc/workspace/ProblemsView.svelte';
	import { workspaceDiagnostics } from '$lib/plc/workspace-state.svelte';

	type Props = {
		serviceType: string;
		initialLogs: import('svelte').ComponentProps<typeof LogViewer>['initialLogs'];
	};

	let { serviceType, initialLogs }: Props = $props();

	type TabId = 'problems' | 'logs';
	let activeTab = $state<TabId>('problems');

	const problemCount = $derived(workspaceDiagnostics.total);
	const errorCount = $derived(workspaceDiagnostics.errorCount);
</script>

<section class="panel">
	<header class="panel-header">
		<div class="tabs" role="tablist">
			<button
				type="button"
				role="tab"
				aria-selected={activeTab === 'problems'}
				class="tab"
				class:active={activeTab === 'problems'}
				onclick={() => (activeTab = 'problems')}
			>
				Problems
				{#if problemCount > 0}
					<span class="badge" class:error={errorCount > 0}>{problemCount}</span>
				{/if}
			</button>
			<button
				type="button"
				role="tab"
				aria-selected={activeTab === 'logs'}
				class="tab"
				class:active={activeTab === 'logs'}
				onclick={() => (activeTab = 'logs')}
			>
				Logs
			</button>
		</div>
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
		padding: 0;
		background: var(--theme-surface);
		border-bottom: 1px solid var(--theme-border);
	}

	.tabs {
		display: flex;
		gap: 0;
	}

	.tab {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.5rem 0.875rem;
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--theme-text-muted);
		background: transparent;
		border: 0;
		border-bottom: 2px solid transparent;
		cursor: pointer;
		transition: color 0.12s ease, border-color 0.12s ease;

		&:hover {
			color: var(--theme-text);
		}

		&.active {
			color: var(--theme-primary);
			border-bottom-color: var(--theme-primary);
		}
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

		&.error {
			color: white;
			background: var(--theme-danger, #e14545);
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
