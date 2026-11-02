<script lang="ts">
  // Inbox / review (PLAN.md §8): upload from the camera roll with notes;
  // unlinked photos → assign to an item, create an item, or delete; and
  // needs_review items from agents → accept or open.
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { api, unwrap, type ItemSummary, type Media, type Schemas } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Icon from '$lib/components/Icon.svelte';
  import ItemCard from '$lib/components/ItemCard.svelte';
  import LoadingState from '$lib/components/LoadingState.svelte';
  import MediaViewer from '$lib/components/MediaViewer.svelte';
  import MultiSelectBar from '$lib/components/MultiSelectBar.svelte';
  import PhotoGrid from '$lib/components/PhotoGrid.svelte';
  import Sheet from '$lib/components/Sheet.svelte';
  import Tabs from '$lib/components/Tabs.svelte';
  import { errorMessage } from '$lib/components/format';
  import { hasVariant } from '$lib/components/gallery';
  import { toast } from '$lib/components/toast.svelte';
  import { toggleId } from '$lib/components/selection';
  import ContextNote from '../../(locations)/shared/ContextNote.svelte';
  import { plural, shortDate, shortDateTime } from '../../(locations)/shared/places';
  import AssignSheet from './AssignSheet.svelte';
  import CreateItemSheet from './CreateItemSheet.svelte';
  import UploadQueue from './UploadQueue.svelte';

  const PAGE = 60;

  let inbox = $state<Schemas['Inbox'] | null>(null);
  let media = $state<Media[]>([]);
  let cursor = $state<string | null>(null);
  let loading = $state(true);
  let loadingMore = $state(false);
  let error = $state<unknown>(null);
  let tab = $state(page.url.searchParams.get('tab') === 'review' ? 'review' : 'photos');

  let selected = $state<string[]>([]);
  let assignOpen = $state(false);
  let createOpen = $state(false);
  let deleteOpen = $state(false);
  let deleting = $state(false);
  let deleteError = $state('');
  let viewerOpen = $state(false);
  let viewerIndex = $state(0);

  async function load() {
    error = null;
    try {
      const res = unwrap(await api.GET('/api/inbox', { params: { query: { limit: PAGE } } }));
      inbox = res;
      media = res.media;
      cursor = res.next_cursor;
      selected = selected.filter((id) => media.some((m) => m.id === id));
      pollPending();
    } catch (e) {
      error = e;
    } finally {
      loading = false;
    }
  }

  async function more() {
    if (!cursor || loadingMore) return;
    loadingMore = true;
    try {
      const res = unwrap(await api.GET('/api/inbox', { params: { query: { limit: PAGE, cursor } } }));
      const have = new Set(media.map((m) => m.id));
      media = [...media, ...res.media.filter((m) => !have.has(m.id))];
      cursor = res.next_cursor;
    } catch (e) {
      toast(errorMessage(e), { tone: 'error' });
    } finally {
      loadingMore = false;
    }
  }

  $effect(() => {
    void load();
  });

  // Variants are generated after upload; refresh just those rows for a while.
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  let pollTries = 0;
  function pollPending(reset = true) {
    if (reset) pollTries = 0;
    clearTimeout(pollTimer);
    if (!media.some((m) => m.variant_status === 'pending') || pollTries > 15) return;
    pollTimer = setTimeout(async () => {
      pollTries++;
      for (const m of media.filter((x) => x.variant_status === 'pending')) {
        try {
          const fresh = unwrap(await api.GET('/api/media/{id}', { params: { path: { id: m.id } } }));
          const i = media.findIndex((x) => x.id === m.id);
          if (i >= 0) media[i] = { ...fresh, context: media[i].context };
        } catch {
          /* gone; next load fixes it */
        }
      }
      pollPending(false);
    }, 2000);
  }
  $effect(() => () => clearTimeout(pollTimer));

  const selectedMedia = $derived(media.filter((m) => selected.includes(m.id)));
  const reviewItems = $derived(inbox?.review_items ?? []);
  const viewerPhotos = $derived(media.filter((m) => hasVariant(m, 'display')).map((m) => ({ media: m, caption: m.context })));

  function setTab(id: string) {
    tab = id;
    const url = new URL(page.url);
    if (id === 'review') url.searchParams.set('tab', 'review');
    else url.searchParams.delete('tab');
    goto(url, { replaceState: true, keepFocus: true, noScroll: true });
  }

  function openViewer(m: Media) {
    const i = viewerPhotos.findIndex((p) => p.media.id === m.id);
    if (i < 0) return;
    viewerIndex = i;
    viewerOpen = true;
  }

  function onassigned(item: ItemSummary, n: number) {
    toast(`Linked ${plural(n, 'photo')} to ${item.title}`, { tone: 'success', action: { label: 'Open', run: () => goto(`/items/${item.id}`) } });
    selected = [];
    void load();
  }

  function oncreated(item: { id: string; title: string }) {
    toast(`Created ${item.title}`, { tone: 'success', action: { label: 'Open', run: () => goto(`/items/${item.id}`) } });
    selected = [];
    void load();
  }

  async function deleteSelected() {
    deleting = true;
    deleteError = '';
    const failed: string[] = [];
    let done = 0;
    for (const m of selectedMedia) {
      try {
        unwrap(await api.DELETE('/api/media/{id}', { params: { path: { id: m.id } } }));
        done++;
      } catch (e) {
        failed.push(`${m.original_name ?? 'photo'}: ${errorMessage(e)}`);
      }
    }
    deleting = false;
    if (failed.length) deleteError = failed.join('\n');
    else deleteOpen = false;
    if (done) toast(`Deleted ${plural(done, 'photo')}`);
    selected = [];
    void load();
  }

  async function accept(it: ItemSummary) {
    try {
      unwrap(await api.PATCH('/api/items/{id}', { params: { path: { id: it.id } }, body: { review_state: 'ok' } }));
      if (inbox) {
        inbox.review_items = inbox.review_items.filter((x) => x.id !== it.id);
        inbox.review_count = Math.max(0, inbox.review_count - 1);
      }
      toast(`Accepted ${it.title}`, {
        tone: 'success',
        action: {
          label: 'Undo',
          run: async () => {
            try {
              unwrap(await api.PATCH('/api/items/{id}', { params: { path: { id: it.id } }, body: { review_state: 'needs_review' } }));
            } catch (e) {
              toast(errorMessage(e), { tone: 'error' });
            }
            void load();
          }
        }
      });
    } catch (e) {
      toast(errorMessage(e), { tone: 'error' });
    }
  }

  const tabs = $derived([
    { id: 'photos', label: 'Photos', count: inbox?.media_count },
    { id: 'review', label: 'To review', count: inbox?.review_count }
  ]);
