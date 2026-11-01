<script lang="ts">
  // Sticky "N selected" bar with actions. Sits above the phone tab bar
  // (--tabbar-height + safe area) and at the bottom on desktop. Renders an
  // in-flow spacer so the last grid row is never hidden behind it.
  import type { Snippet } from 'svelte';
  import Button from './Button.svelte';

  let {
    count,
    total,
    onclear,
    onselectall,
    label,
    alwaysShow = false,
    actions
  }: {
    count: number;
    /** When given with onselectall, shows "Select all N". */
    total?: number;
    onclear: () => void;
    onselectall?: () => void;
    /** Custom count text. Default "N selected". */
    label?: string;
    /** Show with zero selected (e.g. right after entering selection mode). */
    alwaysShow?: boolean;
    /** Action buttons (use Button size="sm" or "md"). */
    actions?: Snippet;
  } = $props();

  const visible = $derived(count > 0 || alwaysShow);
</script>

{#if visible}
  <div class="spacer" aria-hidden="true"></div>
  <div class="bar" role="region" aria-label="Selection">
    <div class="inner">
      <div class="count" aria-live="polite">
        <strong>{label ?? `${count} selected`}</strong>
        {#if onselectall && total !== undefined && count < total}
          <button type="button" class="link" onclick={onselectall}>Select all {total}</button>
        {/if}
      </div>
      <div class="actions">
        {#if actions}{@render actions()}{/if}
        <Button variant="ghost" onclick={onclear}>{count > 0 ? 'Clear' : 'Done'}</Button>
      </div>
    </div>
  </div>
{/if}

<style>
  .spacer {
    height: calc(var(--tap-min) + var(--space-6));
  }
  .bar {
    position: fixed;
    left: 0;
    right: 0;
    bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom) + var(--space-2));
    z-index: var(--z-nav);
    display: flex;
    justify-content: center;
    padding: 0 var(--space-2);
    pointer-events: none;
    animation: rise var(--duration-normal) var(--ease);
  }
  .inner {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: min(100%, var(--content-max));
    min-height: calc(var(--tap-min) + var(--space-3));
    padding: var(--space-1) var(--space-1) var(--space-1) var(--space-4);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-lg);
    background: var(--color-surface);
    box-shadow: var(--shadow-lg);
    pointer-events: auto;
  }
  .count {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    column-gap: var(--space-3);
    font-size: var(--text-sm);
  }
  strong {
    font-weight: var(--weight-bold);
    white-space: nowrap;
  }
  .link {
    min-height: var(--tap-min);
    padding: 0;
    border: 0;
    background: none;
    color: var(--color-accent);
    font-size: var(--text-sm);
    cursor: pointer;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--space-1);
  }
  @media (min-width: 720px) {
    .bar {
      bottom: calc(var(--space-4) + env(safe-area-inset-bottom));
      padding: 0 var(--gutter);
    }
    .inner {
      width: min(100%, 720px);
    }
  }
  @keyframes rise {
    from {
      transform: translateY(12px);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bar {
      animation: none;
    }
  }
</style>
