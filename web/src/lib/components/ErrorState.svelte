<script lang="ts">
  import type { Snippet } from 'svelte';
  import { ApiError } from '$lib/api/client';
  import Button from './Button.svelte';
  import Icon from './Icon.svelte';
  import { errorMessage } from './format';

  let {
    error,
    title = 'Something went wrong',
    message,
    onretry,
    retrying = false,
    compact = false,
    children
  }: {
    /** Any thrown value; ApiError shows its code and status. */
    error?: unknown;
    title?: string;
    /** Overrides the message derived from `error`. */
    message?: string;
    onretry?: () => void;
    retrying?: boolean;
    compact?: boolean;
    /** Extra actions. */
    children?: Snippet;
  } = $props();

  const text = $derived(message ?? (errorMessage(error) || 'Please try again.'));
  const detail = $derived(error instanceof ApiError ? `${error.code} · HTTP ${error.status}` : '');
</script>

<div class="state" class:compact role="alert">
  <span class="art" aria-hidden="true"><Icon name="alert" size={compact ? 24 : 32} /></span>
  <h3>{title}</h3>
  <p class="muted">{text}</p>
  {#if detail}<p class="detail mono">{detail}</p>{/if}
  {#if onretry || children}
    <div class="actions">
      {#if onretry}<Button icon="refresh" loading={retrying} onclick={onretry}>Try again</Button>{/if}
      {#if children}{@render children()}{/if}
    </div>
  {/if}
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
    padding: var(--space-4) var(--space-3);
  }
  .art {
    display: grid;
    place-items: center;
    width: 64px;
    height: 64px;
    margin-bottom: var(--space-1);
    border-radius: var(--radius-pill);
    background: var(--color-danger-soft);
    color: var(--color-danger);
  }
  .compact .art {
    width: 44px;
    height: 44px;
  }
  h3 {
    font-size: var(--text-lg);
  }
  p {
    max-width: 44ch;
  }
  .detail {
    color: var(--color-text-faint);
    font-size: var(--text-xs);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
</style>
