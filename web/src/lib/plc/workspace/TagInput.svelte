<script lang="ts">
	import { XMark } from '@joyautomation/salt/icons';

	type Props = {
		value: string[];
		suggestions?: string[];
		placeholder?: string;
		onchange: (tags: string[]) => void;
	};

	let { value, suggestions = [], placeholder = 'add tag…', onchange }: Props = $props();

	let draft = $state('');
	let listId = `tag-suggestions-${Math.random().toString(36).slice(2, 8)}`;

	function normalize(raw: string): string {
		return raw.trim().toLowerCase().replace(/\s+/g, '-');
	}

	function commit() {
		const t = normalize(draft);
		draft = '';
		if (!t) return;
		if (value.includes(t)) return;
		onchange([...value, t]);
	}

	function remove(tag: string) {
		onchange(value.filter((t) => t !== tag));
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ',') {
			e.preventDefault();
			commit();
			return;
		}
		if (e.key === 'Backspace' && draft === '' && value.length > 0) {
			e.preventDefault();
			onchange(value.slice(0, -1));
		}
	}

	const suggestionList = $derived(suggestions.filter((s) => !value.includes(s)));
</script>

<div class="tag-input">
	{#each value as tag (tag)}
		<span class="chip">
			<span class="chip-label">{tag}</span>
			<button
				type="button"
				class="chip-remove"
				aria-label={`Remove tag ${tag}`}
				onclick={() => remove(tag)}
			>
				<XMark size="0.625rem" />
			</button>
		</span>
	{/each}
	<input
		type="text"
		class="chip-entry"
		bind:value={draft}
		onkeydown={onKey}
		onblur={commit}
		list={listId}
		{placeholder}
	/>
	{#if suggestionList.length > 0}
		<datalist id={listId}>
			{#each suggestionList as s (s)}
				<option value={s}></option>
			{/each}
		</datalist>
	{/if}
</div>

<style lang="scss">
	.tag-input {
		display: inline-flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.25rem;
		padding: 0.1875rem 0.3125rem;
		min-width: 10rem;
		background: var(--theme-background);
		border: 1px solid var(--theme-border);
		border-radius: 0.25rem;

		&:focus-within {
			border-color: var(--theme-primary);
		}
	}

	.chip {
		display: inline-flex;
		align-items: center;
		gap: 0.1875rem;
		padding: 0.0625rem 0.1875rem 0.0625rem 0.375rem;
		font-family: var(--font-mono, monospace);
		font-size: 0.6875rem;
		color: var(--theme-text);
		background: color-mix(in srgb, var(--theme-primary) 14%, transparent);
		border-radius: 0.1875rem;
	}

	.chip-remove {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 0.875rem;
		height: 0.875rem;
		padding: 0;
		color: var(--theme-text-muted);
		background: transparent;
		border: none;
		border-radius: 0.125rem;
		cursor: pointer;

		&:hover {
			color: var(--theme-text);
			background: color-mix(in srgb, var(--theme-text) 12%, transparent);
		}
	}

	.chip-entry {
		flex: 1;
		min-width: 4rem;
		padding: 0.0625rem 0.125rem;
		font-family: var(--font-mono, monospace);
		font-size: 0.75rem;
		color: var(--theme-text);
		background: transparent;
		border: none;
		outline: none;

		&::placeholder {
			color: var(--theme-text-muted);
		}
	}
</style>
