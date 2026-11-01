<script lang="ts">
  // Button (or link styled as one when `href` is set).
  import type { Snippet } from 'svelte';
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';
  import Spinner from './Spinner.svelte';
  import type { IconName } from './icons';

  type Props = Omit<HTMLButtonAttributes, 'children'> & {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
    /** sm is visually 36px but keeps a --tap-min hit area. */
    size?: 'sm' | 'md' | 'lg';
    /** Leading icon. */
    icon?: IconName;
    /** Trailing icon. */
    iconEnd?: IconName;
    /** Accessible name for icon-only buttons (required when there are no children). */
    label?: string;
    loading?: boolean;
    /** Full width. */
    block?: boolean;
    /** Render as <a href>. */
    href?: string;
    target?: string;
    rel?: string;
    download?: string | boolean;
    children?: Snippet;
  };

  let {
    variant = 'secondary',
    size = 'md',
    icon,
    iconEnd,
    label,
    loading = false,
    block = false,
    href,
    target,
    rel,
    download,
    type = 'button',
    disabled = false,
    class: cls = '',
    children,
    ...rest
  }: Props = $props();

  const iconOnly = $derived(!children && !!icon);
  const iconSize = $derived(size === 'lg' ? 22 : size === 'sm' ? 18 : 20);
</script>

{#snippet inner()}
  {#if loading}
    <Spinner size={iconSize - 2} />
  {:else if icon}
    <Icon name={icon} size={iconSize} />
  {/if}
  {#if children}<span class="text">{@render children()}</span>{/if}
  {#if iconEnd && !loading}<Icon name={iconEnd} size={iconSize} />{/if}
{/snippet}

{#if href && !disabled}
  <a
    {href}
    {target}
    rel={rel ?? (target === '_blank' ? 'noopener' : undefined)}
    download={download === true ? '' : download || undefined}
    class="btn {variant} {size} {cls}"
    class:block
    class:icon-only={iconOnly}
    aria-label={label}
    title={iconOnly ? label : undefined}
  >
    {@render inner()}
  </a>
{:else}
  <button
    {...rest}
    {type}
    disabled={disabled || loading}
    aria-busy={loading || undefined}
    class="btn {variant} {size} {cls}"
    class:block
    class:icon-only={iconOnly}
    aria-label={label}
    title={iconOnly ? label : undefined}
  >
    {@render inner()}
  </button>
{/if}

<style>
  .btn {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    padding: 0 var(--space-4);
    border: var(--border-width) solid transparent;
    border-radius: var(--radius-md);
    font-weight: var(--weight-medium);
    font-size: var(--text-md);
    line-height: var(--leading-tight);
    white-space: nowrap;
    cursor: pointer;
    text-decoration: none;
    user-select: none;
    -webkit-tap-highlight-color: transparent;
    transition:
      background-color var(--duration-fast) var(--ease),
      border-color var(--duration-fast) var(--ease),
      color var(--duration-fast) var(--ease);
  }
  .btn:hover {
    text-decoration: none;
  }
  .btn:disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }
  .btn[aria-busy='true'] {
    cursor: progress;
    opacity: 0.8;
  }
  .text {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* sizes */
  .sm {
    min-height: calc(var(--tap-min) - var(--space-2));
    padding: 0 var(--space-3);
    font-size: var(--text-sm);
    border-radius: var(--radius-sm);
  }
  .sm::after {
    /* keep the touch target at --tap-min */
    content: '';
    position: absolute;
    inset: calc(var(--space-1) * -1) 0;
  }
  .lg {
    min-height: calc(var(--tap-min) + var(--space-1));
    padding: 0 var(--space-5);
    font-size: var(--text-lg);
  }
  .block {
    display: flex;
    width: 100%;
  }
  .icon-only {
    padding: 0;
    width: var(--tap-min);
  }
  .icon-only.sm {
    width: calc(var(--tap-min) - var(--space-2));
  }
  .icon-only.sm::after {
    inset: calc(var(--space-1) * -1);
  }

  /* variants */
  .primary {
    background: var(--color-accent);
    color: var(--color-accent-contrast);
  }
  .primary:hover:not(:disabled) {
    background: var(--color-accent-hover);
  }
  .secondary {
    background: var(--color-surface);
    border-color: var(--color-border-strong);
    color: var(--color-text);
  }
  .secondary:hover:not(:disabled) {
    background: var(--color-surface-2);
  }
  .ghost {
    background: transparent;
    color: var(--color-text);
  }
  .ghost:hover:not(:disabled) {
    background: var(--color-surface-2);
  }
  .danger {
    background: var(--color-danger);
    color: var(--color-surface);
  }
  .danger:hover:not(:disabled) {
    background: color-mix(in srgb, var(--color-danger) 88%, var(--color-text));
  }
  .btn:active:not(:disabled) {
    transform: translateY(1px);
  }
  @media (prefers-reduced-motion: reduce) {
    .btn {
      transition: none;
    }
    .btn:active:not(:disabled) {
      transform: none;
    }
  }
</style>
