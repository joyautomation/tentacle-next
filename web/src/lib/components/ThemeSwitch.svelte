<script lang="ts">
	import { themeState, type Theme } from '../../routes/theme.svelte';

	const options: { value: Theme; label: string; icon: string }[] = [
		{ value: 'themeSystem', label: 'System', icon: 'computer' },
		{ value: 'themeLight', label: 'Light', icon: 'sun' },
		{ value: 'themeDark', label: 'Dark', icon: 'moon' }
	];

	function handleChange(newTheme: Theme) {
		themeState.set(newTheme);
	}
</script>

<div class="theme-switch">
	{#each options as option}
		<button
			type="button"
			class="theme-option"
			class:active={themeState.value === option.value}
			onclick={() => handleChange(option.value)}
			title={option.label}
		>
			{#if option.icon === 'computer'}
				<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<rect x="2" y="3" width="20" height="14" rx="2" />
					<line x1="8" y1="21" x2="16" y2="21" />
					<line x1="12" y1="17" x2="12" y2="21" />
				</svg>
			{:else if option.icon === 'sun'}
				<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<circle cx="12" cy="12" r="4" />
					<path d="M12 2v2" />
					<path d="M12 20v2" />
					<path d="m4.93 4.93 1.41 1.41" />
					<path d="m17.66 17.66 1.41 1.41" />
					<path d="M2 12h2" />
					<path d="M20 12h2" />
					<path d="m6.34 17.66-1.41 1.41" />
					<path d="m19.07 4.93-1.41 1.41" />
				</svg>
			{:else if option.icon === 'moon'}
				<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
				</svg>
			{/if}
		</button>
	{/each}
</div>

<style lang="scss">
	.theme-switch {
		display: flex;
		gap: 2px;
		background: var(--theme-neutral-200);
		border-radius: 8px;
		padding: 3px;
	}

	.theme-option {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 44px;
		height: 44px;
		border: none;
		border-radius: 6px;
		background: transparent;
		color: var(--theme-neutral-500);
		cursor: pointer;
		transition: all 0.15s ease;
		padding: 0;

		&:hover {
			color: var(--theme-neutral-700);
			background: var(--theme-neutral-300);
		}

		&.active {
			background: var(--theme-neutral-100);
			color: var(--theme-primary);
			box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
		}

		svg {
			flex-shrink: 0;
		}
	}
</style>
