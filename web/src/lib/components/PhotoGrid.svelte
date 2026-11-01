<script lang="ts">
  // Responsive grid of ItemCards: auto-fill minmax(--grid-card-min, 1fr),
  // always 2 columns on a phone. Optional selection mode (click toggles,
  // shift-click selects a range) and infinite scroll via `onloadmore`.
  import type { Snippet } from 'svelte';
  import type { ItemSummary } from '$lib/api/client';
  import Button from './Button.svelte';
  import ItemCard from './ItemCard.svelte';
  import { rangeSelect, toggleId } from './selection';

  let {
    items,
    selectable = false,
    selected = $bindable([]),
    onselectionchange,
    hrefFor,
    onitemclick,
    chip = 'compact',
    label = 'Items',
    eagerCount = 4,
    hasMore = false,
    loadingMore = false,
    onloadmore,
    empty,
    card
  }: {
    items: ItemSummary[];
    /** Selection mode. */
    selectable?: boolean;
    /** Selected item ids (bindable). */
    selected?: string[];
    onselectionchange?: (ids: string[]) => void;
    /** Link per card; default `/items/<id>`. */
    hrefFor?: (item: ItemSummary) => string;
    /** Makes every card a button instead of a link. */
    onitemclick?: (item: ItemSummary, e: MouseEvent) => void;
    chip?: 'compact' | 'full' | false;
    /** Accessible name of the list. */
    label?: string;
    /** First N images load eagerly. */
    eagerCount?: number;
    hasMore?: boolean;
    loadingMore?: boolean;
    /** Called when the end of the grid scrolls into view (and by the Load more button). */
    onloadmore?: () => void;
    /** Rendered instead of the grid when items is empty. */
    empty?: Snippet;
    /** Custom card renderer. */
    card?: Snippet<[ItemSummary, number]>;
  } = $props();

  let anchor: string | null = null;
  let sentinel = $state<HTMLElement>();

  function toggle(item: ItemSummary, e: MouseEvent) {
    const next = e.shiftKey ? rangeSelect(items.map((i) => i.id), selected, anchor, item.id) : toggleId(selected, item.id);
    anchor = item.id;
    selected = next;
    onselectionchange?.(next);
  }

  $effect(() => {
    if (!sentinel || !hasMore || !onloadmore || typeof IntersectionObserver === 'undefined') return;
    const cb = onloadmore;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting) && !loadingMore) cb();
      },
      { rootMargin: '600px 0px' }
    );
    io.observe(sentinel);
    return () => io.disconnect();
  });

  const selectedSet = $derived(new Set(selected));
</script>

{#if items.length === 0 && empty}
  {@render empty()}
{:else}
  <ul class="grid" aria-label={label}>
    {#each items as item, i (item.id)}
      <li>
        {#if card}
          {@render card(item, i)}
        {:else}
          <ItemCard
            {item}
            href={hrefFor?.(item)}
            {selectable}
            selected={selectedSet.has(item.id)}
            ontoggle={toggle}
            onclick={onitemclick}
            {chip}
            eager={i < eagerCount}
          />
        {/if}
      </li>
    {/each}
  </ul>
  {#if hasMore}
    <div class="more" bind:this={sentinel}>
      <Button variant="ghost" loading={loadingMore} onclick={() => onloadmore?.()}>Load more</Button>
    </div>
  {/if}
{/if}

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(var(--grid-card-min), calc(50% - var(--space-3) / 2)), 1fr));
    gap: var(--space-4) var(--space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    min-width: 0;
  }
  .more {
    display: flex;
    justify-content: center;
    padding: var(--space-5) 0;
  }
</style>
