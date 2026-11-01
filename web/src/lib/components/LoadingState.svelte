<script lang="ts">
  // grid: skeleton cards matching PhotoGrid/ItemCard; list: skeleton rows;
  // spinner: centered spinner with label; inline: small spinner + label.
  import Spinner from './Spinner.svelte';

  let {
    variant = 'grid',
    count = 8,
    label = 'Loading…'
  }: {
    variant?: 'grid' | 'list' | 'spinner' | 'inline';
    count?: number;
    label?: string;
  } = $props();
</script>

{#if variant === 'grid'}
  <div class="grid" role="status" aria-label={label}>
    {#each { length: count }, i (i)}
      <div class="card" aria-hidden="true">
        <div class="sk photo"></div>
        <div class="sk line"></div>
        <div class="sk line short"></div>
      </div>
    {/each}
  </div>
{:else if variant === 'list'}
  <div class="list" role="status" aria-label={label}>
    {#each { length: count }, i (i)}
      <div class="row" aria-hidden="true">
        <div class="sk thumb"></div>
        <div class="lines"><div class="sk line"></div><div class="sk line short"></div></div>
      </div>
    {/each}
  </div>
{:else if variant === 'spinner'}
  <div class="center" role="status">
    <Spinner size={28} />
    <span class="muted">{label}</span>
  </div>
{:else}
  <span class="inline" role="status"><Spinner size={16} /> <span class="muted">{label}</span></span>
{/if}

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(var(--grid-card-min), calc(50% - var(--space-3) / 2)), 1fr));
    gap: var(--space-4) var(--space-3);
  }
  .card {
    display: grid;
    gap: var(--space-2);
  }
  .sk {
    border-radius: var(--radius-sm);
    background: linear-gradient(
      90deg,
      var(--color-surface-2) 0%,
      var(--color-surface-3) 50%,
      var(--color-surface-2) 100%
    );
    background-size: 200% 100%;
    animation: shimmer 1.4s var(--ease) infinite;
  }
  .photo {
    aspect-ratio: 4 / 5;
    border-radius: var(--radius-md);
  }
  .line {
    height: 12px;
  }
  .short {
    width: 55%;
  }
  .list {
    display: grid;
    gap: var(--space-3);
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .thumb {
    width: var(--tap-min);
    height: var(--tap-min);
    flex: none;
  }
  .lines {
    flex: 1;
    display: grid;
    gap: var(--space-2);
  }
  .center {
    display: grid;
    justify-items: center;
    gap: var(--space-3);
    padding: var(--space-7) var(--space-4);
    color: var(--color-text-muted);
  }
  .inline {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-text-muted);
  }
  @keyframes shimmer {
    from {
      background-position: 100% 0;
    }
    to {
      background-position: -100% 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .sk {
      animation: none;
      background: var(--color-surface-2);
    }
  }
</style>
