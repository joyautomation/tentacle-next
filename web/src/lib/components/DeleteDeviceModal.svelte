<script lang="ts">
  import { apiDelete } from "$lib/api/client";
  import { invalidateAll } from "$app/navigation";
  import { state as saltState } from "@joyautomation/salt";

  type Props = {
    deviceId: string;
    varCount: number;
    onClose: () => void;
    onDeleted?: () => void;
  };

  let { deviceId, varCount, onClose, onDeleted }: Props = $props();

  let confirmInput = $state("");
  let saving = $state(false);

  async function removeDevice() {
    saving = true;
    try {
      const result = await apiDelete(`/devices/${encodeURIComponent(deviceId)}`);
      if (result.error) {
        saltState.addNotification({ message: result.error.error, type: "error" });
      } else {
        saltState.addNotification({
          message: `Device "${deviceId}" removed`,
          type: "success",
        });
        await invalidateAll();
        onDeleted?.();
        onClose();
      }
    } catch (err) {
      saltState.addNotification({
        message: err instanceof Error ? err.message : "Failed",
        type: "error",
      });
    } finally {
      saving = false;
    }
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="modal-backdrop"
  onkeydown={(e) => {
    if (e.key === "Escape") onClose();
  }}
  onclick={onClose}
>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="modal" onclick={(e) => e.stopPropagation()}>
    <h2>Delete Device</h2>
    <p class="modal-warning">
      This will permanently remove <strong>{deviceId}</strong>
      and all <strong>{varCount}</strong>
      variable{varCount !== 1 ? "s" : ""} configured on it. Template name
      overrides, deadband settings, and browse data for this device will also
      be lost.
    </p>
    <p class="modal-confirm-label">
      Type <strong>{deviceId}</strong> to confirm:
    </p>
    <input
      class="modal-input"
      bind:value={confirmInput}
      placeholder={deviceId}
      onkeydown={(e) => {
        if (e.key === "Enter" && confirmInput === deviceId) {
          removeDevice();
        }
      }}
    />
    <div class="modal-actions">
      <button class="modal-cancel-btn" onclick={onClose}>Cancel</button>
      <button
        class="modal-delete-btn"
        disabled={confirmInput !== deviceId || saving}
        onclick={removeDevice}
      >
        {saving ? "Deleting…" : "Delete Device"}
      </button>
    </div>
  </div>
</div>

<style lang="scss">
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 1000;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
  }

  .modal {
    background: var(--theme-surface);
    border: 1px solid var(--theme-border);
    border-radius: 0.5rem;
    padding: 1.5rem;
    max-width: 26rem;
    width: 100%;

    h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--theme-text);
      margin: 0 0 1rem;
    }
  }

  .modal-warning {
    font-size: 0.8125rem;
    color: var(--color-red-500, #ef4444);
    line-height: 1.5;
    margin: 0 0 1rem;
  }

  .modal-confirm-label {
    font-size: 0.8125rem;
    color: var(--theme-text-muted);
    margin: 0 0 0.5rem;
  }

  .modal-input {
    width: 100%;
    padding: 0.375rem 0.5rem;
    font-size: 0.8125rem;
    font-family: var(--font-mono, "IBM Plex Mono", monospace);
    border: 1px solid var(--theme-border);
    border-radius: 0.25rem;
    background: var(--theme-input-bg);
    color: var(--theme-text);
    box-sizing: border-box;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }

  .modal-cancel-btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: 1px solid var(--theme-border);
    border-radius: 0.25rem;
    background: var(--theme-surface);
    color: var(--theme-text);
    cursor: pointer;

    &:hover {
      background: color-mix(in srgb, var(--theme-text) 5%, var(--theme-surface));
    }
  }

  .modal-delete-btn {
    padding: 0.375rem 1rem;
    font-size: 0.8125rem;
    font-weight: 500;
    border: none;
    border-radius: 0.25rem;
    background: var(--color-red-500, #ef4444);
    color: white;
    cursor: pointer;

    &:hover:not(:disabled) {
      opacity: 0.9;
    }

    &:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
  }
</style>