</script>

<svelte:head><title>Inbox · Personal Inventory</title></svelte:head>

<div class="page inbox">
  <header class="head">
    <h1>Inbox</h1>
    <p class="muted sub">Photos waiting to become items, and agent-made items waiting for your OK.</p>
  </header>

  <Tabs {tabs} value={tab} onchange={setTab} label="Inbox sections" />

  {#if tab === 'photos'}
    <UploadQueue onuploaded={load} />

    {#if loading}
      <LoadingState variant="grid" count={6} />
    {:else if error}
      <ErrorState {error} onretry={load} />
    {:else if !media.length}
      <EmptyState icon="inbox" title="Inbox is empty" message="Uploaded photos that aren't linked to an item wait here. Agents read them (with your notes) and turn them into items." />
    {:else}
      <div class="list-head">
        <h2>{plural(inbox?.media_count ?? media.length, 'unlinked photo')}</h2>
        {#if selected.length < media.length}
          <Button size="sm" variant="ghost" onclick={() => (selected = media.map((m) => m.id))}>Select all</Button>
        {:else}
          <Button size="sm" variant="ghost" onclick={() => (selected = [])}>Select none</Button>
        {/if}
      </div>
      <ul class="photos" aria-label="Unlinked photos">
        {#each media as m (m.id)}
          {@const sel = selected.includes(m.id)}
          <li class="tile" class:sel>
            <button type="button" class="img" aria-pressed={sel} aria-label="Select {m.original_name ?? 'photo'}" onclick={() => (selected = toggleId(selected, m.id))}>
              {#if hasVariant(m, 'thumb')}
                <img src={m.thumb_url} alt="" loading="lazy" width={m.width ?? undefined} height={m.height ?? undefined} />
              {:else if m.variant_status === 'pending'}
                <span class="ph"><Icon name="image" size={28} /><small>Processing…</small></span>
              {:else if m.variant_status === 'failed'}
                <span class="ph bad"><Icon name="alert" size={28} /><small>No preview</small></span>
              {:else}
                <span class="ph"><Icon name="image" size={28} /><small>{m.ext.toUpperCase()}</small></span>
              {/if}
              <span class="check" aria-hidden="true">{#if sel}<Icon name="check" size={16} />{/if}</span>
            </button>
            {#if hasVariant(m, 'display')}
              <button type="button" class="zoom" aria-label="View larger" onclick={() => openViewer(m)}><span class="zi"><Icon name="search" size={16} /></span></button>
            {/if}
            <div class="info">
              <span class="when muted" title={m.original_name ?? ''}>
                {#if m.taken_at}<time datetime={m.taken_at} title="Taken {shortDateTime(m.taken_at)}">{shortDate(m.taken_at)}</time>{:else}Added {shortDate(m.created_at)}{/if}
                {#if m.original_name}· {m.original_name}{/if}
              </span>
              <ContextNote mediaId={m.id} bind:value={m.context} compact />
            </div>
          </li>
        {/each}
      </ul>
      {#if cursor}
        <div class="more"><Button variant="ghost" loading={loadingMore} onclick={more}>Load more</Button></div>
      {/if}
    {/if}
  {:else}
    {#if loading}
      <LoadingState variant="grid" count={4} />
    {:else if error}
      <ErrorState {error} onretry={load} />
    {:else}
      <PhotoGrid items={reviewItems} label="Items to review">
        {#snippet card(it)}
          <div class="review">
            <ItemCard item={it} chip="compact" />
            <div class="r-actions">
              <Button size="sm" variant="primary" icon="check" onclick={() => accept(it)}>Accept</Button>
              <Button size="sm" variant="ghost" href="/items/{it.id}">Edit</Button>
            </div>
          </div>
        {/snippet}
        {#snippet empty()}
          <EmptyState icon="check" title="All reviewed" message="Items that agents create are marked “needs review” and show up here." />
        {/snippet}
      </PhotoGrid>
      {#if inbox && inbox.review_count > reviewItems.length}
        <p class="muted small">Showing {reviewItems.length} of {inbox.review_count}. <a href="/?review=needs_review">See all in Items</a></p>
      {/if}
    {/if}
  {/if}
</div>

{#if tab === 'photos' && selected.length}
  <MultiSelectBar count={selected.length} onclear={() => (selected = [])}>
    {#snippet actions()}
      <Button size="sm" variant="primary" onclick={() => (assignOpen = true)}>Assign</Button>
      <Button size="sm" onclick={() => (createOpen = true)}>New item</Button>
      <Button size="sm" variant="ghost" label="Delete" icon="close" onclick={() => ((deleteError = ''), (deleteOpen = true))} />
    {/snippet}
  </MultiSelectBar>
{/if}

<AssignSheet bind:open={assignOpen} photos={selectedMedia} {onassigned} />
<CreateItemSheet bind:open={createOpen} photos={selectedMedia} {oncreated} />
<MediaViewer photos={viewerPhotos} bind:index={viewerIndex} bind:open={viewerOpen} title="Inbox photo" />

<Sheet bind:open={deleteOpen} title="Delete {plural(selected.length, 'photo')}?" side="center" size="sm">
  <p>The files are removed from the server. This can't be undone.</p>
  {#if deleteError}<p class="err" role="alert">{deleteError}</p>{/if}
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (deleteOpen = false)}>Cancel</Button>
    <Button variant="danger" loading={deleting} onclick={deleteSelected}>Delete</Button>
  {/snippet}
</Sheet>

<style>
  .inbox {
    display: grid;
    gap: var(--space-4);
  }
  .head {
    display: grid;
    gap: var(--space-1);
  }
  .sub,
  .small {
    font-size: var(--text-sm);
  }
  .list-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    min-height: var(--tap-min);
  }
  h2 {
    font-size: var(--text-lg);
  }
  .photos {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(var(--grid-card-min), calc(50% - var(--space-3) / 2)), 1fr));
    gap: var(--space-4) var(--space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .tile {
    position: relative;
    display: grid;
    gap: var(--space-1);
    min-width: 0;
  }
  .img {
    position: relative;
    display: block;
    width: 100%;
    aspect-ratio: 1;
    padding: 0;
    border: 2px solid transparent;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: var(--color-surface-2);
    cursor: pointer;
  }
  .sel .img {
    border-color: var(--color-accent);
  }
  .img img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .sel .img img {
    opacity: 0.85;
  }
  .ph {
    display: grid;
    place-items: center;
    align-content: center;
    gap: var(--space-1);
    height: 100%;
    color: var(--color-text-faint);
    font-size: var(--text-xs);
  }
  .ph.bad {
    color: var(--color-danger);
  }
  .check {
    position: absolute;
    top: var(--space-2);
    left: var(--space-2);
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: 2px solid var(--color-viewer-fg);
    border-radius: var(--radius-pill);
    background: var(--color-overlay);
    color: var(--color-accent-contrast);
  }
  .sel .check {
    border-color: var(--color-accent);
    background: var(--color-accent);
  }
  .zoom {
    position: absolute;
    top: 0;
    right: 0;
    display: grid;
    place-items: center;
    width: var(--tap-min);
    height: var(--tap-min);
    padding: 0;
    border: 0;
    background: none;
    color: var(--color-viewer-fg);
    cursor: zoom-in;
  }
  .zi {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: var(--radius-pill);
    background: var(--color-overlay);
  }
  .info {
    display: grid;
    min-width: 0;
  }
  .when {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--text-xs);
  }
  .more {
    display: flex;
    justify-content: center;
  }
  .review {
    display: grid;
    gap: var(--space-2);
  }
  .r-actions {
    display: flex;
    gap: var(--space-1);
  }
  .err {
    margin-top: var(--space-3);
    color: var(--color-danger);
    font-size: var(--text-sm);
    white-space: pre-wrap;
  }
</style>
