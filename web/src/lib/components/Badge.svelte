<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from './icons';

  let {
    tone = 'neutral',
    icon,
    overlay = false,
    label,
    children
  }: {
    tone?: 'neutral' | 'accent' | 'info' | 'success' | 'warning' | 'danger';
    icon?: IconName;
    /** Solid style for sitting on top of photos. */
    overlay?: boolean;
    /** Accessible text when the visible content is terse (e.g. "×4"). */
    label?: string;
    children?: Snippet;
  } = $props();
</script>

<span class="badge {tone}" class:overlay title={label}>
  {#if icon}<Icon name={icon} size={14} />{/if}
  {#if children}<span aria-hidden={label ? 'true' : undefined}>{@render children()}</span>{/if}
  {#if label}<span class="visually-hidden">{label}</span>{/if}
</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-height: 22px;
    padding: 0 var(--space-2);
    border-radius: var(--radius-pill);
    font-size: var(--text-xs);
    font-weight: var(--weight-medium);
    line-height: 1;
    white-space: nowrap;
    background: var(--color-surface-2);
    color: var(--color-text-muted);
  }
  .accent {
    background: var(--color-accent-soft);
    color: var(--color-accent);
  }
  .info {
    background: var(--status-staged-soft);
    color: var(--status-staged);
  }
  .success {
    background: var(--color-success-soft);
    color: var(--color-success);
  }
  .warning {
    background: var(--color-warning-soft);
    color: var(--color-warning);
  }
  .danger {
    background: var(--color-danger-soft);
    color: var(--color-danger);
  }
  .overlay {
    box-shadow: var(--shadow-sm);
    background: color-mix(in srgb, var(--color-surface) 92%, transparent);
    color: var(--color-text);
    backdrop-filter: blur(6px);
  }
  .overlay.warning {
    background: var(--color-warning-soft);
    color: var(--color-warning);
  }
  .overlay.accent {
    background: var(--color-accent);
    color: var(--color-accent-contrast);
  }
</style>
