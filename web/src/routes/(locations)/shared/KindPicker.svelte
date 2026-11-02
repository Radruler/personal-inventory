<script lang="ts">
  // Segmented Clothing / Object / Utility control (one tap on a phone).
  import type { Kind } from '$lib/api/client';
  import Icon from '$lib/components/Icon.svelte';
  import { KIND_LABEL } from '$lib/components/format';
  import { KIND_ICON } from '$lib/components/icons';

  let { value = $bindable('clothing'), label = 'Kind' }: { value?: Kind; label?: string } = $props();
  const kinds: Kind[] = ['clothing', 'object', 'utility'];
</script>

<div class="seg" role="radiogroup" aria-label={label}>
  {#each kinds as k (k)}
    <button type="button" role="radio" aria-checked={value === k} class:on={value === k} onclick={() => (value = k)}>
      <Icon name={KIND_ICON[k]} size={20} />
      <span>{KIND_LABEL[k]}</span>
    </button>
  {/each}
</div>

<style>
  .seg {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--space-1);
    padding: var(--space-1);
    border-radius: var(--radius-md);
    background: var(--color-surface-2);
  }
  button {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    min-height: var(--tap-min);
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--color-text-muted);
    font-weight: var(--weight-medium);
    cursor: pointer;
  }
  button.on {
    background: var(--color-surface);
    color: var(--color-accent);
    box-shadow: var(--shadow-sm);
  }
  @media (max-width: 359px) {
    span {
      font-size: var(--text-sm);
    }
  }
</style>
