<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { subscribe } from '$lib/api/subscribe';
  import { api, apiPost } from '$lib/api/client';
  import { state as saltState } from '@joyautomation/salt';

  interface LogEntry {
    timestamp: string;
    level: string;
    message: string;
    serviceType: string;
    moduleId: string;
    logger: string | null;
    _id?: number;
  }

  interface Props {
    serviceType: string;
    initialLogs: LogEntry[];
  }

  let { serviceType, initialLogs }: Props = $props();

  const MAX_LINES = 500;

  let logs = $state<LogEntry[]>([]);
  let logIdCounter = 0;

  // Initialize from SSR data (reverse to show newest first)
  $effect(() => {
    if (initialLogs.length > 0 && logs.length === 0) {
      logs = [...initialLogs].reverse().map(l => ({ ...l, _id: logIdCounter++ }));
    }
  });
  let showDebug = $state(false);
  let showInfo = $state(true);
  let showWarn = $state(true);
  let showError = $state(true);

  let unsubscribe: (() => void) | null = null;

  const filteredLogs = $derived(
    logs.filter(log => {
      if (log.level === 'debug' && !showDebug) return false;
      if (log.level === 'info' && !showInfo) return false;
      if (log.level === 'warn' && !showWarn) return false;
      if (log.level === 'error' && !showError) return false;
      return true;
    })
  );

  function tsToMs(ts: string | number): number {
    if (typeof ts === 'number') return ts;
    if (/^\d+$/.test(ts)) return Number(ts);
    const t = new Date(ts).getTime();
    return isNaN(t) ? 0 : t;
  }

  function formatTime(ts: string | number): string {
    const t = tsToMs(ts);
    if (!t) return String(ts);
    const d = new Date(t);
    const h = String(d.getHours()).padStart(2, '0');
    const m = String(d.getMinutes()).padStart(2, '0');
    const s = String(d.getSeconds()).padStart(2, '0');
    const ms = String(d.getMilliseconds()).padStart(3, '0');
    return `${h}:${m}:${s}.${ms}`;
  }

  function levelColor(level: string): string {
    switch (level) {
      case 'error': return 'var(--color-red-500, #ef4444)';
      case 'warn': return 'var(--color-yellow-500, #eab308)';
      case 'info': return 'var(--color-sky-500, #0ea5e9)';
      case 'debug': return 'var(--color-purple-500, #a855f7)';
      default: return 'var(--theme-text-muted)';
    }
  }

  // Batch incoming logs and flush on animation frame to avoid per-message re-renders
  let pendingLogs: LogEntry[] = [];
  let flushScheduled = false;

  function flushPendingLogs() {
    if (pendingLogs.length === 0) return;
    const batch = pendingLogs;
    pendingLogs = [];
    flushScheduled = false;
    // Prepend batch (newest first) and trim
    logs = [...batch.reverse(), ...logs].slice(0, MAX_LINES);
  }

  function enqueueLog(entry: LogEntry) {
    pendingLogs.push({ ...entry, _id: logIdCounter++ });
    if (!flushScheduled) {
      flushScheduled = true;
      requestAnimationFrame(flushPendingLogs);
    }
  }

  let reportingId: number | null = $state(null);

  async function reportError(entry: LogEntry) {
    reportingId = entry._id ?? null;
    const result = await apiPost('/telemetry/report-error', {
      message: entry.message,
      level: entry.level,
      serviceType: entry.serviceType,
      moduleId: entry.moduleId,
      timestamp: typeof entry.timestamp === 'number' ? entry.timestamp : new Date(entry.timestamp).getTime(),
    });
    reportingId = null;
    if (result.error) {
      saltState.addNotification({ message: 'Failed to report error: ' + result.error.error, type: 'error' });
    } else {
      saltState.addNotification({ message: 'Error reported. Thank you!', type: 'success' });
    }
  }

  // Refresh from the buffered-logs endpoint and merge anything we don't already have.
  // SSE drops silently when the tab backgrounds or the network blips; this fills the gap.
  let refreshing = false;
  let lastRefreshAt = 0;
  async function refreshFromBuffer() {
    if (refreshing) return;
    if (Date.now() - lastRefreshAt < 2000) return;
    refreshing = true;
    lastRefreshAt = Date.now();
    try {
      const result = await api<LogEntry[]>(`/services/${serviceType}/logs?limit=${MAX_LINES}`);
      if (!result.data || result.data.length === 0) return;
      const latestShown = logs.length > 0 ? tsToMs(logs[0].timestamp) : 0;
      const fresh = result.data
        .filter((e) => tsToMs(e.timestamp) > latestShown)
        .reverse() // backend returns oldest→newest; we render newest first
        .map((e) => ({ ...e, _id: logIdCounter++ }));
      if (fresh.length === 0) return;
      logs = [...fresh, ...logs].slice(0, MAX_LINES);
    } finally {
      refreshing = false;
    }
  }

  function onVisibilityChange() {
    if (document.visibilityState === 'visible') {
      refreshFromBuffer();
    }
  }

  onMount(() => {
    // Subscribe to real-time log stream
    unsubscribe = subscribe<LogEntry>(
      `/services/${serviceType}/logs/stream`,
      (data) => {
        enqueueLog(data);
      },
      () => {
        // EventSource auto-reconnects with backoff; once it does, replay the gap.
        refreshFromBuffer();
      }
    );
    document.addEventListener('visibilitychange', onVisibilityChange);
  });

  onDestroy(() => {
    unsubscribe?.();
    document.removeEventListener('visibilitychange', onVisibilityChange);
    pendingLogs = [];
    flushScheduled = false;
  });
