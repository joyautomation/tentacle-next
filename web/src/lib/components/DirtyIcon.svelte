<script lang="ts">
	import { PencilSquare } from '@joyautomation/salt/icons';
	import { slide } from 'svelte/transition';

	type Props = {
		title?: string;
		size?: string;
		slideIn?: boolean;
		inline?: boolean;
	};

	let {
		title = 'Unsaved changes',
		size = '1rem',
		slideIn = false,
		inline = false
	}: Props = $props();
</script>

{#if slideIn}
	<span
		class="dirty-icon"
		class:inline
		{title}
		transition:slide|local={{ axis: 'x', duration: 150 }}
	>
		<PencilSquare {size} />
	</span>
{:else}
	<span class="dirty-icon" class:inline {title}>
		<PencilSquare {size} />
	</span>
{/if}

<style>
	.dirty-icon {
		display: inline-flex;
		align-items: center;
		flex-shrink: 0;
		color: var(--badge-amber-text, var(--theme-warning, #f59e0b));
		vertical-align: middle;
	}

	.dirty-icon.inline {
		margin-right: 0.375rem;
	}

	.dirty-icon :global(svg) {
		flex-shrink: 0;
	}
</style>
