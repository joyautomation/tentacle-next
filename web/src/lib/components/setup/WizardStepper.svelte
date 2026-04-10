<script lang="ts">
  interface Props {
    steps: string[];
    currentStep: number;
    onStepClick?: (step: number) => void;
  }

  let { steps, currentStep, onStepClick }: Props = $props();
</script>

<nav class="wizard-stepper">
  {#each steps as label, i}
    {#if i > 0}
      <div class="connector" class:completed={i <= currentStep}></div>
    {/if}
    <button
      class="step"
      class:active={i === currentStep}
      class:completed={i < currentStep}
      class:future={i > currentStep}
      disabled={i > currentStep}
      onclick={() => i < currentStep && onStepClick?.(i)}
    >
      <span class="step-number">
        {#if i < currentStep}
          <svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14">
            <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
          </svg>
        {:else}
          {i + 1}
        {/if}
      </span>
      <span class="step-label">{label}</span>
    </button>
  {/each}
</nav>

<style lang="scss">
  .wizard-stepper {
    display: flex;
    align-items: flex-start;
    justify-content: center;
    gap: 0;
    padding: 1.5rem 1rem;
  }

  .connector {
    flex: 0 0 3rem;
    height: 2px;
    background: var(--theme-border);
    transition: background 0.3s;
    // Align to the vertical center of the step-number circle (0.25rem padding + 2rem/2 = 1.25rem)
    margin-top: calc(1.25rem - 1px);

    &.completed {
      background: var(--theme-primary);
    }
  }

  .step {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.375rem;
    background: none;
    border: none;
    cursor: default;
    padding: 0.25rem 0.5rem 0;

    &.completed {
      cursor: pointer;
    }
  }

  .step-number {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2rem;
    height: 2rem;
    border-radius: 50%;
    font-size: 0.75rem;
    font-weight: 700;
    transition: all 0.3s;

    .active & {
      background: var(--theme-primary);
      color: white;
    }

    .completed & {
      background: var(--theme-primary);
      color: white;
    }

    .future & {
      background: var(--theme-surface);
      color: var(--theme-text-muted);
      border: 2px solid var(--theme-border);
    }
  }

  .step-label {
    font-size: 0.6875rem;
    font-weight: 500;
    white-space: nowrap;
    transition: color 0.3s;

    .active & {
      color: var(--theme-primary);
    }

    .completed & {
      color: var(--theme-text);
    }

    .future & {
      color: var(--theme-text-muted);
    }
  }

  @media (max-width: 480px) {
    .connector {
      flex: 0 0 1.5rem;
    }

    .step-label {
      display: none;
    }
  }
</style>