</script>

<div class="log-viewer">
  <div class="log-toolbar">
    <div class="filters">
      <label class="filter-toggle" class:active={showDebug}>
        <input type="checkbox" bind:checked={showDebug} />
        <span class="filter-label" style="--level-color: {levelColor('debug')}">DBG</span>
      </label>
      <label class="filter-toggle" class:active={showInfo}>
        <input type="checkbox" bind:checked={showInfo} />
        <span class="filter-label" style="--level-color: {levelColor('info')}">INF</span>
      </label>
      <label class="filter-toggle" class:active={showWarn}>
        <input type="checkbox" bind:checked={showWarn} />
        <span class="filter-label" style="--level-color: {levelColor('warn')}">WRN</span>
      </label>
      <label class="filter-toggle" class:active={showError}>
        <input type="checkbox" bind:checked={showError} />
        <span class="filter-label" style="--level-color: {levelColor('error')}">ERR</span>
      </label>
    </div>
    <div class="toolbar-right">
      <span class="line-count">{filteredLogs.length} lines</span>
    </div>
  </div>

  <div class="log-output">
    {#if filteredLogs.length === 0}
      <div class="empty-state">
        <p>No log entries yet. Logs will appear here in real-time as the service runs.</p>
      </div>
    {:else}
      {#each filteredLogs as entry (entry._id)}
        <div class="log-line">
          <span class="log-time">{formatTime(entry.timestamp)}</span>
          <span class="log-level" style="color: {levelColor(entry.level)}">{entry.level.toUpperCase().padEnd(5)}</span>
          {#if entry.logger}
            <span class="log-logger">[{entry.logger}]</span>
          {/if}
          <span class="log-message">{entry.message}</span>
          {#if entry.level === 'error'}
            <button
              class="report-btn"
              onclick={() => reportError(entry)}
              disabled={reportingId === entry._id}
              title="Report this error"
            >
              {reportingId === entry._id ? '...' : 'Report'}
            </button>
          {/if}
        </div>
      {/each}
    {/if}
  </div>
</div>

<style lang="scss">
  .log-viewer {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    border: 1px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    overflow: hidden;
  }

  .log-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.5rem 0.75rem;
    background: var(--theme-surface);
    border-bottom: 1px solid var(--theme-border);
  }

  .filters {
    display: flex;
    gap: 0.25rem;
  }

  .filter-toggle {
    display: flex;
    align-items: center;
    cursor: pointer;

    input {
      display: none;
    }

    .filter-label {
      padding: 0.125rem 0.5rem;
      font-size: 0.6875rem;
      font-family: 'IBM Plex Mono', monospace;
      font-weight: 600;
      border-radius: var(--rounded-md);
      border: 1px solid var(--theme-border);
      color: var(--theme-text-muted);
      transition: all 0.15s ease;
    }

    &.active .filter-label {
      color: var(--level-color);
      border-color: var(--level-color);
      background: color-mix(in srgb, var(--level-color) 10%, transparent);
    }
  }

  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .line-count {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
  }

  .log-output {
    flex: 1;
    overflow-y: auto;
    background: color-mix(in srgb, var(--theme-background) 80%, #000);
    padding: 0.5rem;
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

  .log-line {
    display: flex;
    gap: 0.5rem;
    padding: 0.0625rem 0.25rem;
    white-space: nowrap;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 5%, transparent);
    }
  }

  .log-time {
    color: var(--theme-text);
    opacity: 0.6;
    flex-shrink: 0;
  }

  .log-level {
    flex-shrink: 0;
    font-weight: 600;
    width: 3.5rem;
  }

  .log-logger {
    color: var(--theme-text-muted);
    flex-shrink: 0;
  }

  .log-message {
    color: var(--theme-text);
    white-space: pre-wrap;
    word-break: break-all;
  }

  .report-btn {
    flex-shrink: 0;
    opacity: 0;
    padding: 0 0.375rem;
    font-size: 0.625rem;
    font-weight: 600;
    font-family: 'Space Grotesk', sans-serif;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-red-500, #ef4444);
    background: none;
    border: 1px solid transparent;
    border-radius: var(--rounded);
    cursor: pointer;
    transition: opacity 0.15s, background 0.15s, border-color 0.15s;

    .log-line:hover & {
      opacity: 1;
    }

    &:hover {
      background: rgba(239, 68, 68, 0.1);
      border-color: rgba(239, 68, 68, 0.3);
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }
</style>
