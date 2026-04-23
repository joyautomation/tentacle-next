<script lang="ts">
  import { slide } from "svelte/transition";
  import type {
    PlcVariableConfig,
    PlcTaskConfig,
    PlcTemplate,
    ProgramListItem,
    TestListItem,
  } from "$lib/types/plc";
  import { workspaceSelection, workspaceTabs } from "../workspace-state.svelte";
  import { ChevronRight, Plus } from "@joyautomation/salt/icons";
  import { apiPut } from "$lib/api/client";
  import { invalidateAll } from "$app/navigation";
  import { state as saltState } from "@joyautomation/salt";

  type Props = {
    variables: Record<string, PlcVariableConfig>;
    tasks: Record<string, PlcTaskConfig>;
    templates: PlcTemplate[];
    programs: ProgramListItem[];
    tests: TestListItem[];
    onCreate?: (kind: "variable" | "task") => void;
    onRunAllTests?: () => void;
    testsRunning?: boolean;
  };

  let {
    variables,
    tasks,
    templates,
    programs,
    tests,
    onCreate,
    onRunAllTests,
    testsRunning,
  }: Props = $props();

  function newProgramTab() {
    // Functions are created in-editor: a blank tab opens, the user types
    // their def, and the program name is derived from the def header on
    // save. No modal.
    workspaceTabs.openNew("program", "starlark");
  }

  function newTestTab() {
    workspaceTabs.openNew("test", "starlark");
  }

  function newTypeTab() {
    // Types are created the same way programs are: a blank tab opens, the
    // user names the type in-editor, renameTab promotes the synthetic id on
    // first save.
    workspaceTabs.openNew("type");
  }

  let sections = $state({
    variables: true,
    types: true,
    tasks: true,
    programs: true,
    tests: true,
  });

  let filter = $state("");
  let activeTags = $state<string[]>([]);

  function matchesTags(itemTags: string[] | undefined): boolean {
    if (activeTags.length === 0) return true;
    const set = new Set(itemTags ?? []);
    return activeTags.every((t) => set.has(t));
  }

  function matchesFilter(name: string): boolean {
    return !filter || name.toLowerCase().includes(filter.toLowerCase());
  }

  const variableEntries = $derived(
    Object.entries(variables)
      .filter(([name]) => matchesFilter(name))
      .sort(([a], [b]) => a.localeCompare(b)),
  );

  const taskEntries = $derived(
    Object.values(tasks)
      .filter((t) => matchesFilter(t.name))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const typeEntries = $derived(
    templates
      .filter((t) => matchesFilter(t.name))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const programEntries = $derived(
    programs
      .filter((p) => matchesFilter(p.name) && matchesTags(p.tags))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const testEntries = $derived(
    tests
      .filter((t) => matchesFilter(t.name) && matchesTags(t.tags))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const allTags = $derived.by(() => {
    const s = new Set<string>();
    for (const p of programs) for (const t of p.tags ?? []) s.add(t);
    for (const t of tests) for (const tag of t.tags ?? []) s.add(tag);
    return Array.from(s).sort();
  });

  function toggleTag(tag: string) {
    activeTags = activeTags.includes(tag)
      ? activeTags.filter((t) => t !== tag)
      : [...activeTags, tag];
  }

  function testDotClass(t: TestListItem): string {
    const status = t.lastResult?.status;
    if (status === "pass") return "pass";
    if (status === "fail") return "fail";
    if (status === "error") return "error";
    return "unknown";
  }

  function toggle(key: keyof typeof sections) {
    sections[key] = !sections[key];
  }

  function languageLabel(lang: string): string {
    if (lang === "starlark") return "PY";
    if (lang === "st" || lang === "structured-text") return "ST";
    if (lang === "ladder") return "LD";
    return lang.slice(0, 2).toUpperCase();
  }

  const VARIABLE_MIME = "application/x-plc-variable";

  let togglingTask = $state<string | null>(null);

  async function toggleTaskEnabled(task: PlcTaskConfig, e: MouseEvent) {
    e.stopPropagation();
    if (togglingTask) return;
    togglingTask = task.name;
    try {
      const body: PlcTaskConfig = { ...task, enabled: !task.enabled };
      const res = await apiPut(
        `/plcs/plc/tasks/${encodeURIComponent(task.name)}`,
        body,
      );
      if (res.error) {
        saltState.addNotification({ message: res.error.error, type: "error" });
        return;
      }
      await invalidateAll();
    } finally {
      togglingTask = null;
    }
  }

  function onVariableDragStart(e: DragEvent, name: string, datatype: string) {
    if (!e.dataTransfer) return;
    const payload = JSON.stringify({ name, datatype });
    e.dataTransfer.setData(VARIABLE_MIME, payload);
    e.dataTransfer.setData("text/plain", name);
    e.dataTransfer.effectAllowed = "copy";
  }
</script>

<div class="navigator">
  <div class="filter-wrap">
    <input
      type="text"
      class="filter-input"
      placeholder="Filter…"
      bind:value={filter}
      aria-label="Filter navigator"
    />
    {#if allTags.length > 0}
      <div class="tag-filter" role="group" aria-label="Filter by tag">
        {#each allTags as tag (tag)}
          <button
            type="button"
            class="tag-chip"
            class:active={activeTags.includes(tag)}
            onclick={() => toggleTag(tag)}
          >
            {tag}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="sections">
    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("variables")}
          aria-expanded={sections.variables}
        >
          <span class="chevron" class:open={sections.variables}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Variables</span>
          <span class="count">{variableEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={() => onCreate?.("variable")}
          title="New variable"
          aria-label="New variable"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.variables}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each variableEntries as [name, cfg] (name)}
            <li>
              <button
                type="button"
                class="item draggable"
                class:selected={workspaceSelection.isSelected("variable", name)}
                onclick={() => workspaceSelection.select("variable", name)}
                draggable="true"
                ondragstart={(e) => onVariableDragStart(e, name, cfg.datatype)}
                title="{cfg.datatype} · drag into editor to insert"
              >
                <span class="grip" aria-hidden="true">⋮⋮</span>
                <span class="badge type">{cfg.datatype.slice(0, 4)}</span>
                <span class="name">{name}</span>
              </button>
            </li>
          {:else}
            <li class="empty">No variables</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("types")}
          aria-expanded={sections.types}
        >
          <span class="chevron" class:open={sections.types}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Types</span>
          <span class="count">{typeEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={newTypeTab}
          title="New type"
          aria-label="New type"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.types}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each typeEntries as tmpl (tmpl.name)}
            <li>
              <button
                type="button"
                class="item"
                class:selected={workspaceSelection.isSelected(
                  "type",
                  tmpl.name,
                )}
                onclick={() => workspaceSelection.select("type", tmpl.name)}
                title={tmpl.description ?? `${tmpl.fields.length} field(s)`}
              >
                <span class="badge type">TYPE</span>
                <span class="name">{tmpl.name}</span>
                <span class="meta">{tmpl.fields.length}</span>
              </button>
            </li>
          {:else}
            <li class="empty">No types</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("tasks")}
          aria-expanded={sections.tasks}
        >
          <span class="chevron" class:open={sections.tasks}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Tasks</span>
          <span class="count">{taskEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={() => onCreate?.("task")}
          title="New task"
          aria-label="New task"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.tasks}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each taskEntries as task (task.name)}
            <li class="task-row">
              <button
                type="button"
                class="item"
                class:selected={workspaceSelection.isSelected(
                  "task",
                  task.name,
                )}
                onclick={() => workspaceSelection.select("task", task.name)}
                title="{task.scanRateMs}ms · {task.programRef || 'no program'}"
              >
                <span class="badge rate">{task.scanRateMs}ms</span>
                <span class="name">{task.name}</span>
              </button>
              <button
                type="button"
                class="task-toggle"
                class:on={task.enabled}
                onclick={(e) => toggleTaskEnabled(task, e)}
                disabled={togglingTask === task.name}
                role="switch"
                aria-checked={task.enabled}
                aria-label={task.enabled
                  ? `Disable task ${task.name}`
                  : `Enable task ${task.name}`}
                title={task.enabled ? "Disable task" : "Enable task"}
              >
                <span class="task-toggle-thumb"></span>
              </button>
            </li>
          {:else}
            <li class="empty">No tasks</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("programs")}
          aria-expanded={sections.programs}
        >
          <span class="chevron" class:open={sections.programs}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Functions</span>
          <span class="count">{programEntries.length}</span>
        </button>
        <button
          type="button"
          class="add-btn"
          onclick={newProgramTab}
          title="New function"
          aria-label="New function"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.programs}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each programEntries as program (program.name)}
            <li>
              <button
                type="button"
                class="item"
                class:selected={workspaceSelection.isSelected(
                  "program",
                  program.name,
                )}
                onclick={() =>
                  workspaceSelection.select("program", program.name)}
                title={program.language}
              >
                <span class="badge lang">{languageLabel(program.language)}</span>
                <span class="name">{program.name}</span>
                {#if program.tags && program.tags.length > 0}
                  <span class="item-tags">
                    {#each program.tags as tag (tag)}
                      <span class="item-tag">{tag}</span>
                    {/each}
                  </span>
                {/if}
              </button>
            </li>
          {:else}
            <li class="empty">No functions</li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <div class="section-header-row">
        <button
          type="button"
          class="section-header"
          onclick={() => toggle("tests")}
          aria-expanded={sections.tests}
        >
          <span class="chevron" class:open={sections.tests}
            ><ChevronRight size="0.75rem" /></span
          >
          <span class="label">Tests</span>
          <span class="count">{testEntries.length}</span>
        </button>
        {#if tests.length > 0}
          <button
            type="button"
            class="add-btn"
            onclick={() => onRunAllTests?.()}
            disabled={testsRunning}
            title="Run all tests"
            aria-label="Run all tests"
          >
            <span class="play-icon" aria-hidden="true">▶</span>
          </button>
        {/if}
        <button
          type="button"
          class="add-btn"
          onclick={newTestTab}
          title="New test"
          aria-label="New test"
        >
          <Plus size="0.875rem" />
        </button>
      </div>
      {#if sections.tests}
        <ul class="items" transition:slide={{ duration: 150 }}>
          {#each testEntries as test (test.name)}
            <li>
              <button
                type="button"
                class="item"
                class:selected={workspaceSelection.isSelected(
                  "test",
                  test.name,
                )}
                onclick={() => workspaceSelection.select("test", test.name)}
                title={test.lastResult?.message ?? "never run"}
              >
                <span
                  class="status-dot"
                  class:pass={testDotClass(test) === "pass"}
                  class:fail={testDotClass(test) === "fail"}
                  class:error={testDotClass(test) === "error"}
                ></span>
                <span class="name">{test.name}</span>
                {#if test.tags && test.tags.length > 0}
                  <span class="item-tags">
                    {#each test.tags as tag (tag)}
                      <span class="item-tag">{tag}</span>
                    {/each}
                  </span>
                {/if}
                {#if test.lastResult}
                  <span class="meta">{test.lastResult.durationMs}ms</span>
                {/if}
              </button>
            </li>
          {:else}
            <li class="empty">No tests</li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
</div>

<style lang="scss">
  .navigator {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .filter-wrap {
    padding: 0.5rem 0.625rem;
    border-bottom: 1px solid var(--theme-border);
  }

  .filter-input {
    width: 100%;
    padding: 0.3125rem 0.5rem;
    font-size: 0.75rem;
    background: var(--theme-background);
    color: var(--theme-text);
    border: 1px solid var(--theme-border);
    border-radius: 0.25rem;

    &:focus {
      outline: none;
      border-color: var(--theme-primary);
    }
  }

  .tag-filter {
    display: flex;
    flex-wrap: wrap;
    gap: 0.1875rem;
    margin-top: 0.375rem;
  }

  .tag-chip {
    padding: 0.0625rem 0.375rem;
    font-family: var(--font-mono, monospace);
    font-size: 0.6875rem;
    color: var(--theme-text-muted);
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: 0.625rem;
    cursor: pointer;

    &:hover {
      color: var(--theme-text);
      background: var(--theme-surface);
    }

    &.active {
      color: var(--theme-primary);
      background: color-mix(in srgb, var(--theme-primary) 14%, transparent);
      border-color: color-mix(in srgb, var(--theme-primary) 40%, var(--theme-border));
    }
  }

  .item-tags {
    display: inline-flex;
    flex-shrink: 0;
    gap: 0.1875rem;
  }

  .item-tag {
    padding: 0 0.25rem;
    font-family: var(--font-mono, monospace);
    font-size: 0.625rem;
    color: var(--theme-text-muted);
    background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    border-radius: 0.1875rem;
  }

  .sections {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .section {
    border-bottom: 1px solid var(--theme-border);
  }

  .section-header-row {
    display: flex;
    align-items: stretch;
  }

  .section-header {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    flex: 1;
    min-width: 0;
    padding: 0.375rem 0.5rem;
    background: transparent;
    border: none;
    border-radius: 0;
    cursor: pointer;
    color: var(--theme-text);
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    text-align: left;

    &:hover {
      background: var(--theme-surface);
    }
  }

  .add-btn {
    aspect-ratio: 1;
    border-radius: 0;
    flex-shrink: 0;
    width: 1.75rem;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    line-height: 1;
    cursor: pointer;
    opacity: 0.7;
    transition:
      opacity 0.12s ease,
      color 0.12s ease,
      background 0.12s ease;

    &:hover {
      opacity: 1;
      color: var(--theme-text);
      background: var(--theme-surface);
    }

    &:focus-visible {
      opacity: 1;
      outline: 2px solid var(--theme-primary);
      outline-offset: -2px;
    }
  }

  .chevron {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--theme-text-muted);
    transition: transform 0.15s ease;

    &.open {
      transform: rotate(90deg);
    }
  }

  .label {
    flex: 1;
  }

  .count {
    padding: 0.0625rem 0.375rem;
    font-size: 0.6875rem;
    font-weight: 500;
    color: var(--theme-text-muted);
    background: var(--theme-surface);
    border-radius: 0.625rem;
  }

  .items {
    list-style: none;
    margin: 0;
    padding: 0 0 0.25rem 0;
  }

  .item {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    width: 100%;
    padding: 0.25rem 0.5rem 0.25rem 0.625rem;
    background: transparent;
    border: none;
    border-radius: 0;
    cursor: pointer;
    color: var(--theme-text);
    font-size: 0.8125rem;
    text-align: left;

    &:hover {
      background: var(--theme-surface);

      .grip {
        opacity: 0.5;
      }
    }

    &.selected {
      background: color-mix(in srgb, var(--theme-primary) 18%, transparent);
      color: var(--theme-text);
    }

    &.draggable {
      cursor: grab;

      &:active {
        cursor: grabbing;
      }
    }
  }

  .grip {
    width: 0.75rem;
    flex-shrink: 0;
    color: var(--theme-text-muted);
    font-size: 0.625rem;
    letter-spacing: -0.1em;
    opacity: 0;
    transition: opacity 0.12s ease;
  }

  .name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono, monospace);
  }

  .badge {
    flex-shrink: 0;
    padding: 0.0625rem 0.3125rem;
    font-size: 0.625rem;
    font-weight: 600;
    color: var(--theme-text-muted);
    background: var(--theme-surface);
    border-radius: 0.1875rem;
    text-transform: uppercase;
    font-family: var(--font-mono, monospace);
    min-width: 2.25rem;
    text-align: center;
  }

  .badge.lang {
    color: var(--theme-primary);
  }

  .meta {
    flex-shrink: 0;
    color: var(--theme-text-muted);
    font-size: 0.75rem;

    &.off {
      opacity: 0.4;
    }
  }

  .task-row {
    display: flex;
    align-items: center;

    .item {
      flex: 1;
      min-width: 0;
    }
  }

  .task-toggle {
    flex-shrink: 0;
    position: relative;
    width: 1.75rem;
    height: 0.875rem;
    margin-right: 0.5rem;
    padding: 0;
    background: var(--theme-border);
    border: 0;
    border-radius: 0.4375rem;
    cursor: pointer;
    transition: background 0.15s ease;

    &:hover:not(:disabled) {
      background: color-mix(in srgb, var(--theme-text-muted) 40%, var(--theme-border));
    }

    &.on {
      background: var(--theme-primary);

      &:hover:not(:disabled) {
        background: color-mix(in srgb, var(--theme-primary) 80%, black);
      }

      .task-toggle-thumb {
        transform: translateX(0.875rem);
        background: var(--theme-on-primary, white);
      }
    }

    &:disabled {
      opacity: 0.6;
      cursor: not-allowed;
    }

    &:focus-visible {
      outline: 2px solid var(--theme-primary);
      outline-offset: 2px;
    }
  }

  .task-toggle-thumb {
    position: absolute;
    top: 0.125rem;
    left: 0.125rem;
    width: 0.625rem;
    height: 0.625rem;
    background: var(--theme-text);
    border-radius: 50%;
    transition: transform 0.15s ease, background 0.15s ease;
  }

  .empty {
    padding: 0.375rem 1rem;
    color: var(--theme-text-muted);
    font-size: 0.75rem;
    font-style: italic;
  }

  .status-dot {
    flex-shrink: 0;
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 50%;
    background: var(--theme-border);

    &.pass {
      background: var(--theme-success, #10b981);
    }
    &.fail {
      background: var(--theme-danger, #ef4444);
    }
    &.error {
      background: var(--theme-warning, #f59e0b);
    }
  }

  .play-icon {
    font-size: 0.625rem;
    line-height: 1;
  }
</style>
