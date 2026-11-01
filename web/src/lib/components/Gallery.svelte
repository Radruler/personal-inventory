<script lang="ts">
  // Item photos grouped into role tabs (Flat lay · On body · Stock · Labels ·
  // Detail · Other; only tabs with photos). The active tab shows a large
  // display-size photo and a thumbnail strip; tapping the photo opens the
  // full-screen MediaViewer (swipe/arrows, link to the original).
  import type { Schemas } from '$lib/api/client';
  import EmptyState from './EmptyState.svelte';
  import Icon from './Icon.svelte';
  import MediaViewer from './MediaViewer.svelte';
  import Tabs from './Tabs.svelte';
  import { roleLabel } from './format';
  import { groupByTab, hasVariant, type GalleryPhoto, type GalleryTabId } from './gallery';

  let {
    media,
    title = 'Item',
    tab = $bindable(),
    emptyMessage = 'No photos yet.'
  }: {
    /** ItemDetail.media / ProductDetail.media (ordered by the server). */
    media: Schemas['MediaLinkView'][];
    /** Item title, used in alt text and the viewer's name. */
    title?: string;
    /** Active tab (bindable). */
    tab?: GalleryTabId;
    emptyMessage?: string;
  } = $props();

  const groups = $derived(groupByTab(media));
  const active = $derived(groups.find((g) => g.id === tab) ?? groups[0]);
  let current = $state(0);
  let viewerOpen = $state(false);
  let viewerIndex = $state(0);

  const photos = $derived(active?.photos ?? []);
  const shown = $derived(photos[Math.min(current, photos.length - 1)]);

  function alt(p: GalleryPhoto, i: number) {
    return p.caption || `${title}, ${roleLabel(p.role).toLowerCase()} photo ${i + 1}`;
  }
  function openViewer(i: number) {
    viewerIndex = i;
    viewerOpen = true;
  }
</script>

{#if !groups.length}
  <EmptyState compact icon="image" title="No photos" message={emptyMessage} />
{:else}
  <div class="gallery">
    <Tabs
      label="Photo groups"
      tabs={groups.map((g) => ({ id: g.id, label: g.label, count: g.photos.length }))}
      value={active.id}
      onchange={(id) => {
        tab = id as GalleryTabId;
        current = 0;
      }}
    >
      {#snippet panel()}
        {#if shown}
          {@const i = photos.indexOf(shown)}
          <button type="button" class="hero" onclick={() => openViewer(i)} aria-label="Open {alt(shown, i)} full screen">
            {#if hasVariant(shown.media, 'display')}
              <img
                src={shown.media.display_url}
                alt={alt(shown, i)}
                width={shown.media.width ?? undefined}
                height={shown.media.height ?? undefined}
                loading="lazy"
                decoding="async"
              />
            {:else}
              <span class="pending">
                <Icon name="image" size={36} />
                {shown.media.variant_status === 'pending' ? 'Processing…' : 'No preview'}
              </span>
            {/if}
            {#if shown.caption || active.id === 'labels' || active.id === 'detail'}
              <span class="cap">{shown.caption || roleLabel(shown.role)}</span>
            {/if}
          </button>
        {/if}
        {#if photos.length > 1}
          <ul class="strip" aria-label="{active.label} photos">
            {#each photos as p, j (p.media.id)}
              <li>
                <button
                  type="button"
                  class="thumb"
                  class:current={p === shown}
                  aria-current={p === shown ? 'true' : undefined}
                  aria-label="Show {alt(p, j)}"
                  onclick={() => (current = j)}
                  ondblclick={() => openViewer(j)}
                >
                  {#if hasVariant(p.media, 'thumb')}
                    <img src={p.media.thumb_url} alt="" loading="lazy" decoding="async" width="72" height="72" />
                  {:else}
                    <Icon name="image" size={20} />
                  {/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      {/snippet}
    </Tabs>
  </div>

  <MediaViewer {title} photos={photos} bind:index={viewerIndex} bind:open={viewerOpen} onclose={() => (current = viewerIndex)} />
{/if}

<style>
  .gallery {
    display: grid;
    gap: var(--space-2);
    min-width: 0;
  }
  .hero {
    position: relative;
    display: grid;
    place-items: center;
    width: 100%;
    aspect-ratio: 4 / 5;
    max-height: min(70dvh, 640px);
    padding: 0;
    border: 0;
    border-radius: var(--radius-lg);
    overflow: hidden;
    background: var(--color-surface-2);
    cursor: zoom-in;
  }
  .hero img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .pending {
    display: grid;
    justify-items: center;
    gap: var(--space-2);
    color: var(--color-text-muted);
    font-size: var(--text-sm);
  }
  .cap {
    position: absolute;
    left: var(--space-2);
    bottom: var(--space-2);
    max-width: calc(100% - var(--space-4));
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--color-surface) 90%, transparent);
    color: var(--color-text);
    font-size: var(--text-xs);
    text-align: left;
  }
  .strip {
    display: flex;
    gap: var(--space-2);
    margin: var(--space-2) 0 0;
    padding: var(--space-1) var(--space-0);
    list-style: none;
    overflow-x: auto;
    scroll-snap-type: x proximity;
    scrollbar-width: thin;
  }
  .strip li {
    flex: none;
    scroll-snap-align: start;
  }
  .thumb {
    display: grid;
    place-items: center;
    width: 64px;
    height: 64px;
    padding: 0;
    border: 2px solid transparent;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--color-surface-2);
    color: var(--color-text-faint);
    cursor: pointer;
  }
  .thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .thumb.current {
    border-color: var(--color-accent);
  }
  @media (min-width: 720px) {
    .hero {
      aspect-ratio: 1;
    }
  }
</style>
