<script lang="ts">
	import {
		workspaceDiagnostics,
		workspaceTabs,
		workspaceSelection,
		type WorkspaceDiagnostic
	} from '$lib/plc/workspace-state.svelte';
	import {
		XCircle,
		ExclamationTriangle,
		InformationCircle
	} from '@joyautomation/salt/icons';

	type Row = {
		uri: string;
		programName: string | null;
		diag: WorkspaceDiagnostic;
	};

	// URI shape: tentacle-plc://programs/<urlencoded-name>.<ext>
	function programNameFromUri(uri: string): string | null {
		const prefix = 'tentacle-plc://programs/';
		if (!uri.startsWith(prefix)) return null;
		const rest = uri.slice(prefix.length);
		const dot = rest.lastIndexOf('.');
		const raw = dot >= 0 ? rest.slice(0, dot) : rest;
		try {
			return decodeURIComponent(raw);
		} catch {
			return raw;
		}
	}

	const severityOrder = { error: 0, warning: 1, info: 2, hint: 3 } as const;

	const rows = $derived.by<Row[]>(() => {
		const out: Row[] = [];
		for (const uri in workspaceDiagnostics.byUri) {
			const name = programNameFromUri(uri);
			for (const d of workspaceDiagnostics.byUri[uri]) {
				out.push({ uri, programName: name, diag: d });
			}
		}
		out.sort((a, b) => {
			const s = severityOrder[a.diag.severity] - severityOrder[b.diag.severity];
			if (s !== 0) return s;
			const n = (a.programName ?? '').localeCompare(b.programName ?? '');
			if (n !== 0) return n;
			return a.diag.startLine - b.diag.startLine;
		});
		return out;
	});

	function gotoRow(row: Row) {
		if (!row.programName) return;
		workspaceSelection.select('program', row.programName);
		workspaceTabs.activate(row.programName);
	}

</script>

<div class="problems">
	{#if rows.length === 0}
		<div class="empty">No problems detected.</div>
	{:else}
		<ul class="rows">
			{#each rows as row (row.uri + ':' + row.diag.startLine + ':' + row.diag.startCol + ':' + row.diag.message)}
				<li>
					<button type="button" class="row" onclick={() => gotoRow(row)}>
						<span class="icon" class:error={row.diag.severity === 'error'} class:warning={row.diag.severity === 'warning'}>
							{#if row.diag.severity === 'error'}
								<XCircle size="1rem" />
							{:else if row.diag.severity === 'warning'}
								<ExclamationTriangle size="1rem" />
							{:else}
								<InformationCircle size="1rem" />
							{/if}
						</span>
						<span class="message">{row.diag.message}</span>
						<span class="location">
							<span class="file">{row.programName ?? row.uri}</span>
							<span class="pos">[Ln {row.diag.startLine + 1}, Col {row.diag.startCol + 1}]</span>
						</span>
					</button>
				</li>
			{/each}
		</ul>
	{/if}
</div>

<style lang="scss">
	.problems {
		height: 100%;
		min-height: 0;
		overflow: auto;
		font-size: 0.8125rem;
	}

	.empty {
		padding: 1rem;
		color: var(--theme-text-muted);
		font-style: italic;
	}

	.rows {
		list-style: none;
		padding: 0;
		margin: 0;
	}

	.row {
		display: flex;
		align-items: baseline;
		gap: 0.5rem;
		width: 100%;
		padding: 0.375rem 0.75rem;
		background: transparent;
		border: 0;
		border-bottom: 1px solid var(--theme-border);
		text-align: left;
		cursor: pointer;
		color: var(--theme-text);
		font-family: inherit;
		font-size: inherit;

		&:hover {
			background: color-mix(in srgb, var(--theme-primary) 8%, transparent);
		}
	}

	.icon {
		display: inline-flex;
		align-items: center;
		flex-shrink: 0;
		color: var(--theme-text-muted);

		&.error {
			color: var(--theme-danger, #e14545);
		}
		&.warning {
			color: var(--theme-warning, #d2a140);
		}
	}

	.message {
		flex: 1;
		word-break: break-word;
	}

	.location {
		flex-shrink: 0;
		color: var(--theme-text-muted);
		font-family: var(--font-mono, monospace);
		font-size: 0.75rem;
	}

	.file {
		margin-right: 0.375rem;
	}
</style>
