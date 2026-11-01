<script lang="ts">
  // Chip: static label, toggle (pass `selected`), link (`href`) or removable.
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from './icons';

  let {
    selected,
    count,
    icon,
    href,
    disabled = false,
    onclick,
    onremove,
    removeLabel = 'Remove',
    children
  }: {
    /** When defined the chip is a toggle button with aria-pressed. */
    selected?: boolean;
    /** Trailing count (facet counts). */
    count?: number;
    icon?: IconName;
    href?: string;
    disabled?: boolean;
    onclick?: (e: MouseEvent) => void;
    /** Shows a remove (×) button. */
    onremove?: () => void;
    removeLabel?: string;
    children: Snippet;
  } = $props();

  const interactive = $derived(selected !== undefined || !!onclick);
</script>

{#snippet body()}
  {#if selected}<Icon name="check" size={16} />{:else if icon}<Icon name={icon} size={16} />{/if}
  <span class="label">{@render children()}</span>
  {#if count !== undefined}<span class="count">{count}</span>{/if}
{/snippet}

{#if onremove}
  <span class="chip removable" class:selected>
    {@render body()}
    <button type="button" class="remove" aria-label={removeLabel} onclick={onremove}>
      <Icon name="close" size={14} />
    </button>
  </span>
{:else if href}
  <a class="chip" class:selected {href}>{@render body()}</a>
{:else if interactive}
  <button type="button" class="chip" class:selected aria-pressed={selected} {disabled} {onclick} class:zero={count === 0 && !selected}>
    {@render body()}
  </button>
{:else}
  <span class="chip static">{@render body()}</span>
{/if}

<style>
  .chip {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-height: calc(var(--tap-min) - var(--space-2));
    max-width: 100%;
    padding: 0 var(--space-3);
    border: var(--border-width) solid var(--color-border);
    border-radius: var(--radius-pill);
    background: var(--color-surface);
    color: var(--color-text);
    font-size: var(--text-sm);
    line-height: var(--leading-tight);
    text-decoration: none;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
    transition:
      background-color var(--duration-fast) var(--ease),
      border-color var(--duration-fast) var(--ease);
  }
  button.chip::after,
  a.chip::after {
    content: '';
    position: absolute;
    inset: calc(var(--space-1) * -1) 0;
  }
  .chip:hover {
    text-decoration: none;
  }
  button.chip:hover:not(:disabled),
  a.chip:hover {
    border-color: var(--color-border-strong);
  }
  .static {
    cursor: default;
    background: var(--color-surface-2);
    border-color: transparent;
  }
  .selected {
    background: var(--color-accent-soft);
    border-color: var(--color-accent);
    color: var(--color-accent);
    font-weight: var(--weight-medium);
  }
  .zero {
    color: var(--color-text-faint);
  }
  .chip:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    color: var(--color-text-muted);
    font-size: var(--text-xs);
    font-variant-numeric: tabular-nums;
  }
  .selected .count {
    color: inherit;
  }
  .removable {
    padding-right: 0;
    cursor: default;
  }
  .remove {
    display: inline-grid;
    place-items: center;
    width: calc(var(--tap-min) - var(--space-2));
    height: calc(var(--tap-min) - var(--space-2));
    margin-left: calc(var(--space-1) * -1);
    border: 0;
    border-radius: var(--radius-pill);
    background: transparent;
    color: inherit;
    cursor: pointer;
  }
  .remove:hover {
    background: var(--color-surface-3);
  }
  @media (prefers-reduced-motion: reduce) {
    .chip {
      transition: none;
    }
  }
</style>
