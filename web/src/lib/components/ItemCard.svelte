<script lang="ts">
  // Photo-first item card: cover thumb (or a calm kind/category placeholder),
  // title, ×N quantity, needs-review badge and a location chip. The whole
  // card is one link (default /items/<id>), one toggle in selection mode, or
  // one button when `onclick` is given.
  import type { ItemSummary } from '$lib/api/client';
  import Badge from './Badge.svelte';
  import Icon from './Icon.svelte';
  import LocationChip from './LocationChip.svelte';
  import { kindIcon } from './icons';
  import { formatCategory, KIND_LABEL } from './format';
  import { hasVariant } from './gallery';

  let {
    item,
    href,
    selectable = false,
    selected = false,
    onclick,
    ontoggle,
    chip = 'compact',
    eager = false
  }: {
    item: ItemSummary;
    /** Link target; default `/items/<id>`. Ignored in selection mode or with onclick. */
    href?: string;
    /** Selection mode: the card toggles instead of navigating. */
    selectable?: boolean;
    selected?: boolean;
    /** Card acts as a button (e.g. "pick this item"). */
    onclick?: (item: ItemSummary, e: MouseEvent) => void;
    /** Selection mode toggle (receives the click for shift-range). */
    ontoggle?: (item: ItemSummary, e: MouseEvent) => void;
    /** Location chip variant, or false to hide it. */
    chip?: 'compact' | 'full' | false;
    /** Load the image eagerly (first row). */
    eager?: boolean;
  } = $props();

  let brokenId = $state<string | null>(null);
  const cover = $derived(item.cover);
  const showImg = $derived(!!cover && hasVariant(cover, 'thumb') && brokenId !== cover.id);
  const category = $derived(formatCategory(item.category));
  const kindLabel = $derived((KIND_LABEL as Record<string, string>)[item.kind] ?? item.kind);
  const pending = $derived(cover?.variant_status === 'pending');
  const review = $derived(item.review_state === 'needs_review');
  const gone = $derived(item.lifecycle === 'gone' || item.placement.status === 'gone');
</script>

<div class="card" class:selected class:selectable class:gone>
  <div class="media">
    {#if showImg && cover}
      <img
        src={cover.thumb_url}
        alt=""
        width={cover.width ?? undefined}
        height={cover.height ?? undefined}
        loading={eager ? 'eager' : 'lazy'}
        decoding="async"
        onerror={() => (brokenId = cover.id)}
      />
    {:else}
      <div class="placeholder" aria-hidden="true">
        <Icon name={kindIcon(item.kind)} size={36} />
        <span class="cat">{category || kindLabel}</span>
        {#if pending}<span class="note">Processing photo…</span>{/if}
      </div>
    {/if}

    {#if review}
      <span class="badge-review"><Badge tone="warning" overlay icon="review" label="Needs review">Review</Badge></span>
    {/if}
    {#if item.quantity > 1}
      <span class="badge-qty"><Badge overlay label="Quantity {item.quantity}">×{item.quantity}</Badge></span>
    {/if}
    {#if selectable}
      <span class="check" aria-hidden="true"><Icon name="check" size={16} /></span>
    {/if}
  </div>

  <div class="meta">
    <h3 class="title">
      {#if selectable}
        <button type="button" class="hit" aria-pressed={selected} onclick={(e) => ontoggle?.(item, e)}>{item.title}</button>
      {:else if onclick}
        <button type="button" class="hit" onclick={(e) => onclick(item, e)}>{item.title}</button>
      {:else}
        <a class="hit" href={href ?? `/items/${item.id}`}>{item.title}</a>
      {/if}
    </h3>
    {#if chip}
      <div class="where"><LocationChip placement={item.placement} variant={chip} /></div>
    {/if}
  </div>
</div>

<style>
  .card {
    --card-ratio: 4 / 5;
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
    border-radius: var(--radius-md);
    -webkit-tap-highlight-color: transparent;
  }
  .card:has(.hit:focus-visible) {
    outline: 2px solid var(--color-focus);
    outline-offset: 3px;
  }
  .media {
    position: relative;
    aspect-ratio: var(--card-ratio);
    overflow: hidden;
    border-radius: var(--radius-md);
    background: var(--color-surface-2);
    transition: box-shadow var(--duration-fast) var(--ease);
  }
  .card:hover .media {
    box-shadow: var(--shadow-md);
  }
  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .gone img,
  .gone .placeholder {
    opacity: 0.55;
    filter: grayscale(0.6);
  }
  .placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    height: 100%;
    padding: var(--space-3);
    color: var(--color-text-faint);
    text-align: center;
  }
  .cat {
    color: var(--color-text-muted);
    font-size: var(--text-xs);
    overflow-wrap: anywhere;
  }
  .note {
    font-size: var(--text-xs);
  }
  .badge-review {
    position: absolute;
    top: var(--space-2);
    right: var(--space-2);
  }
  .badge-qty {
    position: absolute;
    left: var(--space-2);
    bottom: var(--space-2);
  }
  .check {
    position: absolute;
    top: var(--space-2);
    left: var(--space-2);
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 2px solid var(--color-surface);
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--color-text) 30%, transparent);
    color: transparent;
    box-shadow: var(--shadow-sm);
  }
  .selected .check {
    background: var(--color-accent);
    color: var(--color-accent-contrast);
  }
  .selected .media {
    box-shadow: 0 0 0 3px var(--color-accent);
  }
  .selected .media img,
  .selected .media .placeholder {
    transform: scale(0.94);
    border-radius: var(--radius-sm);
  }
  .media img,
  .media .placeholder {
    transition: transform var(--duration-fast) var(--ease);
  }
  .meta {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
    padding: 0 var(--space-0);
  }
  .title {
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
    line-height: var(--leading-tight);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .hit {
    all: unset;
    color: var(--color-text);
    cursor: pointer;
  }
  .hit:hover {
    text-decoration: none;
  }
  .hit::after {
    /* stretch the one interactive element over the whole card */
    content: '';
    position: absolute;
    inset: 0;
    min-height: var(--tap-min);
    border-radius: var(--radius-md);
  }
  .hit:focus-visible {
    outline: none;
  }
  .where {
    display: flex;
    min-width: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .media,
    .media img,
    .media .placeholder {
      transition: none;
    }
  }
</style>
