<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { subscribe } from '$lib/api/subscribe';

  interface TrafficEntry {
    timestamp: string;
    subject: string;
    size: number;
    payload: string;
  }

  interface Props {
    initialTraffic: TrafficEntry[];
  }

  let { initialTraffic }: Props = $props();

  const MAX_LINES = 500;
  const DEFAULT_FILTER = '-*.data.*'; // exclude high-volume data messages by default

  let entries = $state<TrafficEntry[]>([]);
  let filter = $state(DEFAULT_FILTER);
  let appliedFilter = $state(DEFAULT_FILTER);
  let paused = $state(false);
  let selectedIndex = $state<number | null>(null);
  let unsubscribe: (() => void) | null = null;

  // Batch buffer for throttling DOM updates
  let pendingEntries: TrafficEntry[] = [];
  let batchTimer: ReturnType<typeof setTimeout> | null = null;

  function flushBatch() {
    if (pendingEntries.length === 0) return;
    entries = [...pendingEntries.reverse(), ...entries].slice(0, MAX_LINES);
    pendingEntries = [];
    batchTimer = null;
  }

  function addEntry(entry: TrafficEntry) {
    if (paused) return;
    pendingEntries.push(entry);
    if (!batchTimer) {
      batchTimer = setTimeout(flushBatch, 500);
    }
  }

  // Glob-style filter: * and > both mean "any characters", everything else is literal.
  // Prefix with - to negate (exclude matches). Multiple patterns separated by spaces.
  function matchesFilter(subject: string, pattern: string): boolean {
    const parts = pattern.trim().split(/\s+/);
    for (const part of parts) {
      const negate = part.startsWith('-');
      const p = negate ? part.slice(1) : part;
      if (!p) continue;

      let matches: boolean;
      if (!p.includes('*') && !p.includes('>')) {
        matches = subject.toLowerCase().includes(p.toLowerCase());
      } else {
        const escaped = p.replace(/[.+?^${}()|[\]\\]/g, '\\$&');
        const regexStr = escaped.replace(/[*>]/g, '.*');
        matches = new RegExp(regexStr, 'i').test(subject);
      }

      if (negate && matches) return false;
      if (!negate && !matches) return false;
    }
    return true;
  }

  let filteredEntries = $derived(
    appliedFilter
      ? entries.filter((e) => matchesFilter(e.subject, appliedFilter))
      : entries
  );

  // Initialize from SSR data (reverse to show newest first)
  $effect(() => {
    if (initialTraffic.length > 0 && entries.length === 0) {
      entries = [...initialTraffic].reverse();
    }
  });

  function formatTime(ts: string | number): string {
    const d = typeof ts === 'number' || /^\d+$/.test(String(ts))
      ? new Date(Number(ts))
      : new Date(ts);
    if (isNaN(d.getTime())) return String(ts);
    const h = String(d.getHours()).padStart(2, '0');
    const m = String(d.getMinutes()).padStart(2, '0');
    const s = String(d.getSeconds()).padStart(2, '0');
    const ms = String(d.getMilliseconds()).padStart(3, '0');
    return `${h}:${m}:${s}.${ms}`;
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    return `${(bytes / 1024).toFixed(1)} KB`;
  }

  function subjectCategory(subject: string): string {
    if (subject.includes('.data.')) return 'data';
    if (subject.includes('.command.')) return 'command';
    if (subject.startsWith('service.logs.')) return 'logs';
    if (subject.includes('.shutdown')) return 'shutdown';
    if (subject.includes('.status')) return 'status';
    if (subject.includes('.variables')) return 'variables';
    if (subject.includes('.browse')) return 'browse';
    if (subject.includes('.subscribe') || subject.includes('.unsubscribe')) return 'subscribe';
    return 'other';
  }

  function categoryColor(category: string): string {
    switch (category) {
      case 'data': return 'var(--color-cyan-500, #06b6d4)';
      case 'command': return 'var(--color-yellow-500, #eab308)';
      case 'logs': return 'var(--color-purple-500, #a855f7)';
      case 'shutdown': return 'var(--color-red-500, #ef4444)';
      case 'status': return 'var(--color-green-500, #22c55e)';
      case 'variables': return 'var(--color-sky-500, #0ea5e9)';
      case 'browse': return 'var(--color-orange-500, #f97316)';
      case 'subscribe': return 'var(--color-teal-500, #14b8a6)';
      default: return 'var(--theme-text-muted)';
    }
  }

  function applyFilter() {
    appliedFilter = filter.trim();
    reconnectSubscription();
  }

  function handleFilterKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      applyFilter();
    }
  }

  function clearFilter() {
    filter = '';
    appliedFilter = '';
    reconnectSubscription();
  }

  function togglePause() {
    paused = !paused;
  }

  function toggleRow(index: number) {
    selectedIndex = selectedIndex === index ? null : index;
  }

  function formatPayload(payload: string): string {
    try {
      const parsed = JSON.parse(payload);
      return JSON.stringify(parsed, null, 2);
    } catch {
      return payload;
    }
  }

  function truncatePayload(payload: string, max = 80): string {
    if (payload.length <= max) return payload;
    return payload.slice(0, max) + '...';
  }

  function startSubscription() {
    const filterParam = appliedFilter ? `?filter=${encodeURIComponent(appliedFilter)}` : '';

    unsubscribe = subscribe<TrafficEntry>(
      `/nats/traffic/stream${filterParam}`,
      (data) => {
        addEntry(data);
      }
    );
  }

  function reconnectSubscription() {
    unsubscribe?.();
    startSubscription();
  }

  onMount(() => {
    startSubscription();
  });

  onDestroy(() => {
    unsubscribe?.();
    if (batchTimer) clearTimeout(batchTimer);
  });
