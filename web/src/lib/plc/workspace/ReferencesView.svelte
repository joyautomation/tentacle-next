<script lang="ts">
	import {
		workspaceReferences,
		workspaceSelection,
		workspaceTabs,
		workspaceEditorGotos,
		tabId,
		type ReferenceSite
	} from '$lib/plc/workspace-state.svelte';
	import { Link, DocumentText } from '@joyautomation/salt/icons';

	const query = $derived(workspaceReferences.current);

	// Group program-source sites by program so the panel reads like
	// VSCode's references — "N hits in foo.star", "M hits in bar.star".
	type ProgramGroup = { program: string; sites: ReferenceSite[] };

	const groups = $derived.by<ProgramGroup[]>(() => {
		if (!query) return [];
		const byProg = new Map<string, ReferenceSite[]>();
		for (const site of query.sites) {
			if (site.source !== 'program' || !site.program) continue;
			const arr = byProg.get(site.program) ?? [];
			arr.push(site);
			byProg.set(site.program, arr);
		}
		return Array.from(byProg.entries())
			.map(([program, sites]) => ({
				program,
				sites: sites.slice().sort((a, b) => (a.line ?? 0) - (b.line ?? 0))
			}))
			.sort((a, b) => a.program.localeCompare(b.program));
	});

	const taskSites = $derived.by<ReferenceSite[]>(() => {
		if (!query) return [];
		return query.sites.filter((s) => s.source === 'taskProgramRef');
	});

	const totalCount = $derived(query?.sites.length ?? 0);

	function gotoSite(site: ReferenceSite) {
		if (site.source === 'taskProgramRef' && site.task) {
			workspaceSelection.select('task', site.task);
			workspaceTabs.open({ name: site.task, kind: 'task' });
			return;
		}
		if (site.source !== 'program' || !site.program) return;
		workspaceSelection.select('program', site.program);
		const id = tabId('program', site.program);
		workspaceTabs.activate(id);
		// The editor may not be mounted yet (tab just opened). Retry a few
		// animation frames while CodeEditor wires up its view; once
		// registered the handler lives for the tab's lifetime so the jump
		// lands in the right place without a manual scroll.
		if (site.line) {
			tryGoto(id, site.line, site.startCol ?? 1, 0);
		}
	}

	function tryGoto(id: string, line: number, col: number, attempt: number) {
		if (workspaceEditorGotos.invoke(id, line, col)) return;
		if (attempt >= 20) return; // ~300ms ceiling
		requestAnimationFrame(() => tryGoto(id, line, col, attempt + 1));
	}

	// Render a source line with the match span highlighted. The scanner
	// returns byte columns, which for ASCII/ST/Starlark identifiers is
	// equivalent to character columns in practice.
	function splitHighlight(text: string, startCol: number, endCol: number): [string, string, string] {
		if (!text) return ['', '', ''];
		const s = Math.max(0, startCol - 1);
		const e = Math.max(s, endCol - 1);
		return [text.slice(0, s), text.slice(s, e), text.slice(e)];
	}
</script>

