<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from './icons';

  let {
    title,
    message,
    icon = 'inbox',
    compact = false,
    children
  }: {
    title: string;
    message?: string;
    icon?: IconName;
    compact?: boolean;
    /** Actions (buttons/links). */
    children?: Snippet;
  } = $props();
</script>

<div class="state" class:compact>
  <span class="art" aria-hidden="true"><Icon name={icon} size={compact ? 28 : 40} /></span>
  <h3>{title}</h3>
  {#if message}<p class="muted">{message}</p>{/if}
  {#if children}<div class="actions">{@render children()}</div>{/if}
</div>

<style>
  .state {
    display: grid;
    justify-items: center;
    gap: var(--space-2);
    padding: var(--space-7) var(--space-4);
    text-align: center;
  }
  .compact {
    padding: var(--space-5) var(--space-3);
  }
  .art {
    display: grid;
    place-items: center;
    width: 72px;
    height: 72px;
    margin-bottom: var(--space-2);
    border-radius: var(--radius-pill);
    background: var(--color-surface-2);
    color: var(--color-text-muted);
  }
  .compact .art {
    width: 52px;
    height: 52px;
  }
  h3 {
    font-size: var(--text-lg);
  }
  p {
    max-width: 36ch;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
</style>
