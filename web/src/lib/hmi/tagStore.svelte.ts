// Live tag value store. Subscribes once to /api/v1/variables/stream and
// surfaces values via a refcounted hook so widgets can call useTags()
// without each opening their own EventSource.

import { onDestroy } from 'svelte';
import { subscribe } from '$lib/api/subscribe';
import type { HmiBinding } from '$lib/types/hmi';

interface PlcDataMessage {
  moduleId: string;
  deviceId: string;
  variableId: string;
  value: unknown;
  timestamp: number;
  datatype: string;
}

/** Map key: "{moduleId}/{variableId}" — gateway emits one entry per UDT instance with the
 * full UDT object as `value`, so we key by variableId regardless of UDT-ness. */
function keyFor(moduleId: string, variableId: string): string {
  return `${moduleId}/${variableId}`;
}

class TagStore {
  values = $state<Record<string, unknown>>({});
  timestamps = $state<Record<string, number>>({});
  private refCount = 0;
  private cleanup: (() => void) | null = null;

  acquire(): void {
    this.refCount++;
    if (this.refCount === 1) {
      this.cleanup = subscribe<PlcDataMessage>('/variables/stream', (msg) => {
        const k = keyFor(msg.moduleId, msg.variableId);
        // Re-assign to trigger reactivity — Svelte 5 needs new identity for nested objects.
        this.values = { ...this.values, [k]: msg.value };
        this.timestamps = { ...this.timestamps, [k]: msg.timestamp };
      });
    }
  }

  release(): void {
    this.refCount--;
    if (this.refCount === 0 && this.cleanup) {
      this.cleanup();
      this.cleanup = null;
    }
  }

  /** Resolve a binding to its current value (or undefined if unseen yet). */
  resolve(binding: HmiBinding | undefined, udtContext?: { moduleId: string; udtVariable: string }): unknown {
    if (!binding) return undefined;
    if (binding.kind === 'variable') {
      if (!binding.gateway || !binding.variable) return undefined;
      return this.values[keyFor(binding.gateway, binding.variable)];
    }
    if (binding.kind === 'udtMember') {
      const moduleId = binding.gateway ?? udtContext?.moduleId;
      const udtVariable = binding.udtVariable ?? udtContext?.udtVariable;
      if (!moduleId || !udtVariable || !binding.member) return undefined;
      const obj = this.values[keyFor(moduleId, udtVariable)];
      if (obj && typeof obj === 'object') {
        return (obj as Record<string, unknown>)[binding.member];
      }
      return undefined;
    }
    return undefined;
  }
}

export const tagStore = new TagStore();

/** Acquire the live tag stream for the calling component's lifetime. */
export function useLiveTags(): void {
  tagStore.acquire();
  onDestroy(() => tagStore.release());
}
