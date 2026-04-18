<script lang="ts">
  import SystemTopology from '$lib/components/SystemTopology.svelte';
  import ServiceBanner from '$lib/components/ServiceBanner.svelte';
  import TelemetryBanner from '$lib/components/TelemetryBanner.svelte';
  import { onMount } from 'svelte';
  import { api } from '$lib/api/client';

  interface Service {
    serviceType: string;
    moduleId: string;
    startedAt: number;
    version: string | null;
    metadata: Record<string, unknown> | null;
    enabled: boolean;
  }

  // Live-updating services list — starts empty, populated by polling
  let liveServices = $state<Service[]>([]);
  let apiConnected = $state(false);
  let hasPolled = $state(false); // suppress banner until first poll completes

  // Derive monolith mode from the orchestrator service's metadata
  const monolith = $derived(
    liveServices.some(s => s.serviceType === 'orchestrator' && (s.metadata as any)?.mode === 'monolith')
  );

  // Poll services every 5 seconds for real-time topology updates
  onMount(() => {
    // Fingerprint services to detect actual changes (avoid re-render on identical data)
    let lastFingerprint = '';

    function fingerprint(svcs: typeof liveServices): string {
      const now = Date.now();
      return svcs
        .map(s => {
          // Only include metadata that affects topology structure/visual state
          // Exclude volatile counters (recordsWritten, lastFlushTime, bufferSize, etc.)
          const parts = [s.serviceType, s.moduleId, String(s.enabled)];
          if (s.metadata) {
            if (s.metadata.devices != null) parts.push(`d:${s.metadata.devices}`);
            if (s.metadata.mode != null) parts.push(`m:${s.metadata.mode}`);
            if (s.metadata.connected != null) parts.push(`c:${s.metadata.connected}`);
            // History: include a derived "is-flowing" bit so flow animation toggles
            // when batches start/stop landing, without re-rendering on every poll.
            if (s.serviceType === 'history') {
              const raw = s.metadata.lastFlushTime;
              const lastFlush = typeof raw === 'number' ? raw : Number(raw);
              const flowing = Number.isFinite(lastFlush) && lastFlush > 0 && now - lastFlush < 60_000;
              parts.push(`f:${flowing}`);
            }
          }
          return parts.join(':');
        })
        .sort()
        .join('|');
    }

    const poll = async () => {
      try {
        const result = await api<Service[]>('/services');
        if (result.data) {
          const newFp = fingerprint(result.data);
          if (newFp !== lastFingerprint) {
            lastFingerprint = newFp;
            liveServices = result.data;
          }
          apiConnected = true;
        } else {
          if (apiConnected) apiConnected = false;
        }
      } catch {
        if (apiConnected) apiConnected = false;
      }
      hasPolled = true;
    };

    const interval = setInterval(poll, 5000);
    // Initial poll after a short delay to catch fast changes
    setTimeout(poll, 1000);
    // Also poll immediately
    poll();

    return () => clearInterval(interval);
  });
</script>

<div class="page">
  <TelemetryBanner {apiConnected} />
  {#if apiConnected}
    <ServiceBanner />
  {/if}
  {#if hasPolled && !apiConnected}
    <div class="disconnected-banner">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="10" />
        <line x1="12" y1="8" x2="12" y2="12" />
        <line x1="12" y1="16" x2="12.01" y2="16" />
      </svg>
      <div class="banner-text">
        <strong>API service unreachable</strong>
        <span>Start tentacle to view system status.</span>
      </div>
    </div>
  {/if}
  <SystemTopology services={liveServices} {apiConnected} {monolith} />
</div>

<style lang="scss">
  .page {
    padding: 2rem;
  }

  .disconnected-banner {
    display: flex;
    align-items: flex-start;
    gap: 0.75rem;
    padding: 1rem 1.25rem;
    margin-bottom: 1rem;
    background: rgba(239, 68, 68, 0.08);
    border: 1px solid rgba(239, 68, 68, 0.25);
    border-radius: var(--rounded-lg);
    color: var(--theme-text);

    svg {
      flex-shrink: 0;
      margin-top: 0.125rem;
      color: #ef4444;
    }

    .banner-text {
      display: flex;
      flex-direction: column;
      gap: 0.125rem;

      strong {
        font-size: 0.875rem;
        color: #ef4444;
      }

      span {
        font-size: 0.8125rem;
        color: var(--theme-text-muted);
      }
    }
  }
</style>
