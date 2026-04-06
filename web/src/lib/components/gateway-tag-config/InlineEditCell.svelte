<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		editing,
		draft,
		step,
		min = '0',
		cssClass = '',
		onCommit,
		onCancel,
		children
	}: {
		editing: boolean;
		draft: string;
		step?: string;
		min?: string;
		cssClass?: string;
		onCommit: (value: number) => void;
		onCancel: () => void;
		children: Snippet;
	} = $props();

	function commit(el: HTMLInputElement) {
		const val = parseFloat(el.value);
		if (!isNaN(val) && val >= 0) {
			onCommit(val);
			onCancel();
		} else {
			onCancel();
		}
	}
</script>

{#if editing}
	<!-- svelte-ignore a11y_autofocus -->
	<input
		class="il-input {cssClass}"
		type="text"
		value={draft}
		{step}
		{min}
		autofocus
		onblur={(e) => commit(e.currentTarget)}
		onkeydown={(e) => {
			if (e.key === 'Enter' || e.key === 'Tab') {
				e.preventDefault();
				commit(e.currentTarget);
			} else if (e.key === 'Escape') {
				e.preventDefault();
				onCancel();
			}
		}}
	/>
{:else}
	{@render children()}
{/if}

<style lang="scss">
	@use './tag-table';
</style>
