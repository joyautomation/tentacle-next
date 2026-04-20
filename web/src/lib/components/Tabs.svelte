<script lang="ts" module>
	export type TabItem = {
		id: string;
		label?: string;
		href?: string;
		disabled?: boolean;
	};
</script>

<script lang="ts" generics="T extends TabItem">
	import type { Snippet } from 'svelte';

	type Props = {
		tabs: T[];
		active?: string | null;
		onChange?: (id: string) => void;
		position?: 'top' | 'bottom';
		ariaLabel?: string;
		size?: 'sm' | 'md';
		tab?: Snippet<[{ tab: T; active: boolean }]>;
		trailing?: Snippet;
	};

	let {
		tabs,
		active,
		onChange,
		position = 'top',
		ariaLabel,
		size = 'md',
		tab: tabSnippet,
		trailing
	}: Props = $props();

	function handleClick(t: T) {
		if (t.disabled) return;
		onChange?.(t.id);
	}
</script>

<div class="tabs-root" class:bottom={position === 'bottom'} class:sm={size === 'sm'}>
	<div class="tab-strip" role="tablist" aria-label={ariaLabel}>
		{#each tabs as t (t.id)}
			{@const isActive = active === t.id}
			{#if t.href}
				<a
					class="tab"
					class:active={isActive}
					class:disabled={t.disabled}
					href={t.href}
					role="tab"
					aria-selected={isActive}
					tabindex={t.disabled ? -1 : 0}
				>
					{#if tabSnippet}
						{@render tabSnippet({ tab: t, active: isActive })}
					{:else}
						{t.label ?? t.id}
					{/if}
				</a>
			{:else}
				<button
					class="tab"
					class:active={isActive}
					type="button"
					role="tab"
					aria-selected={isActive}
					disabled={t.disabled}
					onclick={() => handleClick(t)}
				>
					{#if tabSnippet}
						{@render tabSnippet({ tab: t, active: isActive })}
					{:else}
						{t.label ?? t.id}
					{/if}
				</button>
			{/if}
		{/each}
		{#if trailing}
			<div class="trailing">{@render trailing()}</div>
		{/if}
	</div>
</div>

<style lang="scss">
	.tabs-root {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}

	.tab-strip {
		display: flex;
		align-items: stretch;
		gap: 0;
		overflow-x: auto;
		-webkit-overflow-scrolling: touch;
		border-bottom: 1px solid var(--theme-border);
		background: var(--theme-surface);
	}

	.bottom .tab-strip {
		border-bottom: 0;
		border-top: 1px solid var(--theme-border);
	}

	.tab {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.75rem 1.125rem;
		font-size: 0.875rem;
		font-weight: 500;
		line-height: 1;
		color: var(--theme-text-muted);
		background: transparent;
		border: 0;
		border-radius: 0;
		text-decoration: none;
		white-space: nowrap;
		flex-shrink: 0;
		cursor: pointer;
		transition: color 0.12s ease, border-color 0.12s ease, background 0.12s ease;

		/* indicator line (default: below the tab) */
		border-bottom: 2px solid transparent;
		margin-bottom: -1px;

		&:hover:not(.disabled):not(:disabled) {
			color: var(--theme-text);
		}

		&.active {
			color: var(--theme-primary);
			border-bottom-color: var(--theme-primary);
		}

		&.disabled,
		&:disabled {
			opacity: 0.5;
			cursor: not-allowed;
		}
	}

	/* indicator flipped to top when tabs sit below content */
	.bottom .tab {
		border-bottom: 0;
		border-top: 2px solid transparent;
		margin-bottom: 0;
		margin-top: -1px;

		&.active {
			border-top-color: var(--theme-primary);
		}
	}

	.sm .tab {
		padding: 0.5rem 0.875rem;
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.trailing {
		display: flex;
		align-items: center;
		margin-left: auto;
		padding: 0 0.5rem;
	}
</style>