<div class="refs">
	{#if !query}
		<div class="empty">
			<div class="hint">
				Select a function or variable in the Inspector and click "Find references" to populate this tab.
			</div>
		</div>
	{:else if query.loading}
		<div class="status-row">Searching for references to <code>{query.name}</code>…</div>
	{:else if query.error}
		<div class="status-row error">Error: {query.error}</div>
	{:else if totalCount === 0}
		<div class="status-row muted">
			No references to <code>{query.name}</code> found.
		</div>
	{:else}
		<div class="summary">
			<Link size="0.875rem" />
			<span>
				<strong>{totalCount}</strong>
				{totalCount === 1 ? 'reference' : 'references'} to
				<code>{query.name}</code>
				<span class="kind-tag">{query.kind}</span>
			</span>
		</div>
		{#each groups as group (group.program)}
			<div class="group">
				<div class="group-head">
					<DocumentText size="0.875rem" />
					<span class="group-name">{group.program}</span>
					<span class="group-count">{group.sites.length}</span>
				</div>
				<ul class="rows">
					{#each group.sites as site (site.line + ':' + site.startCol)}
						{@const parts = splitHighlight(site.lineText ?? '', site.startCol ?? 1, site.endCol ?? 1)}
						<li>
							<button type="button" class="row" onclick={() => gotoSite(site)}>
								<span class="pos">[Ln {site.line}, Col {site.startCol}]</span>
								<span class="line-text">
									<span class="pre">{parts[0]}</span><span class="hit">{parts[1]}</span><span class="post">{parts[2]}</span>
								</span>
							</button>
						</li>
					{/each}
				</ul>
			</div>
		{/each}
		{#if taskSites.length > 0}
			<div class="group">
				<div class="group-head">
					<span class="group-name">Tasks</span>
					<span class="group-count">{taskSites.length}</span>
				</div>
				<ul class="rows">
					{#each taskSites as site (site.task)}
						<li>
							<button type="button" class="row" onclick={() => gotoSite(site)}>
								<span class="pos">programRef</span>
								<span class="line-text"><code>{site.task}</code></span>
							</button>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	{/if}
</div>

<style lang="scss">
	.refs {
		height: 100%;
		min-height: 0;
		overflow: auto;
		font-size: 0.8125rem;
	}

	.empty {
		padding: 0.625rem 0.875rem;
		color: var(--theme-text-muted);
	}

	.status-row {
		padding: 0.625rem 0.875rem;
		color: var(--theme-text-muted);

		&.error {
			color: var(--theme-danger, #c33);
		}

		&.muted {
			font-style: italic;
		}

		code {
			font-family: var(--font-mono, monospace);
			color: var(--theme-text);
			background: color-mix(in srgb, var(--theme-text) 8%, transparent);
			padding: 0 0.25rem;
			border-radius: 0.1875rem;
		}
	}

	.summary {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.5rem 0.875rem;
		border-bottom: 1px solid var(--theme-border);
		color: var(--theme-text);
		background: var(--theme-surface);

		code {
			font-family: var(--font-mono, monospace);
			color: var(--theme-primary);
		}

		strong {
			color: var(--theme-text);
		}
	}

	.kind-tag {
		margin-left: 0.375rem;
		padding: 0.0625rem 0.375rem;
		border-radius: 0.625rem;
		background: color-mix(in srgb, var(--theme-primary) 12%, transparent);
		color: var(--theme-primary);
		font-size: 0.6875rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.group-head {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.375rem 0.875rem;
		color: var(--theme-text-muted);
		background: color-mix(in srgb, var(--theme-surface) 60%, transparent);
		font-size: 0.75rem;
		font-weight: 600;
	}

	.group-name {
		color: var(--theme-text);
		font-family: var(--font-mono, monospace);
	}

	.group-count {
		margin-left: auto;
		padding: 0 0.375rem;
		background: var(--theme-border);
		border-radius: 0.625rem;
		font-size: 0.6875rem;
	}

	.rows {
		list-style: none;
		padding: 0;
		margin: 0;
	}

	.row {
		display: flex;
		align-items: baseline;
		gap: 0.625rem;
		width: 100%;
		padding: 0.3125rem 0.875rem 0.3125rem 1.75rem;
		background: transparent;
		border: 0;
		border-bottom: 1px solid var(--theme-border);
		text-align: left;
		cursor: pointer;
		color: var(--theme-text);
		font-family: var(--font-mono, monospace);
		font-size: 0.8125rem;

		&:hover {
			background: color-mix(in srgb, var(--theme-primary) 8%, transparent);
		}
	}

	.pos {
		flex-shrink: 0;
		min-width: 7rem;
		color: var(--theme-text-muted);
		font-size: 0.75rem;
	}

	.line-text {
		flex: 1;
		white-space: pre;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.pre,
	.post {
		color: var(--theme-text-muted);
	}

	.hit {
		color: var(--theme-primary);
		background: color-mix(in srgb, var(--theme-primary) 18%, transparent);
		border-radius: 0.125rem;
		padding: 0 0.0625rem;
	}
</style>
