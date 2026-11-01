<script lang="ts">
  // "Where is it" chip colored by placement status (--status-* tokens).
  // compact: one short line for cards; full: the server label, staged split
  // into "from → to".
  import type { Placement } from '$lib/api/client';
  import Icon from './Icon.svelte';
  import { STATUS_ICON } from './icons';
  import { chipText, splitStaged, STATUS_LABEL } from './format';

  let {
    placement,
    variant = 'compact',
    href,
    onclick
  }: {
    placement: Placement;
    variant?: 'compact' | 'full';
    /** Makes the chip a link (e.g. to the location page). */
    href?: string;
    /** Makes the chip a button (e.g. open the move sheet). */
    onclick?: (e: MouseEvent) => void;
  } = $props();

  const status = $derived(placement.status);
  const text = $derived(chipText(placement, variant));
  const staged = $derived(variant === 'full' && status === 'staged' ? splitStaged(placement.label) : null);
  const aria = $derived(`${STATUS_LABEL[status] ?? status}: ${placement.label}`);
</script>

{#snippet body()}
  <Icon name={STATUS_ICON[status] ?? 'unknown'} size={variant === 'full' ? 18 : 14} />
  <span class="text" aria-hidden="true">
    {#if staged}
      <span class="from">{staged[0]}</span>
      <span class="arrow">→</span>
      <span class="to">{staged[1]}</span>
    {:else}
      {text}
    {/if}
  </span>
  <span class="visually-hidden">{aria}</span>
{/snippet}

{#if href}
  <a class="chip s-{status} {variant}" {href}>{@render body()}</a>
{:else if onclick}
  <button type="button" class="chip s-{status} {variant}" {onclick}>{@render body()}</button>
{:else}
  <span class="chip s-{status} {variant}">{@render body()}</span>
{/if}

<style>
  .chip {
    --chip-fg: var(--status-unknown);
    --chip-bg: var(--status-unknown-soft);
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    max-width: 100%;
    min-width: 0;
    border: var(--border-width) solid transparent;
    border-radius: var(--radius-pill);
    background: var(--chip-bg);
    color: var(--chip-fg);
    font-weight: var(--weight-medium);
    line-height: var(--leading-tight);
    text-decoration: none;
    vertical-align: middle;
  }
  .s-placed {
    --chip-fg: var(--status-placed);
    --chip-bg: var(--status-placed-soft);
  }
  .s-approximate {
    --chip-fg: var(--status-approximate);
    --chip-bg: var(--status-approximate-soft);
    border-style: dashed;
    border-color: color-mix(in srgb, var(--chip-fg) 45%, transparent);
  }
  .s-staged {
    --chip-fg: var(--status-staged);
    --chip-bg: var(--status-staged-soft);
  }
  .s-unknown {
    --chip-fg: var(--status-unknown);
    --chip-bg: var(--status-unknown-soft);
  }
  .s-gone {
    --chip-fg: var(--status-gone);
    --chip-bg: var(--status-gone-soft);
  }
  .compact {
    min-height: 22px;
    padding: 0 var(--space-2);
    font-size: var(--text-xs);
  }
  .full {
    min-height: calc(var(--tap-min) - var(--space-2));
    padding: var(--space-1) var(--space-3);
    font-size: var(--text-sm);
  }
  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .full .text {
    white-space: normal;
  }
  .from {
    opacity: 0.8;
  }
  .arrow {
    padding: 0 var(--space-0);
  }
  .to {
    font-weight: var(--weight-bold);
  }
  a.chip,
  button.chip {
    position: relative;
    cursor: pointer;
  }
  a.chip:hover,
  button.chip:hover {
    text-decoration: none;
    border-color: color-mix(in srgb, var(--chip-fg) 45%, transparent);
  }
  a.compact::after,
  button.compact::after {
    content: '';
    position: absolute;
    inset: -11px 0;
  }
  a.full::after,
  button.full::after {
    content: '';
    position: absolute;
    inset: -4px 0;
  }
  button.chip {
    font: inherit;
    font-weight: var(--weight-medium);
  }
  button.compact {
    font-size: var(--text-xs);
  }
  button.full {
    font-size: var(--text-sm);
  }
</style>
