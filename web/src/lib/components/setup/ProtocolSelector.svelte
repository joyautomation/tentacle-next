<script lang="ts">
  import { CpuChip, CircleStack, ServerStack, ComputerDesktop } from '@joyautomation/salt/icons';

  interface Props {
    selected: Set<string>;
    onchange: (selected: Set<string>) => void;
  }

  let { selected, onchange }: Props = $props();

  const protocols = [
    { id: 'ethernetip', name: 'EtherNet/IP', desc: 'Allen-Bradley, Rockwell PLCs', icon: CpuChip },
    { id: 'opcua', name: 'OPC UA', desc: 'Universal industrial protocol', icon: CircleStack },
    { id: 'modbus', name: 'Modbus TCP', desc: 'Classic industrial protocol', icon: ServerStack },
    { id: 'snmp', name: 'SNMP', desc: 'Network device monitoring', icon: ComputerDesktop },
  ];

  function toggle(id: string) {
    const next = new Set(selected);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    onchange(next);
  }
</script>

<div class="protocol-grid">
  {#each protocols as proto}
    <button
      class="protocol-card"
      class:selected={selected.has(proto.id)}
      onclick={() => toggle(proto.id)}
    >
      <div class="card-check">
        {#if selected.has(proto.id)}
          <svg viewBox="0 0 20 20" fill="currentColor" width="16" height="16">
            <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
          </svg>
        {/if}
      </div>
      <div class="card-icon">
        <proto.icon size="1.5rem" />
      </div>
      <div class="card-text">
        <span class="card-name">{proto.name}</span>
        <span class="card-desc">{proto.desc}</span>
      </div>
    </button>
  {/each}
</div>

<style lang="scss">
  .protocol-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 0.75rem;
  }

  .protocol-card {
    display: flex;
    align-items: flex-start;
    gap: 0.75rem;
    position: relative;
    background: var(--theme-surface);
    border: 2px solid var(--theme-border);
    border-radius: var(--rounded-lg);
    padding: 1rem;
    cursor: pointer;
    text-align: left;
    transition: border-color 0.2s, box-shadow 0.2s;

    &:hover {
      border-color: var(--theme-primary);
    }

    &.selected {
      border-color: var(--theme-primary);
      box-shadow: 0 0 0 1px var(--theme-primary);
    }
  }

  .card-check {
    width: 20px;
    height: 20px;
    border-radius: 4px;
    border: 2px solid var(--theme-border);
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    transition: all 0.2s;

    .selected & {
      background: var(--theme-primary);
      border-color: var(--theme-primary);
      color: white;
    }
  }

  .card-icon {
    flex-shrink: 0;
    color: var(--theme-text-muted);
    transition: color 0.2s;

    .selected & {
      color: var(--theme-primary);
    }
  }

  .card-text {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    min-width: 0;
  }

  .card-name {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--theme-text);
  }

  .card-desc {
    font-size: 0.75rem;
    color: var(--theme-text-muted);
    line-height: 1.3;
  }
</style>