</script>

<div class="traffic-viewer">
  <div class="traffic-toolbar">
    <div class="filter-group">
      <input
        type="text"
        class="filter-input"
        placeholder="Filter (e.g. ethernetip, service.logs.>, *.data.*)"
        bind:value={filter}
        onkeydown={handleFilterKeydown}
      />
      <button class="filter-btn" onclick={applyFilter}>Filter</button>
      {#if appliedFilter}
        <button class="clear-btn" onclick={clearFilter} title="Clear filter">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M18 6L6 18M6 6l12 12"/>
          </svg>
        </button>
      {/if}
    </div>
    <div class="toolbar-right">
      <span class="entry-count">{appliedFilter ? `${filteredEntries.length} / ${entries.length}` : entries.length} messages</span>
      <button class="pause-btn" class:active={paused} onclick={togglePause} title={paused ? 'Resume' : 'Pause'}>
        {#if paused}
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polygon points="5 3 19 12 5 21 5 3"/>
          </svg>
          Resume
        {:else}
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="6" y="4" width="4" height="16"/>
            <rect x="14" y="4" width="4" height="16"/>
          </svg>
          Pause
        {/if}
      </button>
    </div>
  </div>

  <div class="traffic-output">
    {#if filteredEntries.length === 0}
      <div class="empty-state">
        <p>{appliedFilter ? 'No messages match the current filter.' : 'No NATS traffic captured yet. Messages will appear here in real-time.'}</p>
      </div>
    {:else}
      {#each filteredEntries as entry, i}
        {@const category = subjectCategory(entry.subject)}
        <button
          class="traffic-row"
          class:selected={selectedIndex === i}
          onclick={() => toggleRow(i)}
          type="button"
        >
          <span class="traffic-time">{formatTime(entry.timestamp)}</span>
          <span class="traffic-subject" style="color: {categoryColor(category)}">{entry.subject}</span>
          <span class="traffic-size">{formatSize(entry.size)}</span>
          <span class="traffic-preview">{truncatePayload(entry.payload)}</span>
        </button>
        {#if selectedIndex === i}
          <div class="traffic-detail">
            <pre>{formatPayload(entry.payload)}</pre>
          </div>
        {/if}
      {/each}
    {/if}
  </div>
</div>

<style lang="scss">
  .traffic-viewer {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
  }

  .traffic-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.5rem 0.75rem;
    background: var(--theme-surface);
    border-bottom: 1px solid var(--theme-border);
    flex-wrap: wrap;
  }

  .filter-group {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    flex: 1;
    min-width: 200px;
  }

  .filter-input {
    flex: 1;
    max-width: 360px;
    padding: 0.375rem 0.625rem;
    font-size: 0.75rem;
    font-family: 'IBM Plex Mono', monospace;
    background: var(--theme-background);
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text);
    outline: none;

    &:focus {
      border-color: var(--theme-primary);
    }

    &::placeholder {
      color: var(--theme-text-muted);
    }
  }

  .filter-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--theme-primary);
    background: transparent;
    border: 1px solid var(--theme-primary);
    border-radius: var(--rounded-md);
    cursor: pointer;

    &:hover {
      background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    }
  }

  .clear-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text-muted);
    cursor: pointer;

    &:hover {
      color: var(--color-red-500, #ef4444);
      border-color: var(--color-red-500, #ef4444);
    }
  }

  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .entry-count {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
  }

  .pause-btn, .scroll-btn {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.25rem 0.5rem;
    font-size: 0.75rem;
    font-weight: 500;
    background: transparent;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    color: var(--theme-text-muted);
    cursor: pointer;

    &:hover {
      color: var(--theme-text);
      border-color: var(--theme-text-muted);
    }
  }

  .pause-btn.active {
    color: var(--color-yellow-500, #eab308);
    border-color: var(--color-yellow-500, #eab308);
  }

  .traffic-output {
    flex: 1;
    overflow-y: auto;
    background: color-mix(in srgb, var(--theme-background) 80%, #000);
    padding: 0.25rem;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    line-height: 1.5;
    min-height: 400px;
  }

  .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    min-height: 200px;

    p {
      color: var(--theme-text-muted);
      font-family: 'Space Grotesk', sans-serif;
      font-size: 0.875rem;
    }
  }

  .traffic-row {
    display: flex;
    gap: 0.75rem;
    padding: 0.1875rem 0.375rem;
    white-space: nowrap;
    cursor: pointer;
    border: none;
    background: transparent;
    width: 100%;
    text-align: left;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    line-height: 1.5;
    color: var(--theme-text);

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 5%, transparent);
    }

    &.selected {
      background: color-mix(in srgb, var(--theme-primary) 10%, transparent);
    }
  }

  .traffic-time {
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .traffic-subject {
    flex-shrink: 0;
    font-weight: 600;
    min-width: 12rem;
    max-width: 24rem;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .traffic-size {
    color: var(--theme-text-muted);
    flex-shrink: 0;
    width: 4.5rem;
    text-align: right;
  }

  .traffic-preview {
    color: var(--theme-text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    opacity: 0.7;
  }

  .traffic-detail {
    padding: 0.5rem 0.75rem;
    margin: 0 0.375rem 0.25rem;
    background: color-mix(in srgb, var(--theme-surface) 60%, var(--theme-background));
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-md);
    overflow-x: auto;

    pre {
      margin: 0;
      font-size: 0.6875rem;
      line-height: 1.6;
      color: var(--theme-text);
      white-space: pre-wrap;
      word-break: break-all;
    }
  }
</style>
